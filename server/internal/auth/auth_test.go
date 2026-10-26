package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/db"
)

const ownerEmail = "owner@college.edu"

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(t *testing.T, seed ...string) (*auth.Service, *clock) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	s := &auth.Service{DB: d, OwnerEmail: ownerEmail, Now: c.now}
	if err := s.Bootstrap(ctx, seed); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func id(email string) auth.Identity {
	// One Google account per address, whatever case the address is in.
	return auth.Identity{Subject: "s-" + strings.ToLower(email), Email: email, EmailVerified: true, Name: "N " + email}
}

func TestWorkspaceDomainRequiresHostedDomain(t *testing.T) {
	s, _ := newService(t, "college.edu")
	ctx := context.Background()
	consumer := id("old@college.edu")
	if _, _, _, err := s.SignIn(ctx, consumer, "", ""); err != nil {
		t.Fatalf("before any Workspace sign-in the email suffix is enough: %v", err)
	}
	staff := id("staff@college.edu")
	staff.HostedDomain = "college.edu"
	if _, _, _, err := s.SignIn(ctx, staff, "", ""); err != nil {
		t.Fatalf("Workspace sign-in: %v", err)
	}
	if _, _, _, err := s.SignIn(ctx, consumer, "", ""); !errors.Is(err, auth.ErrDomain) {
		t.Errorf("token without hd for a Workspace domain: %v, want ErrDomain", err)
	}
}

func TestBootstrapSeedsDomainsOnlyOnFreshInstall(t *testing.T) {
	s, _ := newService(t, "college.edu")
	ctx := context.Background()
	if err := s.RemoveDomain(ctx, "college.edu"); err != nil {
		t.Fatal(err)
	}
	if err := s.Bootstrap(ctx, []string{"college.edu"}); err != nil {
		t.Fatal(err)
	}
	ds, _ := s.AllowedDomains(ctx)
	if len(ds) != 0 {
		t.Errorf("restart re-seeded domains the owner removed: %v", ds)
	}
	people, _ := s.Organisers(ctx)
	if len(people) != 1 || people[0].Email != ownerEmail || people[0].Role != auth.Owner || !people[0].Locked {
		t.Errorf("organisers after bootstrap: %+v", people)
	}
}

func TestSignInRules(t *testing.T) {
	s, _ := newService(t, "college.edu")
	ctx := context.Background()
	if _, err := s.GrantRole(ctx, "ta@gmail.com", auth.Admin); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		id   auth.Identity
		want error
		role auth.Role
	}{
		{"allowed domain", id("Student@College.edu"), nil, auth.Student},
		{"subdomain is a different domain", id("x@cs.college.edu"), auth.ErrDomain, ""},
		{"other domain", id("friend@gmail.com"), auth.ErrDomain, ""},
		{"admin on another domain", id("ta@gmail.com"), nil, auth.Admin},
		{"configured owner", id(ownerEmail), nil, auth.Owner},
		{"unverified", auth.Identity{Email: "u@college.edu", Subject: "u"}, auth.ErrUnverified, ""},
	}
	for _, c := range cases {
		u, token, exp, err := s.SignIn(ctx, c.id, "203.0.113.9", "test")
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err %v, want %v", c.name, err, c.want)
			continue
		}
		if c.want != nil {
			continue
		}
		if u.Role != c.role || token == "" || exp.IsZero() || u.Email != strings.ToLower(c.id.Email) {
			t.Errorf("%s: got %+v token=%q exp=%v", c.name, u, token, exp)
		}
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE users SET disabled = 1 WHERE email = 'student@college.edu'"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SignIn(ctx, id("student@college.edu"), "", ""); !errors.Is(err, auth.ErrDisabled) {
		t.Errorf("disabled account: err %v", err)
	}
	var n int
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE email = 'friend@gmail.com'").Scan(&n)
	if n != 0 {
		t.Error("a refused sign-in created a user row")
	}
}

