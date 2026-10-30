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
)

// AI wizard routes. The provider key
// is pasted per session and lives only in AI.Keys: it is
// wrapped in an ai.Secret as soon as the request is decoded and never
// reaches a log line, an error, the database or a job payload.

// Wizard states.
const (
	wizardThinking = "thinking"
	wizardDraft    = "draft_created"
	wizardFailed   = "error"
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
	// BaseURLs points a provider's client at a stand-in API, by provider
	// name. Tests only: nothing a request carries can set it.
	BaseURLs map[string]string

	mu   sync.Mutex
	runs map[string]*wizardRun
}

// wizardRun is one Generate, readable by the admin who started
// it until wizardKeep after it ends.
type wizardRun struct {
	id         string
	kind       string // always generate since v3 removed model solve
	userID     int64
	provider   string
	model      string
	state      string
	err        *wizardErrorJSON
	problemID  int64
	startedAt  time.Time
	finishedAt time.Time
}

type wizardErrorJSON struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Reasons []string `json:"reasons"`
}

type wizardJSON struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	State     string           `json:"state"`
	Error     *wizardErrorJSON `json:"error,omitempty"`
	ProblemID *int64           `json:"problem_id,omitempty"`
	Provider  string           `json:"provider"`
	Model     string           `json:"model"`
	StartedAt int64            `json:"started_at"`
	ServerNow int64            `json:"server_now"`
}

func optID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// start registers a new run for userID with the provider and model of
// entry, and drops runs that ended long ago.
func (a *AI) start(kind string, userID int64, problemID int64, entry ai.KeyEntry) *wizardRun {
	var b [16]byte
	rand.Read(b[:])
	now := time.Now()
	run := &wizardRun{id: hex.EncodeToString(b[:]), kind: kind, userID: userID, state: wizardThinking,
		problemID: problemID, provider: entry.Provider, model: entry.Model, startedAt: now}
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
		Provider: r.provider, Model: r.model,
		StartedAt: r.startedAt.UnixMilli(), ServerNow: time.Now().UnixMilli()}
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
	mux.HandleFunc("GET /api/admin/ai/providers", admin(d.getAIProviders))
	mux.HandleFunc("GET /api/admin/ai/models", admin(d.getAIModels))
	mux.HandleFunc("GET /api/admin/ai/key", admin(d.getAIKey))
	mux.HandleFunc("PUT /api/admin/ai/key", admin(d.putAIKey))
	mux.HandleFunc("DELETE /api/admin/ai/key", admin(d.deleteAIKey))
	mux.HandleFunc("POST /api/admin/ai/wizard", admin(d.startWizard))
	mux.HandleFunc("GET /api/admin/ai/wizard/{id}", admin(d.getWizard))
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

// providerIDs lists the providers this server offers, in the form's order.
func (d Deps) providerIDs() []string {
	ids := append([]string{}, ai.Providers...)
	if ai.Known(ai.ProviderFake, d.testing()) {
		ids = append(ids, ai.ProviderFake)
	}
	return ids
}

type providerJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	HasKey      bool   `json:"has_key"`
	Model       string `json:"model"`
	ActiveUntil *int64 `json:"active_until"`
}

// keyProviderJSON is a provider in KeyStatus: providerJSON plus the
// session's copy of its model list.
type keyProviderJSON struct {
	providerJSON
	Models []ai.Model `json:"models"`
}

type providersJSON struct {
	Providers      []providerJSON `json:"providers"`
	ActiveProvider *string        `json:"active_provider"`
	ServerNow      int64          `json:"server_now"`
}

type keyStatusJSON struct {
	Active      bool              `json:"active"`
	Provider    string            `json:"provider"`
	Model       string            `json:"model"`
	ActiveUntil *int64            `json:"active_until"`
	Providers   []keyProviderJSON `json:"providers"`
	ServerNow   int64             `json:"server_now"`
}

