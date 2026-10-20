package httpapi

// P3 backend: site settings and the config route (C5), the contact form
// and inbox (C6), and queue health on the contest response (C10).

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/retention"
)

func TestP3ConfigCarriesOrganisationAndRetention(t *testing.T) {
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

func TestP3ContactFormAndInbox(t *testing.T) {
	s := newSite(t)
	anon := s.browser()
	msg := map[string]any{"topic": "contest", "message": "Problem B's sample has n = 0."}
	if code, out := anon.do("POST", "/api/contact", msg, s.srv.URL); code != 401 {
		t.Errorf("signed out: %d %v", code, out)
	}
	asha := s.signedIn("asha@college.edu", "Asha Rao")
	if code, out := asha.do("POST", "/api/contact", msg, ""); code != 403 {
		t.Errorf("no Origin: %d %v", code, out)
	}
	if code, out := asha.do("POST", "/api/contact", msg, "https://evil.example"); code != 403 {
		t.Errorf("other Origin: %d %v", code, out)
	}
	for _, c := range []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"topic": "spam", "message": "hi"}, "topic"},
		{map[string]any{"topic": "other", "message": "   "}, "message"},
		{map[string]any{"topic": "other", "message": strings.Repeat("é", 2001)}, "message"},
		{map[string]any{"topic": "other", "message": "bell \a"}, "message"},
		{map[string]any{"topic": "other", "message": "looks " + string(rune(0x202e)) + "reversed"}, "message"},
	} {
		code, out := asha.do("POST", "/api/contact", c.body, s.srv.URL)
		fields, _ := out["error"].(map[string]any)["fields"].(map[string]any)
		if code != 400 || fields[c.field] == nil {
			t.Errorf("%v: %d %v", c.body, code, out)
		}
	}
	// 2,000 characters with newlines is fine; a sixth message in a day is not.
	long := map[string]any{"topic": "privacy", "message": strings.Repeat("ab\n", 666) + "ab"}
	for i := 0; i < 5; i++ {
		body := msg
		if i == 0 {
			body = long
		}
		if code, out := asha.do("POST", "/api/contact", body, s.srv.URL); code != 201 || out["id"] == nil {
			t.Fatalf("send %d: %d %v", i, code, out)
		}
	}
	code, out := asha.do("POST", "/api/contact", msg, s.srv.URL)
	if e, _ := out["error"].(map[string]any); code != 429 || e["code"] != "rate_limited" || e["retry_after_s"] == nil ||
		!strings.Contains(e["message"].(string), "5 messages") {
		t.Errorf("sixth: %d %v", code, out)
	}
	// The site-wide cap: 100 in 24 hours from everyone.
	now := time.Now().UnixMilli()
	for i := 0; i < 95; i++ {
		s.db.Exec("INSERT INTO contact_messages (user_id, topic, message, created_at) VALUES (1, 'other', 'x', ?)", now)
	}
	ravi := s.signedIn("ravi@college.edu", "Ravi")
	code, out = ravi.do("POST", "/api/contact", msg, s.srv.URL)
	if e, _ := out["error"].(map[string]any); code != 429 || !strings.Contains(e["message"].(string), "too many messages today") {
		t.Errorf("site cap: %d %v", code, out)
	}
	s.db.Exec("DELETE FROM contact_messages WHERE user_id = 1")

	// The inbox: owners and admins, never students.
	if code, _ := asha.do("GET", "/api/admin/contact", nil, ""); code != 403 {
		t.Errorf("student inbox: %d", code)
	}
	owner := s.signedIn("owner@college.edu", "Owner")
	code, out = owner.do("GET", "/api/admin/contact", nil, "")
	list := out["messages"].([]any)
	if code != 200 || len(list) != 5 || out["more"] != false {
		t.Fatalf("inbox: %d %v", code, out)
	}
	first := list[0].(map[string]any)
	sender := first["sender"].(map[string]any)
	if sender["email"] != "asha@college.edu" || sender["name"] != "Asha Rao" || first["topic"] != "contest" {
		t.Errorf("newest message %v", first)
	}
	last := list[4].(map[string]any)
	if last["message"] != long["message"] {
		t.Errorf("long message changed: %q", last["message"])
	}
	id := int64(last["id"].(float64))
	if code, out := owner.do("GET", fmt.Sprintf("/api/admin/contact?before=%d", id), nil, ""); code != 200 || len(out["messages"].([]any)) != 0 {
		t.Errorf("page before the oldest: %d %v", code, out)
	}
	if code, _ := owner.do("DELETE", fmt.Sprintf("/api/admin/contact/%d", id), nil, s.srv.URL); code != 204 {
		t.Errorf("delete: %d", code)
	}
	if code, _ := owner.do("DELETE", fmt.Sprintf("/api/admin/contact/%d", id), nil, s.srv.URL); code != 404 {
		t.Errorf("delete again: %d", code)
	}
	var subject, details string
	s.db.QueryRow("SELECT subject_email, details FROM site_audit WHERE action = 'contact_deleted'").Scan(&subject, &details)
	if subject != "asha@college.edu" || !strings.Contains(details, `"topic":"privacy"`) || strings.Contains(details, "ab") {
		t.Errorf("audit row %s %s", subject, details)
	}
}

