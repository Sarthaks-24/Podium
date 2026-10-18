// Package httpapi wires podium-server's HTTP routes: the JSON API under
// /api/, the health check, and the embedded web app for everything else.
// It also owns cross-cutting middleware: security headers, the cross-site
// request check, session authentication, panic recovery and request
// logging. Route shapes are documented in docs/API.md.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/integrity"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// Deps are the services the handlers use.
type Deps struct {
	DB     *sql.DB
	Log    *slog.Logger
	WebApp http.Handler
	// Auth applies the identity rules; IdP is the sign-in provider.
	Auth *auth.Service
	IdP  IdentityProvider
	// BaseURL is the public origin, used for cookie flags and the
	// cross-site check.
	BaseURL string
	// SessionSecret signs the sign-in state cookie.
	SessionSecret []byte
	// Problems is the problem bank.
	Problems *problems.Service
	// Submissions runs and judges students' code; Hub carries live
	// updates to browsers.
	Submissions *submissions.Service
	Hub         *events.Hub
	// Contests runs contests: schedules, access, standings, clarifications.
	Contests *contests.Service
	// Integrity runs integrity checks after a contest and keeps the review
	// cases (D47, D49). Its routes need Contests too.
	Integrity *integrity.Service
	// AI is the wizard's in-memory state (M5): pasted keys, one call per
	// admin, wizard runs. Its routes are mounted only when it is set.
	AI *AI

	// reads limits heavy reads per user; New sets it.
	reads *readLimiter
	// Mode is the run mode (testing, dev or production). Testing mode has
	// no Google sign-in; instead anyone at this machine signs in with a
	// form (decision D48).
	Mode string
}

// New returns the root handler.
func New(d Deps) http.Handler {
	d.reads = newReadLimiter()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.healthz)

	mux.HandleFunc("GET /api/auth/config", d.authConfig)
	if d.Mode == "testing" {
		mux.HandleFunc("POST /api/auth/test-signin", d.testSignIn)
	} else {
		mux.HandleFunc("GET /api/auth/google/start", d.googleStart)
		mux.HandleFunc("GET /api/auth/google/callback", d.googleCallback)
	}
	mux.HandleFunc("POST /api/auth/logout", d.logout)
	mux.HandleFunc("GET /api/me", d.requireRole(auth.Student, d.me))
	mux.HandleFunc("PUT /api/me/preferences", d.requireRole(auth.Student, d.setPreferences))

	mux.HandleFunc("GET /api/admin/people", d.requireRole(auth.Owner, d.listPeople))
	mux.HandleFunc("POST /api/admin/people", d.requireRole(auth.Owner, d.grantRole))
	mux.HandleFunc("DELETE /api/admin/people/{id}", d.requireRole(auth.Owner, d.revokeRole))
	mux.HandleFunc("POST /api/admin/accounts/reset-google", d.requireRole(auth.Owner, d.resetGoogleAccount))
	mux.HandleFunc("POST /api/admin/domains", d.requireRole(auth.Owner, d.addDomain))
	mux.HandleFunc("DELETE /api/admin/domains/{domain}", d.requireRole(auth.Owner, d.removeDomain))

	admin := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	mux.HandleFunc("GET /api/admin/problems", admin(d.listProblems))
	mux.HandleFunc("POST /api/admin/problems", admin(d.createProblem))
	mux.HandleFunc("POST /api/admin/problems/import", admin(d.importProblem))
	mux.HandleFunc("GET /api/admin/problems/{id}", admin(d.getProblem))
	mux.HandleFunc("PUT /api/admin/problems/{id}", admin(d.updateProblem))
	mux.HandleFunc("DELETE /api/admin/problems/{id}", admin(d.archiveProblem))
	mux.HandleFunc("PUT /api/admin/problems/{id}/tests", admin(d.replaceTests))
	mux.HandleFunc("GET /api/admin/problems/{id}/tests/{idx}/{part}", admin(d.downloadTest))
	mux.HandleFunc("GET /api/admin/problems/{id}/export", admin(d.exportProblem))
	mux.HandleFunc("POST /api/admin/problems/{id}/validate", admin(d.validateProblem))
	mux.HandleFunc("GET /api/admin/problems/{id}/validation", admin(d.getValidation))
	mux.HandleFunc("POST /api/admin/problems/{id}/generate", admin(d.generateOutputs))
	mux.HandleFunc("GET /api/admin/problems/{id}/generation", admin(d.getGeneration))
	mux.HandleFunc("POST /api/admin/problems/{id}/generate-inputs", admin(d.generateInputs))
	mux.HandleFunc("GET /api/admin/problems/{id}/input-generation", admin(d.getInputGeneration))
	if d.AI != nil {
		d.aiRoutes(mux)
	}

	// The workspace is for organisers trying a problem until contests
	// arrive in M4, which mounts the same handlers under a contest.
	mux.HandleFunc("GET /api/workspace/{problem}", admin(d.limitReads(d.getWorkspace)))
	mux.HandleFunc("PUT /api/workspace/{problem}/draft", admin(d.saveDraft))
	mux.HandleFunc("POST /api/workspace/{problem}/run", admin(d.runCode))
	mux.HandleFunc("POST /api/workspace/{problem}/submit", admin(d.submitCode))
	mux.HandleFunc("POST /api/workspace/{problem}/events", admin(d.recordEvents))
	mux.HandleFunc("GET /api/submissions/{id}", d.requireRole(auth.Student, d.getSubmission))
	mux.HandleFunc("GET /api/runs/{id}", d.requireRole(auth.Student, d.getRun))
	mux.HandleFunc("GET /api/events", d.requireRole(auth.Student, d.eventStream))

	if d.Contests != nil {
		d.contestRoutes(mux)
		if d.Integrity != nil {
			d.integrityRoutes(mux)
		}
	}

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "This API route does not exist.")
	})
	mux.Handle("/", d.WebApp)
	// recoverer sits inside logRequests so a request that panics is still
	// logged with its 500.
	return d.logRequests(d.recoverer(securityHeaders(d.sameOrigin(mux))))
}

