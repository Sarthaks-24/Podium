package ai

// The AI wizard's routes and its runs. The provider key is pasted per
// session and lives only in Wizard.Keys: it is wrapped in a Secret as
// soon as the request is decoded and never reaches a log line, an error,
// the database or a job payload.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Wizard states.
const (
	wizardThinking = "thinking"
	wizardDraft    = "draft_created"
	wizardFailed   = "error"
	// wizardReview is a run with review whose answer waits for the person.
	wizardReview = "review"
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
	// maxKeptAnswer bounds the provider text a run keeps for "What the AI
	// sent back"; longer text is cut (PLAN v2.1.1 section 4.2).
	maxKeptAnswer = 200 << 10
)

// Wizard is the wizard's server state. Everything is in memory: a restart
// forgets every key and every wizard run.
type Wizard struct {
	// Keys holds pasted keys by session; Slots allows one call per admin.
	Keys  *Keys
	Slots *Slots
	// BaseURLs points a provider's client at a stand-in API, by provider
	// name. Tests only: nothing a request carries can set it.
	BaseURLs map[string]string

	mu    sync.Mutex
	runs  map[string]*wizardRun
	edits editDrafts
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
	// rewrite is the old problem a rewrite run started from, nil otherwise.
	rewrite *rewriteLink
	// answer is the latest text the provider returned, with the key removed
	// and cut to maxKeptAnswer; answerCut says it was cut. Only the person
	// who started the run can read it, and never through the wizard event.
	answer    string
	answerCut bool
	// function, javaLists and review are what the run was asked for. reasons
	// is why a review run's answer failed the checks, nil when it passed.
	function  bool
	javaLists string
	review    bool
	reasons   []string
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
	// RewriteOf names the problem a rewrite run started from and the
	// fingerprint of its content then; absent for any other run.
	RewriteOf *rewriteLink `json:"rewrite_of,omitempty"`
	Provider  string       `json:"provider"`
	Model     string       `json:"model"`
	StartedAt int64        `json:"started_at"`
	ServerNow int64        `json:"server_now"`
}

// ForgetKey drops the keys of a session that is ending.
func (a *Wizard) ForgetKey(token string) {
	if a != nil && a.Keys != nil && token != "" {
		a.Keys.Forget(token)
	}
}

// ForgetSessions drops the keys of sessions the server has ended, named by
// the hash the sessions table stores.
func (a *Wizard) ForgetSessions(ids []string) {
	if a == nil || a.Keys == nil {
		return
	}
	for _, id := range ids {
		a.Keys.ForgetHashed(id)
	}
}

// Handlers serve the wizard's routes for the admins.
type Handlers struct {
	Wizard   *Wizard
	Problems *problems.Service
	// Prompts holds the edited rules and preferences. Nil means the
	// start-up rules and no preferences.
	Prompts *Prompts
	// Hub carries a finished run to the admin's open pages.
	Hub *events.Hub
	Log *slog.Logger
	// Testing offers the test provider (testing mode only).
	Testing bool
	// SessionToken is the request's session cookie, which keys the
	// pasted keys; the role check has already accepted it.
	SessionToken func(*http.Request) string
}

func (d *Handlers) sessionToken(r *http.Request) string { return d.SessionToken(r) }

func (d *Handlers) testing() bool { return d.Testing }

func optID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// start registers a new run for userID with the provider and model of
// entry, and drops runs that ended long ago.
func (a *Wizard) start(kind string, userID int64, problemID int64, entry KeyEntry, rewrite *rewriteLink) *wizardRun {
	var b [16]byte
	rand.Read(b[:])
	now := time.Now()
	run := &wizardRun{id: hex.EncodeToString(b[:]), kind: kind, userID: userID, state: wizardThinking,
		problemID: problemID, provider: entry.Provider, model: entry.Model, startedAt: now, rewrite: rewrite}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runs == nil {
		a.runs = map[string]*wizardRun{}
	}
	for id, r := range a.runs {
		if !r.finishedAt.IsZero() && now.Sub(r.finishedAt) > wizardKeep {
			delete(a.runs, id)
		} else if r.userID == userID {
			// One kept answer per person: a new run replaces the last.
			r.answer, r.answerCut = "", false
		}
	}
	a.runs[run.id] = run
	return run
}

