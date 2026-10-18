package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// AI wizard routes (M5, cache/v1/m5-plan.md section 7). The provider key
// is pasted per session and lives only in AI.Keys (decision D50): it is
// wrapped in an ai.Secret as soon as the request is decoded and never
// reaches a log line, an error, the database or a job payload.

// Wizard states.
const (
	wizardThinking  = "thinking"
	wizardDraft     = "draft_created"
	wizardSubmitted = "submitted"
	wizardFailed    = "error"
)

const (
	// wizardKeep is how long a finished wizard run can still be read.
	wizardKeep = time.Hour
	// maxWizardBody bounds a wizard request: 20,000 characters of pasted
	// text, which JSON escaping can make several times larger.
	maxWizardBody = 256 << 10
	// keyCheckTimeout bounds the cheap call that proves a key on save.
	keyCheckTimeout = 30 * time.Second
	// draftTimeout bounds the database work after the model answered.
	draftTimeout = 30 * time.Second
)

// AI is the wizard's server state. Everything is in memory: a restart
// forgets every key and every wizard run.
type AI struct {
	// Keys holds pasted keys by session; Slots allows one call per admin.
	Keys  *ai.Keys
	Slots *ai.Slots
	// BaseURL points the Anthropic client at a stand-in API. Tests only.
	BaseURL string

	mu   sync.Mutex
	runs map[string]*wizardRun
}

// wizardRun is one Generate or Solve, readable by the admin who started
// it until wizardKeep after it ends.
type wizardRun struct {
	id           string
	kind         string // generate or solve
	userID       int64
	state        string
	err          *wizardErrorJSON
	problemID    int64
	submissionID int64
	startedAt    time.Time
	finishedAt   time.Time
}

type wizardErrorJSON struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Reasons []string `json:"reasons"`
}

type wizardJSON struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind"`
	State        string           `json:"state"`
	Error        *wizardErrorJSON `json:"error,omitempty"`
	ProblemID    *int64           `json:"problem_id,omitempty"`
	SubmissionID *int64           `json:"submission_id,omitempty"`
	StartedAt    int64            `json:"started_at"`
	ServerNow    int64            `json:"server_now"`
}

func optID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// start registers a new run for userID and drops runs that ended long ago.
func (a *AI) start(kind string, userID int64, problemID int64) *wizardRun {
	var b [16]byte
	rand.Read(b[:])
	now := time.Now()
	run := &wizardRun{id: hex.EncodeToString(b[:]), kind: kind, userID: userID, state: wizardThinking,
		problemID: problemID, startedAt: now}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runs == nil {
		a.runs = map[string]*wizardRun{}
	}
	for id, r := range a.runs {
		if !r.finishedAt.IsZero() && now.Sub(r.finishedAt) > wizardKeep {
			delete(a.runs, id)
		}
	}
	a.runs[run.id] = run
	return run
}

// view returns the run's JSON if it exists and belongs to userID.
func (a *AI) view(id string, userID int64) (wizardJSON, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.runs[id]
	if !ok || r.userID != userID {
		return wizardJSON{}, false
	}
	return r.json(), true
}

// json must be called with a.mu held.
func (r *wizardRun) json() wizardJSON {
	return wizardJSON{ID: r.id, Kind: r.kind, State: r.state, Error: r.err, ProblemID: optID(r.problemID),
		SubmissionID: optID(r.submissionID), StartedAt: r.startedAt.UnixMilli(), ServerNow: time.Now().UnixMilli()}
}

// finish records the outcome of a run and returns its JSON.
func (a *AI) finish(run *wizardRun, set func(r *wizardRun)) wizardJSON {
	a.mu.Lock()
	defer a.mu.Unlock()
	set(run)
	run.finishedAt = time.Now()
	return run.json()
}

// wizardError turns an error from a wizard run into what the admin sees.
// Only *ai.Error carries a sentence meant for people; anything else is
// reported as a server problem.
func wizardError(err error) *wizardErrorJSON {
	var e *ai.Error
	if errors.As(err, &e) {
		return &wizardErrorJSON{Code: e.Code, Message: e.Message, Reasons: nonNil(e.Reasons)}
	}
	return &wizardErrorJSON{Code: "internal", Message: "Something went wrong on the server. Try again; if it keeps happening, tell an organiser.", Reasons: []string{}}
}

