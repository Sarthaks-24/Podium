package httpapi

import (
	"testing"
)

// The admin role over HTTP (V4-16, v4 O2): an admin manages students and
// organisers on every People route, is refused with 403 on an admin, an
// owner, the configured owner and themself, cannot give the admin or
// owner role, and loses the Dashboard on the next request after a
// demotion. The exhaustive rule table is auth's TestAllowedChangeEveryCase;
// this proves every route goes through it.
func TestAdminManagesStudentsAndOrganisersOnly(t *testing.T) {
	s := newSite(t)
	origin := s.srv.URL
	owner := s.signedIn("owner@college.edu", "Owner")
	grant := func(b *browser, email, role string) (int, map[string]any) {
		t.Helper()
		return b.do("POST", "/api/admin/people", map[string]string{"email": email, "role": role}, origin)
	}
	account := func(b *browser, action, email string) int {
		t.Helper()
		code, _ := b.do("POST", "/api/admin/accounts/"+action, map[string]string{"email": email}, origin)
		return code
	}

	// A role granted before the first sign-in is the role the person gets.
	if code, body := grant(owner, "boss@college.edu", "admin"); code != 200 || body["role"] != "admin" {
		t.Fatalf("owner grants admin: %d %v", code, body)
	}
	_, peer := grant(owner, "peer@college.edu", "admin")
	peerID := itoa(int64(peer["id"].(float64)))
	boss := s.signedIn("boss@college.edu", "Boss")
	if code, role := boss.me(); code != 200 || role != "admin" {
		t.Fatalf("admin after first sign-in: %d %q", code, role)
	}
	asha := s.signedIn("asha@college.edu", "Asha")

	// What an admin may do: students and organisers.
	code, body := grant(boss, "asha@college.edu", "organiser")
	if code != 200 {
		t.Fatalf("admin grants organiser: %d %v", code, body)
	}
	ashaID := itoa(int64(body["id"].(float64)))
	if code, role := asha.me(); code != 200 || role != "organiser" {
		t.Errorf("asha after the grant: %d %q", code, role)
	}
	if code := account(boss, "end-sessions", "asha@college.edu"); code != 200 {
		t.Errorf("admin ends an organiser's sessions: %d", code)
	}
	if code, _ := boss.do("DELETE", "/api/admin/people/"+ashaID, nil, origin); code != 204 {
		t.Errorf("admin revokes organiser: %d", code)
	}
	if code := account(boss, "disable", "asha@college.edu"); code != 200 {
		t.Errorf("admin disables a student: %d", code)
	}
	if code := account(boss, "enable", "asha@college.edu"); code != 200 {
		t.Errorf("admin enables a student: %d", code)
	}

	// What an admin may not do, each a 403.
	for _, role := range []string{"admin", "owner"} {
		if code, _ := grant(boss, "ravi@college.edu", role); code != 403 {
			t.Errorf("admin grants %s: %d, want 403", role, code)
		}
	}
	for _, email := range []string{"peer@college.edu", "owner@college.edu", "boss@college.edu"} {
		if code, _ := grant(boss, email, "organiser"); code != 403 {
			t.Errorf("admin changes the role of %s: %d, want 403", email, code)
		}
		for _, action := range []string{"end-sessions", "disable", "enable"} {
			if code := account(boss, action, email); code != 403 {
				t.Errorf("admin %s %s: %d, want 403", action, email, code)
			}
		}
	}
	if code, body := boss.do("DELETE", "/api/admin/people/"+peerID, nil, origin); code != 403 || body["error"] == nil {
		t.Errorf("admin removes another admin: %d %v, want 403", code, body)
	}
	if code, _ := boss.do("POST", "/api/admin/accounts/reset-google", map[string]string{"email": "asha@college.edu"}, origin); code != 403 {
		t.Errorf("admin resets a Google link: %d, want 403 (owners only)", code)
	}

	// An owner may act on an admin.
	if code := account(owner, "end-sessions", "peer@college.edu"); code != 200 {
		t.Errorf("owner ends an admin's sessions: %d", code)
	}

	// A demotion takes effect on the admin's next request: the role is
	// read with the session every time.
	if code, _ := grant(owner, "boss@college.edu", "organiser"); code != 200 {
		t.Fatalf("owner demotes the admin: %d", code)
	}
	if code, _ := boss.do("GET", "/api/admin/people", nil, ""); code != 403 {
		t.Errorf("demoted admin reads People: %d, want 403", code)
	}
	if code, role := boss.me(); code != 200 || role != "organiser" {
		t.Errorf("demoted admin: %d %q", code, role)
	}
}