func TestGoogleAccountBinding(t *testing.T) {
	s, _ := newService(t, "college.edu")
	ctx := context.Background()
	first := auth.Identity{Subject: "sub-alice-1", Email: "alice@college.edu", EmailVerified: true}
	if _, _, _, err := s.SignIn(ctx, first, "", ""); err != nil {
		t.Fatal(err)
	}
	// Same address, different Google account (reassigned or recreated).
	other := first
	other.Subject = "sub-someone-else"
	if _, _, _, err := s.SignIn(ctx, other, "", ""); !errors.Is(err, auth.ErrAccount) {
		t.Fatalf("different subject for a bound email: %v", err)
	}
	// Same Google account presenting a different address.
	moved := auth.Identity{Subject: "sub-alice-1", Email: "alice2@college.edu", EmailVerified: true}
	if _, _, _, err := s.SignIn(ctx, moved, "", ""); !errors.Is(err, auth.ErrAccount) {
		t.Fatalf("bound subject with another email: %v", err)
	}
	if _, _, _, err := s.SignIn(ctx, auth.Identity{Email: "nosub@college.edu", EmailVerified: true}, "", ""); !errors.Is(err, auth.ErrAccount) {
		t.Fatalf("identity without a subject: %v", err)
	}
	// An owner resets the link: the new account is accepted and bound.
	_, token, _, _ := s.SignIn(ctx, first, "", "")
	if err := s.ResetGoogleAccount(ctx, "Alice@College.edu"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(ctx, token); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("reset should end existing sessions: %v", err)
	}
	if _, _, _, err := s.SignIn(ctx, other, "", ""); err != nil {
		t.Errorf("after reset: %v", err)
	}
	if _, _, _, err := s.SignIn(ctx, first, "", ""); !errors.Is(err, auth.ErrAccount) {
		t.Errorf("old account after reset and rebind: %v", err)
	}
	var ce *auth.ChangeError
	if err := s.ResetGoogleAccount(ctx, "nobody@college.edu"); !errors.As(err, &ce) || ce.Kind != "not_found" {
		t.Errorf("reset unknown email: %v", err)
	}
}

func TestRoleAndDomainChangesEndSessions(t *testing.T) {
	s, _ := newService(t, "college.edu", "gmail.com")
	ctx := context.Background()
	ta, err := s.GrantRole(ctx, "ta@other.org", auth.Admin)
	if err != nil {
		t.Fatal(err)
	}
	_, taToken, _, _ := s.SignIn(ctx, id("ta@other.org"), "", "")
	_, gmailToken, _, _ := s.SignIn(ctx, id("kid@gmail.com"), "", "")
	_, studentToken, _, _ := s.SignIn(ctx, id("asha@college.edu"), "", "")
	// A look-alike domain that merely ends with "gmail.com" keeps its session.
	s.AddDomain(ctx, "notgmail.com")
	_, lookalikeToken, _, _ := s.SignIn(ctx, id("x@notgmail.com"), "", "")

	if err := s.RevokeRole(ctx, ta.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(ctx, taToken); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("revoked organiser's session still valid: %v", err)
	}
	if err := s.RemoveDomain(ctx, "gmail.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(ctx, gmailToken); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("student on a removed domain still signed in: %v", err)
	}
	if _, _, err := s.Authenticate(ctx, studentToken); err != nil {
		t.Errorf("student on another domain signed out: %v", err)
	}
	if _, _, err := s.Authenticate(ctx, lookalikeToken); err != nil {
		t.Errorf("notgmail.com student signed out by removing gmail.com: %v", err)
	}
}

