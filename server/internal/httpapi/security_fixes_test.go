package httpapi

// Checks for the security council's must-fix items (cache/council-
// 2026-10-18/security-council/verdict.md): co-organisers whose site role is
// student (S1), testing mode behind a proxy (S2), sessions on shared lab
// PCs (S5) and the contest audit log (S9). The account actions (S6) and
// the audit route are in security_routes_test.go.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

// coOrgSite is a site with a published contest, an owner, a student
// co-organiser (ta) and a student (asha), the contest starting in an hour.
type coOrgSite struct {
	*site
	owner, ta, asha *browser
	id              int64
	slug            string
	start           time.Time
}

func newCoOrgSite(t *testing.T, slug string, settings map[string]any) *coOrgSite {
	s := newSite(t)
	c := &coOrgSite{site: s, slug: slug, start: time.Now().Add(time.Hour).Truncate(time.Minute)}
	c.owner = s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(c.owner, slug+"-a")
	c.id = s.createContest(c.owner, slug, c.start, map[string]int64{"A": pa}, settings)
	c.ta = s.signedIn("ta@college.edu", "TA")
	c.asha = s.signedIn("asha@college.edu", "Asha")
	if code, out := c.owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", c.id), map[string]string{"email": "ta@college.edu"}, s.srv.URL); code != 200 {
		t.Fatalf("add co-organiser: %d %v", code, out)
	}
	return c
}

func (c *coOrgSite) admin(path string) string {
	return fmt.Sprintf("/api/admin/contests/%d%s", c.id, path)
}

func TestS1CoOrganiserCannotChangeGradingAfterStart(t *testing.T) {
	c := newCoOrgSite(t, "s1", nil)
	o := c.srv.URL
	// Before the start a co-organiser may still move the schedule.
	if code, out := c.ta.do("PUT", c.admin(""), map[string]any{"ends_at": c.start.Add(150 * time.Minute).UnixMilli()}, o); code != 200 {
		t.Fatalf("co-organiser moving the end before the start: %d %v", code, out)
	}
	c.at(c.start, 5)
	locked := map[string]any{
		"starts_at": c.start.Add(time.Minute).UnixMilli(), "ends_at": c.start.Add(300 * time.Minute).UnixMilli(),
		"start_mode": "window", "scoring_mode": "best_submission", "feedback_mode": "exam",
		"access": "roster", "freeze_minutes": 30, "languages": []string{"python3"}, "graded": false, "max_per_problem": 100,
	}
	for field, v := range locked {
		code, out := c.ta.do("PUT", c.admin(""), map[string]any{field: v}, o)
		if code != 403 || errCode(out) != "locked_after_start" {
			t.Errorf("co-organiser changing %s after the start: %d %v", field, code, out)
		}
	}
	if code, out := c.ta.do("PUT", c.admin(""), map[string]any{"title": "Week 1, renamed", "description_md": "Good luck."}, o); code != 200 {
		t.Errorf("co-organiser changing the title after the start: %d %v", code, out)
	}
	// Owners and admins still may (the web app asks them to confirm).
	if code, out := c.owner.do("PUT", c.admin(""), map[string]any{"feedback_mode": "exam", "freeze_minutes": 30}, o); code != 200 {
		t.Errorf("owner changing the rules after the start: %d %v", code, out)
	}

	// The levers that change results are owners' and admins' only.
	c.asha.do("POST", "/api/contests/s1/start", map[string]any{"accept_honour": true}, o)
	refused := []struct {
		method, path string
		body         any
	}{
		{"PUT", c.admin("/roster"), []map[string]string{{"email": "asha@college.edu"}}},
		{"PUT", c.admin(fmt.Sprintf("/extensions/%d", c.asha.userID())), map[string]any{"minutes": 30}},
		{"POST", c.admin("/extend"), map[string]any{"minutes": 10}},
		{"POST", c.admin("/pause"), nil},
		{"POST", c.admin("/resume"), nil},
		{"POST", c.admin("/rejudge"), map[string]any{"problem_label": "A"}},
	}
	for _, r := range refused {
		if code, out := c.ta.do(r.method, r.path, r.body, o); code != 403 {
			t.Errorf("co-organiser %s %s: %d %v", r.method, r.path, code, out)
		}
	}
	for _, r := range refused[1:] {
		if code, out := c.owner.do(r.method, r.path, r.body, o); code != 200 && code != 202 {
			t.Errorf("owner %s %s: %d %v", r.method, r.path, code, out)
		}
	}
}

