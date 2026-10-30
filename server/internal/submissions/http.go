package submissions

// The organisers' workspace over HTTP (an admin tries a problem from the
// bank, "Try as a student") and reading one run or submission. Students
// reach the same workspace through the contest routes in contests, which
// use WriteError, OrganiserGap and MaxRunBody from here.

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

// Handlers serve the workspace and submission routes.
type Handlers struct {
	Submissions *Service
	Log         *slog.Logger
}

// OrganiserGap is the minimum time between submits when an organiser tries
// a problem. Contests set their own gap.
const OrganiserGap = 5 * time.Second

// MaxRunBody bounds a run or submit: 64 KB of code plus 64 KB of input.
const MaxRunBody = 160 << 10

func (d *Handlers) workspaceProblem(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("problem"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem.")
		return 0, false
	}
	return id, true
}

func (d *Handlers) internalError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.InternalError(d.Log, w, r, err)
}

func (d *Handlers) submissionError(w http.ResponseWriter, r *http.Request, err error) {
	WriteError(d.Log, w, r, err)
}

// WriteError maps this package's errors to responses; anything else is a
// logged 500.
func WriteError(log *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	var input *InputError
	var rate *RateError
	var notReady *NotReadyError
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem or submission.")
	case errors.Is(err, ErrPinMoved):
		httpx.WriteError(w, http.StatusConflict, "not_ready", err.Error())
	case errors.As(err, &notReady):
		httpx.WriteJSON(w, http.StatusConflict, map[string]httpx.APIError{"error": {Code: "not_ready",
			Message: "This problem cannot be judged yet. An organiser needs to fix these first.", Problems: notReady.Problems}})
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", input.Message)
	case errors.As(err, &rate):
		secs := int(rate.RetryAfter.Seconds())
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		httpx.WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
			"code": "rate_limited", "message": rate.Message, "retry_after_s": secs}})
	default:
		httpx.InternalError(log, w, r, err)
	}
}

func (d *Handlers) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	ws, err := d.Submissions.Workspace(r.Context(), id, auth.UserFrom(r.Context()).ID, Scope{})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ws)
}

func (d *Handlers) SaveDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language string `json:"language"`
		Source   string `json:"source"`
	}
	if !httpx.DecodeJSONLimit(w, r, &req, MaxRunBody) {
		return
	}
	if err := d.Submissions.SaveDraft(r.Context(), id, auth.UserFrom(r.Context()).ID, Scope{}, req.Language, req.Source); err != nil {
		d.submissionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Handlers) RunCode(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language string  `json:"language"`
		Source   string  `json:"source"`
		Input    *string `json:"input"`
	}
	if !httpx.DecodeJSONLimit(w, r, &req, MaxRunBody) {
		return
	}
	v, err := d.Submissions.Run(r.Context(), RunRequest{ProblemID: id, UserID: auth.UserFrom(r.Context()).ID,
		Language: req.Language, Source: req.Source, Input: req.Input})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, v)
}

func (d *Handlers) SubmitCode(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language    string `json:"language"`
		Source      string `json:"source"`
		ClientKey   string `json:"client_key"`
		TypedChars  int    `json:"typed_chars"`
		PastedChars int    `json:"pasted_chars"`
		FocusLosses int    `json:"focus_losses"`
		Exits       int    `json:"fullscreen_exits"`
	}
	if !httpx.DecodeJSONLimit(w, r, &req, MaxRunBody) {
		return
	}
	u := auth.UserFrom(r.Context())
	v, dup, err := d.Submissions.Submit(r.Context(), SubmitRequest{ProblemID: id, UserID: u.ID,
		Scope:    Scope{MinGap: OrganiserGap, Reveal: true},
		Language: req.Language, Source: req.Source, ClientKey: req.ClientKey,
		Typed: req.TypedChars, Pasted: req.PastedChars, Blur: req.FocusLosses, Exits: req.Exits,
		ClientHash: d.Submissions.ClientHash(httpx.ClientIP(r), r.UserAgent())})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if dup {
		status = http.StatusOK // a retried click: the first submission
	}
	httpx.WriteJSON(w, status, v)
}

func (d *Handlers) RecordEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Events []Event `json:"events"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := d.Submissions.RecordEvents(r.Context(), id, auth.UserFrom(r.Context()).ID, Scope{}, req.Events); err != nil {
		d.submissionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mayRead lets the author read their own run or submission, and organisers
// (owners and admins) read anyone's. It returns the id and how the viewer sees it: organisers every
// figure, the author the student view of their contest (coarse figures,
// rounded times, exam mode hiding results), so a program cannot leak a
// hidden test through exact figures.
func (d *Handlers) mayRead(w http.ResponseWriter, r *http.Request, wantKind string) (int64, ViewOptions, bool) {
	notFound := func() (int64, ViewOptions, bool) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such submission.")
		return 0, ViewOptions{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound()
	}
	info, err := d.Submissions.Info(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		return notFound()
	}
	if err != nil {
		d.internalError(w, r, err)
		return 0, ViewOptions{}, false
	}
	if info.Kind != wantKind {
		return notFound()
	}
	u := auth.UserFrom(r.Context())
	if u.Role.AtLeast(auth.Admin) {
		return id, ViewOptions{}, true
	}
	if info.UserID == u.ID {
		return id, d.Submissions.ViewFor(r.Context(), info.ContestID, u.ID, true), true
	}
	return notFound()
}

func (d *Handlers) GetSubmission(w http.ResponseWriter, r *http.Request) {
	id, view, ok := d.mayRead(w, r, "submit")
	if !ok {
		return
	}
	v, err := d.Submissions.GetWith(r.Context(), id, view)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

func (d *Handlers) GetRun(w http.ResponseWriter, r *http.Request) {
	id, view, ok := d.mayRead(w, r, "run")
	if !ok {
		return
	}
	v, err := d.Submissions.GetRunWith(r.Context(), id, view)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}