// providerEntries returns what the session holds for each provider.
func (d Deps) providerEntries(r *http.Request) []keyProviderJSON {
	token := d.sessionToken(r)
	var out []keyProviderJSON
	for _, id := range d.providerIDs() {
		p := keyProviderJSON{providerJSON: providerJSON{ID: id, Name: ai.ProviderName(id)}, Models: []ai.Model{}}
		if e, ok := d.AI.Keys.GetProvider(token, id); ok {
			p.HasKey, p.Model, p.ActiveUntil = true, e.Model, millis(e.Expires)
			if e.Models != nil {
				p.Models = e.Models
			}
		}
		out = append(out, p)
	}
	return out
}

func (d Deps) keyStatus(r *http.Request) keyStatusJSON {
	out := keyStatusJSON{Provider: ai.ProviderAnthropic, Providers: d.providerEntries(r), ServerNow: time.Now().UnixMilli()}
	if st, ok := d.AI.Keys.Status(d.sessionToken(r)); ok {
		out.Active, out.Provider, out.Model, out.ActiveUntil = true, st.Provider, st.Model, millis(st.ActiveUntil)
	}
	return out
}

func (d Deps) getAIProviders(w http.ResponseWriter, r *http.Request) {
	out := providersJSON{ServerNow: time.Now().UnixMilli()}
	for _, p := range d.providerEntries(r) {
		out.Providers = append(out.Providers, p.providerJSON)
	}
	if st, ok := d.AI.Keys.Status(d.sessionToken(r)); ok {
		out.ActiveProvider = &st.Provider
	}
	WriteJSON(w, http.StatusOK, out)
}

func (d Deps) getAIKey(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, d.keyStatus(r))
}

// noKeyMessage asks for a key for one provider.
func noKeyMessage(provider string) string {
	name := ai.ProviderName(provider)
	article := "a"
	if strings.ContainsAny(name[:1], "AEIOU") {
		article = "an"
	}
	return "Add " + article + " " + name + " key first."
}

// getAIModels lists a provider's models with the session's key, keeping
// the filtered list for the session.
func (d Deps) getAIModels(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if !ai.Known(provider, d.testing()) {
		WriteError(w, http.StatusBadRequest, "bad_request", "Choose Anthropic, OpenAI or Google Gemini as the provider.")
		return
	}
	token := d.sessionToken(r)
	e, ok := d.AI.Keys.GetProvider(token, provider)
	if !ok {
		WriteError(w, http.StatusConflict, "no_key", noKeyMessage(provider))
		return
	}
	if e.ListedAt.IsZero() || r.URL.Query().Get("refresh") == "1" {
		prov, err := ai.NewProvider(ai.Config{Provider: provider, Key: e.Key, BaseURL: d.AI.BaseURLs[provider]}, d.testing())
		if err != nil {
			WriteError(w, http.StatusConflict, "no_key", noKeyMessage(provider))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), keyCheckTimeout)
		defer cancel()
		models, err := prov.ListModels(ctx)
		if err != nil {
			e := wizardError(err)
			d.Log.Info("ai model listing failed", "user_id", userFrom(r.Context()).ID, "provider", provider, "code", e.Code)
			WriteError(w, http.StatusBadGateway, "provider_error", e.Message)
			return
		}
		d.AI.Keys.SetModels(token, provider, models)
		e, _ = d.AI.Keys.GetProvider(token, provider)
	}
	models := e.Models
	if models == nil {
		models = []ai.Model{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"provider": provider, "models": models,
		"listed_at": e.ListedAt.UnixMilli(), "server_now": time.Now().UnixMilli()})
}

