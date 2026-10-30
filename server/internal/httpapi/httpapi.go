// Package httpapi wires podium-server's HTTP routes: the JSON API under
// /api/, the health check, and the embedded web app for everything else.
// It holds the route table, with the role each route needs, and the
// cross-cutting middleware: security headers, the cross-site request
// check, sign-in and session authentication, rate limits, panic recovery
// and request logging. The handlers live next to their domain (http.go in
// auth, problems, submissions, contests, integrity and ai; ops for the
// health check and metrics). Route shapes are documented in docs/API.md.
package httpapi

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/integrity"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/ops"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// Deps are the services the routes use.
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
	// cases. Its routes need Contests too.
	Integrity *integrity.Service
	// AI is the wizard's in-memory state: pasted keys, one call per
	// admin, wizard runs. Its routes are mounted only when it is set.
	AI *ai.Wizard

	// reads limits heavy reads per user; New sets it.
	reads *readLimiter
	// preAuthLimit is the per-address budget before sign-in and flows the
	// sign-in attempts already finished (preauth.go). New sets both;
	// tests may set preAuthLimit first.
	preAuthLimit  *ipLimiter
	signInLimiter *ipLimiter
	authFailLimit *ipLimiter
	known         *knownSessions
	flows         *usedFlows
	// Mode is the run mode (testing, dev or production). Testing mode has
	// no Google sign-in; instead anyone at this machine signs in with a
	// form.
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
	if d.known == nil {
		d.known = newKnownSessions(nil)
	}
	d.flows = newUsedFlows()

	health := &ops.Handlers{DB: d.DB, Log: d.Log, Jobs: d.queue(), Hub: d.Hub, Version: d.Version}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", d.preAuth(health.Health))

	mux.HandleFunc("GET /api/auth/config", d.preAuth(d.authConfig))
	if d.Mode == "testing" {
		mux.HandleFunc("POST /api/auth/test-signin", d.signInLimit(false, d.testSignIn))
	} else {
		mux.HandleFunc("GET /api/auth/google/start", d.signInLimit(true, d.googleStart))
		mux.HandleFunc("GET /api/auth/google/callback", d.signInLimit(true, d.googleCallback))
	}
	mux.HandleFunc("POST /api/auth/logout", d.preAuth(d.logout))
	people := &auth.Handlers{Auth: d.Auth, Log: d.Log, Now: d.now}
	mux.HandleFunc("GET /api/me", d.requireRole(auth.Student, people.Me))
	mux.HandleFunc("PUT /api/me/preferences", d.requireRole(auth.Student, people.SetPreferences))

	owner := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Owner, h) }
	mux.HandleFunc("GET /api/admin/people", owner(people.ListPeople))
	mux.HandleFunc("POST /api/admin/people", owner(people.GrantRole))
	mux.HandleFunc("DELETE /api/admin/people/{id}", owner(people.RevokeRole))
	mux.HandleFunc("POST /api/admin/accounts/reset-google", owner(people.ResetGoogleAccount))
	mux.HandleFunc("POST /api/admin/accounts/end-sessions", owner(people.EndSessions))
	mux.HandleFunc("POST /api/admin/accounts/disable", owner(people.SetAccountDisabled(true)))
	mux.HandleFunc("POST /api/admin/accounts/enable", owner(people.SetAccountDisabled(false)))
	mux.HandleFunc("GET /api/admin/accounts", d.requireRole(auth.Admin, d.limitReads(people.LookupAccount)))
	mux.HandleFunc("POST /api/admin/domains", owner(people.AddDomain))
	mux.HandleFunc("DELETE /api/admin/domains/{domain}", owner(people.RemoveDomain))
	mux.HandleFunc("GET /api/admin/site-audit", owner(people.SiteAudit))
	mux.HandleFunc("PUT /api/admin/site-settings", owner(people.SetSiteSettings))

	admin := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	bank := &problems.Handlers{Problems: d.Problems, Log: d.Log}
	work := &submissions.Handlers{Submissions: d.Submissions, Log: d.Log}
	mux.HandleFunc("GET /api/admin/problems", admin(bank.ListProblems))
	mux.HandleFunc("POST /api/admin/problems", admin(bank.CreateProblem))
	mux.HandleFunc("POST /api/admin/problems/import", admin(slowBody(bank.ImportProblem)))
	mux.HandleFunc("GET /api/admin/problems/{id}", admin(bank.GetProblem))
	mux.HandleFunc("PUT /api/admin/problems/{id}", admin(bank.UpdateProblem))
	mux.HandleFunc("DELETE /api/admin/problems/{id}", admin(bank.ArchiveProblem))
	mux.HandleFunc("PUT /api/admin/problems/{id}/tests", admin(slowBody(bank.ReplaceTests)))
	mux.HandleFunc("GET /api/admin/problems/{id}/tests/{idx}/{part}", admin(bank.DownloadTest))
	mux.HandleFunc("GET /api/admin/problems/{id}/export", admin(bank.ExportProblem))
	mux.HandleFunc("POST /api/admin/problems/{id}/validate", admin(bank.ValidateProblem))
	mux.HandleFunc("GET /api/admin/problems/{id}/validation", admin(bank.GetValidation))
	mux.HandleFunc("POST /api/admin/problems/{id}/generate", admin(bank.GenerateOutputs))
	mux.HandleFunc("GET /api/admin/problems/{id}/generation", admin(bank.GetGeneration))
	mux.HandleFunc("POST /api/admin/problems/{id}/generate-inputs", admin(bank.GenerateInputs))
	mux.HandleFunc("GET /api/admin/problems/{id}/input-generation", admin(bank.GetInputGeneration))
	if d.AI != nil {
		wizard := &ai.Handlers{Wizard: d.AI, Problems: d.Problems, Hub: d.Hub, Log: d.Log,
			Testing: d.Mode == "testing", SessionToken: d.sessionToken}
		mux.HandleFunc("GET /api/admin/ai/providers", admin(wizard.GetAIProviders))
		mux.HandleFunc("GET /api/admin/ai/models", admin(wizard.GetAIModels))
		mux.HandleFunc("GET /api/admin/ai/key", admin(wizard.GetAIKey))
		mux.HandleFunc("PUT /api/admin/ai/key", admin(wizard.PutAIKey))
		mux.HandleFunc("DELETE /api/admin/ai/key", admin(wizard.DeleteAIKey))
		mux.HandleFunc("POST /api/admin/ai/wizard", admin(wizard.StartWizard))
		mux.HandleFunc("GET /api/admin/ai/wizard/{id}", admin(wizard.GetWizard))
	}

	// The organisers' workspace: an admin tries a problem from the bank
	// ("Try as a student"). Students reach the same workspace through the
	// contest routes under /api/contests/{slug}/problems/{label} below.
	mux.HandleFunc("GET /api/workspace/{problem}", admin(d.limitReads(work.GetWorkspace)))
	mux.HandleFunc("PUT /api/workspace/{problem}/draft", admin(work.SaveDraft))
	mux.HandleFunc("POST /api/workspace/{problem}/run", admin(work.RunCode))
	mux.HandleFunc("POST /api/workspace/{problem}/submit", admin(work.SubmitCode))
	mux.HandleFunc("POST /api/workspace/{problem}/events", admin(work.RecordEvents))
	mux.HandleFunc("GET /api/submissions/{id}", d.requireRole(auth.Student, work.GetSubmission))
	mux.HandleFunc("GET /api/runs/{id}", d.requireRole(auth.Student, work.GetRun))
	if d.Submissions != nil && d.Submissions.Jobs != nil {
		mux.HandleFunc("GET /api/admin/metrics", admin(health.Metrics))
	}
	mux.HandleFunc("GET /api/events", d.requireRole(auth.Student, d.eventStream))

	if d.Contests != nil {
		console := &contests.Handlers{Contests: d.Contests, Submissions: d.Submissions, Problems: d.Problems, Log: d.Log}
		d.contestRoutes(mux, console)
		mux.HandleFunc("GET /api/admin/contests/{id}/audit", admin(console.ContestAudit))
		if d.Integrity != nil {
			review := &integrity.Handlers{Integrity: d.Integrity, Contests: d.Contests, Organised: console.OrganisedContest, Log: d.Log}
			mux.HandleFunc("POST /api/admin/contests/{id}/integrity/run", admin(review.RunIntegrity))
			mux.HandleFunc("GET /api/admin/contests/{id}/integrity", admin(d.limitReads(review.GetIntegrity)))
			mux.HandleFunc("GET /api/admin/contests/{id}/integrity/pairs.csv", admin(d.limitReads(review.ExportPairs)))
			mux.HandleFunc("GET /api/admin/contests/{id}/integrity/signals/{signal}/pair", admin(d.limitReads(review.GetIntegrityPair)))
			mux.HandleFunc("GET /api/admin/contests/{id}/integrity/timeline/{user}", admin(d.limitReads(review.GetIntegrityTimeline)))
			mux.HandleFunc("GET /api/admin/contests/{id}/integrity/counts", admin(d.limitReads(review.GetIntegrityCounts)))
			mux.HandleFunc("GET /api/admin/contests/{id}/integrity/cases", admin(d.limitReads(review.ListIntegrityCases)))
			mux.HandleFunc("PUT /api/admin/contests/{id}/integrity/cases/{user}", admin(review.DecideIntegrityCase))
		}
	}

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "This API route does not exist.")
	})
	mux.Handle("/", d.WebApp)
	// Recover sits inside LogRequests so a request that panics is still
	// logged with its 500. In testing mode localOnlyAll refuses anything
	// that did not come from this machine's own browser.
	return httpx.LogRequests(d.Log, httpx.Recover(d.Log, securityHeaders(d.localOnlyAll(newInFlight().limit(d.sessionCookieName(), d.sameOrigin(mux))))))
}