func TestP3QueueHealthOnTheContestResponse(t *testing.T) {
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

func TestP3SemesterRollover(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	old := s.signedIn("old@college.edu", "Old")
	s.signedIn("recent@college.edu", "Recent")
	now := time.Now()
	s.db.Exec("UPDATE users SET last_login_at = ? WHERE email = 'old@college.edu'", now.Add(-200*24*time.Hour).UnixMilli())
	s.db.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES
		(91, 'term-1', 'Term 1', ?, ?, 1, 1), (92, 'next-term', 'Next', ?, ?, 1, 1)`,
		now.Add(-50*time.Hour).UnixMilli(), now.Add(-48*time.Hour).UnixMilli(), now.Add(48*time.Hour).UnixMilli(), now.Add(50*time.Hour).UnixMilli())

	if code, _ := owner.do("POST", "/api/admin/rollover", map[string]any{"label": ""}, s.srv.URL); code != 400 {
		t.Errorf("no label: %d", code)
	}
	if code, _ := old.do("POST", "/api/admin/rollover", map[string]any{"label": "2026 odd"}, s.srv.URL); code != 403 {
		t.Errorf("student: %d", code)
	}
	// The preview lists and changes nothing.
	code, out := owner.do("POST", "/api/admin/rollover", map[string]any{"label": "2026 odd semester"}, s.srv.URL)
	cs, accounts := out["contests"].([]any), out["accounts"].([]any)
	if code != 200 || out["applied"] != false || len(cs) != 1 || cs[0].(map[string]any)["slug"] != "term-1" ||
		len(accounts) != 1 || accounts[0].(map[string]any)["email"] != "old@college.edu" || out["idle_days"] != 180.0 {
		t.Fatalf("preview: %d %v", code, out)
	}
	if code, _ := old.me(); code != 200 {
		t.Fatal("the preview signed the idle student out")
	}
	code, out = owner.do("POST", "/api/admin/rollover", map[string]any{"label": "2026 odd semester", "apply": true}, s.srv.URL)
	if code != 200 || out["applied"] != true || len(out["accounts"].([]any)) != 1 {
		t.Fatalf("apply: %d %v", code, out)
	}
	var archived, disabled int
	s.db.QueryRow("SELECT archived FROM contests WHERE id = 91").Scan(&archived)
	s.db.QueryRow("SELECT disabled FROM users WHERE email = 'old@college.edu'").Scan(&disabled)
	if archived != 1 || disabled != 1 {
		t.Errorf("archived %d disabled %d", archived, disabled)
	}
	s.db.QueryRow("SELECT archived FROM contests WHERE id = 92").Scan(&archived)
	s.db.QueryRow("SELECT disabled FROM users WHERE email = 'recent@college.edu'").Scan(&disabled)
	if archived != 0 || disabled != 0 {
		t.Errorf("the future contest or the recent student changed")
	}
	if code, _ := old.me(); code != 401 {
		t.Errorf("idle student still signed in: %d", code)
	}
	var details string
	s.db.QueryRow("SELECT details FROM site_audit WHERE action = 'semester_rollover'").Scan(&details)
	if !strings.Contains(details, `"label":"2026 odd semester"`) || !strings.Contains(details, "old@college.edu") ||
		!strings.Contains(details, `"slug":"term-1"`) {
		t.Errorf("audit %s", details)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM contest_audit WHERE contest_id = 91 AND action = 'archive'").Scan(&n)
	if n != 1 {
		t.Errorf("%d archive rows", n)
	}
}
