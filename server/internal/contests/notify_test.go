package contests

import (
	"context"
	"testing"

	"github.com/Sarthaks-24/podium/internal/events"
)

// Organiser-only events reach every role at or above organiser, admins
// included (v4 O2), and never a student; the list is read per event, so a
// demotion stops them at once.
func TestOrganiserEventsReachAdmins(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	if _, err := r.svc.DB.Exec(`INSERT INTO users (id, email, role, created_at) VALUES
		(4, 'org@college.edu', 'organiser', 0), (5, 'boss@college.edu', 'admin', 0)`); err != nil {
		t.Fatal(err)
	}
	hub := &events.Hub{}
	r.svc.Hub = hub
	c := r.contest(t, nil)
	clients := map[int64]*events.Client{}
	for _, id := range []int64{1, 2, 4, 5} {
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
	if g := got(); !g[1] || !g[4] || !g[5] || g[2] {
		t.Errorf("organiser event reached %v, want owner 1, organiser 4 and admin 5, not student 2", g)
	}
	if _, err := r.svc.DB.Exec("UPDATE users SET role = 'student' WHERE id = 5"); err != nil {
		t.Fatal(err)
	}
	r.svc.publish(ctx, c, true, "test", nil)
	if g := got(); g[5] {
		t.Error("a demoted admin still got an organiser event")
	}
}
