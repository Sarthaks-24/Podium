package httpapi

// Site settings and the config route, queue health on the contest
// response, and the organiser contest list without per-contest lookups.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/retention"
)

func TestConfigCarriesOrganisationAndRetention(t *testing.T) {
	s := newSite(t)
	anon := s.browser()
	code, body := anon.do("GET", "/api/auth/config", nil, "")
	if code != 200 || body["contact_email"] != "owner@college.edu" {
		t.Fatalf("config: %d %v", code, body)
	}
	org := body["organisation"].(map[string]any)
	for _, k := range []string{"name", "contact_email", "phone", "privacy_contact"} {
		if v, ok := org[k]; !ok || v != "" {
			t.Errorf("organisation.%s = %v before any setting", k, v)
		}
	}
	// The retention list is the constants file, row for row.
	want, _ := json.Marshal(retention.Periods())
	got, _ := json.Marshal(body["retention"])
	var a, b any
	json.Unmarshal(want, &a)
	json.Unmarshal(got, &b)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Errorf("retention\n got %s\nwant %s", got, want)
	}
	// Nothing about contests, counts or the release before sign-in.
	for k := range body {
		switch k {
		case "allowed_domains", "contact_email", "mode", "organisation", "retention":
		default:
			t.Errorf("config carries %q", k)
		}
	}

	owner := s.signedIn("owner@college.edu", "Owner")
	admin := s.signedIn("admin@college.edu", "Admin")
	owner.do("POST", "/api/admin/people", map[string]any{"email": "admin@college.edu", "role": "admin"}, s.srv.URL)
	settings := map[string]any{"name": "  Example College  ", "contact_email": "Podium@College.edu", "phone": "+91 80 1234 5678",
		"privacy_contact": "privacy@college.edu"}
	if code, out := admin.do("PUT", "/api/admin/site-settings", settings, s.srv.URL); code != 403 {
		t.Errorf("admin wrote site settings: %d %v", code, out)
	}
	code, out := owner.do("PUT", "/api/admin/site-settings", settings, s.srv.URL)
	if code != 200 || out["name"] != "Example College" || out["contact_email"] != "podium@college.edu" {
		t.Fatalf("set: %d %v", code, out)
	}
	_, body = anon.do("GET", "/api/auth/config", nil, "")
	if body["contact_email"] != "podium@college.edu" || body["organisation"].(map[string]any)["phone"] != "+91 80 1234 5678" {
		t.Errorf("config after set: %v", body)
	}
	_, people := owner.do("GET", "/api/admin/people", nil, "")
	if people["organisation"].(map[string]any)["name"] != "Example College" {
		t.Errorf("people organisation: %v", people["organisation"])
	}
	// One audit row naming the fields, never the values.
	var details string
	s.db.QueryRow("SELECT details FROM site_audit WHERE action = 'settings_changed'").Scan(&details)
	if !strings.Contains(details, `"name"`) || strings.Contains(details, "Example") {
		t.Errorf("audit details %s", details)
	}
	// Saving the same values writes nothing.
	owner.do("PUT", "/api/admin/site-settings", settings, s.srv.URL)
	var n int
	s.db.QueryRow("SELECT count(*) FROM site_audit WHERE action = 'settings_changed'").Scan(&n)
	if n != 1 {
		t.Errorf("%d settings rows after an unchanged save", n)
	}

	bad := map[string]any{"name": "Line\nbreak", "contact_email": "not an email", "phone": "call me", "privacy_contact": strings.Repeat("x", 201)}
	code, out = owner.do("PUT", "/api/admin/site-settings", bad, s.srv.URL)
	fields, _ := out["error"].(map[string]any)["fields"].(map[string]any)
	if code != 400 || len(fields) != 4 {
		t.Errorf("bad settings: %d %v", code, out)
	}
	if code, _ := owner.do("PUT", "/api/admin/site-settings", map[string]any{"name": "x"}, s.srv.URL); code != 400 {
		t.Errorf("partial settings: %d", code)
	}
	// Clearing the contact email falls back to the owner's address.
	settings["contact_email"] = ""
	owner.do("PUT", "/api/admin/site-settings", settings, s.srv.URL)
	if _, body = anon.do("GET", "/api/auth/config", nil, ""); body["contact_email"] != "owner@college.edu" {
		t.Errorf("fallback contact %v", body["contact_email"])
	}
}

