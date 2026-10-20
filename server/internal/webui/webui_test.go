package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	h := handlerFor(fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html><title>podium</title>")},
		"assets/app-abc.js":  {Data: []byte("console.log(1)")},
		"favicon.svg":        {Data: []byte("<svg/>")},
		".keep":              {Data: nil},
		"assets/.hidden.txt": {Data: []byte("x")},
	}, Options{})
	cases := []struct {
		method, path string
		status       int
		cache        string
	}{
		{"GET", "/", 200, "no-cache"},
		{"GET", "/contests/week-6", 200, "no-cache"},
		{"HEAD", "/contests/week-6", 200, "no-cache"},
		{"GET", "/assets/app-abc.js", 200, "public, max-age=31536000, immutable"},
		{"GET", "/favicon.svg", 200, "public, max-age=3600"},
		{"GET", "/assets/app-old.js", 404, ""},
		{"GET", "/fonts/missing.woff2", 404, ""},
		{"GET", "/.keep", 404, ""},
		{"GET", "/assets/.hidden.txt", 404, ""},
		{"POST", "/", 405, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, rec.Code, c.status)
		}
		if c.cache != "" && rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s %s: Cache-Control %q, want %q", c.method, c.path, rec.Header().Get("Cache-Control"), c.cache)
		}
	}
}

func TestHandlerNotBuilt(t *testing.T) {
	rec := httptest.NewRecorder()
	handlerFor(fstest.MapFS{}, Options{}).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", rec.Code)
	}
}