// view returns the run's JSON if it exists and belongs to userID.
func (a *Wizard) view(id string, userID int64) (wizardJSON, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.runs[id]
	if !ok || r.userID != userID {
		return wizardJSON{}, false
	}
	return r.json(), true
}

// mark records what a run was asked for, before its goroutine starts.
func (a *Wizard) mark(run *wizardRun, function bool, javaLists string, review bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	run.function, run.javaLists, run.review = function, javaLists, review
}

// keptAnswer returns the kept answer of a run userID started, with the run's
// reasons. found is false for a missing run, another person's run or a
// run that ended more than wizardKeep ago; finished is false while it runs.
func (a *Wizard) keptAnswer(id string, userID int64) (out wizardAnswerJSON, found, finished bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.runs[id]
	if !ok || r.userID != userID || (!r.finishedAt.IsZero() && time.Since(r.finishedAt) > wizardKeep) {
		return out, false, false
	}
	if r.finishedAt.IsZero() {
		return out, true, false
	}
	out = wizardAnswerJSON{Text: r.answer, Cut: r.answerCut, Reasons: []string{}, Kind: StudentsWriteProgram,
		Repairs: []string{}, RewriteOf: r.rewrite, review: r.review}
	if r.function {
		out.Kind, out.JavaLists = StudentsWriteFunction, r.javaLists
	}
	switch {
	case r.err != nil:
		out.Reasons = httpx.NonNil(r.err.Reasons)
	case r.reasons != nil:
		out.Reasons = r.reasons
	}
	return out, true, true
}

type wizardAnswerJSON struct {
	Text    string   `json:"text"`
	Cut     bool     `json:"cut"`
	Reasons []string `json:"reasons"`
	// Kind is program or function and JavaLists the run's Java lists choice
	// (empty for a program). RewriteOf is the rewrite's link, so the page can
	// keep it. Repairs says what Create draft will change in the answer; it
	// is filled for a review run only.
	Kind      string       `json:"kind"`
	JavaLists string       `json:"java_lists"`
	RewriteOf *rewriteLink `json:"rewrite_of,omitempty"`
	Repairs   []string     `json:"repairs"`
	review    bool
}

// json must be called with a.mu held.
func (r *wizardRun) json() wizardJSON {
	out := wizardJSON{ID: r.id, Kind: r.kind, State: r.state, Error: r.err, ProblemID: optID(r.problemID),
		Provider: r.provider, Model: r.model,
		StartedAt: r.startedAt.UnixMilli(), ServerNow: time.Now().UnixMilli()}
	if r.rewrite != nil {
		out.RewriteOf = r.rewrite
	}
	return out
}

// finish records the outcome of a run and returns its JSON.
func (a *Wizard) finish(run *wizardRun, set func(r *wizardRun)) wizardJSON {
	a.mu.Lock()
	defer a.mu.Unlock()
	set(run)
	run.finishedAt = time.Now()
	return run.json()
}

// wizardError turns an error from a wizard run into what the admin sees.
// Only *Error carries a sentence meant for people; anything else is
// reported as a server problem.
func wizardError(err error) *wizardErrorJSON {
	var e *Error
	if errors.As(err, &e) {
		return &wizardErrorJSON{Code: e.Code, Message: e.Message, Reasons: httpx.NonNil(e.Reasons)}
	}
	return &wizardErrorJSON{Code: "internal", Message: "Something went wrong on the server. Try again; if it keeps happening, tell an organiser.", Reasons: []string{}}
}

// providerIDs lists the providers this server offers, in the form's order.
func (d *Handlers) providerIDs() []string {
	ids := append(append([]string{}, Providers...), OtherProviders...)
	if Known(ProviderFake, d.testing()) {
		ids = append(ids, ProviderFake)
	}
	return ids
}

type providerJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	HasKey      bool   `json:"has_key"`
	Model       string `json:"model"`
	ActiveUntil *int64 `json:"active_until"`
	// Other marks a host of the Chat Completions table; HasList says it lists its models.
	Other   bool `json:"other"`
	HasList bool `json:"has_list"`
}

// keyProviderJSON is a provider in KeyStatus: providerJSON plus the
// session's copy of its model list.
type keyProviderJSON struct {
	providerJSON
	Models []Model `json:"models"`
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
	// CustomPrompt is true when PODIUM_AI_PROMPT_FILE replaces the
	// authoring rules. Never the text.
	CustomPrompt bool `json:"custom_prompt"`
	// PromptSource says where the wizard's rules come from: database,
	// file or built-in (v2.0.2). Never the text.
	PromptSource string `json:"prompt_source"`
}

// providerEntries returns what the session holds for each provider.
func (d *Handlers) providerEntries(r *http.Request) []keyProviderJSON {
	token := d.sessionToken(r)
	var out []keyProviderJSON
	for _, id := range d.providerIDs() {
		p := keyProviderJSON{providerJSON: providerJSON{ID: id, Name: ProviderName(id), Other: IsOther(id), HasList: HasModelList(id)}, Models: []Model{}}
		if e, ok := d.Wizard.Keys.GetProvider(token, id); ok {
			p.HasKey, p.Model, p.ActiveUntil = true, e.Model, httpx.Millis(e.Expires)
			if e.Models != nil {
				p.Models = e.Models
			}
		}
		out = append(out, p)
	}
	return out
}

func (d *Handlers) keyStatus(r *http.Request) keyStatusJSON {
	out := keyStatusJSON{Provider: ProviderAnthropic, Providers: d.providerEntries(r), ServerNow: time.Now().UnixMilli(), CustomPrompt: CustomPrompt(), PromptSource: d.wizardRulesSource(r)}
	if st, ok := d.Wizard.Keys.Status(d.sessionToken(r)); ok {
		out.Active, out.Provider, out.Model, out.ActiveUntil = true, st.Provider, st.Model, httpx.Millis(st.ActiveUntil)
	}
	return out
}

// wizardRulesSource is where the wizard's rules come from right now.
func (d *Handlers) wizardRulesSource(r *http.Request) string {
	if d.Prompts != nil {
		if a, err := d.Prompts.Active(r.Context(), KindWizardRules); err == nil {
			return a.Source
		}
	}
	if CustomPrompt() {
		return SourceFile
	}
	return SourceBuiltIn
}

func (d *Handlers) GetAIProviders(w http.ResponseWriter, r *http.Request) {
	out := providersJSON{ServerNow: time.Now().UnixMilli()}
	for _, p := range d.providerEntries(r) {
		out.Providers = append(out.Providers, p.providerJSON)
	}
	if st, ok := d.Wizard.Keys.Status(d.sessionToken(r)); ok {
		out.ActiveProvider = &st.Provider
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (d *Handlers) GetAIKey(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, d.keyStatus(r))
}

// noKeyMessage asks for a key for one provider.
func noKeyMessage(provider string) string {
	name := ProviderName(provider)
	article := "a"
	if strings.ContainsAny(name[:1], "AEIOU") {
		article = "an"
	}
	return "Add " + article + " " + name + " key first."
}

// GetAIModels lists a provider's models with the session's key, keeping
// the filtered list for the session.
func (d *Handlers) GetAIModels(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if !Known(provider, d.testing()) {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", ChooseProvider)
		return
	}
	token := d.sessionToken(r)
	e, ok := d.Wizard.Keys.GetProvider(token, provider)
	if !ok {
		httpx.WriteError(w, http.StatusConflict, "no_key", noKeyMessage(provider))
		return
	}
	if e.ListedAt.IsZero() || r.URL.Query().Get("refresh") == "1" {
		prov, err := NewProvider(Config{Provider: provider, Key: e.Key, BaseURL: d.Wizard.BaseURLs[provider]}, d.testing())
		if err != nil {
			httpx.WriteError(w, http.StatusConflict, "no_key", noKeyMessage(provider))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), keyCheckTimeout)
		defer cancel()
		models, err := prov.ListModels(ctx)
		if err != nil {
			e := wizardError(err)
			d.Log.Info("ai model listing failed", "user_id", auth.UserFrom(r.Context()).ID, "provider", provider, "code", e.Code)
			httpx.WriteError(w, http.StatusBadGateway, "provider_error", e.Message)
			return
		}
		d.Wizard.Keys.SetModels(token, provider, models)
		e, _ = d.Wizard.Keys.GetProvider(token, provider)
	}
	models := e.Models
	if models == nil {
		models = []Model{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"provider": provider, "models": models,
		"listed_at": e.ListedAt.UnixMilli(), "server_now": time.Now().UnixMilli()})
}

