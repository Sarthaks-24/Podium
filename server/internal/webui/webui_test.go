package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const testBase = "https://podium.example"

func TestHandler(t *testing.T) {
	h := handlerFor(testBase, fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>podium</title>")},
		"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		"favicon.svg":        {Data: []byte("<svg/>")},
		".keep":              {Data: nil},
		"assets/.hidden.txt": {Data: []byte("x")},
	})
	cases := []struct {
		method, path string
		status       int
		cache        string
	}{
		{"GET", "/", 200, "private, no-store"},
		{"GET", "/contests/week-6", 200, "no-cache"},
		{"HEAD", "/contests/week-6", 200, "no-cache"},
		{"GET", "/assets/app-abc.js", 200, "public, max-age=31536000, immutable"},
		{"GET", "/favicon.svg", 200, "public, max-age=3600"},
		{"GET", "/assets/app-old.js", 404, ""},
		{"GET", "/fonts/missing.woff2", 404, ""},
		{"GET", "/.keep", 404, ""},
		{"GET", "/assets/.hidden.txt", 404, ""},
		{"POST", "/", 405, ""},
		{"GET", "/robots.txt", 200, "public, max-age=3600"},
		{"GET", "/signin", 200, "no-cache"},
		{"GET", "/privacy", 200, "no-cache"},
		{"GET", "/me", 200, "no-cache"},
		{"GET", "/admin/people", 200, "no-cache"},
		{"GET", "/nope", 404, "no-cache"},
		{"GET", "/contests", 404, "no-cache"},
		{"HEAD", "/nope", 404, "no-cache"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, rec.Code, c.status)
		}
		if c.cache == "no-cache" && rec.Header().Get("X-Robots-Tag") != "noindex" {
			t.Errorf("%s %s: no noindex header", c.method, c.path)
		}
		if c.cache != "" && rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s %s: Cache-Control %q, want %q", c.method, c.path, rec.Header().Get("Cache-Control"), c.cache)
		}
	}
}

func TestRedirects(t *testing.T) {
	h := handlerFor(testBase, fstest.MapFS{"index.html": {Data: []byte("shell")}})
	cases := []struct{ path, to string }{
		{"/contests/week-6/", "/contests/week-6"},
		{"/admin/people/?tab=signin", "/admin/people?tab=signin"},
		{"//evil.example/", "/evil.example"},
		{"/progress", "/"},
		{"/notices", "/"},
		{"/contact", "/"},
		{"/integrity", "/"},
		{"/integrity/cases/3", "/"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != c.to {
			t.Errorf("GET %s: %d to %q, want 301 to %q", c.path, rec.Code, rec.Header().Get("Location"), c.to)
		}
	}
}

// TestRedirectStaysOnSite checks paths a browser would read as another host
// once redirected: it turns a backslash into a slash and drops tabs and newlines.
func TestRedirectStaysOnSite(t *testing.T) {
	h := handlerFor(testBase, fstest.MapFS{"index.html": {Data: []byte("shell")}})
	for _, p := range []string{"/%5Cevil.example/", "/%09/evil.example/", "/%0A/evil.example/", "/a%5C/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if loc := rec.Header().Get("Location"); loc != "" || rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d to %q, want 404 and no redirect", p, rec.Code, loc)
		}
	}
}

