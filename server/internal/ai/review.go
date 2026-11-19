package ai

// Review of an AI answer before any problem exists. A run with review keeps
// the model's answer in memory and creates nothing; the person edits it, and
// POST /api/admin/ai/drafts sends the answer back to become a draft through
// the same decode, repair, check and create code a run without review uses.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/problems"
)

const (
	// maxReviewAnswer is the longest answer a review run keeps and the
	// longest one Create draft accepts.
	maxReviewAnswer = 2 << 20
	// maxReviewBody bounds the request: JSON escaping of JSON text can
	// double every byte of the answer.
	maxReviewBody = 2 * maxReviewAnswer
)

// answerPack is a checked answer ready to become a draft: the draft, the
// model's solutions for the proof and whether it is a function problem.
type answerPack struct {
	draft     problems.Draft
	solutions map[string]string
	function  bool
}

// loadAnswer turns the text of a model's answer into a checked pack. It
// decodes, repairs and checks, which is what a run without review does to
// each provider answer. notes says what the repairs changed. A pack that
// cannot be decoded is an *Error; a pack that decodes but fails the checks
// gives its reasons and no pack.
func loadAnswer(text, kind, javaLists string) (pack *answerPack, notes, reasons []string, err error) {
	if len(text) > maxReviewAnswer {
		return nil, nil, nil, &Error{Code: CodeTooLarge, Message: "The answer is longer than 2 MB."}
	}
	switch kind {
	case StudentsWriteFunction:
		fp, err := DecodeFunctionFor(text, javaLists)
		if err != nil {
			return nil, nil, nil, err
		}
		notes = repairFunction(fp)
		if reasons = CheckFunction(fp); len(reasons) > 0 {
			return nil, notes, reasons, nil
		}
		return &answerPack{draft: ToDraftFunction(fp), solutions: fp.Solutions(), function: true}, notes, nil, nil
	case StudentsWriteProgram:
		wp, err := DecodePack(text)
		if err != nil {
			return nil, nil, nil, err
		}
		notes = repairGeneratorArgs(wp.Subtasks)
		if reasons = Check(wp); len(reasons) > 0 {
			return nil, notes, reasons, nil
		}
		return &answerPack{draft: ToDraft(wp)}, notes, nil, nil
	}
	return nil, nil, nil, &Error{Code: CodeBadRequest, Message: "Choose a whole program or one function."}
}

// repairFunction is the repair step of a function answer, shared by a run
// and by loadAnswer so both repair the same way.
func repairFunction(p *FunctionPack) []string {
	return append(repairFunctionPack(p), repairGeneratorArgs(p.Subtasks)...)
}

// keyShape finds a string that looks like a provider key: one of the key
// prefixes at the start of a word, then a long run of key characters.
var keyShape = func() *regexp.Regexp {
	quoted := make([]string, len(keyPrefixes))
	for i, p := range keyPrefixes {
		quoted[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile(`(?:^|[^A-Za-z0-9])(?:` + strings.Join(quoted, "|") + `)[A-Za-z0-9_-]{16,}`)
}()

func hasKeyShape(text string) bool { return keyShape.MatchString(text) }

// reasonsOf is the reasons an error carries, or its message.
func reasonsOf(err error) []string {
	var e *Error
	if errors.As(err, &e) && len(e.Reasons) > 0 {
		return e.Reasons
	}
	return []string{err.Error()}
}

type draftRequestJSON struct {
	Answer    string `json:"answer"`
	Kind      string `json:"kind"`
	JavaLists string `json:"java_lists"`
}

type draftErrorJSON = map[string]httpx.APIError

// PostDraft creates a draft from an answer the person has read and maybe
// edited. It needs no provider key, but takes the person's AI slot so that a
// double click cannot make two drafts. Neither the body nor the decoder's
// text is logged.
func (d *Handlers) PostDraft(w http.ResponseWriter, r *http.Request) {
	var body draftRequestJSON
	if !decodeReviewBody(w, r, &body) {
		return
	}
	switch body.Kind {
	case StudentsWriteProgram, StudentsWriteFunction:
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose a whole program or one function.")
		return
	}
	if body.Kind == StudentsWriteProgram {
		body.JavaLists = ""
	} else if body.JavaLists == "" {
		body.JavaLists = JavaListsArray
	}
	if body.Kind == StudentsWriteFunction && body.JavaLists != JavaListsArray && body.JavaLists != JavaListsArrayList && body.JavaLists != JavaListsEither {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose arrays, ArrayList or either.")
		return
	}
	if len(body.Answer) > maxReviewAnswer {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "answer_too_large", "The answer is longer than 2 MB. Shorten it and try again.")
		return
	}
	u := auth.UserFrom(r.Context())
	if hasKeyShape(body.Answer) {
		refuseAnswer(w, "The answer contains text that looks like an API key. Remove it and try again.")
		return
	}
	release, ok := d.takeSlot(w, u.ID)
	if !ok {
		return
	}
	defer release()
	pack, _, reasons, err := loadAnswer(body.Answer, body.Kind, body.JavaLists)
	if err != nil {
		refuseAnswer(w, reasonsOf(err)...)
		return
	}
	if len(reasons) > 0 {
		refuseAnswer(w, reasons...)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), draftTimeout)
	defer cancel()
	id, err := d.createFromPack(ctx, pack, u.ID)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == CodeInvalidAnswer {
			refuseAnswer(w, e.Reasons...)
			return
		}
		httpx.InternalError(d.Log, w, r, err)
		return
	}
	d.Log.Info("ai draft from reviewed answer", "user_id", u.ID, "problem_id", id, "kind", body.Kind)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"problem_id": id})
}

// decodeReviewBody reads the request body of PostDraft: JSON of at most
// maxReviewBody bytes, with 413 answer_too_large above it.
func decodeReviewBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewBody)
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "bad_request", "Send the request body as application/json.")
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		err = dec.Decode(&struct{}{})
		if err == io.EOF {
			return true
		}
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "answer_too_large", "The answer is longer than 2 MB. Shorten it and try again.")
	} else {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON for this action.")
	}
	return false
}

// refuseAnswer writes 400 invalid_answer with what is wrong with the answer.
func refuseAnswer(w http.ResponseWriter, reasons ...string) {
	if len(reasons) == 0 {
		reasons = []string{"The answer could not be used."}
	}
	httpx.WriteJSON(w, http.StatusBadRequest, draftErrorJSON{"error": {Code: "invalid_answer",
		Message: "The answer could not be used. Fix what is listed and try again.", Problems: reasons}})
}
