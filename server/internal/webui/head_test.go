package webui

// Tests of the server-filled head (B3.4 to B3.6) against the real
// web/index.html and the checked-in manifests: testdata/routes.json is
// the web build's copy (owned by the frontend; a web test keeps it equal
// to routing.ts), testdata/routes-p3.json adds /privacy (public) and
// /contact (signed in) as P3's frontend will.

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

const testBase = "https://podium.example"

// buildFS is a web build: the real index.html, a manifest and the files
// index.html names, plus extra.
func buildFS(t *testing.T, manifest string, extra fstest.MapFS) fstest.MapFS {
	t.Helper()
	index, err := os.ReadFile("../../../web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		"index.html":                        {Data: index},
		"favicon.svg":                       {Data: []byte("<svg/>")},
		"theme-init.js":                     {Data: []byte("1")},
		"fonts/schibsted-grotesk-400.woff2": {Data: []byte("f")},
		"fonts/schibsted-grotesk-600.woff2": {Data: []byte("f")},
		"src/main.ts":                       {Data: []byte("1")},
	}
	if manifest != "" {
		b, err := os.ReadFile("testdata/" + manifest)
		if err != nil {
			t.Fatal(err)
		}
		fsys["routes.json"] = &fstest.MapFile{Data: b}
	}
	for k, v := range extra {
		fsys[k] = v
	}
	return fsys
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

var (
	robotsMeta = regexp.MustCompile(`<meta name="robots" content="([^"]*)"`)
	canonical  = regexp.MustCompile(`<link rel="canonical" href="([^"]*)"`)
	ogURL      = regexp.MustCompile(`<meta property="og:url" content="([^"]*)"`)
	titleTag   = regexp.MustCompile(`<title>([^<]*)</title>`)
	descMeta   = regexp.MustCompile(`<meta name="description" content="([^"]*)"`)
)

// concrete fills a pattern's parameters.
func concrete(pattern string) string {
	return regexp.MustCompile(`:[a-z]+`).ReplaceAllString(pattern, "x1")
}

// TestManifestWalk: every route of both manifests answers 200 with exactly
// one robots rule and one title; public routes get one canonical equal to
// og:url, an index rule and a description and no X-Robots-Tag; every other
// route keeps noindex, has no canonical and carries X-Robots-Tag. An
// unknown path is a 404 with the not-found page, never indexed.
func TestManifestWalk(t *testing.T) {
	for _, file := range []string{"routes.json", "routes-p3.json"} {
		t.Run(file, func(t *testing.T) {
			b, _ := os.ReadFile("testdata/" + file)
			m, err := ParseManifest(b)
			if err != nil {
				t.Fatal(err)
			}
			h := handlerFor(buildFS(t, file, nil), Options{BaseURL: testBase + "/"})
			for _, r := range m.Routes {
				p := concrete(r.Pattern)
				w := get(h, p)
				body := w.Body.String()
				if w.Code != 200 {
					t.Errorf("%s: status %d", p, w.Code)
				}
				robots := robotsMeta.FindAllStringSubmatch(body, -1)
				canon := canonical.FindAllStringSubmatch(body, -1)
				titles := titleTag.FindAllStringSubmatch(body, -1)
				if len(robots) != 1 || len(titles) != 1 || titles[0][1] != r.Title+" · podium" {
					t.Errorf("%s: robots %v titles %v", p, robots, titles)
					continue
				}
				if r.Public {
					og := ogURL.FindAllStringSubmatch(body, -1)
					if robots[0][1] != "index, follow" || len(canon) != 1 || canon[0][1] != testBase+r.Pattern ||
						len(og) != 1 || og[0][1] != canon[0][1] || w.Header().Get("X-Robots-Tag") != "" ||
						len(descMeta.FindAllString(body, -1)) != 1 {
						t.Errorf("%s public: robots %v canonical %v og %v header %q", p, robots, canon, og, w.Header().Get("X-Robots-Tag"))
					}
				} else if robots[0][1] != "noindex" || len(canon) != 0 || w.Header().Get("X-Robots-Tag") != "noindex" ||
					strings.Contains(body, "og:") || len(descMeta.FindAllString(body, -1)) != 0 {
					t.Errorf("%s private: robots %v canonical %v header %q", p, robots, canon, w.Header().Get("X-Robots-Tag"))
				}
			}
			for _, p := range []string{"/no/such/page", "/signin/extra", "/Signin", "/admin/people/x/y"} {
				w := get(h, p)
				body := w.Body.String()
				robots := robotsMeta.FindAllStringSubmatch(body, -1)
				if w.Code != 404 || len(robots) != 1 || robots[0][1] != "noindex" || canonical.MatchString(body) ||
					w.Header().Get("X-Robots-Tag") != "noindex" || !strings.Contains(body, "<title>Page not found · podium</title>") ||
					!strings.Contains(body, `<div id="app">`) {
					t.Errorf("%s: %d robots %v header %q", p, w.Code, robots, w.Header().Get("X-Robots-Tag"))
				}
			}
			// A trailing slash is the same route; its canonical has none.
			if w := get(h, "/signin/"); w.Code != 200 || canonical.FindStringSubmatch(w.Body.String())[1] != testBase+"/signin" {
				t.Errorf("/signin/: %d", w.Code)
			}
		})
	}
}

func TestPublicHeadCardAndEscaping(t *testing.T) {
	manifest := `{"version":1,"routes":[{"pattern":"/signin","name":"signin","auth":false,"public":true,
		"title":"Sign in <now>","description":"Say \"hi\" & <b>go</b>"}],
		"notFound":{"pattern":"*","name":"not-found","auth":false,"public":true,"title":"Page not found","description":"x"}}`
	fsys := buildFS(t, "", fstest.MapFS{"routes.json": {Data: []byte(manifest)}})
	h := handlerFor(fsys, Options{BaseURL: testBase})
	body := get(h, "/signin").Body.String()
	for _, want := range []string{
		`<title>Sign in &lt;now&gt; · podium</title>`,
		`<meta name="description" content="Say &#34;hi&#34; &amp; &lt;b&gt;go&lt;/b&gt;" />`,
		`<meta property="og:title" content="Sign in &lt;now&gt; · podium" />`,
		`<meta name="twitter:card" content="summary" />`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in\n%s", want, body)
		}
	}
	if strings.Contains(body, "og:image") || strings.Contains(body, "<b>") {
		t.Errorf("image named without og.png, or markup passed through:\n%s", body)
	}
	// With the card in the build: an absolute image and the large card.
	fsys["og.png"] = &fstest.MapFile{Data: []byte("png")}
	body = get(handlerFor(fsys, Options{BaseURL: testBase}), "/signin").Body.String()
	if !strings.Contains(body, `<meta property="og:image" content="https://podium.example/og.png" />`) ||
		!strings.Contains(body, `<meta property="og:image:width" content="1200" />`) ||
		!strings.Contains(body, `<meta name="twitter:card" content="summary_large_image" />`) {
		t.Errorf("card tags:\n%s", body)
	}
	if w := get(handlerFor(fsys, Options{BaseURL: testBase}), "/og.png"); w.Code != 200 {
		t.Errorf("og.png: %d", w.Code)
	}
}