// putAIKey checks a pasted key (and model, when one is given) with one
// call that costs no tokens and keeps it for the session and provider.
// The request body is never logged, and the key is wrapped in a Secret
// before anything else happens.
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
	token := d.sessionToken(r)
	if !ai.Known(req.Provider, d.testing()) {
		if req.Provider == ai.ProviderFake {
			WriteError(w, http.StatusBadRequest, "bad_request", "The test provider exists only in testing mode.")
		} else {
			WriteError(w, http.StatusBadRequest, "bad_request", "Choose Anthropic, OpenAI or Google Gemini as the provider.")
		}
		return
	}
	model, ok := ai.CleanModelID(req.Model)
	if !ok {
		WriteError(w, http.StatusBadRequest, "bad_request", "A model id has only letters, digits and . _ - : and at most 128 characters.")
		return
	}
	held, holds := d.AI.Keys.GetProvider(token, req.Provider)
	if req.Provider == ai.ProviderFake {
		key, model, holds = ai.Secret{}, ai.FakeModel, false
	} else if key.IsZero() {
		if !holds {
			WriteError(w, http.StatusBadRequest, "bad_request", "Paste the provider key.")
			return
		}
		key = held.Key
	} else {
		holds = false
	}
	prov, err := ai.NewProvider(ai.Config{Provider: req.Provider, Model: model, Key: key, BaseURL: d.AI.BaseURLs[req.Provider]}, d.testing())
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", wizardError(err).Message)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), keyCheckTimeout)
	defer cancel()
	var models []ai.Model
	if model == "" {
		// The listing is the check, and fills the picker in one call.
		models, err = prov.ListModels(ctx)
	} else {
		err = prov.Check(ctx)
	}
	if err != nil {
		e := wizardError(err)
		d.Log.Info("ai key refused", "user_id", userFrom(r.Context()).ID, "provider", req.Provider, "model", model, "code", e.Code)
		WriteError(w, http.StatusBadRequest, "bad_key", e.Message)
		return
	}
	if holds {
		d.AI.Keys.SetModel(token, req.Provider, model)
	} else {
		d.AI.Keys.Put(token, ai.KeyEntry{Provider: req.Provider, Model: model, Key: key})
	}
	if models != nil || req.Provider == ai.ProviderFake {
		if req.Provider == ai.ProviderFake {
			models, _ = prov.ListModels(ctx)
		}
		d.AI.Keys.SetModels(token, req.Provider, models)
	}
	d.Log.Info("ai key added", "user_id", userFrom(r.Context()).ID, "provider", req.Provider, "model", model, "kept_key", holds)
	WriteJSON(w, http.StatusOK, d.keyStatus(r))
}

func (d Deps) deleteAIKey(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		d.forgetKey(d.sessionToken(r))
	} else {
		d.AI.Keys.ForgetProvider(d.sessionToken(r), provider)
	}
	d.Log.Info("ai key removed", "user_id", userFrom(r.Context()).ID, "provider", provider)
	w.WriteHeader(http.StatusNoContent)
}

// provider returns a client for the session's key of the named provider,
// or of the active one when name is empty, or writes 409 no_key or
// no_model.
func (d Deps) provider(w http.ResponseWriter, r *http.Request, name string) (ai.Provider, ai.KeyEntry, bool) {
	token := d.sessionToken(r)
	var e ai.KeyEntry
	var ok bool
	if name == "" {
		e, ok = d.AI.Keys.Get(token)
	} else if ai.Known(name, d.testing()) {
		e, ok = d.AI.Keys.GetProvider(token, name)
	}
	if !ok {
		WriteError(w, http.StatusConflict, "no_key", "Add a key first.")
		return nil, ai.KeyEntry{}, false
	}
	if e.Model == "" {
		WriteError(w, http.StatusConflict, "no_model", "Choose a model for "+ai.ProviderName(e.Provider)+" first.")
		return nil, ai.KeyEntry{}, false
	}
	prov, err := ai.NewProvider(ai.Config{Provider: e.Provider, Model: e.Model, Key: e.Key, BaseURL: d.AI.BaseURLs[e.Provider]}, d.testing())
	if err != nil {
		// The key was accepted under another mode; ask for it again
		// rather than guess.
		d.AI.Keys.ForgetProvider(token, e.Provider)
		WriteError(w, http.StatusConflict, "no_key", "Add a key first.")
		return nil, ai.KeyEntry{}, false
	}
	return prov, e, true
}

// takeSlot reserves the admin's one AI call, or writes 429 busy.
func (d Deps) takeSlot(w http.ResponseWriter, userID int64) (func(), bool) {
	release, ok := d.AI.Slots.Take(userID)
	if !ok {
		writeBusy(w, 10, "One AI request at a time. Wait for the current one to finish.")
		return nil, false
	}
	return release, true
}

type wizardRequestJSON struct {
	Mode string `json:"mode"`
	// Provider picks one of the session's keys; empty means the active.
	Provider string `json:"provider"`
	Brief    *struct {
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
	prov, entry, ok := d.provider(w, r, body.Provider)
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
	run := d.AI.start("generate", u.ID, 0, entry)
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
