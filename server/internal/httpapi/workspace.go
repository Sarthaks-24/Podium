package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// organiserGap is the minimum time between submits when an organiser tries
// a problem (M3). Contests set their own gap in M4.
const organiserGap = 5 * time.Second

// clockEvery is how often the event stream sends the server's clock.
const clockEvery = 25 * time.Second

// maxRunBody bounds a run or submit: 64 KB of code plus 64 KB of input.
const maxRunBody = 160 << 10

func (d Deps) workspaceProblem(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("problem"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such problem.")
		return 0, false
	}
	return id, true
}

// submissionError maps the submissions package's errors to responses.
func (d Deps) submissionError(w http.ResponseWriter, r *http.Request, err error) {
	var input *submissions.InputError
	var rate *submissions.RateError
	var notReady *submissions.NotReadyError
	switch {
	case errors.Is(err, submissions.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "There is no such problem or submission.")
	case errors.As(err, &notReady):
		WriteJSON(w, http.StatusConflict, map[string]APIError{"error": {Code: "not_ready",
			Message: "This problem cannot be judged yet. An organiser needs to fix these first.", Problems: notReady.Problems}})
	case errors.As(err, &input):
		WriteError(w, http.StatusBadRequest, "bad_request", input.Message)
	case errors.As(err, &rate):
		secs := int(rate.RetryAfter.Seconds())
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
			"code": "rate_limited", "message": rate.Message, "retry_after_s": secs}})
	default:
		d.internalError(w, r, err)
	}
}

func (d Deps) getWorkspace(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	ws, err := d.Submissions.Workspace(r.Context(), id, userFrom(r.Context()).ID, submissions.Scope{})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, ws)
}

func (d Deps) saveDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language string `json:"language"`
		Source   string `json:"source"`
	}
	if !decodeJSONLimit(w, r, &req, maxRunBody) {
		return
	}
	if err := d.Submissions.SaveDraft(r.Context(), id, userFrom(r.Context()).ID, submissions.Scope{}, req.Language, req.Source); err != nil {
		d.submissionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) runCode(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language string  `json:"language"`
		Source   string  `json:"source"`
		Input    *string `json:"input"`
	}
	if !decodeJSONLimit(w, r, &req, maxRunBody) {
		return
	}
	v, err := d.Submissions.Run(r.Context(), submissions.RunRequest{ProblemID: id, UserID: userFrom(r.Context()).ID,
		Language: req.Language, Source: req.Source, Input: req.Input})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusAccepted, v)
}

func (d Deps) submitCode(w http.ResponseWriter, r *http.Request) {
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
	}
	if !decodeJSONLimit(w, r, &req, maxRunBody) {
		return
	}
	u := userFrom(r.Context())
	v, dup, err := d.Submissions.Submit(r.Context(), submissions.SubmitRequest{ProblemID: id, UserID: u.ID,
		Scope:    submissions.Scope{MinGap: organiserGap},
		Language: req.Language, Source: req.Source, ClientKey: req.ClientKey,
		Typed: req.TypedChars, Pasted: req.PastedChars, Blur: req.FocusLosses,
		ClientHash: d.Submissions.ClientHash(clientIP(r), r.UserAgent())})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if dup {
		status = http.StatusOK // a retried click: the first submission
	}
	WriteJSON(w, status, v)
}