func TestOrganizationJSONLD(t *testing.T) {
	org := Organisation{}
	var fail error
	h := handlerFor(buildFS(t, "routes-p3.json", nil), Options{BaseURL: testBase,
		Organisation: func(*http.Request) (Organisation, error) { return org, fail }})
	ld := regexp.MustCompile(`<script type="application/ld\+json">([^<]*)</script>`)
	if got := ld.FindAllString(get(h, "/signin").Body.String(), -1); len(got) != 0 {
		t.Errorf("JSON-LD before any setting: %v", got)
	}
	org = Organisation{Name: "Example College </script><script>alert(1)</script>", Email: "podium@college.edu"}
	body := get(h, "/signin").Body.String()
	got := ld.FindAllStringSubmatch(body, -1)
	if len(got) != 1 || strings.Contains(got[0][1], "</script>") ||
		strings.ContainsRune(got[0][1], '<') || !strings.Contains(got[0][1], `"name":"Example College \u003c/script`) ||
		!strings.Contains(got[0][1], `"@type":"Organization"`) || !strings.Contains(got[0][1], `"email":"podium@college.edu"`) {
		t.Errorf("JSON-LD: %v", got)
	}
	if strings.Contains(get(h, "/privacy").Body.String(), "ld+json") {
		t.Error("JSON-LD outside /signin")
	}
	fail = errors.New("database locked")
	if strings.Contains(get(h, "/signin").Body.String(), "ld+json") {
		t.Error("JSON-LD despite an error")
	}
}

// TestPrivacyBody: /privacy reads without JavaScript once the build makes
// privacy-body.html, and has an empty block before; the fragment and the
// manifest are never served as files.
func TestPrivacyBody(t *testing.T) {
	text := "<h1>Privacy</h1><p>podium keeps your email, your name and your code.</p>"
	block := regexp.MustCompile(`<!--podium:body-->(.*?)<!--/podium:body-->`)
	without := handlerFor(buildFS(t, "routes-p3.json", nil), Options{BaseURL: testBase})
	if got := block.FindStringSubmatch(get(without, "/privacy").Body.String()); got == nil || got[1] != "" {
		t.Errorf("without the fragment: %v", got)
	}
	with := handlerFor(buildFS(t, "routes-p3.json", fstest.MapFS{"privacy-body.html": {Data: []byte(text)}}), Options{BaseURL: testBase})
	if got := block.FindStringSubmatch(get(with, "/privacy").Body.String()); got == nil || got[1] != text {
		t.Errorf("with the fragment: %v", got)
	}
	if got := block.FindStringSubmatch(get(with, "/signin").Body.String()); got == nil || got[1] != "" {
		t.Errorf("fragment outside /privacy: %v", got)
	}
	for _, p := range []string{"/privacy-body.html", "/routes.json"} {
		if w := get(with, p); w.Code != 404 {
			t.Errorf("%s served: %d", p, w.Code)
		}
	}
	// The checked-in manifest has /privacy but not public, because the
	// text waits for the owner's approval (D85): nothing filled, noindex.
	held := handlerFor(buildFS(t, "routes.json", fstest.MapFS{"privacy-body.html": {Data: []byte(text)}}), Options{BaseURL: testBase})
	if w := get(held, "/privacy"); strings.Contains(w.Body.String(), "keeps your email") || w.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("/privacy before approval: %d, robots %q", w.Code, w.Header().Get("X-Robots-Tag"))
	}
}

