// Package webui serves the built Svelte single-page app embedded in the
// server binary. `make build` copies web/dist into dist/ before compiling;
// without it the server still runs and explains how to build the app.
// Hashed files under /assets/ are cached for a year; index.html never is,
// so a release reaches every browser on the next navigation.
//
// From v1.3.0 (D100) the server also delivers everything a crawler reads,
// from the route manifest the web build writes (dist/routes.json, contract
// C2; head.go): a real 404 status for paths no route matches, the
// <!--podium:head--> block per route (public routes get their title,
// description, canonical, Open Graph and Twitter tags and an index rule;
// every other page keeps the default noindex and gains X-Robots-Tag), the
// <!--podium:body--> block of /privacy from dist/privacy-body.html, and
// robots.txt and sitemap.xml.
package webui

import (
	"embed"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

func init() {
	// Types the icons and manifest of the web build need (FP.4) that Go's
	// table may lack on the VM.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
	mime.AddExtensionType(".ico", "image/x-icon")
	mime.AddExtensionType(".woff2", "font/woff2")
}

// Options are what the head needs from the server.
type Options struct {
	// BaseURL is PODIUM_BASE_URL: canonical, Open Graph and sitemap URLs
	// are absolute from it.
	BaseURL string
	// Organisation returns the site settings for the Organization JSON-LD
	// on /signin; nil or an empty name means no JSON-LD.
	Organisation func(*http.Request) (Organisation, error)
	// Log receives a warning when the manifest cannot be used.
	Log *slog.Logger
}

// Organisation is the part of the site settings the JSON-LD uses.
type Organisation struct {
	Name, Email, Phone string
	// Contact is the address people write to: Email, or the owner's
	// when no organisation address is set (the sign-in page's rule).
	Contact string
}

// Handler returns the SPA handler over the embedded build.
func Handler(o Options) http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return handlerFor(dist, o)
}

// buildOnly are files of the web build the server reads and never serves.
var buildOnly = map[string]bool{"routes.json": true, privacyBody: true}

func handlerFor(dist fs.FS, o Options) http.Handler {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return http.HandlerFunc(notBuilt)
	}
	site := newSite(dist, string(index), o)
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		switch name {
		case "robots.txt":
			site.robots(w)
			return
		case "sitemap.xml":
			site.sitemap(w)
			return
		}
		if name != "" && name != "index.html" && !buildOnly[name] && !strings.HasPrefix(path.Base(name), ".") {
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
		site.page(w, r)
	})
}

func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("The web app is not built into this binary. Run make build, or use make dev for local work.\n"))
}
