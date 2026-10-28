package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
var clockEvery = 25 * time.Second

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
	case errors.Is(err, submissions.ErrPinMoved):
		WriteError(w, http.StatusConflict, "not_ready", err.Error())
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
		Scope:    submissions.Scope{MinGap: organiserGap, Reveal: true},
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
// (owners, admins, and co-organisers of the submission's contest) read
// anyone's. It returns the id and how the viewer sees it: organisers every
// figure, the author the student view of their contest (coarse figures,
// rounded times, exam mode hiding results; decision D47).
func (d Deps) mayRead(w http.ResponseWriter, r *http.Request, wantKind string) (int64, submissions.ViewOptions, bool) {
	notFound := func() (int64, submissions.ViewOptions, bool) {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such submission.")
		return 0, submissions.ViewOptions{}, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return notFound()
	}
	info, err := d.Submissions.Info(r.Context(), id)
	if errors.Is(err, submissions.ErrNotFound) {
		return notFound()
	}
	if err != nil {
		d.internalError(w, r, err)
		return 0, submissions.ViewOptions{}, false
	}
	if info.Kind != wantKind {
		return notFound()
	}
	u := userFrom(r.Context())
	if u.Role.AtLeast(auth.Admin) {
		return id, submissions.ViewOptions{}, true
	}
	if info.UserID == u.ID {
		return id, d.Submissions.ViewFor(r.Context(), info.ContestID, u.ID, true), true
	}
	return notFound()
}

func (d Deps) getSubmission(w http.ResponseWriter, r *http.Request) {
	id, view, ok := d.mayRead(w, r, "submit")
	if !ok {
		return
	}
	v, err := d.Submissions.GetWith(r.Context(), id, view)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (d Deps) getRun(w http.ResponseWriter, r *http.Request) {
	id, view, ok := d.mayRead(w, r, "run")
	if !ok {
		return
	}
	v, err := d.Submissions.GetRunWith(r.Context(), id, view)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

// streamTag names the device a stream comes from, for the second-session
// signal (D47): the address and the browser. A phone changes address when
// it moves from wifi to cellular, and its old stream lingers until TCP
// gives up, so for a phone or tablet browser the address is left out and
// the same browser on a new network is the same device (council C1-051).
// A desktop keeps the address: identical lab machines differ only by it.
// A phone or tablet that sends ?device= (a random id its browser keeps)
// is told apart by it, so two phones of one model on one account are two
// devices (C2-034); the id never replaces a desktop's address, where a
// shared id would hide two machines.
func (d Deps) streamTag(r *http.Request) string {
	ua := r.UserAgent()
	if mobileAgent(ua) {
		if dev := r.URL.Query().Get("device"); deviceID(dev) {
			return d.Submissions.ClientHash("device:"+dev, ua)
		}
		return d.Submissions.ClientHash("", ua)
	}
	return d.Submissions.ClientHash(clientIP(r), ua)
}

// deviceID reports a well-formed ?device= value: 1 to 64 letters and
// digits. Anything else is ignored, as if it were absent.
func deviceID(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// mobileAgent reports a phone or tablet browser by its user agent.
func mobileAgent(ua string) bool {
	for _, m := range []string{"Mobile", "Android", "iPhone", "iPad"} {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// eventStream is the signed-in user's server-sent event stream: run and
// submission updates as they happen, contest events, and the server's
// clock every 25 s. On each clock tick the session is checked again, so a
// stream ends with its session (logout, expiry, a disabled account). A
// stream displaced by the user's fifth tab gets a final "replaced" event,
// so that tab stops reconnecting. Opening a stream from a second machine
// or browser while another is open is recorded for live contests (D47).
func (d Deps) eventStream(w http.ResponseWriter, r *http.Request) {
	_, ok := w.(http.Flusher)
	if !ok || d.Hub == nil {
		WriteError(w, http.StatusNotImplemented, "unsupported", "Live updates are not available; the page will poll instead.")
		return
	}
	u := userFrom(r.Context())
	tag := d.streamTag(r)
	if d.Contests != nil && u.Role == auth.Student {
		for _, other := range d.Hub.Tags(u.ID) {
			if other != tag {
				if err := d.Contests.RecordSecondSession(r.Context(), u.ID); err != nil {
					d.Log.Warn("second session not recorded", "user_id", u.ID, "err", err)
				}
				break
			}
		}
	}
	c, err := d.Hub.SubscribeTagged(u.ID, tag)
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
			if c.Replaced() {
				write("event: replaced\ndata: {}\n\n")
			}
			return // too slow (the browser reconnects), or replaced
		case ev := <-c.C:
			if !write("event: %s\ndata: %s\n\n", ev.Type, ev.Data) {
				return
			}
		case <-t.C:
			if !d.sessionLive(r) || !clock() {
				return
			}
		}
	}
}

// sessionLive reports whether the request's session is still valid and its
// account enabled. Contest events need nothing more here: they are
// addressed at send time to the users who may still enter the contest.
func (d Deps) sessionLive(r *http.Request) bool {
	c, err := r.Cookie(d.sessionCookieName())
	if err != nil {
		return false
	}
	u, err := d.authenticate(r.Context(), c.Value)
	return err == nil && u.ID == userFrom(r.Context()).ID
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
	// The slot is reserved: give it back unless the upload is recorded.
	// The body streams to a temporary file while it is hashed.
	err = d.Blobs.UploadOutput(http.MaxBytesReader(w, r.Body, problems.MaxPackFile), sha, problems.MaxPackFile)
	switch {
	case errors.Is(err, problems.ErrUploadTooLarge):
		d.Jobs.ReleaseUpload(jobID)
		WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "Outputs are limited to 16 MB.")
		return
	case errors.Is(err, problems.ErrUploadHash):
		d.Jobs.ReleaseUpload(jobID)
		WriteError(w, http.StatusBadRequest, "bad_upload", "The upload does not match its hash.")
		return
	case err != nil:
		d.Jobs.ReleaseUpload(jobID)
		d.Log.Error("upload failed", "job_id", jobID, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal", "The server could not store the upload.")
		return
	}
	d.Jobs.RecordUpload(jobID, sha)
	w.WriteHeader(http.StatusNoContent)
}