func (d Deps) recordEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := d.workspaceProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Events []submissions.Event `json:"events"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := d.Submissions.RecordEvents(r.Context(), id, userFrom(r.Context()).ID, submissions.Scope{}, req.Events); err != nil {
		d.submissionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mayRead lets the author read their own run or submission, and organisers
// read anyone's.
func (d Deps) mayRead(w http.ResponseWriter, r *http.Request, wantKind string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such submission.")
		return 0, false
	}
	owner, kind, err := d.Submissions.Owner(r.Context(), id)
	u := userFrom(r.Context())
	if errors.Is(err, submissions.ErrNotFound) || (err == nil && (kind != wantKind || (owner != u.ID && !u.Role.AtLeast(auth.Admin)))) {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such submission.")
		return 0, false
	}
	if err != nil {
		d.internalError(w, r, err)
		return 0, false
	}
	return id, true
}

func (d Deps) getSubmission(w http.ResponseWriter, r *http.Request) {
	id, ok := d.mayRead(w, r, "submit")
	if !ok {
		return
	}
	v, err := d.Submissions.Get(r.Context(), id)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (d Deps) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := d.mayRead(w, r, "run")
	if !ok {
		return
	}
	v, err := d.Submissions.GetRun(r.Context(), id)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

// eventStream is the signed-in user's server-sent event stream: run and
// submission updates as they happen, and the server's clock every 25 s.
func (d Deps) eventStream(w http.ResponseWriter, r *http.Request) {
	_, ok := w.(http.Flusher)
	if !ok || d.Hub == nil {
		WriteError(w, http.StatusNotImplemented, "unsupported", "Live updates are not available; the page will poll instead.")
		return
	}
	c, err := d.Hub.Subscribe(userFrom(r.Context()).ID)
	if err != nil {
		w.Header().Set("Retry-After", "30")
		WriteError(w, http.StatusServiceUnavailable, "busy", "Too many live connections right now; the page will poll instead.")
		return
	}
	defer d.Hub.Unsubscribe(c)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{}) // the stream outlives the server's read timeout
	// Every write has a deadline, so a client that stops reading cannot
	// hold this goroutine and its connection.
	write := func(format string, a ...any) bool {
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, format, a...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	clock := func() bool {
		return write("event: clock\ndata: {\"server_now\":%d}\n\n", time.Now().UnixMilli())
	}
	if !clock() {
		return
	}
	t := time.NewTicker(clockEvery)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.Done:
			return // too slow; the browser reconnects
		case ev := <-c.C:
			if !write("event: %s\ndata: %s\n\n", ev.Type, ev.Data) {
				return
			}
		case <-t.C:
			if !clock() {
				return
			}
		}
	}
}

type generationJSON struct {
	ID         int64                       `json:"id"`
	State      string                      `json:"state"`
	All        bool                        `json:"all"`
	QueuedAt   int64                       `json:"queued_at"`
	FinishedAt *int64                      `json:"finished_at"`
	Results    []generationResultJSON      `json:"results"`
	Compile    string                      `json:"compile_output"`
	Summary    *problems.GenerationSummary `json:"summary"`
	Error      *string                     `json:"error"`
}

type generationResultJSON struct {
	Idx     int    `json:"idx"`
	Verdict string `json:"verdict"`
	TimeMS  int    `json:"time_ms"`
}

func generationOut(g *problems.Generation) generationJSON {
	out := generationJSON{ID: g.ID, State: g.State, All: g.All, QueuedAt: g.QueuedAt.UnixMilli(), FinishedAt: millis(g.FinishedAt),
		Results: []generationResultJSON{}, Summary: g.Summary}
	for _, c := range g.Compile {
		out.Compile = c.Output
	}
	for _, t := range g.Tests {
		out.Results = append(out.Results, generationResultJSON{t.Idx, t.Verdict, t.TimeMS})
	}
	if g.Error != "" {
		out.Error = &g.Error
	}
	return out
}

func (d Deps) generateOutputs(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	var req struct {
		All bool `json:"all"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	g, err := d.Problems.RequestGeneration(r.Context(), id, userFrom(r.Context()).ID, req.All)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("output generation queued", "by", userFrom(r.Context()).ID, "problem_id", id, "all", req.All)
	WriteJSON(w, http.StatusAccepted, generationOut(g))
}

func (d Deps) getGeneration(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	g, err := d.Problems.LatestGeneration(r.Context(), id)
	if errors.Is(err, problems.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "Outputs have not been generated for this problem.")
		return
	}
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, generationOut(g))
}

// uploadBlob takes an output from the worker of a generate job: lease
// checked, at most 16 MB, stored only if it hashes to its name.
func (d JudgeDeps) uploadBlob(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	jobID, _ := strconv.ParseInt(r.Header.Get(judgeapi.HeaderJob), 10, 64)
	ok, err := d.Jobs.MayUpload(r.Context(), jobID, r.Header.Get(judgeapi.HeaderLease))
	if err != nil {
		d.Log.Error("upload check failed", "job_id", jobID, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "The server could not check the lease.")
		return
	}
	if !ok {
		WriteError(w, http.StatusConflict, "lease", "This lease does not allow more uploads.")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, problems.MaxPackFile))
	if err != nil {
		WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "Outputs are limited to 16 MB.")
		return
	}
	if err := d.Blobs.UploadOutput(data, sha); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_upload", "The upload does not match its hash.")
		return
	}
	d.Jobs.RecordUpload(jobID, sha)
	w.WriteHeader(http.StatusNoContent)
}