// aiRoutes mounts the wizard's routes.
func (d Deps) aiRoutes(mux *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	mux.HandleFunc("GET /api/admin/ai/key", admin(d.getAIKey))
	mux.HandleFunc("PUT /api/admin/ai/key", admin(d.putAIKey))
	mux.HandleFunc("DELETE /api/admin/ai/key", admin(d.deleteAIKey))
	mux.HandleFunc("POST /api/admin/ai/wizard", admin(d.startWizard))
	mux.HandleFunc("GET /api/admin/ai/wizard/{id}", admin(d.getWizard))
	mux.HandleFunc("POST /api/admin/problems/{id}/solve", admin(d.solveProblem))
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
func (d Deps) forgetKey(token string) {
	if d.AI != nil && d.AI.Keys != nil && token != "" {
		d.AI.Keys.Forget(token)
	}
}

func (d Deps) testing() bool { return d.Mode == "testing" }

type modelJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type providerJSON struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Models []modelJSON `json:"models"`
}

type keyStatusJSON struct {
	Active      bool           `json:"active"`
	Provider    string         `json:"provider"`
	Model       string         `json:"model"`
	ActiveUntil *int64         `json:"active_until"`
	Providers   []providerJSON `json:"providers"`
	ServerNow   int64          `json:"server_now"`
}

// fakeModel is the one model of the testing-mode provider.
const fakeModel = "canned"

func (d Deps) keyStatus(r *http.Request) keyStatusJSON {
	out := keyStatusJSON{Provider: ai.ProviderAnthropic, Model: ai.DefaultModel, ServerNow: time.Now().UnixMilli()}
	anthropic := providerJSON{ID: ai.ProviderAnthropic, Name: "Anthropic"}
	for _, m := range ai.AnthropicModels {
		anthropic.Models = append(anthropic.Models, modelJSON{m, ai.ModelNames[m]})
	}
	out.Providers = []providerJSON{anthropic}
	if d.testing() {
		out.Providers = append(out.Providers, providerJSON{ID: ai.ProviderFake, Name: "Test provider (canned problems)",
			Models: []modelJSON{{fakeModel, "Canned problems"}}})
	}
	if st, ok := d.AI.Keys.Status(d.sessionToken(r)); ok {
		out.Active, out.Provider, out.Model, out.ActiveUntil = true, st.Provider, st.Model, millis(st.ActiveUntil)
	}
	return out
}

func (d Deps) getAIKey(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, d.keyStatus(r))
}

// putAIKey checks a pasted key with one cheap call and keeps it for the
// session. The request body is never logged, and the key is wrapped in a
// Secret before anything else happens.
func (d Deps) putAIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Key      string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	key := ai.NewSecret(strings.TrimSpace(req.Key))
	req.Key = ""
	if req.Provider == ai.ProviderFake {
		key = ai.Secret{}
		req.Model = fakeModel
	}
	if req.Provider == ai.ProviderAnthropic && req.Model == "" {
		req.Model = ai.DefaultModel
	}
	prov, err := ai.NewProvider(ai.Config{Provider: req.Provider, Model: req.Model, Key: key, BaseURL: d.AI.BaseURL}, d.testing())
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", wizardError(err).Message)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), keyCheckTimeout)
	defer cancel()
	if err := prov.Check(ctx); err != nil {
		e := wizardError(err)
		d.Log.Info("ai key refused", "user_id", userFrom(r.Context()).ID, "provider", req.Provider, "model", req.Model, "code", e.Code)
		WriteError(w, http.StatusBadRequest, "bad_key", e.Message)
		return
	}
	d.AI.Keys.Put(d.sessionToken(r), ai.KeyEntry{Provider: req.Provider, Model: req.Model, Key: key})
	d.Log.Info("ai key added", "user_id", userFrom(r.Context()).ID, "provider", req.Provider, "model", req.Model)
	WriteJSON(w, http.StatusOK, d.keyStatus(r))
}

func (d Deps) deleteAIKey(w http.ResponseWriter, r *http.Request) {
	d.forgetKey(d.sessionToken(r))
	d.Log.Info("ai key removed", "user_id", userFrom(r.Context()).ID)
	w.WriteHeader(http.StatusNoContent)
}

