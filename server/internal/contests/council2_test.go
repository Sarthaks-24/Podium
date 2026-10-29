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

// End now with a personal extension: everyone else's time ends at once and
// the student with extra time keeps it from the new end, as the console's
// confirm says; the contest is over for everyone when that runs out (the
// contest_final view, S1-08).
func TestEndNowKeepsAPersonalExtension(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	var e Edit
	c := r.contest(t, func(x *Edit) { e = *x })
	if err := r.svc.SetExtension(ctx, c.ID, 2, 15, "accommodation", owner); err != nil {
		t.Fatal(err)
	}
	*r.now = at(60)
	e.EndsAt, e.FreezeMinutes = at(60), 0
	c, err := r.svc.Update(ctx, c.ID, e, owner)
	if err != nil {
		t.Fatal(err)
	}
	*r.now = at(61)
	asha, _ := r.svc.State(ctx, c, 2)
	bilal, _ := r.svc.State(ctx, c, 3)
	if asha.Over || !bilal.Over {
		t.Errorf("a minute after End now: Asha (15 extra) over %v, Bilal over %v", asha.Over, bilal.Over)
	}
	if n := count(t, r, "SELECT final_at FROM contest_final WHERE contest_id = ?", c.ID); int64(n) != at(75).UnixMilli() {
		t.Errorf("final_at %d, want the new end plus 15 minutes", n)
	}
	*r.now = at(76)
	if asha, _ = r.svc.State(ctx, c, 2); !asha.Over {
		t.Error("Asha's extra time did not end")
	}
}

// "Exclude from this contest" hides the student from the board and from
// the organiser's CSV export (V3-2).
func TestExcludedStudentIsAbsentFromBoardAndCSV(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	seedBoard(t, r, c, 3, 1)
	if _, err := r.svc.DB.Exec(`INSERT INTO integrity_cases (contest_id, user_id, decision, reason, decided_by, decided_at)
		VALUES (?, 1001, 'excluded', 'Copied.', 1, 0)`, c.ID); err != nil {
		t.Fatal(err)
	}
	board, err := r.svc.BoardNow(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := r.svc.Export(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var csv strings.Builder
	if err := WriteCSV(&csv, c, rows, nil); err != nil {
		t.Fatal(err)
	}
	if len(board.Rows) != 2 || len(rows) != 2 {
		t.Fatalf("board %d rows, export %d rows, want 2 each", len(board.Rows), len(rows))
	}
	for _, row := range board.Rows {
		if row.UserID == 1001 {
			t.Error("the excluded student is on the board")
		}
	}
	if strings.Contains(csv.String(), "s1@college.edu") || !strings.Contains(csv.String(), "s0@college.edu") {
		t.Errorf("CSV:\n%s", csv.String())
	}
}
