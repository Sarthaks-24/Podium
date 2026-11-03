package contests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/events"
)

type queuedRefresh struct {
	delay time.Duration
	fn    func()
}

func queueRefreshes(s *Service) *[]queuedRefresh {
	var queued []queuedRefresh
	s.afterFunc = func(delay time.Duration, fn func()) {
		queued = append(queued, queuedRefresh{delay: delay, fn: fn})
	}
	return &queued
}

func runQueuedRefresh(t *testing.T, queued *[]queuedRefresh) time.Duration {
	t.Helper()
	if len(*queued) == 0 {
		t.Fatal("no refresh was scheduled")
	}
	item := (*queued)[0]
	*queued = (*queued)[1:]
	item.fn()
	return item.delay
}

func TestJobChangeInvalidatesFromTheHookContestID(t *testing.T) {
	r := newSvc(t)
	queueRefreshes(r.svc)
	c := r.contest(t, nil)
	r.svc.DB.Close()
	r.svc.OnJobChange(4, "submit", c.ID)
	if got := r.svc.Version(c.ID); got != 1 {
		t.Fatalf("contest version = %d, want 1 without a database lookup", got)
	}
}

func TestStandingsRefreshOnlyNotifiesACachedVersion(t *testing.T) {
	r := newSvc(t)
	r.svc.BoardEvery = time.Hour
	queued := queueRefreshes(r.svc)
	hub := &events.Hub{}
	r.svc.Hub = hub
	client, err := hub.SubscribeTagged(1, "t")
	if err != nil {
		t.Fatal(err)
	}
	c := r.contest(t, nil)
	ctx := context.Background()
	for _, variant := range []string{VariantFull, VariantFrozen} {
		if _, err := r.svc.Board(ctx, c, variant); err != nil {
			t.Fatal(err)
		}
	}
	r.svc.board.bump(c.ID)
	*r.now = r.now.Add(2 * time.Hour)
	if _, err := r.svc.Board(ctx, c, VariantFull); err != nil {
		t.Fatal(err)
	}
	r.svc.board.bump(c.ID)
	if _, err := r.svc.Board(ctx, c, VariantFull); err != nil {
		t.Fatal(err)
	}
	runQueuedRefresh(t, queued)
	event := <-client.C
	var data struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil || data.Version != 1 {
		t.Fatalf("standings event version = %d, decode error %v, want the lowest cached version 1", data.Version, err)
	}
	r.svc.board.mu.Lock()
	if got := r.svc.board.notified[c.ID]; got != 0 {
		r.svc.board.mu.Unlock()
		t.Fatalf("notified version = %d while a variant is behind, want it unchanged at 0", got)
	}
	r.svc.board.mu.Unlock()

	*r.now = r.now.Add(2 * time.Hour)
	runQueuedRefresh(t, queued)
	select {
	case event := <-client.C:
		var final struct {
			Version int64 `json:"version"`
		}
		if err := json.Unmarshal(event.Data, &final); err != nil || final.Version != 2 {
			t.Fatalf("standings event version = %d, decode error %v, want 2", final.Version, err)
		}
	default:
		t.Fatal("the final cached version event was muted")
	}
}

func TestRefreshRetriesGetError(t *testing.T) {
	r := newSvc(t)
	queued := queueRefreshes(r.svc)
	c := r.contest(t, nil)
	if _, err := r.svc.Board(context.Background(), c, VariantFull); err != nil {
		t.Fatal(err)
	}
	r.svc.DB.Close()
	r.svc.scheduleRefresh(c.ID, 0)
	runQueuedRefresh(t, queued)
	if len(*queued) != 1 || (*queued)[0].delay != refreshRetryDelay {
		t.Fatalf("Get error scheduled %v, want one retry after %v", *queued, refreshRetryDelay)
	}
}

func TestRefreshRetriesBoardError(t *testing.T) {
	r := newSvc(t)
	r.svc.BoardEvery = time.Second
	queued := queueRefreshes(r.svc)
	c := r.contest(t, nil)
	seedBoard(t, r, c, 1, 1)
	if _, err := r.svc.Board(context.Background(), c, VariantFull); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DB.Exec(`UPDATE submissions SET earned = '"bad"' WHERE contest_id = ?`, c.ID); err != nil {
		t.Fatal(err)
	}
	*r.now = r.now.Add(2 * time.Second)
	r.svc.board.bump(c.ID)
	r.svc.scheduleRefresh(c.ID, 0)
	runQueuedRefresh(t, queued)
	if len(*queued) != 1 || (*queued)[0].delay != refreshRetryDelay {
		t.Fatalf("Board error scheduled %v, want one retry after %v", *queued, refreshRetryDelay)
	}
}

// Organiser-only events reach managers, while public events also reach an
// unlisted organiser who has view access.
func TestOrganiserEventsReachAdmins(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	if _, err := r.svc.DB.Exec(`INSERT INTO users (id, email, role, created_at) VALUES
		(4, 'org@college.edu', 'organiser', 0), (5, 'boss@college.edu', 'admin', 0), (6, 'viewer@college.edu', 'organiser', 0)`); err != nil {
		t.Fatal(err)
	}
	hub := &events.Hub{}
	r.svc.Hub = hub
	c := r.contest(t, nil)
	if _, err := r.svc.DB.Exec("INSERT INTO contest_organisers (contest_id, user_id, added_by, added_at) VALUES (?, 4, 1, 0)", c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DB.Exec("UPDATE contests SET published = 1 WHERE id = ?", c.ID); err != nil {
		t.Fatal(err)
	}
	c, err := r.svc.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	clients := map[int64]*events.Client{}
	for _, id := range []int64{1, 2, 4, 5, 6} {
		cl, err := hub.SubscribeTagged(id, "t")
		if err != nil {
			t.Fatal(err)
		}
		clients[id] = cl
	}
	got := func() map[int64]bool {
		out := map[int64]bool{}
		for id, cl := range clients {
			select {
			case <-cl.C:
				out[id] = true
			default:
			}
		}
		return out
	}

	r.svc.publish(ctx, c, true, "test", nil)
	if g := got(); !g[1] || !g[4] || !g[5] || g[2] || g[6] {
		t.Errorf("organiser event reached %v, want owner 1, manager 4 and admin 5 only", g)
	}
	r.svc.publish(ctx, c, false, "test", nil)
	if g := got(); !g[1] || !g[4] || !g[5] || !g[6] || !g[2] {
		t.Errorf("public event reached %v, want viewer 6 and published-contest participants", g)
	}
	if _, err := r.svc.DB.Exec("UPDATE users SET role = 'student' WHERE id = 5"); err != nil {
		t.Fatal(err)
	}
	r.svc.publish(ctx, c, true, "test", nil)
	if g := got(); g[5] {
		t.Error("a demoted admin still got an organiser event")
	}
	if _, err := r.svc.DB.Exec("UPDATE users SET role = 'student' WHERE id = 4"); err != nil {
		t.Fatal(err)
	}
	r.svc.publish(ctx, c, true, "test", nil)
	if g := got(); g[4] {
		t.Error("a demoted listed organiser still got a manager event")
	}
}