func TestOwnerDowngradeKeepsAnOwner(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	co, _ := s.GrantRole(ctx, "co@college.edu", auth.Owner)
	if _, err := s.GrantRole(ctx, "co@college.edu", auth.Admin); err != nil {
		t.Fatalf("downgrading one of two owners: %v", err)
	}
	s.GrantRole(ctx, "co@college.edu", auth.Owner)
	s.OwnerEmail = "moved@college.edu" // the configured owner is now someone else
	s.DB.ExecContext(ctx, "UPDATE users SET role = 'student' WHERE email = ?", ownerEmail)
	var ce *auth.ChangeError
	if _, err := s.GrantRole(ctx, "co@college.edu", auth.Admin); !errors.As(err, &ce) || ce.Kind != "conflict" {
		t.Errorf("downgrading the last owner through grant: %v", err)
	}
	_ = co
}

func TestSessions(t *testing.T) {
	s, c := newService(t, "college.edu")
	ctx := context.Background()
	u, token, _, err := s.SignIn(ctx, id("a@college.edu"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	s.DB.QueryRowContext(ctx, "SELECT id FROM sessions").Scan(&stored)
	if stored == token || len(stored) != 64 {
		t.Errorf("session stored as %q; want the SHA-256 hex of the token, never the token", stored)
	}
	got, renewed, err := s.Authenticate(ctx, token)
	if err != nil || got.ID != u.ID || !renewed.IsZero() {
		t.Fatalf("fresh session: %+v %v %v", got, renewed, err)
	}
	// Use never renews a session: it ends 12 hours after sign-in.
	c.t = c.t.Add(11 * time.Hour)
	if _, renewed, err = s.Authenticate(ctx, token); err != nil || !renewed.IsZero() {
		t.Fatalf("session after 11 hours: renewed %v err %v; want valid and not renewed", renewed, err)
	}
	c.t = c.t.Add(time.Hour)
	if _, _, err := s.Authenticate(ctx, token); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("session 12 hours after sign-in accepted: %v", err)
	}
	// A session from before the cap, stored with a 30-day expiry, ends at
	// the same 12 hours.
	_, old, _, _ := s.SignIn(ctx, id("old@college.edu"), "", "")
	s.DB.ExecContext(ctx, "UPDATE sessions SET expires_at = ? WHERE user_id = (SELECT id FROM users WHERE email = 'old@college.edu')",
		c.t.Add(30*24*time.Hour).UnixMilli())
	c.t = c.t.Add(auth.SessionTTL)
	if _, _, err := s.Authenticate(ctx, old); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("old 30-day session accepted past the 12-hour cap: %v", err)
	}
	if _, _, err := s.Authenticate(ctx, "forged"); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("unknown token accepted: %v", err)
	}

	_, token2, _, _ := s.SignIn(ctx, id("b@college.edu"), "", "")
	s.DB.ExecContext(ctx, "UPDATE users SET disabled = 1 WHERE email = 'b@college.edu'")
	if _, _, err := s.Authenticate(ctx, token2); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("disabled user's session accepted: %v", err)
	}
	_, token3, _, _ := s.SignIn(ctx, id("c@college.edu"), "", "")
	// Made-up and oversized cookies sign nobody out and fail quietly.
	for _, junk := range []string{"forged", strings.Repeat("x", 101)} {
		if err := s.SignOut(ctx, junk); err != nil {
			t.Errorf("sign-out with a made-up token: %v", err)
		}
	}
	if _, _, err := s.Authenticate(ctx, token3); err != nil {
		t.Errorf("a made-up sign-out ended a real session: %v", err)
	}
	if err := s.SignOut(ctx, token3); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(ctx, token3); !errors.Is(err, auth.ErrNoSession) {
		t.Errorf("signed-out session accepted: %v", err)
	}
	// A session nobody touches again is left for the sweep.
	if _, _, _, err := s.SignIn(ctx, id("d@college.edu"), "", ""); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(2 * auth.SessionTTL)
	if n, err := s.DeleteExpiredSessions(ctx); err != nil || n != 1 {
		t.Errorf("sweep removed %d sessions, err %v; want 1", n, err)
	}
}

