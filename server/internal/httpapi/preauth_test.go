package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/db"
)

func TestForgedCookiesAreRateLimited(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	svc := &auth.Service{DB: d, OwnerEmail: "owner@college.edu"}
	h := New(Deps{DB: d, Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), WebApp: http.NotFoundHandler(), Auth: svc,
		BaseURL: "https://podium.example", authFailLimit: newIPLimiter(20, 0.01, nil)})
	limited := 0
	for i := 0; i < 100; i++ {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.RemoteAddr = "203.0.113.5:4000"
		r.AddCookie(&http.Cookie{Name: "__Host-podium_session", Value: "forged" + strconv.Itoa(i)})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 80 {
		t.Errorf("%d of 100 forged cookies got 429, want 80", limited)
	}
}

// A real session from an address that spent its forged-cookie budget is
// still served: the address is a campus NAT, and refusing it would let one
// script lock every student out (V3-19, security audit 2026-10-28 H1).
func TestValidSessionSurvivesAnExhaustedAddress(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "v.db"))
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
	h := New(Deps{DB: d, Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), WebApp: http.NotFoundHandler(), Auth: svc,
		BaseURL: "https://podium.example", authFailLimit: newIPLimiter(20, 0.01, nil)})
	const campus = "203.0.113.5:4000"
	call := func(cookie string) int {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.RemoteAddr = campus
		r.AddCookie(&http.Cookie{Name: "__Host-podium_session", Value: cookie})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	_, token, _, err := svc.TestSignIn(ctx, "asha@college.edu", "Asha", "student", "203.0.113.5", "test")
	if err != nil {
		t.Fatal(err)
	}
	// Asha is working before the flood starts.
	if code := call(token); code != http.StatusOK {
		t.Fatalf("Asha before the flood: %d", code)
	}
	for i := 0; i < 100; i++ {
		call("forged" + strconv.Itoa(i))
	}
	if code := call("forged-after"); code != http.StatusTooManyRequests {
		t.Fatalf("the address is not exhausted: forged cookie got %d", code)
	}
	if code := call(token); code != http.StatusOK {
		t.Errorf("Asha's real session from the exhausted address: %d, want 200", code)
	}
	// Still exhausted for strangers after her request went through.
	if code := call("forged-again"); code != http.StatusTooManyRequests {
		t.Errorf("forged cookie after a real one: %d, want 429", code)
	}
}

// The cache forgets a session a minute after it was last seen valid, and at
// once when the database says it ended.
func TestKnownSessionsExpire(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	k := newKnownSessions(func() time.Time { return now })
	k.add("a")
	k.add("b")
	now = now.Add(knownSessionTTL)
	if !k.has("a") {
		t.Error("forgot a session inside its minute")
	}
	k.forget("b")
	if k.has("b") {
		t.Error("kept an ended session")
	}
	now = now.Add(time.Second)
	if k.has("a") {
		t.Error("kept a session past its minute")
	}
	var none *knownSessions
	none.add("x")
	if none.has("x") {
		t.Error("a nil cache knows a session")
	}
}

