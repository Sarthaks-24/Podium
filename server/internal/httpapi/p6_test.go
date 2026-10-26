package httpapi

import (
	"fmt"
	"strings"
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
