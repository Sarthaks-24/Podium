package ai

// The AI edit routes (v2.0.2 stage E). A draft is made in memory from the
// model's answer and shown as a diff; nothing is saved until Accept, which
// goes through problems.AcceptEdit. The provider key is used exactly as the
// wizard uses it and never reaches a log line, an error or a draft.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// editKeep is how long an unaccepted draft is kept.
const editKeep = time.Hour

// maxEditReasons bounds the parser messages shown for one refused answer.
const maxEditReasons = 20

// Copy for the refusals of Accept (PLAN 3.8).
const (
	copyEditContestRunning = "A contest that holds this problem is running. Accept after it ends."
	copyEditNoDraft        = "There is no such AI draft. It may have expired, been rejected, or been lost in a restart. Ask for the edit again."
)

// Limits on drafts held in memory (the VM has 2 GB).
const (
	// maxDraftsPerUser: the oldest of an organiser's drafts is dropped for a sixth.
	maxDraftsPerUser = 5
	// maxDraftBytes bounds the text of all drafts together.
	maxDraftBytes = 32 << 20
	// maxDraftSize bounds one merged draft.
	maxDraftSize = 2 << 20
	// acceptingKeep is how long a draft may stay in accepting; after it
	// the sweep drops the draft, so a stuck Accept cannot hold it forever.
	acceptingKeep = 2 * time.Minute
)

// errDraftsFull says the server holds too many drafts to keep another.
var errDraftsFull = errors.New("too many AI drafts are waiting")

// editDraft is one unaccepted AI edit, held in memory for the organiser who
// asked. It holds pack text files only, never test data and never a key.
type editDraft struct {
	id          string
	userID      int64
	problemID   int64
	baseHash    string
	instruction string
	files       map[string][]byte
	created     time.Time
	// accepting is set while an Accept of this draft runs, so a second
	// Accept does not run beside it.
	accepting      bool
	acceptingSince time.Time
}

func (d *editDraft) size() int {
	n := 0
	for _, b := range d.files {
		n += len(b)
	}
	return n
}

// editDrafts holds drafts by id. One draft per organiser and problem: a new
// one replaces the old; an organiser keeps at most maxDraftsPerUser.
type editDrafts struct {
	mu sync.Mutex
	m  map[string]*editDraft
}

// sweep drops expired drafts. The caller holds e.mu.
func (e *editDrafts) sweep() {
	now := time.Now()
	for id, d := range e.m {
		if d.accepting {
			if now.Sub(d.acceptingSince) > acceptingKeep {
				delete(e.m, id)
			}
		} else if now.Sub(d.created) > editKeep {
			delete(e.m, id)
		}
	}
}

func (e *editDrafts) put(d *editDraft) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.m == nil {
		e.m = map[string]*editDraft{}
	}
	e.sweep()
	for id, old := range e.m {
		if old.userID == d.userID && old.problemID == d.problemID && !old.accepting {
			delete(e.m, id)
		}
	}
	for {
		var oldest *editDraft
		count := 0
		for _, old := range e.m {
			if old.userID != d.userID {
				continue
			}
			count++
			if !old.accepting && (oldest == nil || old.created.Before(oldest.created)) {
				oldest = old
			}
		}
		if count < maxDraftsPerUser || oldest == nil {
			break
		}
		delete(e.m, oldest.id)
	}
	total := d.size()
	for _, old := range e.m {
		total += old.size()
	}
	if total > maxDraftBytes {
		return errDraftsFull
	}
	e.m[d.id] = d
	return nil
}

func (e *editDrafts) get(id string, userID, problemID int64) (*editDraft, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sweep()
	d, ok := e.m[id]
	if !ok || d.userID != userID || d.problemID != problemID {
		return nil, false
	}
	return d, true
}

// begin hands out a draft for Accept, once at a time: it is not given again
// until release, and gone after drop.
func (e *editDrafts) begin(id string, userID, problemID int64) (*editDraft, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sweep()
	d, ok := e.m[id]
	if !ok || d.userID != userID || d.problemID != problemID || d.accepting {
		return nil, false
	}
	d.accepting, d.acceptingSince = true, time.Now()
	return d, true
}

// release lets a failed Accept be tried again.
func (e *editDrafts) release(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d, ok := e.m[id]; ok {
		d.accepting = false
	}
}

func (e *editDrafts) drop(id string, userID, problemID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d, ok := e.m[id]; ok && d.userID == userID && d.problemID == problemID {
		delete(e.m, id)
	}
}