// contestRoutes mounts the contest API. Student routes need
// a signed-in user and apply the contest's own access rules; organiser
// routes need the admin or owner role.
func (d Deps) contestRoutes(mux *http.ServeMux, console *contests.Handlers) {
	signedIn := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Student, h) }
	organiser := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	mux.HandleFunc("GET /api/contests", signedIn(d.limitReads(console.ListContests)))
	mux.HandleFunc("GET /api/contests/{slug}", signedIn(d.limitReads(console.GetContest)))
	mux.HandleFunc("POST /api/contests/{slug}/start", signedIn(console.StartContest))
	mux.HandleFunc("GET /api/contests/{slug}/problems/{label}", signedIn(d.limitReads(console.GetContestWorkspace)))
	mux.HandleFunc("PUT /api/contests/{slug}/problems/{label}/draft", signedIn(console.SaveContestDraft))
	mux.HandleFunc("POST /api/contests/{slug}/problems/{label}/run", signedIn(console.RunContestCode))
	mux.HandleFunc("POST /api/contests/{slug}/problems/{label}/submit", signedIn(console.SubmitContestCode))
	mux.HandleFunc("POST /api/contests/{slug}/problems/{label}/events", signedIn(console.RecordContestEvents))
	mux.HandleFunc("GET /api/contests/{slug}/standings", signedIn(d.limitReads(console.GetStandings)))
	mux.HandleFunc("POST /api/contests/{slug}/clarifications", signedIn(console.AskClarification))

	mux.HandleFunc("GET /api/admin/contests", organiser(console.ListAdminContests))
	mux.HandleFunc("POST /api/admin/contests", organiser(console.CreateContest))
	mux.HandleFunc("GET /api/admin/contests/{id}", organiser(console.GetAdminContest))
	mux.HandleFunc("PUT /api/admin/contests/{id}", organiser(console.UpdateContest))
	mux.HandleFunc("DELETE /api/admin/contests/{id}", organiser(console.ArchiveContest))
	mux.HandleFunc("POST /api/admin/contests/{id}/clone", organiser(console.CloneContest))
	mux.HandleFunc("PUT /api/admin/contests/{id}/problems", organiser(console.SetContestProblems))
	mux.HandleFunc("PUT /api/admin/contests/{id}/problems/{label}/revision", organiser(console.UpdatePin))
	mux.HandleFunc("PUT /api/admin/contests/{id}/extensions/{user}", organiser(console.SetExtension))
	mux.HandleFunc("POST /api/admin/contests/{id}/extend", organiser(console.ExtendContest))
	mux.HandleFunc("POST /api/admin/contests/{id}/pause", organiser(console.PauseContest(true)))
	mux.HandleFunc("POST /api/admin/contests/{id}/resume", organiser(console.PauseContest(false)))
	mux.HandleFunc("GET /api/admin/contests/{id}/submissions", organiser(console.ContestFeed))
	mux.HandleFunc("POST /api/admin/contests/{id}/rejudge", organiser(console.Rejudge))
	mux.HandleFunc("GET /api/admin/contests/{id}/announcements", organiser(console.ListAnnouncements))
	mux.HandleFunc("POST /api/admin/contests/{id}/announcements", organiser(console.PostAnnouncement))
	mux.HandleFunc("GET /api/admin/contests/{id}/clarifications", organiser(console.ListClarifications))
	mux.HandleFunc("PUT /api/admin/contests/{id}/clarifications/{cid}", organiser(console.AnswerClarification))
	mux.HandleFunc("GET /api/admin/contests/{id}/standings", organiser(console.AdminStandings))
	mux.HandleFunc("GET /api/admin/contests/{id}/reveal", organiser(console.GetReveal))
	mux.HandleFunc("POST /api/admin/contests/{id}/reveal", organiser(console.PostReveal))
	mux.HandleFunc("GET /api/admin/contests/{id}/export.csv", organiser(console.ExportCSV))
	mux.HandleFunc("GET /api/admin/contests/{id}/export.json", organiser(console.ExportJSON))
	mux.HandleFunc("GET /api/admin/contests/{id}/stats", organiser(console.ContestStats))
	mux.HandleFunc("PUT /api/admin/contests/{id}/editorial", organiser(console.SetEditorial))
}

// now is the server's clock: the contests service's, which tests replace.
func (d Deps) now() time.Time {
	if d.Contests != nil {
		return d.Contests.Clock()
	}
	return time.Now()
}

// sessionToken is the request's session cookie; requireRole has already
// checked it.
func (d Deps) sessionToken(r *http.Request) string {
	c, err := r.Cookie(d.sessionCookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

// forgetKey drops the AI key of a session that is ending.
func (d Deps) forgetKey(token string) { d.AI.ForgetKey(token) }

// queue is the judge queue the services share.
func (d Deps) queue() *jobs.Service {
	switch {
	case d.Problems != nil && d.Problems.Jobs != nil:
		return d.Problems.Jobs
	case d.Submissions != nil:
		return d.Submissions.Jobs
	}
	return nil
}

// internalError logs err and answers with a generic 500.
func (d Deps) internalError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.InternalError(d.Log, w, r, err)
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
