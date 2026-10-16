// Package httpapi wires podium-server's HTTP routes: the JSON API under
// /api/, the health check, and the embedded web app for everything else.
// It also owns cross-cutting middleware: security headers, panic recovery
// and request logging. Route shapes are documented in docs/API.md.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Deps are the services the handlers use.
type Deps struct {
	DB     *sql.DB
	Log    *slog.Logger
	WebApp http.Handler
}

// New returns the root handler.
func New(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.healthz)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "This API route does not exist.")
	})
	mux.Handle("/", d.WebApp)
	// recoverer sits inside logRequests so a request that panics is still
	// logged with its 500.
	return d.logRequests(d.recoverer(securityHeaders(mux)))
}

func (d Deps) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := d.DB.PingContext(ctx); err != nil {
		d.Log.Error("health check: database unreachable", "err", err)
		WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "db": "unreachable"})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "db": "ok"})
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// APIError is the body of every error response: a stable machine code and
// a sentence for people that says what happened and what to do.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes an APIError.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]APIError{"error": {Code: code, Message: message}})
}

// contentSecurityPolicy allows only same-origin code. Google avatars are
// the one external image source; inline styles are needed by KaTeX.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https://lh3.googleusercontent.com; font-src 'self'; connect-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self' https://accounts.google.com; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush keeps server-sent event streams working through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logRequests logs server errors at warn and everything else at debug, so
// a 700-student contest does not flood the journal. Query strings are not
// logged because OAuth callbacks carry codes in them.
func (d Deps) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		if rec.status >= 500 {
			level = slog.LevelWarn
		}
		d.Log.Log(r.Context(), level, "request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

func (d Deps) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				d.Log.Error("handler panic", "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server. Try again; if it keeps happening, tell an organiser.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
