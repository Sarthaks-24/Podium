package httpapi

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
)

// Fixes from the final security and code review of v2 (P6, S6.2).

// A reviewer named in a live signal (a student later made an admin) does
// not see it: not in the run, not as a pair, not as their own timeline,
// as they never see a case they are in.
func TestIntegritySignalsHiddenFromTheirOwnStudent(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	run := w.check(w.c1)
	asha, bilal := w.asha.userID(), w.bilal.userID()
	sig := similarSignal(t, run, asha, bilal)
	if _, err := s.db.Exec("UPDATE users SET role = 'admin' WHERE id = ?", asha); err != nil {
		t.Fatal(err)
	}
	code, mine := w.asha.do("GET", w.url(w.c1, ""), nil, "")
	if code != 200 {
		t.Fatalf("run as the promoted student: %d %v", code, mine)
	}
	for _, x := range mine["signals"].([]any) {
		sg := x.(map[string]any)
		if int64(sg["user_a"].(float64)) == asha || int64(sg["user_b"].(float64)) == asha {
			t.Errorf("a signal naming the viewer: %v", sg)
		}
	}
	if code, _ := w.asha.do("GET", w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig)), nil, ""); code != 404 {
		t.Errorf("pair of the viewer's own signal: %d, want 404", code)
	}
	if code, _ := w.asha.do("GET", w.url(w.c1, fmt.Sprintf("/timeline/%d", asha)), nil, ""); code != 404 {
		t.Errorf("the viewer's own timeline: %d, want 404", code)
	}
	if code, _ := w.owner.do("GET", w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig)), nil, ""); code != 200 {
		t.Errorf("owner's pair view: %d", code)
	}
}

// The case list logs one view per student of every listed case, in one
// write.
func TestIntegrityCaseListLogsEveryStudent(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	run := w.check(w.c1)
	sig := similarSignal(t, run, w.asha.userID(), w.bilal.userID())
	if code, out := w.owner.do("POST", w.url(w.c1, "/cases"),
		map[string]any{"title": "Problem A, similar code", "note": "The two programs match token for token.", "signal_ids": []int64{sig}}, s.srv.URL); code != 201 {
		t.Fatalf("open case: %d %v", code, out)
	}
	var before, after int
	s.db.QueryRow("SELECT count(*) FROM record_views WHERE kind = 'case'").Scan(&before)
	if code, out := w.admin.do("GET", w.url(w.c1, "/cases"), nil, ""); code != 200 || len(out["cases"].([]any)) != 1 {
		t.Fatalf("case list: %d %v", code, out)
	}
	s.db.QueryRow("SELECT count(*) FROM record_views WHERE kind = 'case'").Scan(&after)
	if after-before != 2 {
		t.Errorf("case list logged %d views, want 2 (one per student)", after-before)
	}
}

// A reviewer who shares a case with a student reads neither the private
// notes on that student's submissions in its contest nor the rating lines
// of the case (they name its reviewers).
func TestNotesHiddenFromAReviewerWhoSharesTheCase(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	run := w.check(w.c1)
	asha, bilal := w.asha.userID(), w.bilal.userID()
	sig := similarSignal(t, run, asha, bilal)
	if code, out := w.owner.do("POST", w.url(w.c1, "/cases"),
		map[string]any{"title": "Problem A, similar code", "note": "The two programs match token for token.", "signal_ids": []int64{sig}}, s.srv.URL); code != 201 {
		t.Fatalf("open case: %d %v", code, out)
	}
	var sub int64
	if err := s.db.QueryRow("SELECT id FROM submissions WHERE user_id = ? AND contest_id = ? ORDER BY id LIMIT 1", bilal, w.c1).Scan(&sub); err != nil {
		t.Fatal(err)
	}
	if code, out := w.owner.do("POST", fmt.Sprintf("/api/admin/submissions/%d/notes", sub),
		map[string]any{"text": "Same loop shape as Asha's.", "lines": []any{}}, s.srv.URL); code != 201 {
		t.Fatalf("note: %d %v", code, out)
	}
	if _, err := s.db.Exec("UPDATE users SET role = 'admin' WHERE id = ?", asha); err != nil {
		t.Fatal(err)
	}
	if code, _ := w.asha.do("GET", fmt.Sprintf("/api/admin/submissions/%d/notes", sub), nil, ""); code != 404 {
		t.Errorf("notes route for a reviewer in the same case: %d, want 404", code)
	}
	code, rec := w.asha.do("GET", fmt.Sprintf("/api/admin/students/%d/record", bilal), nil, "")
	if code != 200 {
		t.Fatalf("record: %d %v", code, rec)
	}
	if notes := rec["notes"].([]any); len(notes) != 0 {
		t.Errorf("record shows notes from the shared case's contest: %v", notes)
	}
	// The admin who shares no case still reads them.
	if code, out := w.admin.do("GET", fmt.Sprintf("/api/admin/students/%d/record", bilal), nil, ""); code != 200 || len(out["notes"].([]any)) != 1 {
		t.Errorf("uninvolved admin's record view: %d %v", code, out["notes"])
	}
}

