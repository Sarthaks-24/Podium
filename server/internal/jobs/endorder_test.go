package jobs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// endRig is a contest that closes at T on a fake clock, with three
// students: A has 15 minutes more, B and C have none.
type endRig struct {
	s       *Service
	c       *clock
	contest int64
	closeAt time.Time
	a, b, u int64 // user ids; u is student C
}

func newEndRig(t *testing.T) *endRig {
	t.Helper()
	s, c := newMixed(t)
	r := &endRig{s: s, c: c, closeAt: c.t.Add(10 * time.Minute)}
	now := c.t.UnixMilli()
	res, err := s.DB.Exec(`INSERT INTO contests (slug, title, starts_at, ends_at, published, created_at, updated_at)
		VALUES ('end', 'E', ?, ?, 1, ?, ?)`, now-3_600_000, r.closeAt.UnixMilli(), now, now)
	if err != nil {
		t.Fatal(err)
	}
	r.contest, _ = res.LastInsertId()
	for i, dst := range []*int64{&r.a, &r.b, &r.u} {
		res, err := s.DB.Exec(`INSERT INTO users (email, created_at) VALUES (?, ?)`, fmt.Sprintf("s%d@example.test", i), now)
		if err != nil {
			t.Fatal(err)
		}
		*dst, _ = res.LastInsertId()
	}
	if _, err := s.DB.Exec(`INSERT INTO contest_extensions (contest_id, user_id, minutes) VALUES (?, ?, 15)`, r.contest, r.a); err != nil {
		t.Fatal(err)
	}
	return r
}

// queue adds a job of kind by user inside the contest.
func (r *endRig) queue(t *testing.T, kind string, priority int, user int64) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := r.s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.s.Enqueue(ctx, tx, kind, priority, map[string]int{"p": priority}, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("UPDATE jobs SET contest_id = ? WHERE id = ?", r.contest, id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

// drain claims every job of the two kinds, reports each one done, and
// returns the claim order.
func (r *endRig) drain(t *testing.T) []int64 {
	t.Helper()
	var order []int64
	for {
		got := claimKinds(t, r.s, judgeapi.KindRun, judgeapi.KindSubmit)
		if got == nil {
			return order
		}
		order = append(order, got.JobID)
		if err := r.s.Report(context.Background(), judgeapi.Report{JobID: got.JobID, Lease: got.Lease, Final: true}); err != nil {
			t.Fatal(err)
		}
	}
}

func sameOrder(t *testing.T, got, want []int64) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claim order %v, want %v", got, want)
	}
}

// The plan's test (V203-70): B queued a Run before the close, A (15 minutes
// more) and C (practice) after it, five graded submissions wait.
func TestRunsPastTheirOwnContestTimeRankBehindGradedSubmissions(t *testing.T) {
	r := newEndRig(t)
	runB := r.queue(t, judgeapi.KindRun, 10, r.b)
	var subs []int64
	for i := 0; i < 5; i++ {
		subs = append(subs, r.queue(t, judgeapi.KindSubmit, 20+i, r.u))
	}
	r.c.t = r.closeAt.Add(time.Minute)
	runA := r.queue(t, judgeapi.KindRun, 10, r.a)
	runC := r.queue(t, judgeapi.KindRun, 60, r.u)

	order := r.drain(t)
	// A is inside a personal extension, so its Run keeps today's place (the
	// first claim); every graded submission goes before B's and C's.
	sameOrder(t, order, []int64{runA, subs[0], subs[1], subs[2], subs[3], subs[4], runB, runC})
	for _, id := range []int64{runA, runB, runC} {
		if j, _ := r.s.Get(context.Background(), id); j.State != Done {
			t.Errorf("run %d is %q, want done: no Run is cancelled", id, j.State)
		}
	}
}

// Without the new order, today's every-other-claim rule applies.
func TestEndOrderOffKeepsTodaysOrder(t *testing.T) {
	r := newEndRig(t)
	r.s.EndOrderOff = true
	runB := r.queue(t, judgeapi.KindRun, 10, r.b)
	var subs []int64
	for i := 0; i < 5; i++ {
		subs = append(subs, r.queue(t, judgeapi.KindSubmit, 20+i, r.u))
	}
	r.c.t = r.closeAt.Add(time.Minute)
	runA := r.queue(t, judgeapi.KindRun, 10, r.a)
	runC := r.queue(t, judgeapi.KindRun, 60, r.u)
	sameOrder(t, r.drain(t), []int64{runB, subs[0], runA, subs[1], subs[2], subs[3], subs[4], runC})
}