// provider returns a client for the session's key, or writes 409 no_key.
func (d Deps) provider(w http.ResponseWriter, r *http.Request) (ai.Provider, ai.KeyEntry, bool) {
	e, ok := d.AI.Keys.Get(d.sessionToken(r))
	if !ok {
		WriteError(w, http.StatusConflict, "no_key", "Add a key in AI assistant first.")
		return nil, ai.KeyEntry{}, false
	}
	prov, err := ai.NewProvider(ai.Config{Provider: e.Provider, Model: e.Model, Key: e.Key, BaseURL: d.AI.BaseURL}, d.testing())
	if err != nil {
		// The key was accepted under another mode or model list; ask for
		// it again rather than guess.
		d.AI.Keys.Forget(d.sessionToken(r))
		WriteError(w, http.StatusConflict, "no_key", "Add a key in AI assistant first.")
		return nil, ai.KeyEntry{}, false
	}
	return prov, e, true
}

// takeSlot reserves the admin's one AI call, or writes 429 busy.
func (d Deps) takeSlot(w http.ResponseWriter, userID int64) (func(), bool) {
	release, ok := d.AI.Slots.Take(userID)
	if !ok {
		WriteError(w, http.StatusTooManyRequests, "busy", "One AI request at a time. Wait for the current one to finish.")
		return nil, false
	}
	return release, true
}

type wizardRequestJSON struct {
	Mode  string `json:"mode"`
	Brief *struct {
		Topic       string `json:"topic"`
		Difficulty  string `json:"difficulty"`
		Constraints string `json:"constraints"`
		Subtasks    string `json:"subtasks"`
	} `json:"brief"`
	Text string `json:"text"`
}

// startWizard asks the model for a problem in the background. The answer
// becomes a draft with origin ai and the "ai draft" tag, and input
// generation is queued, which chains output generation and validation.
func (d Deps) startWizard(w http.ResponseWriter, r *http.Request) {
	var body wizardRequestJSON
	if !decodeJSONLimit(w, r, &body, maxWizardBody) {
		return
	}
	var req ai.Request
	switch body.Mode {
	case "brief":
		if body.Brief == nil {
			WriteError(w, http.StatusBadRequest, "bad_request", "Write a brief or paste a problem.")
			return
		}
		switch body.Brief.Difficulty {
		case "easy", "medium", "hard":
		default:
			WriteError(w, http.StatusBadRequest, "bad_request", "Choose easy, medium or hard.")
			return
		}
		req.Brief = &ai.Brief{Topic: body.Brief.Topic, Difficulty: body.Brief.Difficulty,
			Constraints: body.Brief.Constraints, SubtaskIdeas: body.Brief.Subtasks}
	case "paste":
		req.Pasted = body.Text
	default:
		WriteError(w, http.StatusBadRequest, "bad_request", "Choose brief or paste.")
		return
	}
	u := userFrom(r.Context())
	prov, entry, ok := d.provider(w, r)
	if !ok {
		return
	}
	if err := ai.CheckRequest(req); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", wizardError(err).Message)
		return
	}
	release, ok := d.takeSlot(w, u.ID)
	if !ok {
		return
	}
	run := d.AI.start("generate", u.ID, 0)
	d.Log.Info("ai wizard started", "user_id", u.ID, "wizard_id", run.id, "mode", body.Mode, "provider", entry.Provider, "model", entry.Model)
	go func() {
		defer release()
		d.publishWizard(u.ID, d.runGenerate(run, prov, req, u.ID))
	}()
	WriteJSON(w, http.StatusAccepted, map[string]any{"wizard_id": run.id, "server_now": time.Now().UnixMilli()})
}

// runGenerate is the background half of startWizard.
func (d Deps) runGenerate(run *wizardRun, prov ai.Provider, req ai.Request, userID int64) wizardJSON {
	ctx, cancel := context.WithTimeout(context.Background(), ai.CallTimeout)
	pack, err := ai.Generate(ctx, prov, req)
	cancel()
	if err != nil {
		return d.failWizard(run, err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), draftTimeout)
	defer cancel()
	p, err := d.Problems.CreateDraft(ctx, ai.ToDraft(pack), userID)
	var fields problems.FieldErrors
	if errors.As(err, &fields) {
		reasons := make([]string, 0, len(fields))
		for k, m := range fields {
			reasons = append(reasons, k+": "+m)
		}
		sort.Strings(reasons)
		err = &ai.Error{Code: ai.CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: reasons}
	}
	if err != nil {
		return d.failWizard(run, err)
	}
	d.Log.Info("ai draft created", "user_id", userID, "wizard_id", run.id, "problem_id", p.ID)
	if g, err := d.Problems.RequestInputGeneration(ctx, p.ID, userID); err != nil {
		// The draft stands; the editor offers Generate inputs.
		d.Log.Warn("ai draft: input generation not queued", "problem_id", p.ID, "err", err)
	} else {
		d.Log.Info("input generation queued", "by", userID, "problem_id", p.ID, "input_generation_id", g.ID)
	}
	return d.AI.finish(run, func(r *wizardRun) { r.state, r.problemID = wizardDraft, p.ID })
}

