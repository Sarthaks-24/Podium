package httpapi

import (
	"fmt"
	"testing"
	"time"
)

// TestContestContact: a contest's contact email is validated like the site
// one, shown to students as typed, kept by a partial edit, and cleared by an
// empty value. Students see organiser names only (an organiser with no name
// is left out); staff keep the email fallback.
func TestContestContact(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "nameless@college.edu", "role": "organiser"}, s.srv.URL); code >= 300 {
		t.Fatalf("grant organiser: %d %v", code, out)
	}
	nameless := s.signedIn("nameless@college.edu", "")
	asha := s.signedIn("asha@college.edu", "Asha")
	problem := s.importProblem(owner, "contact-line")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	for _, bad := range []string{"not an email", "a@b.edu, c@d.edu", "Name <a@b.edu>"} {
		code, out := owner.do("POST", "/api/admin/contests", map[string]any{"title": "bad contact", "starts_at": start.UnixMilli(),
			"ends_at": start.Add(time.Hour).UnixMilli(), "min_gap_s": 0, "contact_email": bad}, s.srv.URL)
		if code != 400 {
			t.Errorf("contact %q: %d %v, want 400", bad, code, out)
		}
	}
	id := s.createContest(owner, "contact-line", start, map[string]int64{"A": problem}, map[string]any{"contact_email": "  Contest@College.edu "})
	page := "/api/contests/contact-line"
	if _, got := asha.do("GET", page, nil, ""); got["contact_email"] != "contest@college.edu" {
		t.Errorf("student contact_email = %v, want contest@college.edu", got["contact_email"])
	}
	// A partial edit keeps the contact; a bad value is refused; "" clears it.
	admin := fmt.Sprintf("/api/admin/contests/%d", id)
	if code, _ := owner.do("PUT", admin, map[string]any{"title": "contact-line"}, s.srv.URL); code != 200 {
		t.Fatalf("partial edit: %d", code)
	}
	if code, out := owner.do("PUT", admin, map[string]any{"contact_email": "nope"}, s.srv.URL); code != 400 {
		t.Errorf("bad edit: %d %v, want 400", code, out)
	}
	if _, got := asha.do("GET", page, nil, ""); got["contact_email"] != "contest@college.edu" {
		t.Errorf("contact after partial edit = %v", got["contact_email"])
	}
	if code, _ := owner.do("PUT", admin, map[string]any{"contact_email": ""}, s.srv.URL); code != 200 {
		t.Fatalf("clear: %d", code)
	}
	if _, got := asha.do("GET", page, nil, ""); got["contact_email"] != "" {
		t.Errorf("cleared contact_email = %v, want empty", got["contact_email"])
	}
	// The nameless organiser is hidden from students and shown by email to staff.
	if code, out := owner.do("POST", admin+"/organisers", map[string]any{"email": "nameless@college.edu"}, s.srv.URL); code != 200 {
		t.Fatalf("add organiser: %d %v", code, out)
	}
	if _, got := asha.do("GET", page, nil, ""); fmt.Sprint(got["organised_by"]) != "[Owner]" {
		t.Errorf("student organised_by = %v, want [Owner]", got["organised_by"])
	}
	if _, got := nameless.do("GET", page, nil, ""); fmt.Sprint(got["organised_by"]) != "[Owner nameless@college.edu]" {
		t.Errorf("staff organised_by = %v, want the email fallback", got["organised_by"])
	}
	// With only nameless organisers left, the student list is empty, not null.
	if _, err := s.db.Exec("UPDATE users SET name = '' WHERE email = 'owner@college.edu'"); err != nil {
		t.Fatal(err)
	}
	if _, got := asha.do("GET", page, nil, ""); fmt.Sprint(got["organised_by"]) != "[]" {
		t.Errorf("student organised_by = %v, want []", got["organised_by"])
	}
}
