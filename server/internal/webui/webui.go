// Package webui serves the built Svelte single-page app embedded in the
// server binary. `make build` copies web/dist into dist/ before compiling;
// without it the server still runs and explains how to build the app.
// Hashed files under /assets/ are cached for a year; index.html never is,
// so a release reaches every browser on the next navigation.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler returns the SPA handler. Paths that match a file are served from
// the bundle; every other path gets index.html so client-side routes work
// (the SPA renders its own 404 page).
func Handler() http.Handler {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return handlerFor(dist)
}

func handlerFor(dist fs.FS) http.Handler {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return http.HandlerFunc(notBuilt)
	}
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
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
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
}

func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("The web app is not built into this binary. Run make build, or use make dev for local work.\n"))
}