func TestS1ExtensionsOnlyForParticipants(t *testing.T) {
	c := newCoOrgSite(t, "s1x", nil)
	o := c.srv.URL
	zed := c.signedIn("zed@college.edu", "Zed")
	c.at(c.start, 5)
	code, out := c.owner.do("PUT", c.admin(fmt.Sprintf("/extensions/%d", zed.userID())), map[string]any{"minutes": 30}, o)
	if code != 409 || errCode(out) != "not_participant" {
		t.Errorf("extension for someone not taking part: %d %v", code, out)
	}
	// The co-organiser is not a student with a clock either.
	if code, _ := c.owner.do("PUT", c.admin(fmt.Sprintf("/extensions/%d", c.ta.userID())), map[string]any{"minutes": 30}, o); code != 409 {
		t.Errorf("extension for the co-organiser: %d", code)
	}
	c.asha.do("POST", "/api/contests/s1x/start", map[string]any{"accept_honour": true}, o)
	if code, out := c.owner.do("PUT", c.admin(fmt.Sprintf("/extensions/%d", c.asha.userID())), map[string]any{"minutes": 30}, o); code != 200 {
		t.Errorf("extension for a student who started: %d %v", code, out)
	}
	// On the roster but not started yet is enough.
	code, _ = c.owner.raw("PUT", c.admin("/roster"), "text/csv", []byte("email\nzed@college.edu\n"), o)
	if code != 200 {
		t.Fatalf("roster %d", code)
	}
	if code, out := c.owner.do("PUT", c.admin(fmt.Sprintf("/extensions/%d", zed.userID())), map[string]any{"minutes": 15}, o); code != 200 {
		t.Errorf("extension for a student on the roster: %d %v", code, out)
	}
}

func TestS1CoOrganiserGetsStudentHideRulesInExamMode(t *testing.T) {
	c := newCoOrgSite(t, "s1e", map[string]any{"feedback_mode": "exam"})
	o := c.srv.URL
	c.at(c.start, 5)
	c.asha.do("POST", "/api/contests/s1e/start", map[string]any{"accept_honour": true}, o)
	c.submit(c.asha, "s1e", "A", "correct")

	_, feed := c.owner.do("GET", c.admin("/submissions"), nil, "")
	if item := feed["submissions"].([]any)[0].(map[string]any); item["verdict"] != "AC" {
		t.Fatalf("owner's feed: %v", item)
	}
	code, feed := c.ta.do("GET", c.admin("/submissions"), nil, "")
	item := feed["submissions"].([]any)[0].(map[string]any)
	if code != 200 || item["verdict"] != "" || item["score"] != float64(0) || item["state"] != "submitted" || item["hidden"] != true || feed["results_hidden"] != true {
		t.Errorf("co-organiser's exam-mode feed shows a result: %d %v", code, item)
	}
	if code, out := c.ta.do("GET", c.admin("/submissions?verdict=AC"), nil, ""); code != 403 || errCode(out) != "results_hidden" {
		t.Errorf("co-organiser filtering an exam feed by verdict: %d %v", code, out)
	}
	for _, path := range []string{c.admin("/standings"), "/api/contests/s1e/standings"} {
		if code, out := c.ta.do("GET", path, nil, ""); code != 403 || errCode(out) != "board_closed" {
			t.Errorf("co-organiser reading %s in exam mode: %d %v", path, code, out)
		}
	}
	for _, path := range []string{c.admin("/stats"), c.admin("/export.csv"), c.admin("/export.json")} {
		if code, out := c.ta.do("GET", path, nil, ""); code != 403 || errCode(out) != "results_hidden" {
			t.Errorf("co-organiser reading %s in exam mode: %d %v", path, code, out)
		}
	}
	if code, _ := c.owner.do("GET", c.admin("/stats"), nil, ""); code != 200 {
		t.Errorf("owner's stats: %d", code)
	}
	// The co-organiser's own submission is no oracle either.
	code, sub := c.ta.do("POST", "/api/contests/s1e/problems/A/submit", map[string]any{"language": "cpp17", "source": "int32", "client_key": "ta1"}, o)
	if code != 202 {
		t.Fatalf("co-organiser submit: %d %v", code, sub)
	}
	c.judgeAll()
	_, got := c.ta.do("GET", "/api/submissions/"+strconv.Itoa(int(sub["id"].(float64))), nil, "")
	if got["state"] != "submitted" || got["verdict"] != nil && got["verdict"] != "" {
		t.Errorf("co-organiser sees their own exam-mode verdict: %v", got)
	}
	// Once every student's time is up, they see what students see.
	c.at(c.start, 125)
	_, feed = c.ta.do("GET", c.admin("/submissions"), nil, "")
	for _, it := range feed["submissions"].([]any) {
		if it.(map[string]any)["hidden"] == true {
			t.Errorf("result still hidden after the end: %v", it)
		}
	}
	if code, out := c.ta.do("GET", c.admin("/standings"), nil, ""); code != 200 || out["variant"] != "full" {
		t.Errorf("co-organiser's board after the end: %d %v", code, out)
	}
}