// The class view's most failed test is the first test that failed, never
// one skipped after a compile error or a failed sample.
func TestMostFailedSkipsSkippedTests(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
	ce := `{"subtasks":[{"idx":0,"tests":[{"verdict":"SK"},{"verdict":"SK"}]},{"idx":1,"tests":[{"verdict":"SK"}]}]}`
	wa := `{"subtasks":[{"idx":0,"tests":[{"verdict":"AC"},{"verdict":"AC"}]},{"idx":1,"tests":[{"verdict":"WA"}]}]}`
	for i, res := range []string{ce, ce, ce, wa} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, score, result, created_at)
			VALUES (1, 1, ?, 'submit', 'cpp17', 'x', 's', 'done', 0, ?, 1)`, i+1, res); err != nil {
			t.Fatal(err)
		}
	}
	conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	conn.Close()
	got, err := Deps{DB: d}.mostFailed(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SubtaskIdx != 1 || got.Test != 1 || got.Failed != 1 {
		t.Errorf("most failed: %+v, want subtask 1 test 1 failed once", got)
	}
}

// A co-organiser whose site role is student sees a classmate's extra
// minutes but not the reason (it can be health data), and the reveal's
// full board only once students see it.
func TestStudentCoOrganiserNeitherReasonsNorHeldBoard(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	if code, out := w.owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", w.c1, w.chen.userID()),
		map[string]any{"minutes": 30, "reason": "Medical appointment"}, s.srv.URL); code != 200 {
		t.Fatalf("extension: %d %v", code, out)
	}
	reason := func(b *browser) any {
		code, out := b.do("GET", fmt.Sprintf("/api/admin/contests/%d", w.c1), nil, "")
		if code != 200 {
			t.Fatalf("contest: %d %v", code, out)
		}
		ext := out["extensions"].([]any)
		if len(ext) != 1 {
			t.Fatalf("extensions: %v", ext)
		}
		return ext[0].(map[string]any)["reason"]
	}
	// The audit row names the student by id only, and never the reason.
	var details string
	s.db.QueryRow("SELECT details FROM contest_audit WHERE contest_id = ? AND action = 'extension'", w.c1).Scan(&details)
	if details == "" || strings.Contains(details, "chen@") || strings.Contains(details, "Medical") || !strings.Contains(details, `"reason_given":true`) {
		t.Errorf("extension audit row: %s", details)
	}
	if got := reason(w.owner); got != "Medical appointment" {
		t.Errorf("owner reads the reason: %v", got)
	}
	if got := reason(w.ta); got != "" {
		t.Errorf("student co-organiser reads the reason: %v", got)
	}
	if _, err := s.db.Exec("UPDATE contests SET hold_reveal = 1, freeze_minutes = 60 WHERE id = ?", w.c1); err != nil {
		t.Fatal(err)
	}
	s.at(w.start, 200)
	path := fmt.Sprintf("/api/admin/contests/%d/reveal", w.c1)
	if code, out := w.ta.do("GET", path, nil, ""); code != 403 || out["error"].(map[string]any)["code"] != "results_hidden" {
		t.Errorf("student co-organiser reads the held board: %d %v", code, out)
	}
	if code, _ := w.owner.do("GET", path, nil, ""); code != 200 {
		t.Errorf("owner's reveal: %d", code)
	}
}
