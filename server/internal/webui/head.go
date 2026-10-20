package webui

// The server-filled head and body blocks (D100, B3.4 to B3.6). The web
// build owns index.html and the words (titles and descriptions in
// routes.json); the server only replaces what lies between the marker
// pairs, and the first <title>, at request time. A slipped merge on
// either side leaves pages hidden: without a manifest, or with one this
// server does not understand, every page keeps noindex.

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Files of the web build the head reads.
const (
	manifestFile = "routes.json"
	privacyBody  = "privacy-body.html"
	// ogImage is the one Open Graph card (FP.4): 1200 x 630 PNG at the
	// root of the build. Without it the head names no image.
	ogImage = "og.png"
)

// siteName ends every title, as web/src/lib/head.ts does.
const siteName = "podium"

// Marker pairs in web/index.html (contract C2).
const (
	headOpen, headClose = "<!--podium:head-->", "<!--/podium:head-->"
	bodyOpen, bodyClose = "<!--podium:body-->", "<!--/podium:body-->"
)

// Route is one route of routes.json.
type Route struct {
	Pattern     string `json:"pattern"`
	Name        string `json:"name"`
	Auth        bool   `json:"auth"`
	Public      bool   `json:"public"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Manifest is routes.json, version 1 (D131).
type Manifest struct {
	Version  int     `json:"version"`
	Routes   []Route `json:"routes"`
	NotFound Route   `json:"notFound"`
}

// ParseManifest reads routes.json and refuses a version it does not know.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Version != 1 {
		return nil, errVersion(m.Version)
	}
	return &m, nil
}

type errVersion int

func (e errVersion) Error() string {
	return "routes.json version " + strconv.Itoa(int(e)) + " is not one this server reads (1)"
}

func segments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Match finds the route for an escaped path as routing.ts matchRoute does:
// trailing slashes ignored, case sensitive, a ":" segment takes any one
// segment that decodes. It returns nil when no route matches.
func (m *Manifest) Match(escapedPath string) *Route {
	parts := segments(escapedPath)
	for i := range m.Routes {
		pattern := segments(m.Routes[i].Pattern)
		if len(pattern) != len(parts) {
			continue
		}
		ok := true
		for j, seg := range pattern {
			if strings.HasPrefix(seg, ":") {
				if _, err := url.PathUnescape(parts[j]); err != nil {
					ok = false
				}
			} else if seg != parts[j] {
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok {
			return &m.Routes[i]
		}
	}
	return nil
}

// fixed reports whether a route has no parameters, so its pattern is its
// one URL.
func (r Route) fixed() bool { return !strings.Contains(r.Pattern, ":") && r.Pattern != "*" }

// site holds the build and the options; page renders one request.
type site struct {
	index    string
	manifest *Manifest
	base     string
	privacy  string
	hasOG    bool
	org      func(*http.Request) (Organisation, error)
	log      *slog.Logger
}

func newSite(dist fs.FS, index string, o Options) *site {
	s := &site{index: index, base: strings.TrimRight(o.BaseURL, "/"), org: o.Organisation, log: o.Log}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if b, err := fs.ReadFile(dist, manifestFile); err == nil {
		if m, err := ParseManifest(b); err != nil {
			s.log.Warn("web app route manifest not used: every page stays noindex", "err", err)
		} else {
			s.manifest = m
		}
	} else {
		s.log.Warn("web app has no route manifest: every page stays noindex and answers 200")
	}
	// The privacy text is rendered at build time from the approved
	// Markdown (FP.1); until the build makes it, the block stays empty.
	if b, err := fs.ReadFile(dist, privacyBody); err == nil {
		s.privacy = string(b)
	}
	if st, err := fs.Stat(dist, ogImage); err == nil && !st.IsDir() {
		s.hasOG = true
	}
	return s
}

// replaceBlock replaces what lies between open and close in doc, if both
// are there in that order.
func replaceBlock(doc, open, close, with string) string {
	i := strings.Index(doc, open)
	if i < 0 {
		return doc
	}
	j := strings.Index(doc[i+len(open):], close)
	if j < 0 {
		return doc
	}
	start, end := i+len(open), i+len(open)+j
	return doc[:start] + with + doc[end:]
}

// replaceTitle replaces the text of the document's first <title>.
func replaceTitle(doc, title string) string {
	i := strings.Index(doc, "<title>")
	if i < 0 {
		return doc
	}
	j := strings.Index(doc[i:], "</title>")
	if j < 0 {
		return doc
	}
	return doc[:i+len("<title>")] + html.EscapeString(title) + doc[i+j:]
}

// fullTitle is "Sign in · podium", as head.ts formatTitle writes it.
func fullTitle(t string) string {
	if t = strings.TrimSpace(t); t == "" {
		return siteName
	}
	return t + " · " + siteName
}

// page answers a client route with index.html, its head filled for the
// route the manifest gives the path.
func (s *site) page(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Type", "text/html; charset=utf-8")
	if s.manifest == nil {
		h.Set("X-Robots-Tag", "noindex")
		w.Write([]byte(s.index))
		return
	}
	route := s.manifest.Match(r.URL.EscapedPath())
	status := http.StatusOK
	if route == nil {
		route, status = &s.manifest.NotFound, http.StatusNotFound
	}
	doc := replaceTitle(s.index, fullTitle(route.Title))
	// A 404 page is never indexed and has no canonical URL, whatever the
	// manifest says about its being public.
	if route.Public && status == http.StatusOK && s.base != "" {
		doc = replaceBlock(doc, headOpen, headClose, s.publicHead(r, route))
	} else {
		h.Set("X-Robots-Tag", "noindex")
	}
	if route.Pattern == "/privacy" {
		doc = replaceBlock(doc, bodyOpen, bodyClose, s.privacy)
	}
	w.WriteHeader(status)
	w.Write([]byte(doc))
}

// canonical is the absolute URL of a public page: its pattern, or for a
// public route with parameters the request's own path.
func (s *site) canonical(r *http.Request, route *Route) string {
	if route.fixed() {
		return s.base + route.Pattern
	}
	return s.base + "/" + strings.Join(segments(r.URL.EscapedPath()), "/")
}

// publicHead is the head block of a public page.
func (s *site) publicHead(r *http.Request, route *Route) string {
	title, desc, canon := fullTitle(route.Title), strings.TrimSpace(route.Description), s.canonical(r, route)
	var b strings.Builder
	// line writes one tag; every %s value is escaped for an attribute.
	line := func(format string, args ...string) {
		esc := make([]any, len(args))
		for i, a := range args {
			esc[i] = html.EscapeString(a)
		}
		fmt.Fprintf(&b, "\n    "+format, esc...)
	}
	line(`<meta name="robots" content="index, follow" />`)
	if desc != "" {
		line(`<meta name="description" content="%s" />`, desc)
	}
	line(`<link rel="canonical" href="%s" />`, canon)
	line(`<meta property="og:type" content="website" />`)
	line(`<meta property="og:site_name" content="%s" />`, siteName)
	line(`<meta property="og:title" content="%s" />`, title)
	if desc != "" {
		line(`<meta property="og:description" content="%s" />`, desc)
	}
	line(`<meta property="og:url" content="%s" />`, canon)
	if s.hasOG {
		line(`<meta property="og:image" content="%s" />`, s.base+"/"+ogImage)
		line(`<meta property="og:image:type" content="image/png" />`)
		line(`<meta property="og:image:width" content="1200" />`)
		line(`<meta property="og:image:height" content="630" />`)
		line(`<meta property="og:image:alt" content="%s" />`, siteName)
		line(`<meta name="twitter:card" content="summary_large_image" />`)
	} else {
		line(`<meta name="twitter:card" content="summary" />`)
	}
	if route.Pattern == "/signin" {
		if ld := s.organisationLD(r); ld != "" {
			b.WriteString("\n    " + `<script type="application/ld+json">` + ld + `</script>`)
		}
	}
	b.WriteString("\n    ")
	return b.String()
}

// organisationLD is the Organization JSON-LD of /signin, from the site
// settings only: nothing until an owner has entered the name (B3.6).
func (s *site) organisationLD(r *http.Request) string {
	if s.org == nil {
		return ""
	}
	o, err := s.org(r)
	if err != nil {
		s.log.Warn("sign-in page: site settings unreadable, JSON-LD left out", "err", err)
		return ""
	}
	if strings.TrimSpace(o.Name) == "" {
		return ""
	}
	ld := map[string]string{"@context": "https://schema.org", "@type": "Organization", "name": o.Name}
	if o.Email != "" {
		ld["email"] = o.Email
	}
	if o.Phone != "" {
		ld["telephone"] = o.Phone
	}
	// json.Marshal escapes <, > and &, so no value can close the script.
	b, _ := json.Marshal(ld)
	return string(b)
}

// robots answers robots.txt: crawling allowed except the API, so a crawler
// can read each private page's noindex; the sitemap named.
func (s *site) robots(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	body := "User-agent: *\nAllow: /\nDisallow: /api/\n"
	if s.base != "" {
		body += "\nSitemap: " + s.base + "/sitemap.xml\n"
	}
	w.Write([]byte(body))
}

// sitemap answers sitemap.xml: every public route without parameters.
func (s *site) sitemap(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, u := range s.sitemapURLs() {
		b.WriteString("  <url><loc>" + html.EscapeString(u) + "</loc></url>\n")
	}
	b.WriteString("</urlset>\n")
	w.Write([]byte(b.String()))
}

func (s *site) sitemapURLs() []string {
	if s.manifest == nil || s.base == "" {
		return nil
	}
	var out []string
	for _, r := range s.manifest.Routes {
		if r.Public && r.fixed() {
			out = append(out, s.base+r.Pattern)
		}
	}
	return out
}
