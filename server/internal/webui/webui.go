// Package webui serves the built Svelte single-page app embedded in the
// server binary. `make build` copies web/dist into dist/ before compiling;
// without it the server still runs and explains how to build the app.
// Hashed files under /assets/ are cached for a year; index.html never is,
// so a release reaches every browser on the next navigation. A path the
// app has no screen for gets the app with status 404 (V4-9). / without a
// session cookie is the prerendered landing page (v4 P2, V4-8), the one page
// with no noindex; every other page is a college tool behind sign-in and
// says noindex. robots.txt still disallows everything until P3.
package webui

import (
	"bytes"
	"embed"
	"html"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

func init() {
	// Types the icons and fonts of the web build need that Go's table may
	// lack on the VM.
	mime.AddExtensionType(".ico", "image/x-icon")
	mime.AddExtensionType(".woff2", "font/woff2")
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Handler returns the SPA handler over the embedded build; base is the
// public URL (config BaseURL), for the landing page's links, robots.txt
// and the sitemap.
func Handler(base string) http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return handlerFor(base, dist)
}

// SessionCookies are the session cookie's two names (httpapi
// sessionCookieName: __Host- prefixed on https); TestSessionCookieNames in
// httpapi keeps them in step.
var SessionCookies = []string{"__Host-podium_session", "podium_session"}

func hasSession(r *http.Request) bool {
	return slices.ContainsFunc(SessionCookies, func(name string) bool {
		_, err := r.Cookie(name)
		return err == nil
	})
}

func handlerFor(base string, dist fs.FS) http.Handler {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return http.HandlerFunc(notBuilt)
	}
	// The prerendered landing page (v4 P2), when the build made one.
	landing, _ := fs.ReadFile(dist, "landing.html")
	// The prerender leaves the site address as a token (V4-7): filled once here.
	landing = bytes.ReplaceAll(landing, []byte("%BASE_URL%"), []byte(base))
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// One address per page: a trailing slash goes to the cleaned path,
		// which never starts with "//". A browser reads a backslash as a slash
		// and drops tabs and newlines, so "/\evil.com" would leave the site:
		// no page has those characters, and such a path gets the 404 instead.
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") && !strings.ContainsFunc(name, offSite) {
			to := "/" + name
			if r.URL.RawQuery != "" {
				to += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, to, http.StatusMovedPermanently)
			return
		}
		// Pages v2 had outside /admin/ and /contests/, in case one is bookmarked.
		if slices.Contains(retiredPaths, name) || strings.HasPrefix(name, "integrity/") {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
			return
		}
		// v4 P3: only the landing page is for search engines ("/$" is longer
		// than "/", so it wins for / alone). Privacy joins once its text is
		// approved and it is prerendered; until then it says noindex.
		if name == "robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write([]byte("User-agent: *\nAllow: /$\nDisallow: /\nSitemap: " + base + "/sitemap.xml\n"))
			return
		}
		if name == "sitemap.xml" {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
				`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>` + html.EscapeString(base+"/") + "</loc></url></urlset>\n"))
			return
		}
		if name != "" && name != "index.html" && !strings.HasPrefix(path.Base(name), ".") {
			if st, err := fs.Stat(dist, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// A missing file (an old hashed chunk after a deploy, a font, a
		// favicon) is a real 404: serving index.html as JavaScript would fail
		// with a confusing MIME error under nosniff. Only extension-less
		// paths are client-side routes.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if name == "" {
			// V4-8: / is the landing page without a session cookie and the
			// app with one (a stale cookie gets the app, which shows the
			// landing page itself), so no cache may keep either for the other.
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Add("Vary", "Cookie")
			if landing != nil && !hasSession(r) {
				w.Write(landing)
				return
			}
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		w.Header().Set("X-Robots-Tag", "noindex")
		// Any other path still gets the app, which draws "Page not found",
		// but with the status crawlers and link checkers read.
		if !appRoute("/" + name) {
			w.WriteHeader(http.StatusNotFound)
		}
		w.Write(index)
	})
}

// appPaths and appPrefixes are the paths the web app's router has a screen
// for (web/src/lib/routing.ts); TestRoutesMatchRouter keeps them in step.
var (
	appPaths    = []string{"/", "/signin", "/privacy", "/me"}
	appPrefixes = []string{"/contests/", "/admin/"}

	retiredPaths = []string{"progress", "notices", "contact", "integrity"}
)

func appRoute(p string) bool {
	if slices.Contains(appPaths, p) {
		return true
	}
	return slices.ContainsFunc(appPrefixes, func(pre string) bool { return strings.HasPrefix(p, pre) })
}

// offSite reports a character that makes a browser read a redirect target
// differently from the server: a backslash, or a control character.
func offSite(r rune) bool { return r == '\\' || r < 0x20 || r == 0x7f }

func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("The web app is not built into this binary. Run make build, or use make dev for local work.\n"))
}
