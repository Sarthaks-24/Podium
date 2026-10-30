package httpapi

// Checks for the routes the security council's fixes added (2026-10-18):
// the owner's account actions (S6) and the contest audit log (S9). They
// are served by auth/http.go and contests/http_admin.go.

import (
	"fmt"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

func TestOwnerEndsSessionsAndDisablesAccounts(t *testing.T) {
	old := clockEvery
	clockEvery = 100 * time.Millisecond
	defer func() { clockEvery = old }()
	s := newSite(t)
	o := s.srv.URL
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	laptop := s.signedIn("asha@college.edu", "Asha")
	admin := s.signedIn("prof@college.edu", "Prof")
	owner.do("POST", "/api/admin/people", map[string]string{"email": "prof@college.edu", "role": "organiser"}, o)

	// Only owners have the account actions.
	for _, path := range []string{"/api/admin/accounts/end-sessions", "/api/admin/accounts/disable", "/api/admin/accounts/enable"} {
		if code, _ := admin.do("POST", path, map[string]string{"email": "asha@college.edu"}, o); code != 401 && code != 403 {
			t.Errorf("admin %s: %d", path, code)
		}
		if code, _ := asha.do("POST", path, map[string]string{"email": "asha@college.edu"}, o); code != 403 {
			t.Errorf("student %s: %d", path, code)
		}
	}

	// End sessions: every browser of the person is signed out.
	code, out := owner.do("POST", "/api/admin/accounts/end-sessions", map[string]string{"email": "Asha@College.edu"}, o)
	if code != 200 || out["ended"] != float64(2) {
		t.Fatalf("end sessions: %d %v", code, out)
	}
	for _, b := range []*browser{asha, laptop} {
		if code, _ := b.me(); code != 401 {
			t.Errorf("session survived end sessions: %d", code)
		}
	}
	if code, _ := owner.do("POST", "/api/admin/accounts/end-sessions", map[string]string{"email": "nobody@college.edu"}, o); code != 404 {
		t.Errorf("end sessions of an unknown email: %d", code)
	}

	// Disable: their open stream closes at the next clock tick, every
	// signed-in route refuses them, and they cannot sign in again.
	asha = s.signedIn("asha@college.edu", "Asha")
	events, stop := openStream(t, asha, "lab-pc")
	defer stop()
	waitFor(t, events, "clock")
	code, out = owner.do("POST", "/api/admin/accounts/disable", map[string]string{"email": "asha@college.edu"}, o)
	if code != 200 || out["ended"] != float64(1) || out["account"].(map[string]any)["disabled"] != true {
		t.Fatalf("disable: %d %v", code, out)
	}
	closed := time.After(3 * time.Second)
	for open := true; open; {
		select {
		case _, ok := <-events:
			open = ok
		case <-closed:
			t.Fatal("a disabled account's stream stayed open")
		}
	}
	if code, _ := asha.do("GET", "/api/contests", nil, ""); code != 401 {
		t.Errorf("disabled account on a signed-in route: %d", code)
	}
	if final := s.browser().signIn(authtest.Person{Email: "asha@college.edu", Verified: true}, "/"); final.Query().Get("error") != "disabled" {
		t.Errorf("disabled account signing in landed on %s", final)
	}
	_, people := owner.do("GET", "/api/admin/people", nil, "")
	if list, _ := people["disabled"].([]any); len(list) != 1 || list[0].(map[string]any)["email"] != "asha@college.edu" {
		t.Errorf("people page disabled list: %v", people["disabled"])
	}

	// Enable: they may sign in again.
	if code, out := owner.do("POST", "/api/admin/accounts/enable", map[string]string{"email": "asha@college.edu"}, o); code != 200 {
		t.Fatalf("enable: %d %v", code, out)
	}
	if code, _ := s.signedIn("asha@college.edu", "Asha").me(); code != 200 {
		t.Errorf("re-enabled account: %d", code)
	}

	// The configured owner and the owner acting cannot be disabled.
	if code, _ := owner.do("POST", "/api/admin/accounts/disable", map[string]string{"email": "owner@college.edu"}, o); code != 409 {
		t.Errorf("disabling the configured owner: %d", code)
	}
	owner.do("POST", "/api/admin/people", map[string]string{"email": "dean@college.edu", "role": "owner"}, o)
	dean := s.signedIn("dean@college.edu", "Dean")
	if code, _ := dean.do("POST", "/api/admin/accounts/disable", map[string]string{"email": "dean@college.edu"}, o); code != 409 {
		t.Errorf("an owner disabling themselves: %d", code)
	}
	// Cross-site requests are refused like every other change.
	if code, _ := owner.do("POST", "/api/admin/accounts/disable", map[string]string{"email": "prof@college.edu"}, "https://evil.example"); code != 403 {
		t.Errorf("cross-site disable: %d", code)
	}
}

func TestAuditRoute(t *testing.T) {
	c := newCoOrgSite(t, "s9r", nil)
	o := c.srv.URL
	c.owner.do("PUT", c.admin(""), map[string]any{"min_gap_s": 60}, o)
	c.owner.do("POST", c.admin("/pause"), nil, o)

	for _, b := range []*browser{c.owner} {
		code, out := b.do("GET", c.admin("/audit"), nil, "")
		if code != 200 || out["has_more"] != false {
			t.Fatalf("audit log: %d %v", code, out)
		}
		entries := out["entries"].([]any)
		first := entries[0].(map[string]any)
		actor := first["actor"].(map[string]any)
		if first["action"] != "pause" || actor["email"] != "owner@college.edu" || actor["role"] != "owner" || first["created_at"] == nil {
			t.Errorf("newest audit row: %v", first)
		}
		if len(entries) != 4 { // problems, published, min_gap_s, pause
			t.Errorf("%d audit rows: %v", len(entries), entries)
		}
	}
	// Students, even of the contest, are refused before the contest is read.
	if code, _ := c.asha.do("GET", c.admin("/audit"), nil, ""); code != 403 {
		t.Errorf("student reading the audit log: %d", code)
	}
	if code, _ := c.owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/audit", c.id+100), nil, ""); code != 404 {
		t.Errorf("audit log of a missing contest: %d", code)
	}
}

