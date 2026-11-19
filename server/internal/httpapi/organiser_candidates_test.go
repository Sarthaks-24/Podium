package httpapi

import (
	"fmt"
	"testing"
	"time"
)

// TestOrganiserCandidatesRoute: managers get the people who can still be
// added, in email order and without the ones already listed; a student is
// refused by role and an organiser who cannot manage the contest gets the
// same 404 as the other organiser routes.
func TestOrganiserCandidatesRoute(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	for _, email := range []string{"lead@college.edu", "bee@college.edu", "outsider@college.edu"} {
		if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": email, "role": "organiser"}, s.srv.URL); code >= 300 {
			t.Fatalf("grant organiser %s: %d %v", email, code, out)
		}
	}
	lead := s.signedIn("lead@college.edu", "Lead")
	outsider := s.signedIn("outsider@college.edu", "Outsider")
	asha := s.signedIn("asha@college.edu", "Asha")
	problem := s.importProblem(owner, "organiser-candidates")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "organiser-candidates", start, map[string]int64{"A": problem}, nil)
	route := fmt.Sprintf("/api/admin/contests/%d/organisers", id)
	if code, out := owner.do("POST", route, map[string]any{"email": "lead@college.edu"}, s.srv.URL); code != 200 {
		t.Fatalf("owner adds lead: %d %v", code, out)
	}
	code, out := lead.do("GET", route+"/candidates", nil, "")
	if code != 200 {
		t.Fatalf("manager candidates: %d %v", code, out)
	}
	list, _ := out["candidates"].([]any)
	var emails []string
	for _, v := range list {
		row := v.(map[string]any)
		if _, has := row["added_at"]; has {
			t.Errorf("candidate carries added_at: %v", row)
		}
		emails = append(emails, row["email"].(string))
	}
	if fmt.Sprint(emails) != "[bee@college.edu outsider@college.edu]" {
		t.Errorf("candidates = %v, want bee and outsider only", emails)
	}
	if code, _ := asha.do("GET", route+"/candidates", nil, ""); code != 403 {
		t.Errorf("student: %d, want 403", code)
	}
	if code, _ := outsider.do("GET", route+"/candidates", nil, ""); code != 404 {
		t.Errorf("non-manager organiser: %d, want 404", code)
	}
}
