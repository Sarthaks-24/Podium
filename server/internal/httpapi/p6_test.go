package httpapi

import (
	"fmt"
	"testing"
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
