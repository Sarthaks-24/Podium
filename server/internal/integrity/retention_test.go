package integrity

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
)

// Signals are kept until the contest ended for everyone a retention period
// ago (the contest_final view, S1-08): extra time moves the cutoff.
func TestRetainSignalsWaitsForExtraTime(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	end := r.clock.Add(-contests.RetentionPeriod - time.Hour)
	mustExec(t, r.db, "UPDATE contests SET starts_at = ?, ends_at = ? WHERE id = ?", end.Add(-time.Hour).UnixMilli(), end.UnixMilli(), r.contest)
	mustExec(t, r.db, "INSERT INTO contest_extensions (contest_id, user_id, minutes) VALUES (?, 1, 120)", r.contest)
	res := mustExec(t, r.db, "INSERT INTO integrity_runs (contest_id, state, started_at) VALUES (?, 'done', 0)", r.contest)
	run, _ := res.LastInsertId()
	mustExec(t, r.db, `INSERT INTO integrity_signals (run_id, contest_id, kind, strength, score, user_a) VALUES (?, ?, 'similar', 'strong', 1, 1)`, run, r.contest)
	if signals, runs, err := r.svc.RetainSignals(ctx); err != nil || signals != 0 || runs != 0 {
		t.Fatalf("inside a student's extra time: %d signals, %d runs, %v", signals, runs, err)
	}
	*r.clock = r.clock.Add(61 * time.Minute)
	if signals, runs, err := r.svc.RetainSignals(ctx); err != nil || signals != 1 || runs != 1 {
		t.Fatalf("after the extra time: %d signals, %d runs, %v", signals, runs, err)
	}
}
