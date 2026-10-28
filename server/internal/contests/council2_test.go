package contests

// Tests for council run 2 (2026-10-21), backend part: fixes and gate
// proofs that had no test (C2-019).

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// A revision whose subtasks cannot be read is refused, never pinned with
// no points: that would score the problem at 0 for everyone (stage A5).
// The schema refuses invalid JSON, so the bad value is valid JSON of the
// wrong shape.
func TestPinUpdateRefusesUnreadableSubtasks(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	r.svc.Problems = &problems.Service{DB: r.svc.DB}
	c := r.contest(t, nil)
	execNoFK(t, r,
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash, validated_hash)
			VALUES (100, 'a-plus-b', 'A plus B', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h2', 'h2')`,
		`INSERT INTO problem_revisions (id, problem_id, rev, content_hash, title, created_at, subtasks)
			VALUES (1, 100, 1, 'h1', 'A plus B', 0, '[{"idx":1,"points":100}]'), (2, 100, 2, 'h2', 'A plus B', 0, '"twenty"')`,
		fmt.Sprintf(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id, points)
			VALUES (%d, 100, 'A', 0, 1, '[100]')`, c.ID))
	_, err := r.svc.UpdatePin(ctx, c.ID, "A", 2, Actor{ID: 1})
	if err == nil || !strings.Contains(err.Error(), "subtasks") {
		t.Fatalf("pin to an unreadable revision: %v, want an error naming subtasks", err)
	}
	var points string
	if err := r.svc.DB.QueryRow("SELECT points FROM contest_problems WHERE contest_id = ?", c.ID).Scan(&points); err != nil || points != "[100]" {
		t.Errorf("points after the refused update: %q %v", points, err)
	}
	if _, err := r.svc.DB.Exec(`UPDATE problem_revisions SET subtasks = 'not json' WHERE id = 2`); err == nil {
		t.Error("the schema took invalid JSON in problem_revisions.subtasks")
	}
}

// The AI-draft guard fails closed: tags it cannot read refuse the problem
// instead of reading as "no tags" (stage A5). The bad value is valid JSON
// of the wrong shape, which the schema lets through.
func TestUnreadableTagsRefuseTheProblem(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	execNoFK(t, r, `INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash, validated_hash, tags)
		VALUES (100, 'drafted', 'Drafted', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h', 'h', '{"ai-draft":true}')`)
	_, err := r.svc.SetProblems(ctx, c.ID, []ProblemChoice{{ProblemID: 100, Label: "A"}}, Actor{ID: 1})
	if err == nil || !strings.Contains(err.Error(), "tags") {
		t.Fatalf("adding a problem with unreadable tags: %v, want an error naming tags", err)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_problems WHERE contest_id = ?", c.ID); n != 0 {
		t.Errorf("%d problems in the contest after the refusal", n)
	}
}