func TestS1CoOrganiserSeesTheFrozenBoard(t *testing.T) {
	c := newCoOrgSite(t, "s1f", map[string]any{"freeze_minutes": 60})
	o := c.srv.URL
	c.at(c.start, 1)
	c.asha.do("POST", "/api/contests/s1f/start", map[string]any{"accept_honour": true}, o)
	c.at(c.start, 70) // inside the freeze
	c.submit(c.asha, "s1f", "A", "correct")
	for _, path := range []string{c.admin("/standings"), c.admin("/standings?variant=full"), "/api/contests/s1f/standings"} {
		code, board := c.ta.do("GET", path, nil, "")
		if code != 200 || board["variant"] != "frozen" {
			t.Fatalf("co-organiser's board %s during the freeze: %d %v", path, code, board)
		}
		if row := board["rows"].([]any)[0].(map[string]any); row["total"] != float64(0) {
			t.Errorf("co-organiser sees a score from the freeze on %s: %v", path, row)
		}
	}
	_, feed := c.ta.do("GET", c.admin("/submissions"), nil, "")
	if item := feed["submissions"].([]any)[0].(map[string]any); item["hidden"] != true {
		t.Errorf("co-organiser's feed shows a verdict from the freeze: %v", item)
	}
	if _, board := c.owner.do("GET", c.admin("/standings"), nil, ""); board["variant"] != "full" {
		t.Errorf("owner's board: %v", board["variant"])
	}
}