func TestRoleChanges(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	var ce *auth.ChangeError
	if _, err := s.GrantRole(ctx, "not an email", auth.Admin); !errors.As(err, &ce) || ce.Kind != "invalid" {
		t.Errorf("invalid email: %v", err)
	}
	if _, err := s.GrantRole(ctx, "x@y.edu", "student"); !errors.As(err, &ce) || ce.Kind != "invalid" {
		t.Errorf("invalid role: %v", err)
	}
	if _, err := s.GrantRole(ctx, ownerEmail, auth.Admin); !errors.As(err, &ce) || ce.Kind != "conflict" {
		t.Errorf("demoting the configured owner via grant: %v", err)
	}
	p, err := s.GrantRole(ctx, "Co@Other.org", auth.Owner)
	if err != nil || p.Email != "co@other.org" || p.Role != auth.Owner || p.LastLoginAt != nil {
		t.Fatalf("grant owner: %+v %v", p, err)
	}
	if err := s.RevokeRole(ctx, p.ID); err != nil {
		t.Fatalf("revoking one of two owners: %v", err)
	}
	people, _ := s.Organisers(ctx)
	if err := s.RevokeRole(ctx, people[0].ID); !errors.As(err, &ce) || ce.Kind != "conflict" {
		t.Errorf("revoking the configured owner: %v", err)
	}
	if err := s.RevokeRole(ctx, p.ID); !errors.As(err, &ce) || ce.Kind != "not_found" {
		t.Errorf("revoking a student: %v", err)
	}
	// The last-owner guard, with the configured owner moved aside.
	s.OwnerEmail = "someone-else@college.edu"
	if err := s.RevokeRole(ctx, people[0].ID); !errors.As(err, &ce) || ce.Message != "At least one owner must remain." {
		t.Errorf("revoking the last owner: %v", err)
	}
}