// failWizard records a failed run. An *ai.Error is logged by its code
// only (it never holds the key); anything else is a server fault.
func (d Deps) failWizard(run *wizardRun, err error) wizardJSON {
	var aiErr *ai.Error
	if errors.As(err, &aiErr) {
		d.Log.Info("ai request ended without a result", "user_id", run.userID, "wizard_id", run.id, "code", aiErr.Code)
	} else {
		d.Log.Error("ai request failed", "wizard_id", run.id, "err", err)
	}
	e := wizardError(err)
	return d.AI.finish(run, func(r *wizardRun) { r.state, r.err = wizardFailed, e })
}

func (d Deps) publishWizard(userID int64, v wizardJSON) {
	if d.Hub != nil {
		d.Hub.Publish(userID, "wizard", v)
	}
}

func (d Deps) getWizard(w http.ResponseWriter, r *http.Request) {
	v, ok := d.AI.view(r.PathValue("id"), userFrom(r.Context()).ID)
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such AI request. It may have ended over an hour ago or before a restart.")
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

// solveProblem sends the statement alone to the model ("Can a model
// solve this?") and judges its answer as the admin's own submission.
func (d Deps) solveProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	p, err := d.Problems.Get(r.Context(), id)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	u := userFrom(r.Context())
	prov, entry, ok := d.provider(w, r)
	if !ok {
		return
	}
	// Refused before the call, so no tokens are spent on a problem that
	// cannot be judged.
	if probs := p.Problems(); len(probs) > 0 {
		d.problemError(w, r, &problems.NotReadyError{Problems: probs})
		return
	}
	release, ok := d.takeSlot(w, u.ID)
	if !ok {
		return
	}
	run := d.AI.start("solve", u.ID, id)
	d.Log.Info("ai solve started", "user_id", u.ID, "wizard_id", run.id, "problem_id", id, "provider", entry.Provider, "model", entry.Model)
	statement := p.Statement
	go func() {
		defer release()
		d.publishWizard(u.ID, d.runSolve(run, prov, statement, id, u.ID))
	}()
	WriteJSON(w, http.StatusAccepted, map[string]any{"wizard_id": run.id, "server_now": time.Now().UnixMilli()})
}

// runSolve is the background half of solveProblem.
func (d Deps) runSolve(run *wizardRun, prov ai.Provider, statement string, problemID, userID int64) wizardJSON {
	ctx, cancel := context.WithTimeout(context.Background(), ai.CallTimeout)
	src, err := ai.Solve(ctx, prov, statement)
	cancel()
	if err != nil {
		return d.failWizard(run, err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), draftTimeout)
	defer cancel()
	v, _, err := d.Submissions.Submit(ctx, submissions.SubmitRequest{ProblemID: problemID, UserID: userID,
		Language: "cpp17", Source: src, ClientKey: "ai-solve-" + run.id})
	var input *submissions.InputError
	var rate *submissions.RateError
	var notReady *submissions.NotReadyError
	switch {
	case errors.As(err, &notReady):
		err = &ai.Error{Code: "not_ready", Message: "The problem cannot be judged yet. Fix these first.", Reasons: notReady.Problems}
	case errors.As(err, &rate):
		err = &ai.Error{Code: ai.CodeRateLimited, Message: rate.Message}
	case errors.As(err, &input):
		err = &ai.Error{Code: ai.CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: []string{input.Message}}
	case errors.Is(err, submissions.ErrNotFound):
		err = &ai.Error{Code: "not_found", Message: "The problem no longer exists."}
	}
	if err != nil {
		return d.failWizard(run, err)
	}
	d.Log.Info("ai solution submitted", "user_id", userID, "wizard_id", run.id, "problem_id", problemID, "submission_id", v.ID)
	return d.AI.finish(run, func(r *wizardRun) { r.state, r.submissionID = wizardSubmitted, v.ID })
}