func TestRobotsAndSitemap(t *testing.T) {
	h := handlerFor(buildFS(t, "routes-p3.json", nil), Options{BaseURL: testBase})
	w := get(h, "/robots.txt")
	if w.Code != 200 || w.Body.String() != "User-agent: *\nAllow: /\nDisallow: /api/\n\nSitemap: https://podium.example/sitemap.xml\n" ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("robots.txt: %d %q", w.Code, w.Body)
	}
	w = get(h, "/sitemap.xml")
	var set struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &set); err != nil || w.Code != 200 {
		t.Fatalf("sitemap.xml: %d %v", w.Code, err)
	}
	var locs []string
	for _, u := range set.URLs {
		locs = append(locs, u.Loc)
	}
	if strings.Join(locs, " ") != "https://podium.example/signin https://podium.example/privacy" {
		t.Errorf("sitemap %v", locs)
	}
}

// TestNoManifestStaysClosed: an old build without routes.json, or one with
// a version this server does not read, keeps every page noindex.
func TestNoManifestStaysClosed(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"none":      buildFS(t, "", nil),
		"version 2": buildFS(t, "", fstest.MapFS{"routes.json": {Data: []byte(`{"version":2,"routes":[]}`)}}),
	} {
		h := handlerFor(fsys, Options{BaseURL: testBase})
		for _, p := range []string{"/signin", "/no/such/page"} {
			w := get(h, p)
			if w.Code != 200 || w.Header().Get("X-Robots-Tag") != "noindex" || canonical.MatchString(w.Body.String()) {
				t.Errorf("%s %s: %d %q", name, p, w.Code, w.Header().Get("X-Robots-Tag"))
			}
		}
	}
}

func TestStaticTypes(t *testing.T) {
	h := handlerFor(buildFS(t, "routes.json", fstest.MapFS{
		"site.webmanifest":     {Data: []byte(`{"name":"podium"}`)},
		"favicon.ico":          {Data: []byte{0, 0, 1, 0}},
		"apple-touch-icon.png": {Data: []byte("png")},
	}), Options{BaseURL: testBase})
	for p, want := range map[string]string{
		"/site.webmanifest":                  "application/manifest+json",
		"/favicon.ico":                       "image/x-icon",
		"/apple-touch-icon.png":              "image/png",
		"/fonts/schibsted-grotesk-400.woff2": "font/woff2",
	} {
		if w := get(h, p); w.Code != 200 || w.Header().Get("Content-Type") != want {
			t.Errorf("%s: %d %q", p, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

// TestLinkCrawl follows every same-site link the public pages, robots.txt
// and the sitemap name (canonical, og:url, og:image, icons, styles,
// scripts, sitemap entries) and expects each to answer 200 (web-quality
// item 20, backend half).
func TestLinkCrawl(t *testing.T) {
	h := handlerFor(buildFS(t, "routes-p3.json", fstest.MapFS{"og.png": {Data: []byte("png")}}), Options{BaseURL: testBase})
	ref := regexp.MustCompile(`(?:href|src|content)="([^"]+)"|<loc>([^<]+)</loc>|Sitemap: (\S+)`)
	seen := map[string]bool{}
	queue := []string{"/signin", "/privacy", "/robots.txt", "/sitemap.xml"}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		w := get(h, p)
		if w.Code != 200 {
			t.Errorf("%s: %d", p, w.Code)
			continue
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") && !strings.Contains(ct, "xml") && !strings.HasPrefix(ct, "text/plain") {
			continue
		}
		for _, m := range ref.FindAllStringSubmatch(w.Body.String(), -1) {
			link := m[1] + m[2] + m[3]
			u, err := url.Parse(link)
			if err != nil || (u.Host != "" && u.Scheme+"://"+u.Host != testBase) || !strings.HasPrefix(u.Path, "/") {
				continue
			}
			queue = append(queue, u.Path)
		}
	}
	for _, want := range []string{"/favicon.svg", "/og.png", "/theme-init.js", "/fonts/schibsted-grotesk-600.woff2"} {
		if !seen[want] {
			t.Errorf("crawl never reached %s (seen %v)", want, seen)
		}
	}
}