func TestS9OrganiserActionsAreAudited(t *testing.T) {
	c := newCoOrgSite(t, "s9", map[string]any{"hold_reveal": true, "freeze_minutes": 10})
	o := c.srv.URL
	ok := func(code int, out map[string]any) {
		t.Helper()
		if code >= 300 {
			t.Fatalf("%d %v", code, out)
		}
	}
	ok(c.owner.do("PUT", c.admin(""), map[string]any{"min_gap_s": 45, "title": "Audited"}, o))
	ok(c.ta.do("PUT", c.admin(""), map[string]any{"description_md": "Read the rules."}, o))
	ok(c.owner.raw("PUT", c.admin("/roster"), "text/csv", []byte("email\nasha@college.edu\n"), o))
	c.at(c.start, 5)
	c.asha.do("POST", "/api/contests/s9/start", map[string]any{"accept_honour": true}, o)
	c.submit(c.asha, "s9", "A", "correct")
	ok(c.owner.do("PUT", c.admin(fmt.Sprintf("/extensions/%d", c.asha.userID())), map[string]any{"minutes": 20, "reason": "laptop crashed"}, o))
	ok(c.owner.do("POST", c.admin("/extend"), map[string]any{"minutes": 5}, o))
	ok(c.owner.do("POST", c.admin("/pause"), nil, o))
	ok(c.owner.do("POST", c.admin("/resume"), nil, o))
	ok(c.owner.do("POST", c.admin("/rejudge"), map[string]any{"problem_label": "A"}, o))
	c.judgeAll()
	ok(c.owner.do("PUT", c.admin("/editorial"), map[string]any{"editorial_md": "Add them.", "published": true}, o))
	c.at(c.start, 200)
	ok(c.owner.do("POST", c.admin("/reveal"), nil, o))

	counts := map[string]int{}
	rows, err := c.db.Query("SELECT action, actor_email FROM contest_audit WHERE contest_id = ?", c.id)
	if err != nil {
		t.Fatal(err)
	}
	var taActions []string
	for rows.Next() {
		var action, email string
		rows.Scan(&action, &email)
		counts[action]++
		if email == "ta@college.edu" {
			taActions = append(taActions, action)
		}
	}
	rows.Close()
	// Creating the contest pinned its problem and published it; the
	// co-organiser was added; then everything above.
	want := map[string]int{"problems": 1, "settings": 3, "organisers": 1, "roster": 1, "extension": 1, "extend": 1,
		"pause": 1, "resume": 1, "rejudge": 1, "editorial": 1, "reveal": 1}
	for action, n := range want {
		if counts[action] != n {
			t.Errorf("%s: %d audit rows, want %d (all: %v)", action, counts[action], n, counts)
		}
	}
	if len(taActions) != 1 || taActions[0] != "settings" {
		t.Errorf("co-organiser's audit rows: %v", taActions)
	}
	var details string
	c.db.QueryRow("SELECT details FROM contest_audit WHERE contest_id = ? AND action = 'settings' AND details LIKE '%min_gap_s%'", c.id).Scan(&details)
	if !strings.Contains(details, `"min_gap_s":{"from":0,"to":45}`) {
		t.Errorf("settings change details: %s", details)
	}
	// Append only: nothing can rewrite or remove a row.
	if _, err := c.db.Exec("UPDATE contest_audit SET action = 'x' WHERE contest_id = ?", c.id); err == nil || !strings.Contains(err.Error(), "append only") {
		t.Errorf("audit row updated: %v", err)
	}
	if _, err := c.db.Exec("DELETE FROM contest_audit WHERE contest_id = ?", c.id); err == nil || !strings.Contains(err.Error(), "append only") {
		t.Errorf("audit row deleted: %v", err)
	}
	// The JSON export carries the log.
	_, export := c.owner.do("GET", c.admin("/export.json"), nil, "")
	if log, _ := export["audit"].([]any); len(log) < 13 {
		t.Errorf("export audit: %d rows", len(log))
	}
}

// testingRequest sends a request to the testing-mode site with extra
// headers and a Host of its own.
func testingRequest(b *browser, method, path, host string, headers map[string]string) (int, map[string]any) {
	b.s.t.Helper()
	req, _ := http.NewRequest(method, b.s.srv.URL+path, strings.NewReader(`{"email":"x@y.z","role":"owner"}`))
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return send(b, req)
}

