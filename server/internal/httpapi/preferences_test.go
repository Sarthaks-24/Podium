package httpapi

import (
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

// Display preferences follow the account, so a second browser (another lab
// machine) gets the theme chosen on the first (decision D42).
func TestPreferencesFollowTheAccount(t *testing.T) {
	s := newSite(t)
	asha := authtest.Person{Email: "asha@college.edu", Verified: true, Name: "Asha"}

	first := s.browser()
	first.signIn(asha, "/")
	code, body := first.do("GET", "/api/me", nil, "")
	if code != 200 || body["theme"] != "" || body["verdicts"] != "standard" {
		t.Fatalf("defaults: %d theme=%v verdicts=%v", code, body["theme"], body["verdicts"])
	}

	code, body = first.do("PUT", "/api/me/preferences", map[string]string{"theme": "hc-dark", "verdicts": "signal"}, s.srv.URL)
	if code != 200 || body["theme"] != "hc-dark" || body["verdicts"] != "signal" {
		t.Fatalf("save: %d %v", code, body)
	}

	second := s.browser()
	second.signIn(asha, "/")
	_, body = second.do("GET", "/api/me", nil, "")
	if body["theme"] != "hc-dark" || body["verdicts"] != "signal" {
		t.Errorf("second machine: theme=%v verdicts=%v", body["theme"], body["verdicts"])
	}

	// Back to following the system.
	if code, _ := first.do("PUT", "/api/me/preferences", map[string]string{"theme": "", "verdicts": "standard"}, s.srv.URL); code != 200 {
		t.Errorf("reset: %d", code)
	}

	for _, bad := range []map[string]string{
		{"theme": "Dark", "verdicts": "standard"},
		{"theme": "../x", "verdicts": "standard"},
		{"theme": "a-theme-id-that-is-far-too-long-to-be-real", "verdicts": "standard"},
		{"theme": "dark", "verdicts": "neon"},
		{"theme": "dark"},
	} {
		if code, _ := first.do("PUT", "/api/me/preferences", bad, s.srv.URL); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}

	if code, _ := s.browser().do("PUT", "/api/me/preferences", map[string]string{"theme": "dark", "verdicts": "standard"}, s.srv.URL); code != 401 {
		t.Errorf("signed out: %d, want 401", code)
	}
}
