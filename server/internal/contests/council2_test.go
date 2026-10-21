package contests

// Tests for council run 2 (2026-10-21), backend part: fixes and gate
// proofs that had no test (C2-019).

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// A pin update whose label moved after it was read is refused with
// ErrPinMoved and changes nothing (1812046).
func TestUpdatePinOvertaken(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	execNoFK(t, r, fmt.Sprintf(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id, points)
		VALUES (%d, 100, 'A', 0, 2, '[100]')`, c.ID))
	read := ContestProblem{Label: "A", ProblemID: 100, RevisionID: 1}
	from := &problems.Revision{ID: 1, ProblemID: 100, Rev: 1}
	to := &problems.Revision{ID: 3, ProblemID: 100, Rev: 3}
	_, err := r.svc.applyPin(ctx, c, read, "A", from, to, Actor{ID: 1})
	if !errors.Is(err, ErrPinMoved) {
		t.Fatalf("update from a stale pin: %v, want ErrPinMoved", err)
	}
	if n := count(t, r, "SELECT revision_id FROM contest_problems WHERE contest_id = ? AND label = 'A'", c.ID); n != 2 {
		t.Errorf("pin is revision %d after the refused update, want 2", n)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = ?", c.ID, AuditRevision); n != 0 {
		t.Errorf("%d revision audit rows after the refused update", n)
	}
}
