package httpapi

// Golden test of the ordinary session as it is today (v2.0.2): the exact
// cookie, the stored expiry and the 12 hour cap. The v2.1.0 "keep me signed
// in" work must leave all of this unchanged for a person who is not
// remembered, so these values are written out, not derived from constants.

import (
	"context"
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

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/db"
)

func TestOrdinarySessionCookieAndExpiryGolden(t *testing.T) {
	ctx := context.Background()
	d, _ := db.Open(ctx, filepath.Join(t.TempDir(), "g.db"))
	defer d.Close()
	db.Migrate(ctx, d)
	signedInAt := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	now := signedInAt
	svc := &auth.Service{DB: d, OwnerEmail: "owner@college.edu", Now: func() time.Time { return now }}
	svc.Bootstrap(ctx)
	idp := authtest.New(t)
	base := "https://contest.example.edu"
	h := New(Deps{
		DB: d, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), WebApp: http.NotFoundHandler(),
		Auth: svc, IdP: auth.NewProvider(idp.Endpoints(), idp.ClientID, idp.ClientSecret, RedirectURL(base)),
		BaseURL: base, SessionSecret: []byte(strings.Repeat("g", 32)),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", base+"/api/auth/google/start?next=/", nil))
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
	golden := regexp.MustCompile(`^__Host-podium_session=[A-Za-z0-9_-]{43}; Path=/; HttpOnly; Secure; SameSite=Lax$`)
	if !golden.MatchString(raw) {
		t.Fatalf("session cookie is %q, want a value then exactly: Path=/; HttpOnly; Secure; SameSite=Lax (no Max-Age, no Expires)", raw)
	}

	var created, expires int64
	if err := d.QueryRowContext(ctx, "SELECT created_at, expires_at FROM sessions").Scan(&created, &expires); err != nil {
		t.Fatal(err)
	}
	if created != signedInAt.UnixMilli() || expires != signedInAt.Add(12*time.Hour).UnixMilli() {
		t.Errorf("session row created_at %d expires_at %d, want sign-in time and exactly 12 hours later (%d)",
			created, expires, signedInAt.Add(12*time.Hour).UnixMilli())
	}

	token := cookieNamed(rec.Result().Cookies(), "__Host-podium_session").Value
	now = signedInAt.Add(12*time.Hour - time.Millisecond)
	if _, _, err := svc.Authenticate(ctx, token); err != nil {
		t.Errorf("session one millisecond before 12 hours refused: %v", err)
	}
	var after int64
	d.QueryRowContext(ctx, "SELECT expires_at FROM sessions").Scan(&after)
	if after != expires {
		t.Errorf("use renewed the session: expires_at %d -> %d", expires, after)
	}
	now = signedInAt.Add(12 * time.Hour)
	if _, _, err := svc.Authenticate(ctx, token); err == nil {
		t.Error("session at exactly 12 hours accepted")
	}
}
