package integrity

// Tests for council run 2 (2026-10-21), backend part: fixes that had no
// test (C2-019).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A case decided between Propose's read and its write is refused inside
// the transaction (1812046).
func TestProposeAfterCaseDecided(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	s1a := r.sub(t, 1, "A", 100, 10)
	ref, _ := r.svc.Submission(ctx, s1a)
	kase, err := r.svc.OpenManualCase(ctx, ref, 100, "Problem A, a question", reason)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Decide(ctx, kase.ID, 100, AwaitingStudent, reason, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Explain(ctx, kase.ID, 1, "I wrote it myself from my notes."); err != nil {
		t.Fatal(err)
	}
	seen := seenAll(t, r.rig, kase.ID)
	// Propose reads the clock once between its read of the case and its
	// transaction; another reviewer clears the case there.
	tick := r.svc.Now
	fired := false
	r.svc.Now = func() time.Time {
		if !fired {
			fired = true
			mustExec(t, r.db, "UPDATE integrity_cases SET state = 'cleared' WHERE id = ?", kase.ID)
		}
		return tick()
	}
	_, err = r.svc.Propose(ctx, kase.ID, 100, ProposeInput{Outcome: OutcomeWarning, Note: reason, Reason: forStudent, Seen: seen})
	r.svc.Now = tick
	var in *InputError
	if !errors.As(err, &in) || !strings.Contains(in.Message, "changed while you were proposing") {
		t.Fatalf("propose on a case cleared meanwhile: %v", err)
	}
	var n int
	r.db.QueryRow("SELECT count(*) FROM integrity_proposals WHERE case_id = ?", kase.ID).Scan(&n)
	if n != 0 {
		t.Errorf("%d proposals stored on a cleared case", n)
	}
}

// A decision's board refresh rebuilds the summaries even when the
// request that made it is gone (C1-006 b): the decision is committed.
func TestBoardChangedRebuildsOnACancelledContext(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	r.sub(t, 1, "A", 100, 10)
	c, err := r.contests.Get(ctx, r.contest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.contests.BuildSummaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(time.Minute)
	gone, cancel := context.WithCancel(ctx)
	cancel()
	r.svc.boardChanged(gone, r.contest)
	var at int64
	r.db.QueryRow("SELECT summarised_at FROM contests WHERE id = ?", r.contest).Scan(&at)
	if at != r.clock.UnixMilli() {
		t.Errorf("summarised_at %d after the refresh, want %d (rebuilt)", at, r.clock.UnixMilli())
	}
}