func TestQueueHealthOnTheContestResponse(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "queue-health")
	start := time.Now().Add(time.Minute)
	id := s.createContest(owner, "queue", start, map[string]int64{"A": pid}, nil)
	s.at(start, 5)
	get := func() map[string]any {
		t.Helper()
		code, out := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", id), nil, "")
		if code != 200 {
			t.Fatalf("contest: %d %v", code, out)
		}
		return out
	}
	out := get()
	q := out["queue"].(map[string]any)
	if q["waiting"] != 0.0 || q["judging"] != 0.0 || q["errors"] != 0.0 || q["oldest_wait_s"] != 0.0 ||
		out["unanswered"] != 0.0 || out["judge_last_claim_at"] != nil {
		t.Fatalf("idle: %v %v %v", q, out["unanswered"], out["judge_last_claim_at"])
	}
	asha := s.signedIn("asha@college.edu", "Asha")
	for i, b := range []*browser{asha, s.signedIn("ravi@college.edu", "Ravi")} {
		b.do("POST", "/api/contests/queue/start", map[string]any{"accept_honour": true}, s.srv.URL)
		code, sub := b.do("POST", "/api/contests/queue/problems/A/submit",
			map[string]any{"language": "cpp17", "source": "correct", "client_key": fmt.Sprintf("k%d", i)}, s.srv.URL)
		if code != 202 {
			t.Fatalf("submit: %d %v", code, sub)
		}
	}
	if code, out := asha.do("POST", "/api/contests/queue/clarifications", map[string]any{"question": "Is n at least 1?"}, s.srv.URL); code != 201 {
		t.Fatalf("ask: %d %v", code, out)
	}
	s.at(start, 6)
	out = get()
	q = out["queue"].(map[string]any)
	if q["waiting"] != 2.0 || q["judging"] != 0.0 || q["oldest_wait_s"].(float64) < 59 || out["unanswered"] != 1.0 {
		t.Errorf("two waiting: %v unanswered %v", q, out["unanswered"])
	}
	// A judge takes one: judging 1, waiting 1, and it was seen.
	resp, _ := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: []string{"submit"}, Worker: "fake"})
	if resp.StatusCode != 200 {
		t.Fatalf("claim %d", resp.StatusCode)
	}
	out = get()
	q = out["queue"].(map[string]any)
	if q["waiting"] != 1.0 || q["judging"] != 1.0 || out["judge_last_claim_at"] == nil {
		t.Errorf("one judging: %v last claim %v", q, out["judge_last_claim_at"])
	}
	// A judge error counts until the submission is judged again.
	s.db.Exec("UPDATE submissions SET state = 'error' WHERE id = (SELECT min(id) FROM submissions)")
	if q = get()["queue"].(map[string]any); q["errors"] != 1.0 {
		t.Errorf("errors %v", q)
	}
}

// The organiser list builds each row from the two list queries alone
// (S1-08, no query per contest): rows carry no extensions and no queue,
// which only GET /api/admin/contests/{id} reads.
func TestAdminContestListHasNoPerContestLookups(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	for i := 0; i < 20; i++ {
		s.createContest(owner, fmt.Sprintf("list-%02d", i), start, map[string]int64{"A": pid}, nil)
	}
	code, out := owner.do("GET", "/api/admin/contests", nil, "")
	list, _ := out["contests"].([]any)
	if code != 200 || len(list) != 20 {
		t.Fatalf("list: %d, %d rows", code, len(list))
	}
	for _, row := range list {
		c := row.(map[string]any)
		if _, ok := c["queue"]; ok {
			t.Fatalf("a list row carries the queue: %v", c["slug"])
		}
		if _, ok := c["extensions"]; ok {
			t.Fatalf("a list row carries extensions: %v", c["slug"])
		}
	}
}