// "End now" moves the close earlier: a Run queued as in-time becomes late.
func TestEndingEarlyMovesQueuedRunsBehindSubmissions(t *testing.T) {
	r := newEndRig(t)
	run := r.queue(t, judgeapi.KindRun, 10, r.b)
	sub := r.queue(t, judgeapi.KindSubmit, 20, r.u)
	if _, err := r.s.DB.Exec("UPDATE contests SET ends_at = ? WHERE id = ?", r.c.t.Add(-time.Second).UnixMilli(), r.contest); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, r.drain(t), []int64{sub, run})
}

// "Extend" moves the close later: a Run that would be late under the old
// close keeps its place.
func TestExtendingKeepsQueuedRunsInPlace(t *testing.T) {
	r := newEndRig(t)
	run := r.queue(t, judgeapi.KindRun, 10, r.b)
	sub := r.queue(t, judgeapi.KindSubmit, 20, r.u)
	r.c.t = r.closeAt.Add(time.Minute) // past the old close
	if _, err := r.s.DB.Exec("UPDATE contests SET extended_minutes = 30 WHERE id = ?", r.contest); err != nil {
		t.Fatal(err)
	}
	sameOrder(t, r.drain(t), []int64{run, sub})
}

// A Run outside any contest is unchanged, and a Run in a contest that has
// not closed is too.
func TestRunsOutsideAContestKeepTheirOrder(t *testing.T) {
	s, _ := newMixed(t)
	run := add(t, s, judgeapi.KindRun, 10, 0)
	sub := add(t, s, judgeapi.KindSubmit, 20, 0)
	sameOrder(t, drainAll(t, s), []int64{run, sub})
}

func drainAll(t *testing.T, s *Service) []int64 {
	t.Helper()
	var order []int64
	for {
		got := claimKinds(t, s, judgeapi.KindRun, judgeapi.KindSubmit)
		if got == nil {
			return order
		}
		order = append(order, got.JobID)
	}
}

// Nothing else is claimable: a late Run is still handed out, not held back.
func TestALateRunIsClaimedWhenNothingElseWaits(t *testing.T) {
	r := newEndRig(t)
	r.c.t = r.closeAt.Add(time.Hour)
	run := r.queue(t, judgeapi.KindRun, 10, r.b)
	if got := claimKinds(t, r.s, judgeapi.KindRun, judgeapi.KindSubmit); got == nil || got.JobID != run {
		t.Fatalf("claimed %+v, want the late run", got)
	}
}

// The claim runs on every claim: it must keep walking the (state, priority,
// id) index and never sort.
func TestClaimQueriesUseTheirIndex(t *testing.T) {
	s, _ := newMixed(t)
	for mode := pickAll; mode <= pickLateRuns; mode++ {
		q := claimSQL(2, mode)
		args := []any{judgeapi.KindRun, judgeapi.KindSubmit}
		if mode != pickAll {
			args = append(args, int64(1))
		}
		rows, err := s.DB.Query("EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		all := strings.Join(plan, "\n")
		for _, d := range plan {
			if strings.HasPrefix(d, "SCAN") && !strings.Contains(d, "USING") {
				t.Errorf("mode %d plan scans a table without an index: %s\n%s", mode, d, all)
			}
		}
		if !strings.Contains(all, "jobs_by_state") || strings.Contains(all, "TEMP B-TREE") {
			t.Errorf("mode %d plan does not walk jobs_by_state without a sort:\n%s", mode, all)
		}
	}
}

// Many late Runs wait at the head of the index (priority 10), the worst case
// after a contest ends: the claim skips each one and must stay cheap. The
// average is logged (go test -v: 2.4 ms on a laptop, 155 ms under the race detector on WSL). The one-second bound is only a backstop; TestClaimQueriesUseTheirIndex is what proves the plan.
func TestClaimStaysCheapWithManyLateRuns(t *testing.T) {
	r := newEndRig(t)
	r.c.t = r.closeAt.Add(time.Hour)
	for i := 0; i < 2000; i++ {
		r.queue(t, judgeapi.KindRun, 10, r.b)
	}
	sub := r.queue(t, judgeapi.KindSubmit, 20, r.u)
	ctx := context.Background()
	req := judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindRun, judgeapi.KindSubmit}, Worker: "w"}
	const rounds = 20
	start := time.Now()
	for i := 0; i < rounds; i++ {
		c, err := r.s.Claim(ctx, req)
		if err != nil || c == nil || c.JobID != sub {
			t.Fatalf("claim %+v %v, want the graded submission", c, err)
		}
		// Put it back so every round sees the same queue.
		if _, err := r.s.DB.Exec("UPDATE jobs SET state = 'queued', lease_hash = NULL, lease_expires_at = NULL WHERE id = ?", c.JobID); err != nil {
			t.Fatal(err)
		}
	}
	per := time.Since(start) / rounds
	t.Logf("claim with 2000 late runs ahead of it: %v each", per)
	if per > time.Second {
		t.Errorf("a claim took %v with 2000 late runs queued", per)
	}
}
