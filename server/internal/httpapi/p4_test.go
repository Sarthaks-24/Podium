package httpapi

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestIntegrityDecisionsOverHTTP: an organiser records one decision per
// student with a reason; "excluded" takes the student off the board and a
// later decision brings them back. Students and student co-organisers
// cannot decide. The similar pairs download as CSV, and the students table
// counts contests taken.
func TestIntegrityDecisionsOverHTTP(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	o := s.srv.URL
	asha := w.asha.userID()
	s.at(w.start, 130) // over for everyone
	w.check(w.c1)

	decide := w.url(w.c1, fmt.Sprintf("/cases/%d", asha))
	excluded := map[string]any{"decision": "excluded", "reason": "Matches another student's program line for line."}
	if code, _ := w.asha.do("PUT", decide, excluded, o); code != 403 {
		t.Errorf("a student decides: %d, want 403", code)
	}
	if code, out := w.owner.do("PUT", decide, map[string]any{"decision": "excluded", "reason": "short"}, o); code != 400 {
		t.Errorf("a short reason: %d %v", code, out)
	}
	code, out := w.owner.do("PUT", decide, excluded, o)
	if code != 200 || out["case"].(map[string]any)["decision"] != "excluded" {
		t.Fatalf("decide: %d %v", code, out)
	}
	onBoard := func() bool {
		t.Helper()
		code, st := w.owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", w.c1), nil, "")
		if code != 200 {
			t.Fatalf("standings: %d %v", code, st)
		}
		for _, r := range st["rows"].([]any) {
			if int64(r.(map[string]any)["user_id"].(float64)) == asha {
				return true
			}
		}
		return false
	}
	if onBoard() {
		t.Error("an excluded student is still on the board")
	}
	if code, out := w.owner.do("PUT", decide, map[string]any{"decision": "no_action", "reason": "She showed her earlier drafts to us."}, o); code != 200 {
		t.Fatalf("second decision: %d %v", code, out)
	}
	if !onBoard() {
		t.Error("the student did not come back after No action")
	}
	code, list := w.admin.do("GET", w.url(w.c1, "/cases"), nil, "")
	if code != 200 || len(list["cases"].([]any)) != 1 {
		t.Errorf("cases: %d %v", code, list)
	}

	resp, err := w.owner.client.Get(o + w.url(w.c1, "/pairs.csv"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(string(body), "problem,strength,score,") || !strings.Contains(resp.Header.Get("Content-Type"), "text/csv") {
		t.Errorf("pairs.csv: %d %q", resp.StatusCode, body)
	}

	if code, _ := w.asha.do("GET", "/api/admin/students", nil, ""); code != 403 {
		t.Errorf("a student reads the students table: %d", code)
	}
	code, studs := w.admin.do("GET", "/api/admin/students?q=asha", nil, "")
	rows, _ := studs["students"].([]any)
	if code != 200 || len(rows) != 1 || rows[0].(map[string]any)["contests"] != float64(1) {
		t.Errorf("students: %d %v", code, studs)
	}
}