// GET /api/admin/accounts?email= finds any account, students included, for
// owners and admins (People's Account help, audit O10). Everyone else gets
// the same 403 for an address that exists and one that does not, so the
// route reveals nothing about which addresses have accounts.
func TestAccountLookup(t *testing.T) {
	s := newSite(t)
	o := s.srv.URL
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha Rao")
	admin := s.signedIn("prof@college.edu", "Prof")
	owner.do("POST", "/api/admin/people", map[string]string{"email": "prof@college.edu", "role": "organiser"}, o)

	for who, b := range map[string]*browser{"owner": owner, "admin": admin} {
		code, out := b.do("GET", "/api/admin/accounts?email=%20Asha@College.EDU%20", nil, "")
		if code != 200 {
			t.Fatalf("%s lookup: %d %v", who, code, out)
		}
		a, _ := out["account"].(map[string]any)
		if a["email"] != "asha@college.edu" || a["name"] != "Asha Rao" || a["role"] != "student" || a["disabled"] != false || a["last_login_at"] == nil {
			t.Errorf("%s lookup: %v", who, out)
		}
		if _, ok := out["server_now"]; !ok {
			t.Errorf("%s lookup has no server_now: %v", who, out)
		}
		if code, out := b.do("GET", "/api/admin/accounts?email=nobody@college.edu", nil, ""); code != 404 {
			t.Errorf("%s unknown address: %d %v", who, code, out)
		}
		if code, _ := b.do("GET", "/api/admin/accounts", nil, ""); code != 400 {
			t.Errorf("%s without an address: %d", who, code)
		}
	}

	// A student, signed in or not, learns nothing: one answer for both.
	for _, email := range []string{"prof@college.edu", "nobody@college.edu"} {
		code, out := asha.do("GET", "/api/admin/accounts?email="+email, nil, "")
		if code != 403 {
			t.Errorf("student looks up %s: %d %v", email, code, out)
		}
		if _, ok := out["account"]; ok {
			t.Errorf("student got an account for %s: %v", email, out)
		}
		if code, _ := s.browser().do("GET", "/api/admin/accounts?email="+email, nil, ""); code != 401 {
			t.Errorf("signed-out lookup of %s: %d", email, code)
		}
	}
}