// PutAIKey checks a pasted key (and model, when one is given) with one
// call that costs no tokens and keeps it for the session and provider.
// The request body is never logged, and the key is wrapped in a Secret
// before anything else happens.
func (d *Handlers) PutAIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Key      string `json:"key"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	key := NewSecret(strings.TrimSpace(req.Key))
	req.Key = ""
	token := d.sessionToken(r)
	if !Known(req.Provider, d.testing()) {
		if req.Provider == ProviderFake {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The test provider exists only in testing mode.")
		} else {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", ChooseProvider)
		}
		return
	}
	if LooksLikeKey(req.Model) {
		req.Model = ""
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", KeyInModelField)
		return
	}
	model, ok := CleanModelIDFor(req.Provider, req.Model)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "A model id has only letters, digits and . _ - : and at most 128 characters.")
		return
	}
	held, holds := d.Wizard.Keys.GetProvider(token, req.Provider)
	if req.Provider == ProviderFake {
		key, model, holds = Secret{}, FakeModel, false
	} else if key.IsZero() {
		if !holds {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Paste the provider key.")
			return
		}
		key = held.Key
	} else {
		holds = false
	}
	prov, err := NewProvider(Config{Provider: req.Provider, Model: model, Key: key, BaseURL: d.Wizard.BaseURLs[req.Provider]}, d.testing())
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", wizardError(err).Message)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), keyCheckTimeout)
	defer cancel()
	var models []Model
	if model == "" {
		// The listing is the check, and fills the picker in one call.
		models, err = prov.ListModels(ctx)
	} else {
		err = prov.Check(ctx)
	}
	if err != nil {
		e := wizardError(err)
		d.Log.Info("ai key refused", "user_id", auth.UserFrom(r.Context()).ID, "provider", req.Provider, "model", model, "code", e.Code)
		httpx.WriteError(w, http.StatusBadRequest, "bad_key", e.Message)
		return
	}
	if holds {
		d.Wizard.Keys.SetModel(token, req.Provider, model)
	} else {
		d.Wizard.Keys.Put(token, KeyEntry{Provider: req.Provider, Model: model, Key: key})
	}
	if models != nil || req.Provider == ProviderFake {
		if req.Provider == ProviderFake {
			models, _ = prov.ListModels(ctx)
		}
		d.Wizard.Keys.SetModels(token, req.Provider, models)
	}
	d.Log.Info("ai key added", "user_id", auth.UserFrom(r.Context()).ID, "provider", req.Provider, "model", model, "kept_key", holds)
	httpx.WriteJSON(w, http.StatusOK, d.keyStatus(r))
}

func (d *Handlers) DeleteAIKey(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		d.Wizard.ForgetKey(d.sessionToken(r))
	} else {
		d.Wizard.Keys.ForgetProvider(d.sessionToken(r), provider)
	}
	d.Log.Info("ai key removed", "user_id", auth.UserFrom(r.Context()).ID, "provider", provider)
	w.WriteHeader(http.StatusNoContent)
}

// provider returns a client for the session's key of the named provider,
// or of the active one when name is empty, or writes 409 no_key or
// no_model.
func (d *Handlers) provider(w http.ResponseWriter, r *http.Request, name string) (Provider, KeyEntry, bool) {
	token := d.sessionToken(r)
	var e KeyEntry
	var ok bool
	if name == "" {
		e, ok = d.Wizard.Keys.Get(token)
	} else if Known(name, d.testing()) {
		e, ok = d.Wizard.Keys.GetProvider(token, name)
	}
	if !ok {
		httpx.WriteError(w, http.StatusConflict, "no_key", "Add a key first.")
		return nil, KeyEntry{}, false
	}
	if e.Model == "" {
		httpx.WriteError(w, http.StatusConflict, "no_model", "Choose a model for "+ProviderName(e.Provider)+" first.")
		return nil, KeyEntry{}, false
	}
	prov, err := NewProvider(Config{Provider: e.Provider, Model: e.Model, Key: e.Key, BaseURL: d.Wizard.BaseURLs[e.Provider]}, d.testing())
	if err != nil {
		// The key was accepted under another mode; ask for it again
		// rather than guess.
		d.Wizard.Keys.ForgetProvider(token, e.Provider)
		httpx.WriteError(w, http.StatusConflict, "no_key", "Add a key first.")
		return nil, KeyEntry{}, false
	}
	return prov, e, true
}

// takeSlot reserves the admin's one AI call, or writes 429 busy.
func (d *Handlers) takeSlot(w http.ResponseWriter, userID int64) (func(), bool) {
	release, ok := d.Wizard.Slots.Take(userID)
	if !ok {
		httpx.WriteBusy(w, 10, "One AI request at a time. Wait for the current one to finish.")
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
	// StudentsWrite is program (the default) or function; JavaLists, for a
	// function, is array (the default), arraylist or either. Absent means the default.
	StudentsWrite *string `json:"students_write"`
	JavaLists     *string `json:"java_lists"`
	// RewriteOf, in paste mode, is the problem to write again: Text then
	// says what should change (stage W).
	RewriteOf *int64 `json:"rewrite_of"`
	// Review keeps the model's answer for the person to read and edit before
	// any problem is created. Absent means false.
	Review bool `json:"review"`
}

// StartWizard asks the model for a problem in the background. The answer
// becomes a draft with origin ai and the "ai draft" tag, and input
// generation is queued, which chains output generation and validation.
func (d *Handlers) StartWizard(w http.ResponseWriter, r *http.Request) {
	var body wizardRequestJSON
	if !httpx.DecodeJSONLimit(w, r, &body, maxWizardBody) {
		return
	}
	var req Request
	var rewrite *rewriteLink
	switch body.Mode {
	case "brief":
		if body.RewriteOf != nil {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose paste to rewrite a problem.")
			return
		}
		if body.Brief == nil {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Write a brief or paste a problem.")
			return
		}
		switch body.Brief.Difficulty {
		case "easy", "medium", "hard":
		default:
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose easy, medium or hard.")
			return
		}
		req.Brief = &Brief{Topic: body.Brief.Topic, Difficulty: body.Brief.Difficulty,
			Constraints: body.Brief.Constraints, SubtaskIdeas: body.Brief.Subtasks}
	case "paste":
		req.Pasted = body.Text
		if body.RewriteOf != nil {
			var ok bool
			if req.Old, rewrite, ok = d.loadRewrite(w, r, *body.RewriteOf); !ok {
				return
			}
		}
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose brief or paste.")
		return
	}
	function := false
	if s := body.StudentsWrite; s != nil {
		switch *s {
		case StudentsWriteProgram:
		case StudentsWriteFunction:
			function = true
		default:
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose a whole program or one function.")
			return
		}
	}
	if function {
		req.JavaLists = JavaListsArray
		if l := body.JavaLists; l != nil {
			if *l != JavaListsArray && *l != JavaListsArrayList && *l != JavaListsEither {
				httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose arrays, ArrayList or either.")
				return
			}
			req.JavaLists = *l
		}
	}
	u := auth.UserFrom(r.Context())
	prov, entry, ok := d.provider(w, r, body.Provider)
	if !ok {
		return
	}
	if err := CheckRequest(req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", wizardError(err).Message)
		return
	}
	if d.Prompts != nil {
		// Built once here: the retry and every provider call of this run
		// use the same text, whatever is saved meanwhile.
		var system string
		var err error
		if function {
			system, err = d.Prompts.FunctionSystem(r.Context(), req.JavaLists)
		} else {
			system, err = d.Prompts.WizardSystem(r.Context())
		}
		if err != nil {
			httpx.InternalError(d.Log, w, r, err)
			return
		}
		req.System = system
	}
	release, ok := d.takeSlot(w, u.ID)
	if !ok {
		return
	}
	run := d.Wizard.start("generate", u.ID, 0, entry, rewrite)
	d.Wizard.mark(run, function, req.JavaLists, body.Review)
	d.Log.Info("ai wizard started", "user_id", u.ID, "wizard_id", run.id, "mode", body.Mode, "function", function, "review", body.Review, "rewrite", rewrite != nil, "provider", entry.Provider, "model", entry.Model)
	go func() {
		defer release()
		d.publishWizard(u.ID, d.runGenerate(run, prov, req, function, u.ID, entry.Key))
	}()
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"wizard_id": run.id, "server_now": time.Now().UnixMilli()})
}

// runGenerate is the background half of startWizard.
// For a function run the draft is a function problem and the queued input
// generation carries the proof spec: the model's solutions.
func (d *Handlers) runGenerate(run *wizardRun, prov Provider, req Request, function bool, userID int64, key Secret) wizardJSON {
	ctx, cancel := context.WithTimeout(context.Background(), wizardRunTimeout)
	attempt := 0
	req.onAttempt = func(took time.Duration) {
		attempt++
		d.Log.Info("ai wizard attempt", "wizard_id", run.id, "attempt", attempt, "seconds", int(took.Seconds()))
	}
	var answer string
	req.onAnswer = func(text string) { answer = text }
	// The key goes before the cut, so no part of it survives at the cut point.
	keep := func(r *wizardRun) {
		text := key.scrub(answer)
		limit := maxKeptAnswer
		if r.review {
			limit = maxReviewAnswer
		}
		r.answer, r.answerCut = clipText(text, limit), len(text) > limit
	}
	var pack *answerPack
	var err error
	if function {
		var fp *FunctionPack
		if fp, err = GenerateFunction(ctx, prov, req); err == nil {
			pack = &answerPack{draft: ToDraftFunction(fp), solutions: fp.Solutions(), function: true}
		}
	} else {
		var wp *WizardPack
		if wp, err = Generate(ctx, prov, req); err == nil {
			pack = &answerPack{draft: ToDraft(wp)}
		}
	}
	cancel()
	if run.review {
		// Nothing is created: the answer waits for the person. A failed
		// answer that carries text waits too, so it can be corrected.
		var aiErr *Error
		switch {
		case err == nil:
			return d.finishReview(run, nil, keep)
		case errors.As(err, &aiErr) && aiErr.Code == CodeInvalidAnswer && answer != "":
			d.Log.Info("ai request ended in review with a failed answer", "user_id", userID, "wizard_id", run.id)
			return d.finishReview(run, httpx.NonNil(aiErr.Reasons), keep)
		}
	}
	if err != nil {
		return d.failWizard(run, err, key, keep)
	}
	ctx, cancel = context.WithTimeout(context.Background(), draftTimeout)
	defer cancel()
	id, err := d.createFromPack(ctx, pack, userID)
	if err != nil {
		return d.failWizard(run, err, key, keep)
	}
	d.Log.Info("ai draft made by wizard", "user_id", userID, "wizard_id", run.id, "problem_id", id)
	return d.Wizard.finish(run, func(r *wizardRun) { r.state, r.problemID = wizardDraft, id; keep(r) })
}

// finishReview ends a review run: its answer waits for the person, with the
// reasons the checks gave, or none.
func (d *Handlers) finishReview(run *wizardRun, reasons []string, keep func(*wizardRun)) wizardJSON {
	return d.Wizard.finish(run, func(r *wizardRun) { r.state, r.reasons = wizardReview, reasons; keep(r) })
}

// createFromPack makes the draft for a checked answer and queues input
// generation, which chains output generation and validation. A run with
// review and a run without it both end here, so they make the same draft.
// The origin and the "ai draft" tag come from CreateDraft, never from a request.
func (d *Handlers) createFromPack(ctx context.Context, pack *answerPack, userID int64) (int64, error) {
	p, err := d.Problems.CreateDraft(ctx, pack.draft, userID)
	var fields problems.FieldErrors
	if errors.As(err, &fields) {
		reasons := make([]string, 0, len(fields))
		for k, m := range fields {
			reasons = append(reasons, k+": "+m)
		}
		sort.Strings(reasons)
		err = &Error{Code: CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: reasons}
	}
	if err != nil {
		return 0, err
	}
	d.Log.Info("ai draft created", "user_id", userID, "problem_id", p.ID)
	var g *problems.InputGeneration
	if pack.function {
		g, err = d.Problems.RequestInputGenerationProof(ctx, p.ID, userID, pack.solutions)
	} else {
		g, err = d.Problems.RequestInputGeneration(ctx, p.ID, userID)
	}
	if err != nil {
		// The draft stands; the editor offers Generate inputs.
		d.Log.Warn("ai draft: input generation not queued", "problem_id", p.ID, "err", err)
	} else {
		d.Log.Info("input generation queued", "by", userID, "problem_id", p.ID, "input_generation_id", g.ID)
	}
	return p.ID, nil
}

// failWizard records a failed run. An *Error is logged by its code
// only (it never holds the key); anything else is a server fault.
func (d *Handlers) failWizard(run *wizardRun, err error, key Secret, keep func(*wizardRun)) wizardJSON {
	var aiErr *Error
	if errors.As(err, &aiErr) {
		d.Log.Info("ai request ended without a result", "user_id", run.userID, "wizard_id", run.id, "code", aiErr.Code)
	} else {
		d.logProviderFault("ai request failed", key, err, "wizard_id", run.id)
	}
	e := wizardError(err)
	return d.Wizard.finish(run, func(r *wizardRun) { r.state, r.err = wizardFailed, e; keep(r) })
}

// logProviderFault logs an error a provider client did not wrap in an *Error,
// with the session's key stripped from its text first.
func (d *Handlers) logProviderFault(msg string, key Secret, err error, attrs ...any) {
	d.Log.Error(msg, append(attrs, "err", key.scrub(err.Error()))...)
}

func (d *Handlers) publishWizard(userID int64, v wizardJSON) {
	if d.Hub != nil {
		d.Hub.Publish(userID, "wizard", v)
	}
}

// noWizard is the refusal of a run that is missing, someone else's or gone.
const noWizard = "There is no such AI request. It may have ended over an hour ago or before a restart."

func (d *Handlers) GetWizard(w http.ResponseWriter, r *http.Request) {
	v, ok := d.Wizard.view(r.PathValue("id"), auth.UserFrom(r.Context()).ID)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", noWizard)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, v)
}

// GetWizardAnswer returns what the provider sent back for a finished run, to
// the person who started it only.
func (d *Handlers) GetWizardAnswer(w http.ResponseWriter, r *http.Request) {
	v, found, finished := d.Wizard.keptAnswer(r.PathValue("id"), auth.UserFrom(r.Context()).ID)
	switch {
	case !found:
		httpx.WriteError(w, http.StatusNotFound, "not_found", noWizard)
	case !finished:
		httpx.WriteError(w, http.StatusConflict, "not_finished", "The AI request has not finished yet.")
	default:
		if v.review && !v.Cut && v.Text != "" {
			// What Create draft will change, from the text as it stands.
			if _, notes, _, _ := loadAnswer(v.Text, v.Kind, v.JavaLists); notes != nil {
				v.Repairs = notes
			}
		}
		httpx.WriteJSON(w, http.StatusOK, v)
	}
}
