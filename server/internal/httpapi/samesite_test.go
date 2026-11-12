package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// cookieMutation sends POST path with the session cookie added by hand and
// exactly the headers given, so the test controls Origin and Sec-Fetch-Site.
func cookieMutation(t *testing.T, s *site, cookie *http.Cookie, path string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", s.srv.URL+path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return send(s.browser(), req)
}

func sessionCookieOf(t *testing.T, s *site) *http.Cookie {
	t.Helper()
	b := s.browser()
	b.signIn(authtest.Person{Email: "asha@college.edu", Verified: true, Name: "Asha"}, "/")
	u, _ := url.Parse(s.srv.URL)
	for _, c := range b.client.Jar.Cookies(u) {
		if strings.Contains(c.Name, "podium_session") {
			return c
		}
	}
	t.Fatal("no session cookie after sign-in")
	return nil
}

// A mutation that carries the session cookie but neither Origin nor
// Sec-Fetch-Site is refused: every browser sends one of them.
func TestCookieMutationNeedsOriginOrFetchSite(t *testing.T) {
	s := newSite(t)
	cookie := sessionCookieOf(t, s)
	code, out := cookieMutation(t, s, cookie, "/api/auth/logout", nil)
	if code != 403 || errCode(out) != "cross_site" {
		t.Fatalf("cookie mutation with neither header: %d %v", code, out)
	}
	e, _ := out["error"].(map[string]any)
	want := "This request had no Origin or Sec-Fetch-Site header and was refused. Use the web app, or send an Origin header equal to the server address."
	if e["message"] != want {
		t.Errorf("copy: %q", e["message"])
	}
	// The refused request did not sign the session out.
	if code, _ := send(s.browser(), withCookie(t, s, cookie, "GET", "/api/me")); code != 200 {
		t.Errorf("session after the refusal: %d", code)
	}
	if code, out := cookieMutation(t, s, cookie, "/api/auth/logout", map[string]string{"Sec-Fetch-Site": "same-origin"}); code != 204 && code != 200 {
		t.Errorf("Sec-Fetch-Site only: %d %v", code, out)
	}
	cookie = sessionCookieOf(t, s)
	if code, out := cookieMutation(t, s, cookie, "/api/auth/logout", map[string]string{"Origin": s.srv.URL}); code != 204 && code != 200 {
		t.Errorf("Origin only: %d %v", code, out)
	}
	cookie = sessionCookieOf(t, s)
	if code, out := cookieMutation(t, s, cookie, "/api/auth/logout", map[string]string{"Origin": "http://evil.example"}); code != 403 || errCode(out) != "cross_site" {
		t.Errorf("foreign Origin: %d %v", code, out)
	}
}

func withCookie(t *testing.T, s *site, c *http.Cookie, method, path string) *http.Request {
	req, _ := http.NewRequest(method, s.srv.URL+path, nil)
	req.AddCookie(c)
	return req
}

// The rule is about cookie-authenticated requests: with no session cookie a
// POST is not refused for missing headers, a GET never is, and the judge's
// bearer-token API (its own listener, no cookie) is not touched.
func TestMissingHeadersOnlyRefusedWithSessionCookie(t *testing.T) {
	s := newSite(t)
	cookie := sessionCookieOf(t, s)
	if code, out := cookieMutation(t, s, nil, "/api/auth/logout", nil); code == 403 {
		t.Errorf("POST with no cookie and no headers: %d %v", code, out)
	}
	if code, out := send(s.browser(), withCookie(t, s, cookie, "GET", "/api/me")); code != 200 {
		t.Errorf("GET with cookie and no headers: %d %v", code, out)
	}
	resp, _ := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: []string{"submit"}, Worker: "fake"})
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		t.Errorf("bearer worker call with no Origin: %d", resp.StatusCode)
	}
}