func TestP3PreAuthLimitPerAddress(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	svc := &auth.Service{DB: d, OwnerEmail: "owner@college.edu"}
	svc.Bootstrap(ctx, []string{"college.edu"})
	h := New(Deps{DB: d, Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), WebApp: http.NotFoundHandler(), Auth: svc,
		BaseURL: "https://podium.example", preAuthLimit: newIPLimiter(3, 0.01, nil), signInLimiter: newIPLimiter(0, 0.01, nil)})
	call := func(path, peer, xff string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = peer
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// The budget is shared by every pre-auth route of one address.
	for i, p := range []string{"/api/auth/config", "/healthz", "/api/auth/config"} {
		if w := call(p, "203.0.113.5:4000", ""); w.Code != 200 {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
	w := call("/api/auth/config", "203.0.113.5:4001", "")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !bytes.Contains(w.Body.Bytes(), []byte(`"rate_limited"`)) {
		t.Fatalf("fourth: %d %s %v", w.Code, w.Body, w.Header())
	}
	if w := call("/api/auth/config", "203.0.113.6:4000", ""); w.Code != 200 {
		t.Errorf("another address: %d", w.Code)
	}
	// Behind Caddy the forwarded address counts, not the loopback peer.
	for i := 0; i < 3; i++ {
		call("/api/auth/config", "127.0.0.1:5000", "198.51.100.7")
	}
	if w := call("/api/auth/config", "127.0.0.1:5000", "198.51.100.7"); w.Code != 429 {
		t.Errorf("forwarded address past its budget: %d", w.Code)
	}
	// A request from the VM itself is never counted.
	for i := 0; i < 5; i++ {
		if w := call("/healthz", "127.0.0.1:5000", ""); w.Code != 200 {
			t.Fatalf("loopback %d: %d", i, w.Code)
		}
	}
	// IPv6 addresses share a budget per /64.
	for i := 0; i < 3; i++ {
		call("/api/auth/config", "[2001:db8:1:2::10]:4000", "")
	}
	if w := call("/api/auth/config", "[2001:db8:1:2::99]:4000", ""); w.Code != 429 {
		t.Errorf("same /64: %d", w.Code)
	}
	if w := call("/api/auth/config", "[2001:db8:1:3::10]:4000", ""); w.Code != 200 {
		t.Errorf("another /64: %d", w.Code)
	}
	// A refused sign-in is a browser navigation: it lands on /signin.
	for _, p := range []string{"/api/auth/google/start?next=/", "/api/auth/google/callback?state=x&code=y"} {
		w := call(p, "203.0.113.9:4000", "")
		if w.Code != 302 || w.Header().Get("Location") != "/signin?error=rate_limited" || w.Header().Get("Retry-After") == "" {
			t.Errorf("%s refused: %d %v", p, w.Code, w.Header())
		}
	}
	// Signed-in routes are not part of this budget.
	if w := call("/api/me", "203.0.113.5:4002", ""); w.Code != 401 {
		t.Errorf("/api/me from a limited address: %d", w.Code)
	}
}

func TestP3IPLimiterRefillsAndBoundsItsTable(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := newIPLimiter(2, 1, func() time.Time { return now })
	l.allow("a")
	l.allow("a")
	if ok, wait := l.allow("a"); ok || wait <= 0 || wait > time.Second {
		t.Fatalf("empty bucket: %v %v", ok, wait)
	}
	now = now.Add(1500 * time.Millisecond)
	if ok, _ := l.allow("a"); !ok {
		t.Fatal("not refilled")
	}
	// Past the table's bound a new address still gets its own full
	// bucket: a sweep at most once a second frees refilled ones, or one
	// arbitrary bucket makes room.
	for i := 0; i < preAuthMaxKeys+50; i++ {
		l.allow(string(rune(i + 1000)))
	}
	if len(l.buckets) > preAuthMaxKeys {
		t.Fatalf("%d buckets", len(l.buckets))
	}
	l.allow("x")
	l.allow("x")
	if ok, _ := l.allow("stranger"); !ok {
		t.Error("a new address shared a drained budget")
	}
	now = now.Add(time.Minute)
	l.allow("fresh")
	if _, ok := l.buckets["fresh"]; !ok || len(l.buckets) > 10 {
		t.Errorf("sweep left %d buckets", len(l.buckets))
	}
}

// TestP3AClassSignsInFromOneAddress: 700 students behind one NAT sign in
// within a minute, each loading the config three times and making one
// start and one callback (security review 2026-10-20, finding 1).
func TestP3AClassSignsInFromOneAddress(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	cheap, signIn := newIPLimiter(PreAuthBurst, PreAuthRate, clock), newIPLimiter(SignInBurst, SignInRate, clock)
	for i := 0; i < 700; i++ {
		now = now.Add(60 * time.Second / 700)
		for j := 0; j < 3; j++ {
			if ok, _ := cheap.allow("198.51.100.1"); !ok {
				t.Fatalf("student %d: config refused", i)
			}
		}
		for j := 0; j < 2; j++ {
			if ok, _ := signIn.allow("198.51.100.1"); !ok {
				t.Fatalf("student %d: sign-in refused", i)
			}
		}
	}
}

// TestP3SignInCallbackIsSingleUse: a normal sign-in still works, and a
// state cookie replayed with made-up codes ends as "expired" without a
// call to Google after its first use (P0 security review, finding 4).
func TestP3SignInCallbackIsSingleUse(t *testing.T) {
	s := newSite(t)
	s.idp.SignInAs(authtest.Person{Email: "asha@college.edu", Verified: true, Name: "Asha"})
	b := s.browser()
	var callback *url.URL
	b.client.CheckRedirect = func(r *http.Request, _ []*http.Request) error {
		if r.URL.Path == "/api/auth/google/callback" && callback == nil {
			callback = r.URL
		}
		return nil
	}
	resp, err := b.client.Get(s.srv.URL + "/api/auth/google/start?next=/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if callback == nil || resp.Request.URL.Path != "/" {
		t.Fatalf("first sign-in landed on %s", resp.Request.URL)
	}
	// A second attempt by hand: keep its state cookie and call the
	// callback with it several times, as a script would.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start, err := noFollow.Get(s.srv.URL + "/api/auth/google/start?next=/")
	if err != nil {
		t.Fatal(err)
	}
	start.Body.Close()
	var flow *http.Cookie
	for _, c := range start.Cookies() {
		if c.Name == "podium_signin" {
			flow = c
		}
	}
	loc, _ := url.Parse(start.Header.Get("Location"))
	state := loc.Query().Get("state")
	finish := func(code string) string {
		req, _ := http.NewRequest("GET", s.srv.URL+"/api/auth/google/callback?state="+url.QueryEscape(state)+"&code="+code, nil)
		req.AddCookie(flow)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Location")
	}
	// A junk code first: Google refuses it, and the attempt is spent.
	if got := finish("junk-1"); got != "/signin?error=google" {
		t.Fatalf("first junk code: %s", got)
	}
	for i := 0; i < 3; i++ {
		if got := finish("junk-2"); got != "/signin?error=expired" {
			t.Fatalf("replay %d: %s", i, got)
		}
	}
}

func TestP3UsedFlowsTable(t *testing.T) {
	u := newUsedFlows()
	if fresh, rec := u.take("s1", 100, 50); !fresh || !rec {
		t.Fatal("first take")
	}
	if fresh, _ := u.take("s1", 100, 50); fresh {
		t.Fatal("second take of s1 was fresh")
	}
	for i := 0; i < usedFlowsMax; i++ {
		u.take(string(rune(i+1000)), 10, 5)
	}
	// Full of live entries: a new state passes unrecorded.
	if fresh, rec := u.take("late", 100, 5); !fresh || rec {
		t.Fatalf("full table: %v %v", fresh, rec)
	}
	// Once those expire, the sweep makes room.
	if fresh, rec := u.take("later", 200, 150); !fresh || !rec || len(u.seen) > 10 {
		t.Fatalf("after expiry: %v %v %d", fresh, rec, len(u.seen))
	}
}

// X-Forwarded-For counts only from Caddy on this host: a request from any
// other peer is keyed by that peer, whatever it claims (C1-026).
func TestP3PreAuthIgnoresForwardedForFromOtherPeers(t *testing.T) {
	for _, c := range []struct {
		remote, fwd, want string
	}{
		{"203.0.113.5:4100", "198.51.100.7", "203.0.113.5"},
		{"203.0.113.5:4100", "127.0.0.1", "203.0.113.5"},
		{"[2001:db8:1:2::9]:4100", "198.51.100.7", "2001:db8:1:2::/64"},
		{"127.0.0.1:4100", "198.51.100.7", "198.51.100.7"},
		{"127.0.0.1:4100", "10.0.0.1, 198.51.100.7", "198.51.100.7"},
	} {
		r := httptest.NewRequest("GET", "/api/auth/config", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", c.fwd)
		if got := addressKey(r); got != c.want {
			t.Errorf("peer %s, X-Forwarded-For %q: key %q, want %q", c.remote, c.fwd, got, c.want)
		}
	}
}
