package httpapi

import "testing"

// Testing mode signs anyone in from a form; no other mode has the route
// (decision D48).
func TestTestingModeSignIn(t *testing.T) {
	s := newSiteMode(t, "testing")
	b := s.browser()
	if code, body := b.do("GET", "/api/auth/config", nil, ""); code != 200 || body["mode"] != "testing" {
		t.Fatalf("config: %d %v", code, body)
	}
	if code, _ := b.do("GET", "/api/auth/google/start", nil, ""); code != 404 {
		t.Errorf("google sign-in available in testing mode: %d", code)
	}
	code, body := b.do("POST", "/api/auth/test-signin", map[string]string{"email": "TA@Example.com", "name": "TA", "role": "organiser", "next": "//evil.example"}, s.srv.URL)
	if code != 200 || body["next"] != "/" {
		t.Fatalf("test sign-in: %d %v", code, body)
	}
	if code, role := b.me(); code != 200 || role != "organiser" {
		t.Errorf("after test sign-in: %d %s", code, role)
	}
	if code, _ := b.do("GET", "/api/admin/problems", nil, ""); code != 200 {
		t.Errorf("admin in testing mode cannot list problems: %d", code)
	}
	// The configured owner cannot be demoted from the form.
	owner := s.browser()
	owner.do("POST", "/api/auth/test-signin", map[string]string{"email": "owner@college.edu", "role": "student"}, s.srv.URL)
	if _, role := owner.me(); role != "owner" {
		t.Errorf("configured owner signed in as %s", role)
	}
	for _, bad := range []map[string]string{{"email": "not-an-email", "role": "student"}, {"email": "a@b.c", "role": "root"}} {
		if code, _ := s.browser().do("POST", "/api/auth/test-signin", bad, s.srv.URL); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	if code, _ := s.browser().do("POST", "/api/auth/test-signin", map[string]string{"email": "x@y.z", "role": "student"}, "https://evil.example"); code != 403 {
		t.Errorf("cross-site test sign-in: %d", code)
	}
}

func TestProductionHasNoTestSignIn(t *testing.T) {
	s := newSite(t)
	b := s.browser()
	if code, _ := b.do("POST", "/api/auth/test-signin", map[string]string{"email": "a@college.edu", "role": "owner"}, s.srv.URL); code != 404 {
		t.Errorf("test sign-in in production: %d", code)
	}
	if code, body := b.do("GET", "/api/auth/config", nil, ""); code != 200 || body["mode"] != "production" {
		t.Errorf("config: %d %v", code, body)
	}
}