// contestRoutes mounts the contest API (milestone M4). Every route needs a
// signed-in user; the handlers apply the contest's own access rules.
func (d Deps) contestRoutes(mux *http.ServeMux) {
	signedIn := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Student, h) }
	mux.HandleFunc("GET /api/contests", signedIn(d.limitReads(d.listContests)))
	mux.HandleFunc("GET /api/contests/{slug}", signedIn(d.limitReads(d.getContest)))
	mux.HandleFunc("POST /api/contests/{slug}/start", signedIn(d.startContest))
	mux.HandleFunc("GET /api/contests/{slug}/problems/{label}", signedIn(d.limitReads(d.getContestWorkspace)))
	mux.HandleFunc("PUT /api/contests/{slug}/problems/{label}/draft", signedIn(d.saveContestDraft))
	mux.HandleFunc("POST /api/contests/{slug}/problems/{label}/run", signedIn(d.runContestCode))
	mux.HandleFunc("POST /api/contests/{slug}/problems/{label}/submit", signedIn(d.submitContestCode))
	mux.HandleFunc("POST /api/contests/{slug}/problems/{label}/events", signedIn(d.recordContestEvents))
	mux.HandleFunc("GET /api/contests/{slug}/standings", signedIn(d.limitReads(d.getStandings)))
	mux.HandleFunc("POST /api/contests/{slug}/clarifications", signedIn(d.askClarification))

	mux.HandleFunc("GET /api/admin/contests", signedIn(d.listAdminContests))
	mux.HandleFunc("POST /api/admin/contests", signedIn(d.createContest))
	mux.HandleFunc("GET /api/admin/contests/{id}", signedIn(d.getAdminContest))
	mux.HandleFunc("PUT /api/admin/contests/{id}", signedIn(d.updateContest))
	mux.HandleFunc("DELETE /api/admin/contests/{id}", signedIn(d.archiveContest))
	mux.HandleFunc("POST /api/admin/contests/{id}/clone", signedIn(d.cloneContest))
	mux.HandleFunc("PUT /api/admin/contests/{id}/problems", signedIn(d.setContestProblems))
	mux.HandleFunc("GET /api/admin/contests/{id}/roster", signedIn(d.getRoster))
	mux.HandleFunc("PUT /api/admin/contests/{id}/roster", signedIn(d.setRoster))
	mux.HandleFunc("POST /api/admin/contests/{id}/admins", signedIn(d.changeContestAdmin(true)))
	mux.HandleFunc("DELETE /api/admin/contests/{id}/admins", signedIn(d.changeContestAdmin(false)))
	mux.HandleFunc("PUT /api/admin/contests/{id}/extensions/{user}", signedIn(d.setExtension))
	mux.HandleFunc("POST /api/admin/contests/{id}/extend", signedIn(d.extendContest))
	mux.HandleFunc("POST /api/admin/contests/{id}/pause", signedIn(d.pauseContest(true)))
	mux.HandleFunc("POST /api/admin/contests/{id}/resume", signedIn(d.pauseContest(false)))
	mux.HandleFunc("GET /api/admin/contests/{id}/submissions", signedIn(d.contestFeed))
	mux.HandleFunc("POST /api/admin/contests/{id}/rejudge", signedIn(d.rejudge))
	mux.HandleFunc("GET /api/admin/contests/{id}/announcements", signedIn(d.listAnnouncements))
	mux.HandleFunc("POST /api/admin/contests/{id}/announcements", signedIn(d.postAnnouncement))
	mux.HandleFunc("GET /api/admin/contests/{id}/clarifications", signedIn(d.listClarifications))
	mux.HandleFunc("PUT /api/admin/contests/{id}/clarifications/{cid}", signedIn(d.answerClarification))
	mux.HandleFunc("GET /api/admin/contests/{id}/standings", signedIn(d.adminStandings))
	mux.HandleFunc("GET /api/admin/contests/{id}/reveal", signedIn(d.getReveal))
	mux.HandleFunc("POST /api/admin/contests/{id}/reveal", signedIn(d.postReveal))
	mux.HandleFunc("GET /api/admin/contests/{id}/export.csv", signedIn(d.exportCSV))
	mux.HandleFunc("GET /api/admin/contests/{id}/export.json", signedIn(d.exportJSON))
	mux.HandleFunc("GET /api/admin/contests/{id}/stats", signedIn(d.contestStats))
	mux.HandleFunc("PUT /api/admin/contests/{id}/editorial", signedIn(d.setEditorial))
}

// internalError logs err and answers with a generic 500.
func (d Deps) internalError(w http.ResponseWriter, r *http.Request, err error) {
	d.Log.Error("request failed", "path", r.URL.Path, "err", err)
	WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server. Try again; if it keeps happening, tell an organiser.")
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
// logged because OAuth callbacks carry codes in them, and request bodies
// never are: PUT /api/admin/ai/key carries a provider key (CLAUDE.md 3a).
// A test checks that the key never reaches this log.
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
