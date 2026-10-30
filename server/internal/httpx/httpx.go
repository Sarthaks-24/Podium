// Package httpx holds the small HTTP pieces every handler package shares:
// JSON answers and the error body, request body decoding, the client's
// address, and the request log and panic recovery middleware. It imports
// no domain package, so the domains' handlers and the router in httpapi
// can all use it.
package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

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
	// Fields maps a form field to what is wrong with it.
	Fields map[string]string `json:"fields,omitempty"`
	// Problems lists everything wrong with an upload or a request to
	// validate, so it can be fixed in one pass.
	Problems []string `json:"problems,omitempty"`
}

// WriteError writes an APIError.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]APIError{"error": {Code: code, Message: message}})
}

// internalMessage is what a person sees when the server failed.
const internalMessage = "Something went wrong on the server. Try again; if it keeps happening, tell an organiser."

// InternalError logs err and answers with a generic 500.
func InternalError(log *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	log.Error("request failed", "path", r.URL.Path, "err", err)
	WriteError(w, http.StatusInternalServerError, "internal", internalMessage)
}

// WriteBusy refuses a request because its one slot is taken: 429 with code
// "busy" and a Retry-After of secs, the same shape as the rate limits, so a
// client retries every "busy" the same way.
func WriteBusy(w http.ResponseWriter, secs int, message string) {
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	WriteJSON(w, http.StatusTooManyRequests, map[string]map[string]any{"error": {
		"code": "busy", "message": message, "retry_after_s": secs}})
}

// MaxBody bounds every JSON request body.
const MaxBody = 64 << 10

// DecodeJSON reads a JSON body into v and writes a 400 on failure. It
// requires Content-Type application/json, which a plain cross-site HTML
// form cannot send.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return DecodeJSONLimit(w, r, v, MaxBody)
}

// DecodeJSONLimit is DecodeJSON with another body limit.
func DecodeJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, "bad_request", "Send the request body as application/json.")
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		WriteError(w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON for this action.")
		return false
	}
	return true
}

// ParseID reads a positive id from a path value.
func ParseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// Millis is t in Unix milliseconds, or nil for the zero time.
func Millis(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

// NonNil turns a nil list into an empty one, so it encodes as [].
func NonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Ptr returns a pointer to a copy of v.
func Ptr[T any](v T) *T { return &v }

// Attachment sets the headers of a file download.
func Attachment(w http.ResponseWriter, contentType, name string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
}

// ForwardedHeaders are set by reverse proxies. In testing mode any of them
// means the request did not come from this machine's own browser: Caddy
// (or any other proxy) in front of a testing-mode server must never work.
var ForwardedHeaders = []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip", "X-Forwarded-Host"}

// Loopback reports whether the request came straight from this machine: a
// loopback peer and none of a proxy's forwarding headers (Caddy on the same
// host connects from loopback and always adds X-Forwarded-For).
func Loopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, h := range ForwardedHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
}

// ClientIP is the address recorded with a session. Behind Caddy on the
// same host the peer is loopback, so the forwarded address is used then.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			if p := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); p != nil {
				return p.String()
			}
		}
	}
	return host
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

// LogRequests logs server errors at warn and everything else at debug, so
// a 700-student contest does not flood the journal. Query strings are not
// logged because OAuth callbacks carry codes in them, and request bodies
// never are: PUT /api/admin/ai/key carries a provider key (CLAUDE.md 3a).
// A test checks that the key never reaches this log.
func LogRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		if rec.status >= 500 {
			level = slog.LevelWarn
		}
		log.Log(r.Context(), level, "request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

// Recover answers a handler's panic with a logged 500. It sits inside
// LogRequests, so a request that panics is still logged with its status.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("handler panic", "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				WriteError(w, http.StatusInternalServerError, "internal", internalMessage)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