type editFileJSON struct {
	Path string `json:"path"`
	// Status is "changed" or "added".
	Status string `json:"status"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func problemIDOf(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := httpx.ParseID(r.PathValue("id"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem.")
	}
	return id, ok
}

// copyEditRefused leads every refusal of a reply the pack cannot take; the
// reasons go under "Details".
const copyEditRefused = "The AI's edit would break this problem, so nothing was saved. Press Ask for a draft to try again, describe a smaller change, or rewrite the whole problem."

// maxSentBack bounds the model's text shown with a refusal.
const maxSentBack = 200 << 10

// editFail is a refusal of the reply that is not retried and has its own
// status and code.
type editFail struct {
	status        int
	code, message string
}

func (e *editFail) Error() string { return e.message }

type sentFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// sentAnswer is one answer of the model as shown under a refusal: its summary
// and returned files, and whether the cap cut it short.
type sentAnswer struct {
	Summary string     `json:"summary"`
	Files   []sentFile `json:"files"`
	Cut     bool       `json:"cut"`
}

// sentBack lists what the AI sent back, newest first, from this request's
// answers only. Test data is left out and the whole text is capped.
func sentBack(answers []*EditAnswer) map[string][]sentAnswer {
	budget := maxSentBack
	out := []sentAnswer{}
	for i := len(answers) - 1; i >= 0; i-- {
		a := sentAnswer{Summary: answers[i].Summary, Files: []sentFile{}}
		for _, f := range answers[i].Files {
			if strings.HasPrefix(f.Path, "tests/") {
				continue
			}
			content := f.Content
			if len(content) > budget {
				content, a.Cut = clipText(content, budget), true
			}
			budget -= len(content)
			a.Files = append(a.Files, sentFile{Path: f.Path, Content: content})
		}
		out = append(out, a)
	}
	return map[string][]sentAnswer{"answers": out}
}

// editRefusal writes a refusal of a reply: the first sentence, the reasons
// (at most maxEditReasons) and what the AI sent back.
func editRefusal(w http.ResponseWriter, status int, code, message string, reasons []string, answers []*EditAnswer) {
	if len(reasons) > maxEditReasons {
		reasons = append(append([]string{}, reasons[:maxEditReasons]...), fmt.Sprintf("And %d more.", len(reasons)-maxEditReasons))
	}
	e := httpx.APIError{Code: code, Message: message, Problems: reasons}
	if len(answers) > 0 {
		e.SentBack = sentBack(answers)
	}
	httpx.WriteJSON(w, status, map[string]httpx.APIError{"error": e})
}

// editProblemError answers the errors the problems package gives an edit.
func (d *Handlers) editProblemError(w http.ResponseWriter, r *http.Request, err error) {
	var fields problems.FieldErrors
	var pack *problems.PackError
	var policy *problems.EditPolicyError
	var live *problems.ReplaceLiveError
	var langLocked *problems.LanguageLockedError
	switch {
	case errors.Is(err, problems.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem.")
	case errors.Is(err, problems.ErrDeleted):
		httpx.WriteError(w, http.StatusConflict, "deleted", err.Error())
	case errors.Is(err, problems.ErrEditStale):
		httpx.WriteError(w, http.StatusConflict, "changed", err.Error())
	case errors.As(err, &live):
		httpx.WriteError(w, http.StatusConflict, "contest_running", copyEditContestRunning)
	case errors.Is(err, problems.ErrModeLocked):
		httpx.WriteError(w, http.StatusConflict, "mode_locked", err.Error())
	case errors.As(err, &langLocked):
		httpx.WriteError(w, http.StatusConflict, "mode_locked", err.Error())
	case errors.As(err, &pack):
		editRefusal(w, http.StatusUnprocessableEntity, CodeInvalidAnswer, copyEditRefused, pack.Problems, nil)
	case errors.As(err, &policy):
		editRefusal(w, http.StatusUnprocessableEntity, CodeInvalidAnswer, copyEditRefused, policy.Problems, nil)
	case errors.As(err, &fields):
		msg := "Some fields need fixing."
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			msg = fields[keys[0]]
		}
		httpx.WriteError(w, http.StatusConflict, "bad_request", msg)
	default:
		httpx.InternalError(d.Log, w, r, err)
	}
}

// AIEdit asks the model to edit a problem and returns a draft. The call is
// made while the request waits (up to CallTimeout). Nothing is saved.
func (d *Handlers) AIEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := problemIDOf(w, r)
	if !ok {
		return
	}
	var body struct {
		Instruction string `json:"instruction"`
		Provider    string `json:"provider"`
	}
	if !httpx.DecodeJSONLimit(w, r, &body, maxWizardBody) {
		return
	}
	if err := CheckInstruction(body.Instruction); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", wizardError(err).Message)
		return
	}
	snap, err := d.Problems.EditSnapshot(r.Context(), id)
	if err != nil {
		d.editProblemError(w, r, err)
		return
	}
	prov, entry, ok := d.provider(w, r, body.Provider)
	if !ok {
		return
	}
	var rules, prefs string
	if d.Prompts != nil {
		if rules, prefs, err = d.Prompts.PackPromptInputs(r.Context()); err != nil {
			httpx.InternalError(d.Log, w, r, err)
			return
		}
	}
	packPrompt, err := problems.PackPromptWith(rules, prefs)
	if err != nil {
		httpx.InternalError(d.Log, w, r, err)
		return
	}
	u := auth.UserFrom(r.Context())
	release, ok := d.takeSlot(w, u.ID)
	if !ok {
		return
	}
	defer release()

	req := EditRequest{Instruction: body.Instruction, System: BuildEditSystem(packPrompt)}
	for name, content := range snap.Files {
		req.Files = append(req.Files, EditFile{Path: name, Content: string(content)})
	}
	// Both calls share one deadline: the retry runs inside CallTimeout.
	ctx, cancel := context.WithTimeout(r.Context(), CallTimeout)
	defer cancel()
	var answers []*EditAnswer
	res, err := Repair(func(rt Retry) (*editResult, error) {
		req.Previous, req.Fix, req.Shorter = rt.Previous, rt.Fix, rt.Shorter
		answer, err := prov.Edit(ctx, req)
		if err != nil {
			return nil, err
		}
		answers = append(answers, answer)
		return d.mergeAnswer(ctx, snap, answer)
	})
	if err != nil {
		d.editRefused(w, r, err, u.ID, id, entry, answers)
		return
	}
	draft := &editDraft{id: res.id, userID: u.ID, problemID: id, baseHash: snap.Hash,
		instruction: body.Instruction, files: res.merged, created: time.Now()}
	if draft.size() > maxDraftSize {
		httpx.WriteError(w, http.StatusUnprocessableEntity, CodeInvalidAnswer, copyEditTooLarge)
		return
	}
	if err := d.Wizard.edits.put(draft); err != nil {
		httpx.WriteBusy(w, 30, "The server holds too many AI drafts right now. Accept or reject one, or try again in a minute.")
		return
	}

	files := []editFileJSON{}
	for _, path := range sortedKeys(res.merged) {
		old, had := snap.Files[path]
		if had && string(old) == string(res.merged[path]) {
			continue
		}
		status := "changed"
		if !had {
			status = "added"
		}
		files = append(files, editFileJSON{Path: path, Status: status, Before: string(old), After: string(res.merged[path])})
	}
	d.Log.Info("ai edit drafted", "user_id", u.ID, "problem_id", id, "provider", entry.Provider, "model", entry.Model, "files", len(files), "kept", len(res.kept))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"draft_id": draft.id, "summary": res.summary, "files": files, "kept": httpx.NonNil(res.kept),
		"provider": entry.Provider, "model": entry.Model, "server_now": time.Now().UnixMilli()})
}

const copyEditTooLarge = "The edited problem would be larger than 2 MB of text. Ask for a smaller change."

// editResult is a merged draft before it is stored.
type editResult struct {
	id      string
	merged  map[string][]byte
	kept    []string
	summary string
}

// mergeAnswer turns one model answer into a merged draft. A refusal that
// asking again cannot fix comes back as *editFail or *problems.EditPolicyError
// and ends the run; one the pack parser gives comes back as an invalid_answer
// *Error carrying the answer, which Repair retries once.
func (d *Handlers) mergeAnswer(ctx context.Context, snap *problems.EditSnapshot, answer *EditAnswer) (*editResult, error) {
	if len(answer.Files) == 0 {
		if answer.Unsupported != "" {
			return nil, &editFail{http.StatusUnprocessableEntity, "not_in_pack", "A pack cannot carry that change: " + answer.Unsupported}
		}
		return nil, &editFail{http.StatusUnprocessableEntity, "no_change", "The model did not change anything. Say more exactly what to change and ask again."}
	}
	reply := map[string][]byte{}
	var dup []string
	replySize := 0
	for _, f := range answer.Files {
		replySize += len(f.Content)
	}
	if replySize > maxDraftSize {
		return nil, &editFail{http.StatusUnprocessableEntity, CodeInvalidAnswer, copyEditTooLarge}
	}
	for _, f := range answer.Files {
		if _, seen := reply[f.Path]; seen {
			dup = append(dup, f.Path+" appears twice in the answer.")
		}
		reply[f.Path] = []byte(f.Content)
	}
	if len(dup) > 0 {
		return nil, &Error{Code: CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: dup, answer: answer.Raw}
	}
	merged, kept, err := d.Problems.MergeEdit(ctx, snap, reply)
	var pack *problems.PackError
	if errors.As(err, &pack) {
		return nil, &Error{Code: CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: pack.Problems, answer: answer.Raw}
	}
	if err != nil {
		return nil, err
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	return &editResult{id: hex.EncodeToString(b[:]), merged: merged, kept: kept, summary: answer.Summary}, nil
}

// editRefused answers a run that ended without a draft and logs it: who, which
// problem and model, the names of the files the last answer carried, and the
// number of reasons with the start of each. Never the content, never the key.
func (d *Handlers) editRefused(w http.ResponseWriter, r *http.Request, err error, userID, problemID int64, entry KeyEntry, answers []*EditAnswer) {
	var fail *editFail
	var policy *problems.EditPolicyError
	var e *Error
	var reasons []string
	switch {
	case errors.As(err, &fail):
		httpx.WriteError(w, fail.status, fail.code, fail.message)
		return
	case errors.As(err, &policy):
		reasons = policy.Problems
		if policy.ModeChange {
			editRefusal(w, http.StatusUnprocessableEntity, "mode_change", policy.Problems[0], nil, answers)
		} else {
			editRefusal(w, http.StatusUnprocessableEntity, CodeInvalidAnswer, copyEditRefused, reasons, answers)
		}
	case errors.As(err, &e):
		if e.Code != CodeInvalidAnswer {
			d.Log.Info("ai edit ended without a result", "user_id", userID, "problem_id", problemID, "provider", entry.Provider, "code", e.Code)
			we := wizardError(err)
			httpx.WriteJSON(w, http.StatusBadGateway, map[string]httpx.APIError{"error": {Code: we.Code, Message: we.Message, Problems: we.Reasons}})
			return
		}
		reasons = e.Reasons
		editRefusal(w, http.StatusUnprocessableEntity, CodeInvalidAnswer, copyEditRefused, reasons, answers)
	default:
		d.editProblemError(w, r, err)
		return
	}
	var names, starts []string
	if len(answers) > 0 {
		for _, f := range answers[len(answers)-1].Files {
			names = append(names, f.Path)
		}
	}
	for _, reason := range reasons {
		starts = append(starts, clipText(reason, 200))
	}
	d.Log.Info("ai edit refused", "user_id", userID, "problem_id", problemID, "provider", entry.Provider, "model", entry.Model,
		"reply_files", names, "reasons", len(reasons), "reason_starts", starts)
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AIEditAccept saves a draft as one new revision of the problem, marks the
// problem as an AI draft and queues Validate. A running contest, a deleted
// problem or a problem that changed since the draft refuse it.
func (d *Handlers) AIEditAccept(w http.ResponseWriter, r *http.Request) {
	id, ok := problemIDOf(w, r)
	if !ok {
		return
	}
	u := auth.UserFrom(r.Context())
	draft, ok := d.Wizard.edits.begin(r.PathValue("draft"), u.ID, id)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", copyEditNoDraft)
		return
	}
	p, err := d.Problems.AcceptEdit(r.Context(), id, draft.baseHash, draft.files, u.ID, draft.instruction)
	if err != nil {
		d.Wizard.edits.release(draft.id)
		d.editProblemError(w, r, err)
		return
	}
	d.Wizard.edits.drop(draft.id, u.ID, id)
	d.Log.Info("ai edit accepted", "user_id", u.ID, "problem_id", id)
	out := map[string]any{"problem_id": p.ID, "validation_queued": false, "not_ready": []string{}}
	if _, err := d.Problems.RequestValidation(r.Context(), id, u.ID); err == nil {
		out["validation_queued"] = true
	} else {
		var nr *problems.NotReadyError
		if errors.As(err, &nr) {
			out["not_ready"] = nr.Problems
		} else {
			d.Log.Warn("ai edit: validation not queued", "problem_id", id, "err", err)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// AIEditReject forgets a draft. It changes nothing else.
func (d *Handlers) AIEditReject(w http.ResponseWriter, r *http.Request) {
	id, ok := problemIDOf(w, r)
	if !ok {
		return
	}
	d.Wizard.edits.drop(r.PathValue("draft"), auth.UserFrom(r.Context()).ID, id)
	w.WriteHeader(http.StatusNoContent)
}
