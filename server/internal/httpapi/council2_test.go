package httpapi

// Tests for the fixes of council run 2 (2026-10-21,
// cache/v2-plan/current-inconsistencies/run-2-2026-10-21), backend part.

import (
	"fmt"
	"testing"
	"time"
)

// C2-014: an unpublished draft past its start shows students nothing, so
// it marks no problem; a problem whose hidden tests a contest has shown
// stays in that contest, so the bank's mark cannot disappear.
func TestC2014ShownMarkNeedsPublishAndStays(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c2014-a")
	pb := s.importProblem(owner, "c2014-b")
	pc := s.importProblem(owner, "c2014-c")
	draft := s.createContest(owner, "c2014-draft", start, map[string]int64{"A": pa}, map[string]any{"graded": false})
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", draft), map[string]any{"published": false}, o); code != 200 {
		t.Fatalf("unpublish: %d %v", code, out)
	}
	ungraded := s.createContest(owner, "c2014-ungraded", start, map[string]int64{"A": pb}, map[string]any{"graded": false})

	s.at(start, 5)
	if s.shownInPractice(owner, pa) {
		t.Error("an unpublished draft past its start marked its problem")
	}
	if !s.shownInPractice(owner, pb) {
		t.Fatal("a published ungraded contest past its start did not mark its problem")
	}
	problems := fmt.Sprintf("/api/admin/contests/%d/problems", ungraded)
	code, out := owner.do("PUT", problems, []map[string]any{{"problem_id": pc, "label": "A"}}, o)
	if code != 400 || fieldErr(out, "problems") == "" {
		t.Errorf("removing a shown problem: %d %v, want 400 with fields.problems", code, out)
	}
	if !s.shownInPractice(owner, pb) {
		t.Error("the refused removal lost the mark")
	}
	// Adding beside it is still allowed.
	if code, out := owner.do("PUT", problems, []map[string]any{{"problem_id": pb, "label": "A"}, {"problem_id": pc, "label": "B"}}, o); code != 200 {
		t.Errorf("adding a problem beside a shown one: %d %v", code, out)
	}
	// The draft never showed anything, so its problem may still go.
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", draft), []map[string]any{}, o); code != 200 {
		t.Errorf("removing the draft's problem: %d %v", code, out)
	}
}

// fieldErr is the message of one field error in an error body.
func fieldErr(body map[string]any, field string) string {
	e, _ := body["error"].(map[string]any)
	f, _ := e["fields"].(map[string]any)
	m, _ := f[field].(string)
	return m
}
