package ai

// The fix route: "Fix <language> with AI". The model writes one language of
// a function problem again from the statement, the reference and the public
// samples; the answer is checked, then queued as one proof round that adds the
// language only if it passes. The key and the provider slot are the wizard's:
// provider and takeSlot.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// AIFix asks the model for one language of a function problem and queues the
// proof of its answer. The call is made while the request waits (up to
// CallTimeout). Nothing is saved: the round saves the language only on a pass.
func (d *Handlers) AIFix(w http.ResponseWriter, r *http.Request) {
	id, ok := problemIDOf(w, r)
	if !ok {
		return
	}
	lang := r.PathValue("language")
	if problems.ExtByLanguage[lang] == "" {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", problems.BadLanguageMessage)
		return
	}
	var body struct {
		Provider string `json:"provider"`
	}
	if !httpx.DecodeJSONLimit(w, r, &body, maxWizardBody) {
		return
	}
	snap, err := d.Problems.FixSnapshot(r.Context(), id, lang)
	if err != nil {
		problems.WriteError(d.Log, w, r, err)
		return
	}
	prov, entry, ok := d.provider(w, r, body.Provider)
	if !ok {
		return
	}
	u := auth.UserFrom(r.Context())
	release, ok := d.takeSlot(w, u.ID)
	if !ok {
		return
	}
	defer release()

	req := FixRequest{Language: lang, Statement: snap.Statement, Reference: snap.Reference.Source,
		Samples: snap.Samples, Failure: FailureText(snap.Failure)}
	ctx, cancel := context.WithTimeout(r.Context(), CallTimeout)
	answer, err := GenerateFix(ctx, prov, req)
	cancel()
	if err != nil {
		d.fixRefused(w, err, u.ID, id, lang, entry)
		return
	}
	err = d.Problems.RequestLanguageProof(r.Context(), id, u.ID, problems.ProofRequest{Language: lang, Solution: answer.Solution,
		Parts:    &problems.LanguageParts{Top: answer.LockedTop, Starter: answer.Starter, Bottom: answer.LockedBottom},
		WantHash: snap.ContentHash})
	if err != nil {
		problems.WriteError(d.Log, w, r, err)
		return
	}
	d.Log.Info("ai fix queued", "user_id", u.ID, "problem_id", id, "language", lang, "provider", entry.Provider, "model", entry.Model)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"state": "checking", "language": lang,
		"provider": entry.Provider, "model": entry.Model, "server_now": time.Now().UnixMilli()})
}

// fixRefused answers a fix that ended without a queued round and logs it:
// who, which problem, language, provider and model, and the number of
// reasons. Never the content, never the key.
func (d *Handlers) fixRefused(w http.ResponseWriter, err error, userID, problemID int64, lang string, entry KeyEntry) {
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInvalidAnswer {
		if e == nil {
			d.logProviderFault("ai fix failed", entry.Key, err, "problem_id", problemID, "language", lang)
		} else {
			d.Log.Info("ai fix ended without a result", "user_id", userID, "problem_id", problemID, "language", lang, "provider", entry.Provider, "code", e.Code)
		}
		we := wizardError(err)
		httpx.WriteJSON(w, http.StatusBadGateway, map[string]httpx.APIError{"error": {Code: we.Code, Message: we.Message, Problems: we.Reasons}})
		return
	}
	editRefusal(w, http.StatusUnprocessableEntity, CodeInvalidAnswer,
		"The AI's "+judge.Languages[lang].Name+" code could not be used, so nothing was changed. Press Fix again, or write the code yourself.", e.Reasons, nil)
	d.Log.Info("ai fix refused", "user_id", userID, "problem_id", problemID, "language", lang, "provider", entry.Provider, "model", entry.Model, "reasons", len(e.Reasons))
}
