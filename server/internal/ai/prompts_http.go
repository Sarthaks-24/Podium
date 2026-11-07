package ai

// The prompt editor's routes: GET /api/admin/ai/prompts,
// PUT /api/admin/ai/prompts/{kind} and POST /api/admin/ai/prompts/{kind}/reset.
// The owner edits the rules of both prompts; an admin edits only the
// preferences. Nothing here echoes a body into a log line or an audit row.

import (
	"errors"
	"net/http"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/problems"
)

const (
	// maxPromptBody bounds a prompt request: 16 KB of rules as JSON.
	maxPromptBody = 128 << 10
	// historyLimit is how many versions of a kind the editor lists.
	historyLimit = 30

	copyNotAllowed = "You do not have access to this."
	copyStale      = "Someone saved a newer version. Reload to see it, then save again."
	copyNoVersion  = "That version does not exist."
	copyNoSuchKind = "There is no such prompt."
)

var kindLabels = map[string]string{
	KindWizardRules: "Wizard prompt rules",
	KindPackRules:   "Outside-AI prompt rules",
	KindPreferences: "Preferences",
}

// canEditKind says whether a person with this role may change the kind.
func canEditKind(role auth.Role, kind string) bool {
	if kind == KindPreferences {
		return role.AtLeast(auth.Admin)
	}
	return role.AtLeast(auth.Owner)
}

type promptVersionJSON struct {
	PromptVersion
	DefaultChanged bool `json:"default_changed"`
}

type promptKindJSON struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	CanEdit bool   `json:"can_edit"`
	// Active is the text in use and where it comes from.
	Active ActivePrompt `json:"active"`
	// Default is the built-in text, shown read-only.
	Default string `json:"default"`
	// DefaultChanged is true when the active database version was saved
	// against an older built-in text.
	DefaultChanged bool `json:"default_changed"`
	// Fixed is the part of the prompt the owner cannot edit, shown
	// read-only beside the rules.
	Fixed   string              `json:"fixed"`
	History []promptVersionJSON `json:"history"`
	// Limit is the longest body: bytes for rules, characters for preferences.
	Limit int `json:"limit"`
}

type promptsJSON struct {
	Kinds     []promptKindJSON `json:"kinds"`
	ServerNow int64            `json:"server_now"`
}

type promptPutJSON struct {
	Body      string `json:"body"`
	BaseID    int64  `json:"base_id"`
	DryRun    bool   `json:"dry_run"`
	RestoreOf int64  `json:"restore_of"`
}

type promptResetJSON struct {
	BaseID int64 `json:"base_id"`
}

func (d *Handlers) promptsFor(w http.ResponseWriter) *Prompts {
	if d.Prompts == nil {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "This API route does not exist.")
	}
	return d.Prompts
}

// fixedText is what the editor shows read-only for a kind.
func fixedText(kind string) (string, error) {
	switch kind {
	case KindWizardRules:
		return fixedPrompt, nil
	case KindPackRules:
		return problems.PackPromptGenerated()
	}
	return "", nil
}

func (d *Handlers) kindView(r *http.Request, p *Prompts, role auth.Role, kind string) (promptKindJSON, error) {
	ctx := r.Context()
	active, err := p.Active(ctx, kind)
	if err != nil {
		return promptKindJSON{}, err
	}
	hist, err := p.History(ctx, kind, historyLimit)
	if err != nil {
		return promptKindJSON{}, err
	}
	fixed, err := fixedText(kind)
	if err != nil {
		return promptKindJSON{}, err
	}
	limit := MaxRulesBytes
	if kind == KindPreferences {
		limit = MaxPreferencesChars
	}
	out := promptKindJSON{Kind: kind, Label: kindLabels[kind], CanEdit: canEditKind(role, kind), Active: active,
		Default: DefaultText(kind), Fixed: fixed, History: []promptVersionJSON{}, Limit: limit}
	for _, v := range hist {
		out.History = append(out.History, promptVersionJSON{v, p.DefaultChanged(v)})
	}
	if active.Version != nil {
		out.DefaultChanged = p.DefaultChanged(*active.Version)
	}
	return out, nil
}

