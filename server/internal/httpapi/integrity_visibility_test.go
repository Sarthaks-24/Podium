package httpapi

import (
	"fmt"
	"testing"
)

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
	if _, err := s.db.Exec("UPDATE users SET role = 'organiser' WHERE id = ?", asha); err != nil {
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