func TestS2TestingModeRefusesProxiedRequests(t *testing.T) {
	s := newSiteMode(t, "testing")
	b := s.browser()
	o := s.srv.URL
	if code, _ := b.do("POST", "/api/auth/test-signin", map[string]string{"email": "owner@college.edu", "role": "owner"}, o); code != 200 {
		t.Fatalf("local test sign-in: %d", code)
	}
	if code, _ := testingRequest(b, "GET", "/api/me", "", nil); code != 200 {
		t.Fatalf("local request: %d", code)
	}
	cases := []struct {
		name    string
		method  string
		path    string
		host    string
		headers map[string]string
	}{
		{"public Host", "GET", "/api/me", "contest.example.edu", nil},
		{"public Host with port", "GET", "/api/me", "contest.example.edu:443", nil},
		{"X-Forwarded-For", "GET", "/api/me", "", map[string]string{"X-Forwarded-For": "203.0.113.9"}},
		{"Forwarded", "GET", "/api/auth/config", "", map[string]string{"Forwarded": "for=203.0.113.9"}},
		{"X-Real-IP", "GET", "/api/me", "", map[string]string{"X-Real-IP": "203.0.113.9"}},
		{"sign-in via Caddy", "POST", "/api/auth/test-signin", "", map[string]string{"Origin": o, "X-Forwarded-For": "203.0.113.9"}},
		{"sign-in for a public Host", "POST", "/api/auth/test-signin", "contest.example.edu", map[string]string{"Origin": o}},
	}
	for _, tc := range cases {
		if code, out := testingRequest(b, tc.method, tc.path, tc.host, tc.headers); code != 403 || errCode(out) != "testing_mode_local" {
			t.Errorf("%s: %d %v", tc.name, code, out)
		}
	}
	// localhost or [::1] with any port is this machine. (The cookie jar
	// files cookies under the Host, so the session cookie goes by hand.)
	u, _ := url.Parse(o)
	cookie := b.client.Jar.Cookies(u)[0]
	for _, host := range []string{"localhost:5173", "[::1]:8080", "LOCALHOST"} {
		req, _ := http.NewRequest("GET", o+"/api/me", nil)
		req.Host = host
		req.AddCookie(cookie)
		if code, out := send(s.browser(), req); code != 200 {
			t.Errorf("Host %s: %d %v", host, code, out)
		}
	}
	// Test sign-in needs the page's own Origin.
	if code, out := testingRequest(b, "POST", "/api/auth/test-signin", "", nil); code != 403 || errCode(out) != "cross_site" {
		t.Errorf("test sign-in without Origin: %d %v", code, out)
	}
	if code, _ := testingRequest(b, "POST", "/api/auth/test-signin", "", map[string]string{"Origin": "http://localhost:9999"}); code != 403 {
		t.Errorf("test sign-in from another local origin: %d", code)
	}
	// Production behind Caddy is normal.
	p := newSite(t)
	if code, _ := testingRequest(p.browser(), "GET", "/api/auth/config", "contest.example.edu", map[string]string{"X-Forwarded-For": "203.0.113.9"}); code != 200 {
		t.Errorf("production request through a proxy: %d", code)
	}
}

func TestS5SessionCookieAndSignOut(t *testing.T) {
	s := newSiteMode(t, "testing")
	b := s.browser()
	req, _ := http.NewRequest("POST", s.srv.URL+"/api/auth/test-signin", strings.NewReader(`{"email":"asha@college.edu","role":"student"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", s.srv.URL)
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var token string
	for _, raw := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(raw, "podium_session=") {
			if strings.Contains(raw, "Max-Age") || strings.Contains(raw, "Expires") {
				t.Errorf("session cookie outlives the browser: %s", raw)
			}
			token = strings.TrimPrefix(strings.SplitN(raw, ";", 2)[0], "podium_session=")
		}
	}
	if token == "" {
		t.Fatal("no session cookie")
	}
	if code, _ := b.do("POST", "/api/auth/logout", nil, s.srv.URL); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	// The cookie value kept by someone else is dead: sign-out ended the
	// session on the server, not only in this browser.
	again, _ := http.NewRequest("GET", s.srv.URL+"/api/me", nil)
	again.AddCookie(&http.Cookie{Name: "podium_session", Value: token})
	if code, _ := send(s.browser(), again); code != 401 {
		t.Errorf("signed-out session's cookie replayed: %d", code)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM sessions").Scan(&n)
	if n != 0 {
		t.Errorf("%d sessions left after sign-out", n)
	}
}

func TestS5GoogleSignInAsksForAFreshLogin(t *testing.T) {
	s := newSite(t)
	s.signedIn("asha@college.edu", "Asha")
	if q := s.idp.LastAuthQuery; q.Get("prompt") != "select_account" || q.Get("max_age") != "300" {
		t.Errorf("Google sign-in request: prompt %q max_age %q", q.Get("prompt"), q.Get("max_age"))
	}
	// An ID token whose sign-in is an hour old is refused.
	s.idp.Mutate = func(c map[string]any) { c["auth_time"] = time.Now().Add(-time.Hour).Unix() }
	defer func() { s.idp.Mutate = nil }()
	final := s.browser().signIn(authtest.Person{Email: "bilal@college.edu", Verified: true}, "/")
	if final.Path != "/signin" || final.Query().Get("error") != "google" {
		t.Errorf("stale Google sign-in landed on %s", final)
	}
}

// send runs req with b's cookies and decodes the JSON answer.
func send(b *browser, req *http.Request) (int, map[string]any) {
	b.s.t.Helper()
	resp, err := b.client.Do(req)
	if err != nil {
		b.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}
