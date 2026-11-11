package httpapi

// Keep me signed in over HTTP (v2.1.0, PF-14): the sign-in choice and its
// signed state, the cookie, renewal, the owner's setting, the account menu
// switch and the provider keys of every session the server ends. The rules
// themselves are tested in auth/keep_test.go with a fake clock.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/db"
)

const thirtyDays = 30 * 24 * 60 * 60

func keepPolicy(students, organisers, admins, owner auth.KeepMode) map[string]any {
	return map[string]any{"students": students, "organisers": organisers, "admins": admins, "owner": owner, "days": 30, "until_sign_out": false}
}

func (s *site) setKeep(p map[string]any) {
	s.t.Helper()
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, body := owner.do("PUT", "/api/admin/keep-signed-in", p, s.srv.URL); code != 200 {
		s.t.Fatalf("save the setting: %d %v", code, body)
	}
}

// signInRaw runs the sign-in flow by hand, with startQuery added to the
// start URL, and returns the callback's response without following it.
// tamper may change the sign-in state cookie on the way.
func (s *site) signInRaw(email, startQuery string, tamper func(*http.Cookie)) *http.Response {
	s.t.Helper()
	if strings.HasSuffix(email, "@college.edu") {
		s.db.Exec("INSERT OR IGNORE INTO users (email, created_at) VALUES (lower(?), 0)", email)
	}
	s.idp.SignInAs(authtest.Person{Email: email, Verified: true, Name: "N"})
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start, err := noFollow.Get(s.srv.URL + "/api/auth/google/start?next=/" + startQuery)
	if err != nil {
		s.t.Fatal(err)
	}
	start.Body.Close()
	flow := cookieNamed(start.Cookies(), "podium_signin")
	if flow == nil {
		s.t.Fatalf("no sign-in state cookie: %v", start.Header.Values("Set-Cookie"))
	}
	if tamper != nil {
		tamper(flow)
	}
	idp, err := noFollow.Get(start.Header.Get("Location"))
	if err != nil {
		s.t.Fatal(err)
	}
	idp.Body.Close()
	req, _ := http.NewRequest("GET", idp.Header.Get("Location"), nil)
	req.AddCookie(flow)
	resp, err := noFollow.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func sessionOf(resp *http.Response) *http.Cookie {
	return cookieNamed(resp.Cookies(), "podium_session")
}

func (s *site) callAs(c *http.Cookie, method, path string, body any) (*http.Response, map[string]any) {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		r = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, s.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", s.srv.URL)
	if c != nil {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestKeepSignInCookieFollowsTheChoiceAndTheSetting(t *testing.T) {
	s := newSite(t)
	s.setKeep(keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))

	kept := sessionOf(s.signInRaw("asha@college.edu", "&remember=1", nil))
	if kept == nil || kept.MaxAge != thirtyDays || !kept.HttpOnly || kept.SameSite != http.SameSiteLaxMode || kept.Path != "/" || kept.Domain != "" {
		t.Errorf("ticked sign-in cookie: %+v, want Max-Age %d, HttpOnly, SameSite=Lax, Path=/", kept, thirtyDays)
	}
	for name, q := range map[string]string{"no tick": "", "an old client": "&remember=0", "a bad value": "&remember=yes"} {
		resp := s.signInRaw("asha@college.edu", q, nil)
		if c := sessionOf(resp); c == nil || c.MaxAge != 0 || !c.Expires.IsZero() {
			t.Errorf("%s: cookie %+v, want a browser-session cookie", name, c)
		}
		if cookieNamed(resp.Cookies(), "podium_notice") != nil {
			t.Errorf("%s: the person was told a tick was not used", name)
		}
	}
	// The same person, now under "Nobody": the tick is ignored, once.
	s.setKeep(keepPolicy(auth.KeepNobody, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
	resp := s.signInRaw("asha@college.edu", "&remember=1", nil)
	if c := sessionOf(resp); c == nil || c.MaxAge != 0 {
		t.Errorf("tick under Nobody: cookie %+v, want an ordinary one", c)
	}
	n := cookieNamed(resp.Cookies(), "podium_notice")
	if n == nil || n.Value != "keep_ignored" || n.MaxAge <= 0 || n.HttpOnly || n.Path != "/" {
		t.Errorf("notice cookie %+v, want keep_ignored, short-lived, readable by the page", n)
	}
}

func TestKeepTamperedStateCannotSetTheChoice(t *testing.T) {
	s := newSite(t)
	s.setKeep(keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
	// No tick was sent; the cookie is changed to say there was one.
	resp := s.signInRaw("asha@college.edu", "", func(c *http.Cookie) {
		payload, sig, _ := strings.Cut(c.Value, ".")
		body, _ := base64.RawURLEncoding.DecodeString(payload)
		var m map[string]any
		json.Unmarshal(body, &m)
		m["r"] = true
		body, _ = json.Marshal(m)
		c.Value = base64.RawURLEncoding.EncodeToString(body) + "." + sig
	})
	if loc := resp.Header.Get("Location"); loc != "/signin?error=expired" || sessionOf(resp) != nil {
		t.Errorf("tampered state: went to %q with session %v, want the expired page and no session", loc, sessionOf(resp))
	}
	// The tick travels inside the signed value: it is not a query parameter
	// of the callback.
	resp = s.signInRaw("asha@college.edu", "", nil)
	if c := sessionOf(resp); c == nil || c.MaxAge != 0 {
		t.Errorf("an untouched, unticked sign-in: %+v", c)
	}
}

func TestKeepTestingModeSignInTakesTheSameTick(t *testing.T) {
	s := newSiteMode(t, "testing")
	if _, err := s.authSvc.SetKeepPolicy(auth.WithActor(context.Background(), auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner}),
		auth.KeepPolicy{Students: auth.KeepChoose, Organisers: auth.KeepNobody, Admins: auth.KeepNobody, Owner: auth.KeepNobody, Days: 30}); err != nil {
		t.Fatal(err)
	}
	post := func(body map[string]any) *http.Response {
		buf, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", s.srv.URL+"/api/auth/test-signin", bytes.NewReader(buf))
		req.Header.Set("Origin", s.srv.URL)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	// The whole header, as on the Google path (http base URL: no Secure, no __Host- prefix).
	keptHeader := regexp.MustCompile(`^podium_session=[A-Za-z0-9_-]{43}; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax$`)
	plainHeader := regexp.MustCompile(`^podium_session=[A-Za-z0-9_-]{43}; Path=/; HttpOnly; SameSite=Lax$`)
	raw := func(resp *http.Response) string {
		for _, v := range resp.Header.Values("Set-Cookie") {
			if strings.HasPrefix(v, "podium_session=") {
				return v
			}
		}
		return ""
	}
	if h := raw(post(map[string]any{"email": "asha@college.edu", "role": "student", "remember": true})); !keptHeader.MatchString(h) {
		t.Errorf("ticked: %q, want Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax", h)
	}
	if h := raw(post(map[string]any{"email": "asha@college.edu", "role": "student"})); !plainHeader.MatchString(h) {
		t.Errorf("no tick (an old client): %q, want a browser-session cookie with HttpOnly; SameSite=Lax", h)
	}
	resp := post(map[string]any{"email": "ta@college.edu", "role": "organiser", "remember": true})
	if c := sessionOf(resp); c == nil || c.MaxAge != 0 || cookieNamed(resp.Cookies(), "podium_notice") == nil {
		t.Errorf("tick for a role set to Nobody: %+v, want an ordinary cookie and the notice", c)
	}
}

// The kept cookie on https carries every flag the ordinary one does, and a
// Max-Age.
func TestKeepCookieFlagsOnHTTPS(t *testing.T) {
	ctx := context.Background()
	d, _ := db.Open(ctx, filepath.Join(t.TempDir(), "k.db"))
	defer d.Close()
	db.Migrate(ctx, d)
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	svc := &auth.Service{DB: d, OwnerEmail: "owner@college.edu", Now: func() time.Time { return now }}
	svc.Bootstrap(ctx)
	if _, err := svc.SetKeepPolicy(auth.WithActor(ctx, auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner}),
		auth.KeepPolicy{Students: auth.KeepNobody, Organisers: auth.KeepNobody, Admins: auth.KeepNobody, Owner: auth.KeepChoose, Days: 30}); err != nil {
		t.Fatal(err)
	}
	idp := authtest.New(t)
	base := "https://contest.example.edu"
	h := New(Deps{
		DB: d, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), WebApp: http.NotFoundHandler(),
		Auth: svc, IdP: auth.NewProvider(idp.Endpoints(), idp.ClientID, idp.ClientSecret, RedirectURL(base)),
		BaseURL: base, SessionSecret: []byte(strings.Repeat("k", 32)),
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", base+"/api/auth/google/start?next=/&remember=1", nil))
	flow := cookieNamed(rec.Result().Cookies(), "__Host-podium_signin")
	idp.SignInAs(authtest.Person{Email: "owner@college.edu", Verified: true})
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back, _ := url.Parse(resp.Header.Get("Location"))
	req := httptest.NewRequest("GET", back.String(), nil)
	req.AddCookie(flow)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var raw string
	for _, v := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(v, "__Host-podium_session=") {
			raw = v
		}
	}
	want := regexp.MustCompile(`^__Host-podium_session=[A-Za-z0-9_-]{43}; Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax$`)
	if !want.MatchString(raw) {
		t.Errorf("kept session cookie is %q, want a value then: Path=/; Max-Age=2592000; HttpOnly; Secure; SameSite=Lax", raw)
	}
}

func TestKeepAUseAnHourLaterSendsTheCookieAgainWithItsMaxAge(t *testing.T) {
	s := newSite(t)
	fake := time.Now()
	s.authSvc.Now = func() time.Time { return fake }
	s.setKeep(keepPolicy(auth.KeepEveryone, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
	c := sessionOf(s.signInRaw("asha@college.edu", "", nil))
	if c == nil || c.MaxAge != thirtyDays {
		t.Fatalf("sign-in cookie %+v", c)
	}
	fake = fake.Add(30 * time.Minute)
	if resp, _ := s.callAs(c, "GET", "/api/me", nil); resp.StatusCode != 200 || sessionOf(resp) != nil {
		t.Errorf("a use inside the hour: %d, cookie %v; want no new cookie", resp.StatusCode, sessionOf(resp))
	}
	fake = fake.Add(2 * time.Hour)
	resp, _ := s.callAs(c, "GET", "/api/me", nil)
	if r := sessionOf(resp); resp.StatusCode != 200 || r == nil || r.MaxAge != thirtyDays || r.Value != c.Value || !r.HttpOnly || r.SameSite != http.SameSiteLaxMode {
		t.Errorf("a use two hours later: %d, cookie %+v; want the same token with Max-Age %d", resp.StatusCode, r, thirtyDays)
	}
}

func TestKeepSettingRouteAllowsTheOwnerOnly(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	admin := s.testAs("adm@college.edu", auth.Admin)
	org := s.testAs("org@college.edu", auth.Organiser)
	student := s.signedIn("asha@college.edu", "Asha")
	body := keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody)

	for name, b := range map[string]*browser{"admin": admin, "organiser": org, "student": student} {
		if code, _ := b.do("GET", "/api/admin/keep-signed-in", nil, ""); code != 403 {
			t.Errorf("%s GET: %d, want 403", name, code)
		}
		if code, _ := b.do("PUT", "/api/admin/keep-signed-in", body, s.srv.URL); code != 403 {
			t.Errorf("%s PUT: %d, want 403", name, code)
		}
	}
	if code, _ := s.browser().do("PUT", "/api/admin/keep-signed-in", body, s.srv.URL); code != 401 {
		t.Errorf("signed out PUT: %d, want 401", code)
	}

	code, out := owner.do("GET", "/api/admin/keep-signed-in", nil, "")
	pol, _ := out["policy"].(map[string]any)
	if code != 200 || pol["students"] != "nobody" || pol["days"] != float64(30) || pol["until_sign_out"] != false || out["you_remembered"] != false {
		t.Errorf("owner GET: %d %v, want the defaults", code, out)
	}
	code, out = owner.do("PUT", "/api/admin/keep-signed-in", body, s.srv.URL)
	pol, _ = out["policy"].(map[string]any)
	if code != 200 || pol["students"] != "choose" || out["signed_out"] != float64(0) {
		t.Errorf("owner PUT: %d %v", code, out)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM site_audit WHERE action = 'keep_signed_in_changed'").Scan(&n)
	if n != 1 {
		t.Errorf("%d audit rows after one save, want 1", n)
	}
	// The audit tab shows it.
	if _, hist := owner.do("GET", "/api/admin/site-audit?action=keep_signed_in_changed", nil, ""); len(hist["entries"].([]any)) != 1 {
		t.Errorf("site audit: %v", hist)
	}
}

func TestKeepSettingRouteRefusesBadInput(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	bad := func(mod func(map[string]any), field, copy string) {
		t.Helper()
		p := keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody)
		mod(p)
		code, out := owner.do("PUT", "/api/admin/keep-signed-in", p, s.srv.URL)
		e, _ := out["error"].(map[string]any)
		fields, _ := e["fields"].(map[string]any)
		if code != 400 || fields[field] != copy {
			t.Errorf("%s: %d %v, want 400 and %q under %s", field, code, out, copy, field)
		}
	}
	bad(func(p map[string]any) { p["days"] = 0 }, "days", "Enter a whole number of days from 1 to 365.")
	bad(func(p map[string]any) { p["days"] = 366 }, "days", "Enter a whole number of days from 1 to 365.")
	bad(func(p map[string]any) { p["admins"] = "always" }, "admins", "Choose Nobody, Each person chooses or Everyone.")
	// A request that leaves a field out is refused, not read as zero.
	code, out := owner.do("PUT", "/api/admin/keep-signed-in", map[string]any{"students": "choose"}, s.srv.URL)
	if e, _ := out["error"].(map[string]any); code != 400 || e["message"] != "Send students, organisers, admins, owner, days and until_sign_out." {
		t.Errorf("missing fields: %d %v", code, out)
	}
	if code, _ := owner.do("PUT", "/api/admin/keep-signed-in", map[string]any{"students": 5}, s.srv.URL); code != 400 {
		t.Errorf("wrong type: %d, want 400", code)
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM site_audit WHERE action = 'keep_signed_in_changed'").Scan(&n)
	if n != 0 {
		t.Errorf("refused saves wrote %d audit rows", n)
	}
}

// testAs signs in through testing mode is not available on this site, so a
// person of the role is made on the list and signs in with Google.
func (s *site) testAs(email string, role auth.Role) *browser {
	s.t.Helper()
	if _, err := s.authSvc.GrantRole(auth.WithActor(context.Background(), auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner}), email, role); err != nil {
		s.t.Fatal(err)
	}
	return s.signedIn(email, "N")
}

func TestKeepSwitchToNobodyOverHTTPCountsAndEndsRememberedSessions(t *testing.T) {
	s := newSite(t)
	s.setKeep(keepPolicy(auth.KeepEveryone, auth.KeepNobody, auth.KeepNobody, auth.KeepEveryone))
	a := sessionOf(s.signInRaw("asha@college.edu", "", nil))
	b := sessionOf(s.signInRaw("bo@college.edu", "", nil))
	ownerCookie := sessionOf(s.signInRaw("owner@college.edu", "", nil))
	// Keys held for the sessions that are about to end.
	s.ai.Keys.Put(a.Value, ai.KeyEntry{Provider: "anthropic", Key: ai.NewSecret(fakeAIKey)})

	resp, out := s.callAs(ownerCookie, "GET", "/api/admin/keep-signed-in", nil)
	counts, _ := out["remembered"].(map[string]any)
	if resp.StatusCode != 200 || counts["students"] != float64(2) || counts["owner"] != float64(1) || out["you_remembered"] != true {
		t.Fatalf("counts: %d %v, want two students, the owner, and the owner's own session remembered", resp.StatusCode, out)
	}
	resp, out = s.callAs(ownerCookie, "PUT", "/api/admin/keep-signed-in", keepPolicy(auth.KeepNobody, auth.KeepNobody, auth.KeepNobody, auth.KeepEveryone))
	if resp.StatusCode != 200 || out["signed_out"] != float64(2) {
		t.Fatalf("switch: %d %v, want 2 signed out", resp.StatusCode, out)
	}
	for name, c := range map[string]*http.Cookie{"asha": a, "bo": b} {
		if r, _ := s.callAs(c, "GET", "/api/me", nil); r.StatusCode != 401 {
			t.Errorf("%s after the switch: %d, want 401", name, r.StatusCode)
		}
	}
	if r, _ := s.callAs(ownerCookie, "GET", "/api/me", nil); r.StatusCode != 200 {
		t.Errorf("the owner, still on Everyone: %d", r.StatusCode)
	}
	if _, ok := s.ai.Keys.Get(a.Value); ok {
		t.Error("a provider key outlived the bulk switch")
	}
}

func TestKeepMeAndTheAccountMenuSwitch(t *testing.T) {
	s := newSite(t)
	s.setKeep(keepPolicy(auth.KeepNobody, auth.KeepNobody, auth.KeepChoose, auth.KeepNobody))
	s.testAs("adm@college.edu", auth.Admin)
	c := sessionOf(s.signInRaw("adm@college.edu", "", nil))

	_, me := s.callAs(c, "GET", "/api/me", nil)
	if me["keep_mode"] != "choose" || me["remembered"] != false || me["role"] != "admin" {
		t.Fatalf("/api/me: %v", me)
	}
	resp, out := s.callAs(c, "PUT", "/api/me/keep-signed-in", map[string]any{"keep": true})
	if r := sessionOf(resp); resp.StatusCode != 200 || out["remembered"] != true || r == nil || r.MaxAge != thirtyDays || r.Value != c.Value {
		t.Fatalf("turn on: %d %v cookie %+v", resp.StatusCode, out, r)
	}
	if _, me = s.callAs(c, "GET", "/api/me", nil); me["remembered"] != true {
		t.Errorf("/api/me after turning on: %v", me)
	}
	resp, out = s.callAs(c, "PUT", "/api/me/keep-signed-in", map[string]any{"keep": false})
	if r := sessionOf(resp); resp.StatusCode != 200 || out["remembered"] != false || r == nil || r.MaxAge != 0 || !r.Expires.IsZero() {
		t.Errorf("turn off: %d %v cookie %+v, want a browser-session cookie", resp.StatusCode, out, r)
	}
	// Bad input and the other settings.
	if r, _ := s.callAs(c, "PUT", "/api/me/keep-signed-in", map[string]any{}); r.StatusCode != 400 {
		t.Errorf("no keep field: %d, want 400", r.StatusCode)
	}
	if r, _ := s.callAs(nil, "PUT", "/api/me/keep-signed-in", map[string]any{"keep": true}); r.StatusCode != 401 {
		t.Errorf("signed out: %d, want 401", r.StatusCode)
	}
	s.setKeep(keepPolicy(auth.KeepNobody, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
	c = sessionOf(s.signInRaw("adm@college.edu", "", nil))
	resp, out = s.callAs(c, "PUT", "/api/me/keep-signed-in", map[string]any{"keep": true})
	e, _ := out["error"].(map[string]any)
	if resp.StatusCode != 409 || e["message"] != "Podium does not let you choose whether to stay signed in." {
		t.Errorf("under Nobody: %d %v, want 409 and the copy", resp.StatusCode, out)
	}
	// An organiser or student under "Each person chooses" may use it too.
	s.setKeep(keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
	st := sessionOf(s.signInRaw("asha@college.edu", "", nil))
	if resp, out = s.callAs(st, "PUT", "/api/me/keep-signed-in", map[string]any{"keep": true}); resp.StatusCode != 200 {
		t.Errorf("student under choose: %d %v", resp.StatusCode, out)
	}
}

func TestKeepAuthConfigOffersTheTickAndServesTheSessionLength(t *testing.T) {
	s := newSite(t)
	cfg := func() map[string]any {
		_, body := s.browser().do("GET", "/api/auth/config", nil, "")
		return body
	}
	sessions := func(body map[string]any) map[string]any {
		for _, r := range body["retention"].([]any) {
			if r.(map[string]any)["id"] == "sessions" {
				return r.(map[string]any)
			}
		}
		t.Fatal("no sessions row")
		return nil
	}
	body := cfg()
	keep, _ := body["keep_signed_in"].(map[string]any)
	if keep["offer_tick"] != false || sessions(body)["kept"] != "12 hours after sign-in" {
		t.Fatalf("defaults: %v / %v", keep, sessions(body))
	}
	// Only students on "Each person chooses" put the box on the page.
	s.setKeep(keepPolicy(auth.KeepNobody, auth.KeepChoose, auth.KeepEveryone, auth.KeepChoose))
	if keep, _ = cfg()["keep_signed_in"].(map[string]any); keep["offer_tick"] != false {
		t.Errorf("tick offered when only other roles choose: %v", keep)
	}
	s.setKeep(keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
	body = cfg()
	keep, _ = body["keep_signed_in"].(map[string]any)
	if keep["offer_tick"] != true || keep["days"] != float64(30) || keep["until_sign_out"] != false {
		t.Errorf("students choose: %v", keep)
	}
	if got, want := sessions(body)["kept"], "12 hours after sign-in, or 30 days after last use for people who stay signed in"; got != want {
		t.Errorf("privacy row %q, want %q", got, want)
	}
	p := keepPolicy(auth.KeepChoose, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody)
	p["until_sign_out"] = true
	s.setKeep(p)
	body = cfg()
	keep, _ = body["keep_signed_in"].(map[string]any)
	if keep["until_sign_out"] != true || !strings.Contains(sessions(body)["kept"].(string), "until they sign out") {
		t.Errorf("until sign out: %v / %v", keep, sessions(body)["kept"])
	}
}

func TestKeepADemotedAdminIsRefusedOnTheNextRequest(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	admin := s.testAs("adm@college.edu", auth.Admin)
	if code, _ := admin.do("GET", "/api/admin/people", nil, ""); code != 200 {
		t.Fatalf("admin reading people: %d", code)
	}
	if code, _ := owner.do("POST", "/api/admin/people", map[string]string{"email": "adm@college.edu", "role": "organiser"}, s.srv.URL); code != 200 {
		t.Fatal("demote")
	}
	if code, _ := admin.do("GET", "/api/admin/people", nil, ""); code != 403 {
		t.Errorf("a demoted admin's next request: %d, want 403", code)
	}
}

// Every way the server ends a session forgets its provider keys, through
// the hook New wires into the identity service.
func TestKeepEveryServerSideEndForgetsTheKeysOverHTTP(t *testing.T) {
	ctx := auth.WithActor(context.Background(), auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner})
	ends := map[string]func(s *site){
		"sign-out":     func(s *site) {},
		"end sessions": func(s *site) { s.authSvc.EndSessions(ctx, "asha@college.edu") },
		"disable":      func(s *site) { s.authSvc.SetDisabled(ctx, "asha@college.edu", true, 1) },
		"reset google": func(s *site) { s.authSvc.ResetGoogleAccount(ctx, "asha@college.edu") },
		"remove":       func(s *site) { s.authSvc.Remove(ctx, "asha@college.edu") },
		"role change":  func(s *site) { s.authSvc.GrantRole(ctx, "asha@college.edu", auth.Organiser) },
		"cleanup": func(s *site) {
			s.authSvc.Now = func() time.Time { return time.Now().Add(400 * 24 * time.Hour) }
			s.authSvc.DeleteExpiredSessions(ctx)
		},
		"switch to nobody": func(s *site) {
			s.setKeep(keepPolicy(auth.KeepNobody, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
		},
	}
	for name, end := range ends {
		s := newSite(t)
		s.setKeep(keepPolicy(auth.KeepEveryone, auth.KeepNobody, auth.KeepNobody, auth.KeepNobody))
		c := sessionOf(s.signInRaw("asha@college.edu", "", nil))
		s.ai.Keys.Put(c.Value, ai.KeyEntry{Provider: "anthropic", Key: ai.NewSecret(fakeAIKey)})
		if name == "sign-out" {
			s.callAs(c, "POST", "/api/auth/logout", nil)
		} else {
			end(s)
		}
		if _, ok := s.ai.Keys.Get(c.Value); ok {
			t.Errorf("%s: the provider key outlived the session", name)
		}
		var n int
		s.db.QueryRow("SELECT count(*) FROM sessions WHERE user_id = (SELECT id FROM users WHERE email = 'asha@college.edu')").Scan(&n)
		if name != "role change" && n != 0 {
			t.Errorf("%s: %d sessions left", name, n)
		}
	}
}

func TestKeepGoldenOrdinarySessionIsUnchangedByTheDefaultSetting(t *testing.T) {
	s := newSite(t)
	resp := s.signInRaw("asha@college.edu", "&remember=1", nil)
	c := sessionOf(resp)
	if c == nil || c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Fatalf("default setting, ticked box: %+v", c)
	}
	var created, expires int64
	s.db.QueryRow("SELECT created_at, expires_at FROM sessions").Scan(&created, &expires)
	if expires-created != int64(12*time.Hour/time.Millisecond) {
		t.Errorf("ordinary session lasts %d ms, want 12 hours", expires-created)
	}
}
