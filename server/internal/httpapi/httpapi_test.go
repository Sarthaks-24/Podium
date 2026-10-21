package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
)

func newTestHandler(t *testing.T, web http.Handler) (http.Handler, *bytes.Buffer) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(Deps{DB: d, Log: log, WebApp: web}), &logs
}

func TestHealthzAndHeaders(t *testing.T) {
	h, _ := newTestHandler(t, http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"db":"ok"`) {
		t.Fatalf("healthz: %d %s", rec.Code, rec.Body)
	}
	for _, hdr := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("missing header %s", hdr)
		}
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	h, _ := newTestHandler(t, http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	var body map[string]APIError
	if rec.Code != 404 || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["error"].Code != "not_found" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestPanicIsRecoveredAndLogged(t *testing.T) {
	h, logs := newTestHandler(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/anything", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	l := logs.String()
	if !strings.Contains(l, `"msg":"handler panic"`) || !strings.Contains(l, `"status":500`) {
		t.Errorf("want the panic and the 500 request line logged, got:\n%s", l)
	}
}

// TestHealthzVersionLoopbackOnly: the release is in /healthz only for a
// caller on this machine, never through a proxy (B1.1).
func TestHealthzVersionLoopbackOnly(t *testing.T) {
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	h := New(Deps{DB: d, Log: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), WebApp: http.NotFoundHandler(), Version: "v9.9.9"})
	get := func(remote string, header ...string) map[string]any {
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.RemoteAddr = remote
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	if out := get("127.0.0.1:5000"); out["version"] != "v9.9.9" || !strings.HasPrefix(fmt.Sprint(out["schema"]), "00") {
		t.Errorf("loopback: %v", out)
	}
	if out := get("[::1]:5000"); out["version"] != "v9.9.9" {
		t.Errorf("loopback v6: %v", out)
	}
	if out := get("127.0.0.1:5000", "X-Forwarded-For", "203.0.113.9"); out["version"] != nil || out["schema"] != nil || out["jobs_queued"] != nil || out["status"] != "ok" {
		t.Errorf("through the proxy: %v", out)
	}
	if out := get("203.0.113.9:5000"); out["version"] != nil {
		t.Errorf("remote: %v", out)
	}
}