// GetPrompts serves GET /api/admin/ai/prompts: every kind for the owner, the
// preferences for an admin.
func (d *Handlers) GetPrompts(w http.ResponseWriter, r *http.Request) {
	p := d.promptsFor(w)
	if p == nil {
		return
	}
	u := auth.UserFrom(r.Context())
	if !u.Role.AtLeast(auth.Admin) {
		httpx.WriteError(w, http.StatusForbidden, "not_allowed", copyNotAllowed)
		return
	}
	out := promptsJSON{Kinds: []promptKindJSON{}, ServerNow: time.Now().UnixMilli()}
	for _, kind := range []string{KindWizardRules, KindPackRules, KindPreferences} {
		if !canEditKind(u.Role, kind) {
			continue
		}
		v, err := d.kindView(r, p, u.Role, kind)
		if err != nil {
			httpx.InternalError(d.Log, w, r, err)
			return
		}
		out.Kinds = append(out.Kinds, v)
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

// promptWrite checks the role and kind of a write and returns the person.
func (d *Handlers) promptWrite(w http.ResponseWriter, r *http.Request) (*Prompts, auth.User, string, bool) {
	p := d.promptsFor(w)
	if p == nil {
		return nil, auth.User{}, "", false
	}
	kind := r.PathValue("kind")
	u := auth.UserFrom(r.Context())
	if !IsPromptKind(kind) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", copyNoSuchKind)
		return nil, u, kind, false
	}
	if !canEditKind(u.Role, kind) {
		httpx.WriteError(w, http.StatusForbidden, "not_allowed", copyNotAllowed)
		return nil, u, kind, false
	}
	return p, u, kind, true
}

// promptWriteError maps a store error to a response.
func (d *Handlers) promptWriteError(w http.ResponseWriter, r *http.Request, err error) {
	var pe *PromptError
	switch {
	case errors.Is(err, ErrStale):
		httpx.WriteError(w, http.StatusConflict, "stale_version", copyStale)
	case errors.Is(err, ErrNoVersion):
		httpx.WriteError(w, http.StatusNotFound, "not_found", copyNoVersion)
	case errors.As(err, &pe):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_prompt", pe.Message)
	default:
		httpx.InternalError(d.Log, w, r, err)
	}
}

// PutPrompt serves PUT /api/admin/ai/prompts/{kind}: a save, a restore
// (restore_of) or, with dry_run, the full prompt that a save would send.
func (d *Handlers) PutPrompt(w http.ResponseWriter, r *http.Request) {
	p, u, kind, ok := d.promptWrite(w, r)
	if !ok {
		return
	}
	var body promptPutJSON
	if !httpx.DecodeJSONLimit(w, r, &body, maxPromptBody) {
		return
	}
	ctx := r.Context()
	if body.DryRun {
		text := body.Body
		if body.RestoreOf > 0 {
			hist, err := p.History(ctx, kind, 1<<20)
			if err != nil {
				d.promptWriteError(w, r, err)
				return
			}
			found := false
			for _, v := range hist {
				if v.ID == body.RestoreOf && !v.IsReset {
					text, found = v.Body, true
				}
			}
			if !found {
				d.promptWriteError(w, r, ErrNoVersion)
				return
			}
		}
		previews, err := p.Preview(ctx, kind, text)
		if err != nil {
			d.promptWriteError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"dry_run": true, "previews": previews})
		return
	}
	var v PromptVersion
	var err error
	if body.RestoreOf > 0 {
		v, err = p.Restore(ctx, kind, body.RestoreOf, body.BaseID)
	} else {
		v, err = p.Save(ctx, kind, body.Body, body.BaseID)
	}
	if err != nil {
		d.promptWriteError(w, r, err)
		return
	}
	d.Log.Info("ai prompt saved", "user_id", u.ID, "kind", kind, "version_id", v.ID, "sha", sha(v.Body))
	d.writeKind(w, r, p, u, kind)
}

// ResetPrompt serves POST /api/admin/ai/prompts/{kind}/reset.
func (d *Handlers) ResetPrompt(w http.ResponseWriter, r *http.Request) {
	p, u, kind, ok := d.promptWrite(w, r)
	if !ok {
		return
	}
	var body promptResetJSON
	if !httpx.DecodeJSONLimit(w, r, &body, maxPromptBody) {
		return
	}
	v, err := p.Reset(r.Context(), kind, body.BaseID)
	if err != nil {
		d.promptWriteError(w, r, err)
		return
	}
	d.Log.Info("ai prompt reset", "user_id", u.ID, "kind", kind, "version_id", v.ID)
	d.writeKind(w, r, p, u, kind)
}

func (d *Handlers) writeKind(w http.ResponseWriter, r *http.Request, p *Prompts, u auth.User, kind string) {
	v, err := d.kindView(r, p, u.Role, kind)
	if err != nil {
		httpx.InternalError(d.Log, w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kind": v, "server_now": time.Now().UnixMilli()})
}
