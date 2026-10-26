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
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contact"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/db/migrations"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/integrity"
	"github.com/Sarthaks-24/podium/internal/notifications"
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
	// Contact keeps the contact form's messages (C6). New makes one from
	// DB and Auth when it is not set.
	Contact *contact.Service
	// Notifications is the inbox (C9). New makes one from DB, Contests and
	// Integrity when it is not set.
	Notifications *notifications.Service

	// reads limits heavy reads per user; New sets it.
	reads *readLimiter
	// preAuthLimit is the per-address budget before sign-in and flows the
	// sign-in attempts already finished (preauth.go). New sets both;
	// tests may set preAuthLimit first.
	preAuthLimit  *ipLimiter
	signInLimiter *ipLimiter
	authFailLimit *ipLimiter
	flows         *usedFlows
	// Mode is the run mode (testing, dev or production). Testing mode has
	// no Google sign-in; instead anyone at this machine signs in with a
	// form (decision D48).
	Mode string
	// Version is the server's release version, shown in the metrics.
	Version string
}

// New returns the root handler.
func New(d Deps) http.Handler {
	d.reads = newReadLimiter()
	if d.preAuthLimit == nil {
		d.preAuthLimit = newIPLimiter(PreAuthBurst, PreAuthRate, nil)
	}
	if d.signInLimiter == nil {
		d.signInLimiter = newIPLimiter(SignInBurst, SignInRate, nil)
	}
	if d.authFailLimit == nil {
		d.authFailLimit = newIPLimiter(AuthFailBurst, AuthFailRate, nil)
	}
	d.flows = newUsedFlows()
	if d.Contact == nil {
		d.Contact = &contact.Service{DB: d.DB, Auth: d.Auth}
		if d.Contests != nil {
			d.Contact.Now = d.Contests.Clock
		}
	}
	if d.Notifications == nil && d.DB != nil {
		d.Notifications = &notifications.Service{DB: d.DB}
		if d.Contests != nil {
			d.Notifications.Now, d.Notifications.Live = d.Contests.Clock, d.Contests.InLiveContest
		}
		if d.Integrity != nil {
			d.Notifications.Due = d.Integrity.NoticeDue
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.preAuth(d.healthz))

	mux.HandleFunc("GET /api/auth/config", d.preAuth(d.authConfig))
	if d.Mode == "testing" {
		mux.HandleFunc("POST /api/auth/test-signin", d.signInLimit(false, d.testSignIn))
	} else {
		mux.HandleFunc("GET /api/auth/google/start", d.signInLimit(true, d.googleStart))
		mux.HandleFunc("GET /api/auth/google/callback", d.signInLimit(true, d.googleCallback))
	}
	mux.HandleFunc("POST /api/auth/logout", d.preAuth(d.logout))
	mux.HandleFunc("GET /api/me", d.requireRole(auth.Student, d.me))
	mux.HandleFunc("PUT /api/me/preferences", d.requireRole(auth.Student, d.setPreferences))

	mux.HandleFunc("GET /api/admin/people", d.requireRole(auth.Owner, d.listPeople))
	mux.HandleFunc("POST /api/admin/people", d.requireRole(auth.Owner, d.grantRole))
	mux.HandleFunc("DELETE /api/admin/people/{id}", d.requireRole(auth.Owner, d.revokeRole))
	mux.HandleFunc("POST /api/admin/accounts/reset-google", d.requireRole(auth.Owner, d.resetGoogleAccount))
	d.securityRoutes(mux)
	mux.HandleFunc("POST /api/admin/domains", d.requireRole(auth.Owner, d.addDomain))
	mux.HandleFunc("DELETE /api/admin/domains/{domain}", d.requireRole(auth.Owner, d.removeDomain))
	mux.HandleFunc("GET /api/admin/site-audit", d.requireRole(auth.Owner, d.siteAudit))
	mux.HandleFunc("PUT /api/admin/site-settings", d.requireRole(auth.Owner, d.setSiteSettings))
	mux.HandleFunc("POST /api/admin/rollover", d.requireRole(auth.Owner, d.semesterRollover))
	mux.HandleFunc("POST /api/contact", d.requireRole(auth.Student, d.sendContact))
	mux.HandleFunc("GET /api/admin/contact", d.requireRole(auth.Admin, d.listContact))
	mux.HandleFunc("DELETE /api/admin/contact/{id}", d.requireRole(auth.Admin, d.deleteContact))

	admin := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	mux.HandleFunc("GET /api/admin/problems", admin(d.listProblems))
	mux.HandleFunc("POST /api/admin/problems", admin(d.createProblem))
	mux.HandleFunc("POST /api/admin/problems/import", admin(slowBody(d.importProblem)))
	mux.HandleFunc("GET /api/admin/problems/{id}", admin(d.getProblem))
	mux.HandleFunc("PUT /api/admin/problems/{id}", admin(d.updateProblem))
	mux.HandleFunc("DELETE /api/admin/problems/{id}", admin(d.archiveProblem))
	mux.HandleFunc("PUT /api/admin/problems/{id}/tests", admin(slowBody(d.replaceTests)))
	mux.HandleFunc("GET /api/admin/problems/{id}/tests/{idx}/{part}", admin(d.downloadTest))
	mux.HandleFunc("GET /api/admin/problems/{id}/export", admin(d.exportProblem))
	mux.HandleFunc("POST /api/admin/problems/{id}/validate", admin(d.validateProblem))
	mux.HandleFunc("GET /api/admin/problems/{id}/validation", admin(d.getValidation))
	mux.HandleFunc("POST /api/admin/problems/{id}/generate", admin(d.generateOutputs))
	mux.HandleFunc("GET /api/admin/problems/{id}/generation", admin(d.getGeneration))
	mux.HandleFunc("POST /api/admin/problems/{id}/generate-inputs", admin(d.generateInputs))
	mux.HandleFunc("GET /api/admin/problems/{id}/input-generation", admin(d.getInputGeneration))
	mux.HandleFunc("GET /api/admin/problems/{id}/revisions/diff", admin(d.diffProblemRevisions))
	mux.HandleFunc("GET /api/admin/problems/{id}/revisions/{rev}", admin(d.getProblemRevision))
	if d.AI != nil {
		d.aiRoutes(mux)
	}

	// The organisers' workspace: an admin tries a problem from the bank
	// ("Try as a student"). Students reach the same workspace through the
	// contest routes under /api/contests/{slug}/problems/{label} below.
	mux.HandleFunc("GET /api/workspace/{problem}", admin(d.limitReads(d.getWorkspace)))
	mux.HandleFunc("PUT /api/workspace/{problem}/draft", admin(d.saveDraft))
	mux.HandleFunc("POST /api/workspace/{problem}/run", admin(d.runCode))
	mux.HandleFunc("POST /api/workspace/{problem}/submit", admin(d.submitCode))
	mux.HandleFunc("POST /api/workspace/{problem}/events", admin(d.recordEvents))
	mux.HandleFunc("GET /api/submissions/{id}", d.requireRole(auth.Student, d.getSubmission))
	mux.HandleFunc("GET /api/runs/{id}", d.requireRole(auth.Student, d.getRun))
	if d.Submissions != nil && d.Submissions.Jobs != nil {
		mux.HandleFunc("GET /api/admin/metrics", admin(d.metrics))
	}
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
	// logged with its 500. In testing mode localOnlyAll refuses anything
	// that did not come from this machine's own browser (S2).
	return d.logRequests(d.recoverer(securityHeaders(d.localOnlyAll(newInFlight().limit(d.sessionCookieName(), d.sameOrigin(d.renamedContest(mux)))))))
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
	mux.HandleFunc("GET /api/contests/{slug}/problems/{label}/revisions", signedIn(d.limitReads(d.statementHistory)))
	mux.HandleFunc("GET /api/contests/{slug}/standings", signedIn(d.limitReads(d.getStandings)))
	mux.HandleFunc("POST /api/contests/{slug}/clarifications", signedIn(d.askClarification))

	mux.HandleFunc("GET /api/admin/contests", signedIn(d.listAdminContests))
	mux.HandleFunc("POST /api/admin/contests", signedIn(d.createContest))
	mux.HandleFunc("GET /api/admin/contests/{id}", signedIn(d.getAdminContest))
	mux.HandleFunc("PUT /api/admin/contests/{id}", signedIn(d.updateContest))
	mux.HandleFunc("DELETE /api/admin/contests/{id}", signedIn(d.archiveContest))
	mux.HandleFunc("POST /api/admin/contests/{id}/clone", signedIn(d.cloneContest))
	mux.HandleFunc("PUT /api/admin/contests/{id}/problems", signedIn(d.setContestProblems))
	mux.HandleFunc("PUT /api/admin/contests/{id}/problems/{label}/revision", signedIn(d.updatePin))
	mux.HandleFunc("GET /api/admin/contests/{id}/roster", signedIn(d.getRoster))
	mux.HandleFunc("PUT /api/admin/contests/{id}/roster", signedIn(slowBody(d.setRoster)))
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

// notices is the inbox service, nil without a database.
func (d Deps) notices() *notifications.Service { return d.Notifications }

// now is the server's clock: the contests service's, which tests replace.
func (d Deps) now() time.Time {
	if d.Contests != nil {
		return d.Contests.Clock()
	}
	return time.Now()
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
	out := map[string]any{"status": "ok", "db": "ok"}
	if loopbackRequest(r) {
		// Only a caller on this machine learns the release (install.sh
		// verify, the deploy scripts): through Caddy the version stays
		// private, so a scanner cannot match it against known bugs.
		out["version"], out["schema"] = d.Version, migrations.Latest()
		// The judge queue, for podium-check's stalled-judge alert (council
		// C1-048): jobs waiting, the oldest one's age, and how long since
		// a judge last asked for work (null when none has since start).
		var queued, oldest int64
		if err := d.DB.QueryRowContext(ctx, "SELECT count(*), coalesce(min(created_at), 0) FROM jobs WHERE state = 'queued'").
			Scan(&queued, &oldest); err == nil {
			now := time.Now()
			out["jobs_queued"] = queued
			out["oldest_queued_s"] = int64(0)
			if queued > 0 {
				out["oldest_queued_s"] = max(0, int64(now.Sub(time.UnixMilli(oldest)).Seconds()))
			}
			out["judge_last_claim_s"] = nil
			if d.Problems != nil && d.Problems.Jobs != nil {
				if at, ok := d.Problems.Jobs.LastClaimAt(); ok {
					out["judge_last_claim_s"] = max(0, int64(now.Sub(at).Seconds()))
				}
			}
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

// loopbackRequest reports whether the request came straight from this
// machine: a loopback peer and none of a proxy's forwarding headers
// (Caddy on the same host connects from loopback and always adds
// X-Forwarded-For).
func loopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, h := range forwardedHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
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
