package httpapi

// Fixes from council run 1 (cache/v2-plan/current-inconsistencies/
// run-1-2026-10-21/FINDINGS.md), one test per finding id.

import (
	"fmt"
	"testing"
	"time"
)

// shownInPractice reads the bank flag of one problem.
func (s *site) shownInPractice(owner *browser, pid int64) bool {
	s.t.Helper()
	code, out := owner.do("GET", "/api/admin/problems", nil, "")
	if code != 200 {
		s.t.Fatalf("bank: %d %v", code, out)
	}
	for _, p := range out["problems"].([]any) {
		m := p.(map[string]any)
		if int64(m["id"].(float64)) == pid {
			v, ok := m["shown_in_practice"].(bool)
			if !ok {
				s.t.Fatalf("no shown_in_practice in %v", m)
			}
			return v
		}
	}
	s.t.Fatalf("problem %d not in the bank", pid)
	return false
}

// C1-015: the bank marks a problem whose hidden tests a contest showed
// (ungraded and started, or practice after the end), and turning grading
// off after the start needs an explicit confirm.
func TestC1015HiddenTestsShownInPracticeAndUngradedConfirm(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c1015-a")
	pb := s.importProblem(owner, "c1015-b")
	pc := s.importProblem(owner, "c1015-c")
	graded := s.createContest(owner, "c1015-graded", start, map[string]int64{"A": pa}, nil)
	s.createContest(owner, "c1015-practice", start, map[string]int64{"A": pb}, map[string]any{"practice_after_end": true})
	s.createContest(owner, "c1015-closed", start, map[string]int64{"A": pc}, map[string]any{"practice_after_end": false})
	for _, p := range []int64{pa, pb, pc} {
		if s.shownInPractice(owner, p) {
			t.Errorf("problem %d marked before any contest started", p)
		}
	}

	s.at(start, 5)
	path := fmt.Sprintf("/api/admin/contests/%d", graded)
	code, out := owner.do("PUT", path, map[string]any{"graded": false}, o)
	if code != 400 || errCode(out) != "confirm_ungraded" {
		t.Fatalf("turning grading off after the start without a confirm: %d %v", code, out)
	}
	if s.shownInPractice(owner, pa) {
		t.Error("refused change still marked the problem")
	}
	if code, out := owner.do("PUT", path, map[string]any{"graded": false, "confirm_ungraded": true}, o); code != 200 || out["graded"] != false {
		t.Fatalf("with the confirm: %d %v", code, out)
	}
	if !s.shownInPractice(owner, pa) {
		t.Error("an ungraded started contest's problem is not marked")
	}
	// Turning it on again needs no confirm.
	if code, out := owner.do("PUT", path, map[string]any{"graded": true}, o); code != 200 {
		t.Errorf("grading on again: %d %v", code, out)
	}

	if s.shownInPractice(owner, pb) {
		t.Error("practice marked before the end")
	}
	s.at(start, 121)
	if !s.shownInPractice(owner, pb) {
		t.Error("a problem open for practice is not marked")
	}
	if s.shownInPractice(owner, pc) {
		t.Error("a graded contest without practice marked its problem")
	}
}