// TestNotFoundServesShell checks that an unknown path still gets the app,
// which draws the "Page not found" screen, with the status a crawler reads.
func TestNotFoundServesShell(t *testing.T) {
	h := handlerFor(testBase, fstest.MapFS{"index.html": {Data: []byte("shell")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != http.StatusNotFound || rec.Body.String() != "shell" {
		t.Errorf("GET /nope: %d %q, want 404 and the app shell", rec.Code, rec.Body.String())
	}
}

// TestRoutesMatchRouter reads the router's table in the web app, so a screen
// added there without a matching entry here fails instead of answering 404.
func TestRoutesMatchRouter(t *testing.T) {
	src, err := os.ReadFile("../../../web/src/lib/routing.ts")
	if err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, m := range regexp.MustCompile(`pattern: '([^']+)'`).FindAllStringSubmatch(string(src), -1) {
		if m[1] != "*" {
			patterns = append(patterns, regexp.MustCompile(`:[a-z]+`).ReplaceAllString(m[1], "x"))
		}
	}
	if len(patterns) < 10 {
		t.Fatalf("found %d patterns in routing.ts; has its format changed?", len(patterns))
	}
	for _, p := range patterns {
		if !appRoute(p) {
			t.Errorf("routing.ts has %s, but the server answers it with 404: add it to appPaths or appPrefixes", p)
		}
	}
	for _, p := range appPaths {
		if !slices.Contains(patterns, p) {
			t.Errorf("appPaths has %s, which routing.ts no longer has", p)
		}
	}
	for _, pre := range appPrefixes {
		if !slices.ContainsFunc(patterns, func(q string) bool { return strings.HasPrefix(q, pre) }) {
			t.Errorf("appPrefixes has %s, which no route in routing.ts starts with", pre)
		}
	}
}

func TestHandlerNotBuilt(t *testing.T) {
	rec := httptest.NewRecorder()
	handlerFor(testBase, fstest.MapFS{}).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", rec.Code)
	}
}

// v4 P2 (V4-8): / without a session cookie is the prerendered landing page,
// indexable; with either cookie name it is the app, noindex. Both are
// private and vary on the cookie, so no cache hands one to the other.
func TestLandingPage(t *testing.T) {
	h := handlerFor(testBase, fstest.MapFS{
		"index.html":   {Data: []byte("app shell")},
		"landing.html": {Data: []byte("<h1>Programming contests at this college</h1>")},
	})
	get := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: cookie, Value: "x"})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	out := get("")
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Programming contests") || out.Header().Get("X-Robots-Tag") != "" {
		t.Errorf("signed out: %d %q robots %q, want the landing page, indexable", out.Code, out.Body.String(), out.Header().Get("X-Robots-Tag"))
	}
	for _, name := range append([]string{""}, SessionCookies...) {
		rec := get(name)
		if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("Vary") != "Cookie" {
			t.Errorf("cookie %q: Cache-Control %q Vary %q", name, rec.Header().Get("Cache-Control"), rec.Header().Get("Vary"))
		}
		if name != "" && (rec.Body.String() != "app shell" || rec.Header().Get("X-Robots-Tag") != "noindex") {
			t.Errorf("cookie %q: %q robots %q, want the app, noindex", name, rec.Body.String(), rec.Header().Get("X-Robots-Tag"))
		}
	}
	// Only / is the landing page: another path without a cookie is the app.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/contests/week-6", nil))
	if rec.Body.String() != "app shell" {
		t.Errorf("/contests/week-6 signed out: %q, want the app", rec.Body.String())
	}
}

// v4 P3: robots.txt lets search engines read / alone and names the
// sitemap, which lists /; the landing page's %BASE_URL% is the configured
// address.
func TestRobotsSitemapAndBaseURL(t *testing.T) {
	h := handlerFor(testBase, fstest.MapFS{
		"index.html":   {Data: []byte("app")},
		"landing.html": {Data: []byte(`<link rel="canonical" href="%BASE_URL%/" />`)},
	})
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec
	}
	robots := get("/robots.txt").Body.String()
	if robots != "User-agent: *\nAllow: /$\nDisallow: /\nSitemap: https://podium.example/sitemap.xml\n" {
		t.Errorf("robots.txt: %q", robots)
	}
	site := get("/sitemap.xml")
	if !strings.Contains(site.Body.String(), "<loc>https://podium.example/</loc>") || strings.Count(site.Body.String(), "<loc>") != 1 || !strings.HasPrefix(site.Header().Get("Content-Type"), "application/xml") {
		t.Errorf("sitemap: %q %q", site.Header().Get("Content-Type"), site.Body.String())
	}
	if body := get("/").Body.String(); body != `<link rel="canonical" href="https://podium.example/" />` {
		t.Errorf("landing page: %q, want the address filled in", body)
	}
}
