package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
