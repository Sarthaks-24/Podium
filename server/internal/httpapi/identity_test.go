package httpapi

// End-to-end checks for milestone M1 (PLAN.md section 19): sign in with an
// allowed and a disallowed domain, an owner adds and removes an admin, and
// the session cookie carries the right flags. A fake Google provider stands
// in for the real one; every redirect is followed as a browser would.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/integrity"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

type site struct {
	t   *testing.T
	srv *httptest.Server
	idp *authtest.IdP
	// bank and judge are the problem bank and the judge worker API,
	// which runs on its own listener as in production.
	bank  *problems.Service
	judge *httptest.Server
	subs  *submissions.Service
	hub   *events.Hub
	// contests runs contests; db is the database; clock shifts the time
	// seen by contests, submissions and jobs (sign-in uses the real time).
	contests *contests.Service
	db       *sql.DB
	dbPath   string
	clock    *testClock
	// integrity runs integrity checks on the same clock.
	integrity *integrity.Service
	// logs holds the handlers' and the judge API's log lines at
	// logLevel (info unless a test lowers it).
	logs     *syncBuffer
	logLevel *slog.LevelVar
	// ai is the wizard's state; the fake provider exists in testing mode.
	ai *AI
	// authSvc applies the identity rules.
	authSvc *auth.Service
	// restart replaces the handler with one built from fresh services on
	// the same database, as a server restart does: every in-memory state
	// (event hub, rate limits, boards) starts again, the database stays.
	restart func()
}

// syncBuffer is a bytes.Buffer safe for concurrent handlers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testClock is the real time plus an offset tests can move forward.
type testClock struct {
	mu     sync.Mutex
	offset time.Duration
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

// set moves the clock so that now is t.
func (c *testClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = time.Until(t)
}

// judgeToken is the worker token the test site's judge API accepts.
const judgeToken = "test-judge-token-test-judge-token-000000"

func newSite(t *testing.T) *site { return newSiteMode(t, "production") }

// newSiteMode builds the test site in the given run mode.
func newSiteMode(t *testing.T, mode string) *site {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "e2e.db")
	d, err := db.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	svc := &auth.Service{DB: d, OwnerEmail: "owner@college.edu"}
	if err := svc.Bootstrap(ctx, []string{"college.edu"}); err != nil {
		t.Fatal(err)
	}
	idp := authtest.New(t)
	logs, level := &syncBuffer{}, &slog.LevelVar{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: level}))
	blobs := problems.Blobs{Dir: filepath.Join(t.TempDir(), "tests")}
	clock := &testClock{}
	jobSvc := &jobs.Service{DB: d, Now: clock.now}
	jobSvc.Kinds = map[string]jobs.Kind{
		judgeapi.KindValidate:       problems.ValidateKind{},
		judgeapi.KindGenerate:       problems.GenerateKind{Store: blobs, Jobs: jobSvc, Now: clock.now},
		judgeapi.KindGenerateInputs: problems.GenerateInputsKind{Store: blobs, Jobs: jobSvc, Now: clock.now},
		judgeapi.KindSubmit:         submissions.SubmitKind{Now: clock.now},
		judgeapi.KindRun:            submissions.RunKind{Now: clock.now},
	}
	s := &site{t: t, idp: idp, bank: &problems.Service{DB: d, Blobs: blobs, Jobs: jobSvc, Now: clock.now}, hub: &events.Hub{}, db: d, clock: clock,
		dbPath: dbPath, logs: logs, logLevel: level, ai: &AI{Keys: &ai.Keys{}, Slots: &ai.Slots{}}}
	s.subs = &submissions.Service{DB: d, Jobs: jobSvc, Problems: s.bank, Hub: s.hub, Now: clock.now, HashKey: []byte("test-hash-key")}
	s.authSvc = svc
	jobSvc.Discard = func(shas []string) { blobs.DiscardUnrecorded(context.Background(), d, shas) }
	// Boards recompute on the first read after a change, so a test reads
	// what it just did; the rate limit has its own test.
	s.contests = &contests.Service{DB: d, Problems: s.bank, Hub: s.hub, Now: clock.now, BoardEvery: time.Nanosecond}
	s.subs.ContestView = s.contests.ViewFor
	s.integrity = &integrity.Service{DB: d, Problems: s.bank, Contests: s.contests, Now: clock.now}
	inputEvents := &InputGenerationEvents{Problems: s.bank, Hub: s.hub}
	jobSvc.OnChange = func(id int64, kind string) {
		s.subs.OnJobChange(id, kind)
		s.contests.OnJobChange(id, kind)
		inputEvents.OnJobChange(id, kind)
	}
	s.judge = httptest.NewServer(NewJudge(JudgeDeps{Log: log, Jobs: jobSvc, Blobs: blobs, Token: judgeToken}))
	t.Cleanup(s.judge.Close)
	// The server's URL is only known once it starts, so the handler is
	// built on first request.
	var h http.Handler
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(s.srv.Close)
	build := func() http.Handler {
		return New(Deps{
			DB: d, Log: log,
			WebApp:  http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "web app") }),
			Auth:    svc,
			IdP:     auth.NewProvider(idp.Endpoints(), idp.ClientID, idp.ClientSecret, RedirectURL(s.srv.URL)),
			BaseURL: s.srv.URL, SessionSecret: []byte(strings.Repeat("e", 32)),
			Problems: s.bank, Submissions: s.subs, Hub: s.hub, Contests: s.contests, Integrity: s.integrity, Mode: mode,
			AI: s.ai,
		})
	}
	h = build()
	s.restart = func() {
		svc = &auth.Service{DB: d, OwnerEmail: "owner@college.edu"}
		s.hub = &events.Hub{}
		s.subs = &submissions.Service{DB: d, Jobs: jobSvc, Problems: s.bank, Hub: s.hub, Now: clock.now, HashKey: []byte("test-hash-key")}
		s.contests = &contests.Service{DB: d, Problems: s.bank, Hub: s.hub, Now: clock.now, BoardEvery: time.Nanosecond}
		s.subs.ContestView = s.contests.ViewFor
		s.ai = &AI{Keys: &ai.Keys{}, Slots: &ai.Slots{}}
		h = build()
	}
	return s
}

