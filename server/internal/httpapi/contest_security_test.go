package httpapi

// Checks for the M4 security review (cache/council-2026-10-18/
// m4-security.md), ported from its reproductions: co-organisers and the
// problem bank, the exam-mode timing oracle, the fixed-start freeze with a
// personal extension, read limits, and the roster and count additions.

import (
	"fmt"
	"testing"
	"time"
)

func TestFixedFreezeIgnoresPersonalExtension(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	a := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "fz", start, map[string]int64{"A": a}, map[string]any{"freeze_minutes": 60})
	asha := s.signedIn("asha@college.edu", "Asha")
	bilal := s.signedIn("bilal@college.edu", "Bilal")
	s.at(start, 1)
	for _, b := range []*browser{asha, bilal} {
		b.do("POST", "/api/contests/fz/start", map[string]any{"accept_honour": true}, s.srv.URL)
	}
	owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", id, bilal.userID()), map[string]any{"minutes": 30}, s.srv.URL)
	s.at(start, 70) // inside the freeze for everyone
	s.submit(bilal, "fz", "A", "correct")
	s.submit(asha, "fz", "A", "correct")
	_, board := asha.do("GET", "/api/contests/fz/standings", nil, "")
	if board["variant"] != "frozen" {
		t.Fatalf("board %v", board)
	}
	for _, r := range board["rows"].([]any) {
		row := r.(map[string]any)
		cell := row["cells"].([]any)[0].(map[string]any)
		if row["total"] != float64(0) || cell["pending"] != float64(1) {
			t.Errorf("%v: a score after the freeze shows: %v", row["name"], row)
		}
	}
}

func TestHeavyReadsAreRateLimited(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "rl", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "Asha")
	limited := 0
	for i := 0; i < 40; i++ {
		code, out := asha.do("GET", "/api/contests/rl", nil, "")
		if code == 429 {
			limited++
			if out["error"].(map[string]any)["retry_after_s"] == nil {
				t.Fatalf("429 without retry_after_s: %v", out)
			}
		}
	}
	if limited == 0 {
		t.Error("40 contest reads in a burst were never limited")
	}
	// Another user is not affected.
	if code, _ := owner.do("GET", "/api/contests/rl", nil, ""); code != 200 {
		t.Errorf("another user: %d", code)
	}
}