func TestDomains(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	d, created, err := s.AddDomain(ctx, " @College.EDU ")
	if err != nil || !created || d.Domain != "college.edu" {
		t.Fatalf("add: %+v %v %v", d, created, err)
	}
	if _, created, _ := s.AddDomain(ctx, "college.edu"); created {
		t.Error("adding twice reported created")
	}
	var ce *auth.ChangeError
	if _, _, err := s.AddDomain(ctx, "not a domain"); !errors.As(err, &ce) || ce.Kind != "invalid" {
		t.Errorf("invalid domain: %v", err)
	}
	if err := s.RemoveDomain(ctx, "nope.edu"); !errors.As(err, &ce) || ce.Kind != "not_found" {
		t.Errorf("removing a missing domain: %v", err)
	}
	if err := s.RemoveDomain(ctx, "College.edu"); err != nil {
		t.Errorf("remove: %v", err)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/contests/week-6?tab=a": "/contests/week-6?tab=a",
		"":                       "/",
		"//evil.example":         "/",
		"/\\evil.example":        "/",
		"https://evil.example":   "/",
		"/api/auth/logout":       "/",
		"/x\r\nSet-Cookie: a=b":  "/",
		"relative":               "/",
		"/x/../api/auth/logout":  "/",
		"/a/./b/../c?q=1":        "/a/c?q=1",
		"/admin/people/":         "/admin/people/",
		"/api":                   "/",
	} {
		if got := auth.SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlowCookie(t *testing.T) {
	secret := []byte(strings.Repeat("k", 32))
	now := time.Now()
	f := auth.NewFlow("//evil", now)
	if f.Next != "/" || f.State == "" || f.Nonce == "" || len(f.Verifier) < 43 {
		t.Fatalf("NewFlow: %+v", f)
	}
	v, err := auth.SealFlow(secret, f)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := auth.OpenFlow(secret, v, now); err != nil || got != f {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	tampered := strings.Replace(v, v[:4], "AAAA", 1)
	for name, c := range map[string]struct {
		value  string
		secret []byte
		at     time.Time
	}{
		"tampered":     {tampered, secret, now},
		"other secret": {v, []byte(strings.Repeat("z", 32)), now},
		"expired":      {v, secret, now.Add(auth.FlowTTL + time.Minute)},
		"garbage":      {"x", secret, now},
	} {
		if _, err := auth.OpenFlow(c.secret, c.value, c.at); !errors.Is(err, auth.ErrFlow) {
			t.Errorf("%s: err %v, want ErrFlow", name, err)
		}
	}
}

// exchange runs the provider flow against the fake IdP and returns the
// identity or the error from verifying the ID token.
func exchange(t *testing.T, idp *authtest.IdP, mutate func(map[string]any)) (auth.Identity, error) {
	t.Helper()
	p := auth.NewProvider(idp.Endpoints(), idp.ClientID, idp.ClientSecret, "https://contest.example.edu/api/auth/google/callback")
	idp.SignInAs(authtest.Person{Email: "a@college.edu", Verified: true, Name: "A"})
	idp.Mutate = mutate
	f := auth.NewFlow("/", time.Now())
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(p.AuthCodeURL(f.State, f.Nonce, f.Verifier, "college.edu"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("state") != f.State {
		t.Fatalf("state not echoed: %v", loc)
	}
	if idp.LastAuthQuery.Get("hd") != "college.edu" || idp.LastAuthQuery.Get("nonce") != f.Nonce {
		t.Errorf("auth request query: %v", idp.LastAuthQuery)
	}
	return p.Exchange(context.Background(), loc.Query().Get("code"), f.Verifier, f.Nonce, time.Now())
}

func TestProviderExchange(t *testing.T) {
	idp := authtest.New(t)
	got, err := exchange(t, idp, nil)
	if err != nil || got.Email != "a@college.edu" || !got.EmailVerified {
		t.Fatalf("happy path: %+v %v", got, err)
	}
	rejections := map[string]func(map[string]any){
		"wrong audience": func(c map[string]any) { c["aud"] = "someone-else" },
		"wrong issuer":   func(c map[string]any) { c["iss"] = "https://evil.example" },
		"wrong nonce":    func(c map[string]any) { c["nonce"] = "replayed" },
		"expired":        func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"future iat":     func(c map[string]any) { c["iat"] = time.Now().Add(time.Hour).Unix() },
		"no email":       func(c map[string]any) { delete(c, "email") },
		"extra audience without azp": func(c map[string]any) {
			c["aud"] = []string{idp.ClientID, "another-client"}
		},
		"azp names another client": func(c map[string]any) { c["azp"] = "another-client" },
	}
	for name, m := range rejections {
		if _, err := exchange(t, idp, m); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
	if got, err := exchange(t, idp, func(c map[string]any) { c["email_verified"] = "true" }); err != nil || !got.EmailVerified {
		t.Errorf("string email_verified: %+v %v", got, err)
	}
	if _, err := exchange(t, idp, func(c map[string]any) {
		c["aud"], c["azp"] = []string{idp.ClientID, "another-client"}, idp.ClientID
	}); err != nil {
		t.Errorf("extra audience with matching azp: %v", err)
	}
}

func TestProviderRejectsWrongVerifier(t *testing.T) {
	idp := authtest.New(t)
	p := auth.NewProvider(idp.Endpoints(), idp.ClientID, idp.ClientSecret, "https://c.example.edu/cb")
	idp.SignInAs(authtest.Person{Email: "a@college.edu", Verified: true})
	f := auth.NewFlow("/", time.Now())
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ := client.Get(p.AuthCodeURL(f.State, f.Nonce, f.Verifier, ""))
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	other := auth.NewFlow("/", time.Now())
	if _, err := p.Exchange(context.Background(), loc.Query().Get("code"), other.Verifier, f.Nonce, time.Now()); err == nil {
		t.Error("exchange succeeded with a different PKCE verifier")
	}
}