// browser is one person's cookie jar.
type browser struct {
	s      *site
	client *http.Client
}

func (s *site) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{s: s, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// signIn runs the whole flow as person and returns the final URL.
func (b *browser) signIn(person authtest.Person, next string) *url.URL {
	b.s.t.Helper()
	b.s.idp.SignInAs(person)
	resp, err := b.client.Get(b.s.srv.URL + "/api/auth/google/start?next=" + url.QueryEscape(next))
	if err != nil {
		b.s.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.Request.URL
}

func (b *browser) do(method, path string, body any, origin string) (int, map[string]any) {
	b.s.t.Helper()
	var r io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		r = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, b.s.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (b *browser) me() (int, string) {
	code, body := b.do("GET", "/api/me", nil, "")
	role, _ := body["role"].(string)
	return code, role
}

func TestM1SignInAllowedAndDisallowedDomain(t *testing.T) {
	s := newSite(t)

	student := s.browser()
	final := student.signIn(authtest.Person{Email: "asha@college.edu", Verified: true, Name: "Asha"}, "/contests/week-6")
	if final.Path != "/contests/week-6" {
		t.Fatalf("allowed domain landed on %s", final)
	}
	if code, role := student.me(); code != 200 || role != "student" {
		t.Fatalf("allowed domain: /api/me %d %q", code, role)
	}
	if code, _ := student.do("GET", "/api/admin/people", nil, ""); code != 403 {
		t.Errorf("student reading people: %d, want 403", code)
	}

	outsider := s.browser()
	final = outsider.signIn(authtest.Person{Email: "friend@gmail.com", Verified: true}, "/")
	if final.Path != "/signin" || final.Query().Get("error") != "domain" {
		t.Fatalf("disallowed domain landed on %s", final)
	}
	if code, _ := outsider.me(); code != 401 {
		t.Errorf("disallowed domain got a session: /api/me %d", code)
	}

	unverified := s.browser()
	final = unverified.signIn(authtest.Person{Email: "new@college.edu", Verified: false}, "/")
	if final.Query().Get("error") != "unverified" {
		t.Errorf("unverified email landed on %s", final)
	}

	// Open redirect attempts end on the home page.
	final = s.browser().signIn(authtest.Person{Email: "b@college.edu", Verified: true}, "//evil.example/x")
	if final.Host != strings.TrimPrefix(s.srv.URL, "http://") || final.Path != "/" {
		t.Errorf("next=//evil.example landed on %s", final)
	}
}

func TestM1OwnerAddsAndRemovesAdmin(t *testing.T) {
	s := newSite(t)
	owner := s.browser()
	if final := owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/admin/people"); final.Path != "/admin/people" {
		t.Fatalf("owner landed on %s", final)
	}
	origin := s.srv.URL

	code, body := owner.do("POST", "/api/admin/people", map[string]string{"email": "TA@gmail.com", "role": "admin"}, origin)
	if code != 200 || body["email"] != "ta@gmail.com" || body["role"] != "admin" {
		t.Fatalf("add admin: %d %v", code, body)
	}
	taID := int64(body["id"].(float64))

	// The new admin is on a domain that is not allowed; the role admits them.
	ta := s.browser()
	if final := ta.signIn(authtest.Person{Email: "ta@gmail.com", Verified: true, Name: "TA"}, "/"); final.Path != "/" {
		t.Fatalf("admin on another domain landed on %s", final)
	}
	if code, role := ta.me(); code != 200 || role != "admin" {
		t.Fatalf("admin: /api/me %d %q", code, role)
	}

	code, body = owner.do("GET", "/api/admin/people", nil, "")
	people, _ := body["people"].([]any)
	if code != 200 || len(people) != 2 {
		t.Fatalf("people: %d %v", code, body)
	}

	if code, _ := owner.do("DELETE", "/api/admin/people/"+itoa(taID), nil, origin); code != 204 {
		t.Fatalf("remove admin: %d", code)
	}
	if code, _ := ta.me(); code != 401 {
		t.Errorf("removed admin's next request: %d, want 401 (their sessions end)", code)
	}
	// They are on a disallowed domain, so they cannot sign in again.
	if final := s.browser().signIn(authtest.Person{Email: "ta@gmail.com", Verified: true}, "/"); final.Query().Get("error") != "domain" {
		t.Errorf("removed admin signing in again landed on %s", final)
	}

	// The configured owner cannot be removed, and errors are JSON.
	code, body = owner.do("POST", "/api/admin/people", map[string]string{"email": "owner@college.edu", "role": "admin"}, origin)
	if code != 409 || body["error"] == nil {
		t.Errorf("changing the configured owner: %d %v", code, body)
	}

	// Domains.
	if code, _ := owner.do("POST", "/api/admin/domains", map[string]string{"domain": "@cs.college.edu"}, origin); code != 201 {
		t.Errorf("add domain: %d", code)
	}
	if code, _ := owner.do("DELETE", "/api/admin/domains/cs.college.edu", nil, origin); code != 204 {
		t.Errorf("remove domain: %d", code)
	}
	if code, _ := owner.do("POST", "/api/admin/domains", map[string]string{"domain": "x", "extra": "y"}, origin); code != 400 {
		t.Errorf("unknown field: %d, want 400", code)
	}

	// A text/plain body, which a cross-site form could send, is refused.
	req, _ := http.NewRequest("POST", s.srv.URL+"/api/admin/domains", strings.NewReader(`{"domain":"evil.edu"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", origin)
	if resp, err := owner.client.Do(req); err != nil || resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain body: %v %v, want 415", resp.StatusCode, err)
	}

	// Cross-site writes are refused even with a valid session.
	if code, body := owner.do("POST", "/api/admin/domains", map[string]string{"domain": "evil.edu"}, "https://evil.example"); code != 403 {
		t.Errorf("cross-site POST: %d %v", code, body)
	}

	if code, _ := owner.do("POST", "/api/auth/logout", nil, origin); code != 204 {
		t.Errorf("logout: %d", code)
	}
	if code, _ := owner.me(); code != 401 {
		t.Errorf("after logout /api/me: %d", code)
	}
}

func TestM1CallbackWithoutStateIsExpired(t *testing.T) {
	s := newSite(t)
	b := s.browser()
	resp, err := b.client.Get(s.srv.URL + "/api/auth/google/callback?code=x&state=y")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/signin" || resp.Request.URL.Query().Get("error") != "expired" {
		t.Errorf("landed on %s", resp.Request.URL)
	}
}

func TestM1AuthConfig(t *testing.T) {
	s := newSite(t)
	code, body := s.browser().do("GET", "/api/auth/config", nil, "")
	if code != 200 || body["contact_email"] != "owner@college.edu" || len(body["allowed_domains"].([]any)) != 1 {
		t.Errorf("config: %d %v", code, body)
	}
}

// TestM1SessionCookieFlags drives the flow by hand against an https base
// URL and inspects the Set-Cookie headers.
func TestM1SessionCookieFlags(t *testing.T) {
	ctx := context.Background()
	d, _ := db.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	defer d.Close()
	db.Migrate(ctx, d)
	svc := &auth.Service{DB: d, OwnerEmail: "owner@college.edu"}
	svc.Bootstrap(ctx, []string{"college.edu"})
	idp := authtest.New(t)
	base := "https://contest.example.edu"
	h := New(Deps{
		DB: d, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), WebApp: http.NotFoundHandler(),
		Auth: svc, IdP: auth.NewProvider(idp.Endpoints(), idp.ClientID, idp.ClientSecret, RedirectURL(base)),
		BaseURL: base, SessionSecret: []byte(strings.Repeat("e", 32)),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", base+"/api/auth/google/start?next=/", nil))
	flow := cookieNamed(rec.Result().Cookies(), "__Host-podium_signin")
	if flow == nil || !flow.HttpOnly || !flow.Secure || flow.Path != "/" || flow.Domain != "" || flow.SameSite != http.SameSiteLaxMode {
		t.Fatalf("sign-in state cookie: %+v", flow)
	}

	idp.SignInAs(authtest.Person{Email: "a@college.edu", Verified: true})
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	if back.Host != "contest.example.edu" || back.Path != "/api/auth/google/callback" {
		t.Fatalf("provider sent the browser to %s", back)
	}

	req := httptest.NewRequest("GET", back.String(), nil)
	req.AddCookie(flow)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback: %d -> %s", rec.Code, rec.Header().Get("Location"))
	}
	sc := cookieNamed(rec.Result().Cookies(), "__Host-podium_session")
	if sc == nil {
		t.Fatalf("no __Host-podium_session cookie in %v", rec.Header().Values("Set-Cookie"))
	}
	if !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode || sc.Path != "/" || sc.Domain != "" {
		t.Errorf("session cookie flags: %+v", sc)
	}
	// A browser-session cookie: closing the browser on a shared lab PC
	// forgets it (the server ends the session 12 hours after sign-in).
	for _, raw := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(raw, "__Host-podium_session=") && (strings.Contains(raw, "Max-Age") || strings.Contains(raw, "Expires")) {
			t.Errorf("session cookie has a lifetime, want a browser-session cookie: %s", raw)
		}
	}
	if len(sc.Value) < 40 {
		t.Errorf("session token looks short: %d chars", len(sc.Value))
	}
	if cleared := cookieNamed(rec.Result().Cookies(), "__Host-podium_signin"); cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("sign-in state cookie not cleared: %+v", cleared)
	}
	// The same state cannot be replayed: the flow cookie was single-use in
	// the browser, and a fresh request without it is refused.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", back.String(), nil))
	if loc := rec.Header().Get("Location"); loc != "/signin?error=expired" {
		t.Errorf("replayed callback went to %s", loc)
	}
}

func cookieNamed(cs []*http.Cookie, name string) *http.Cookie {
	var found *http.Cookie
	for _, c := range cs {
		if c.Name == name {
			found = c
		}
	}
	return found
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
