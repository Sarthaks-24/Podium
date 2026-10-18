package httpapi

// Checks for the routes the security council's fixes added (2026-10-18):
// the owner's account actions (S6) and the contest audit log (S9). They
// are mounted by securityRoutes (people.go).

import (
	"fmt"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

func TestS6OwnerEndsSessionsAndDisablesAccounts(t *testing.T) {
	old := clockEvery
	clockEvery = 100 * time.Millisecond
	defer func() { clockEvery = old }()
	s := newSite(t)
	o := s.srv.URL
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	laptop := s.signedIn("asha@college.edu", "Asha")
	admin := s.signedIn("prof@college.edu", "Prof")
	owner.do("POST", "/api/admin/people", map[string]string{"email": "prof@college.edu", "role": "admin"}, o)

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

// A co-organiser reading a classmate's submission gets the student rules
// too (the workspace.go part of the patch): nothing in exam mode until
// every window is over.
func TestS1CoOrganiserReadsSubmissionsAsAStudent(t *testing.T) {
	c := newCoOrgSite(t, "s1r", map[string]any{"feedback_mode": "exam"})
	o := c.srv.URL
	c.at(c.start, 5)
	c.asha.do("POST", "/api/contests/s1r/start", map[string]any{"accept_honour": true}, o)
	_, sub := c.submit(c.asha, "s1r", "A", "correct")
	path := fmt.Sprintf("/api/submissions/%d", int64(sub["id"].(float64)))
	if code, got := c.ta.do("GET", path, nil, ""); code != 200 || got["state"] != "submitted" {
		t.Errorf("co-organiser reading an exam-mode submission: %d %v", code, got)
	}
	if _, got := c.owner.do("GET", path, nil, ""); got["verdict"] != "AC" {
		t.Errorf("owner reading it: %v", got)
	}
	c.at(c.start, 125)
	if _, got := c.ta.do("GET", path, nil, ""); got["verdict"] != "AC" || got["max_memory_kb"] != float64(16384) {
		t.Errorf("co-organiser after the end (coarse figures): %v", got)
	}
}

func TestS9AuditRoute(t *testing.T) {
	c := newCoOrgSite(t, "s9r", nil)
	o := c.srv.URL
	c.owner.do("PUT", c.admin(""), map[string]any{"min_gap_s": 60}, o)
	c.owner.do("POST", c.admin("/pause"), nil, o)

	for _, b := range []*browser{c.owner, c.ta} {
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
		if len(entries) != 5 { // problems, published, organisers, min_gap_s, pause
			t.Errorf("%d audit rows: %v", len(entries), entries)
		}
	}
	// Students, even of the contest, get the same 404 as a missing contest.
	if code, _ := c.asha.do("GET", c.admin("/audit"), nil, ""); code != 404 {
		t.Errorf("student reading the audit log: %d", code)
	}
	if code, _ := c.owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/audit", c.id+100), nil, ""); code != 404 {
		t.Errorf("audit log of a missing contest: %d", code)
	}
}
