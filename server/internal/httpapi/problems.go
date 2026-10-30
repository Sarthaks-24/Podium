package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// maxEditBody bounds a problem save: a 64 KB statement and four 64 KB
// sources, as JSON.
const maxEditBody = 512 << 10

// previewBytes is how much of each test file the editor shows inline.
const previewBytes = 200

// packOps lets one pack import, test replacement or export run at a time:
// each can hold over 100 MB, and the VM has 2 GB.
var packOps = make(chan struct{}, 1)

// withPackSlot waits for the pack slot; it writes 429 busy and returns
// false when the request ends first.
func withPackSlot(w http.ResponseWriter, r *http.Request) (func(), bool) {
	select {
	case packOps <- struct{}{}:
		return func() { <-packOps }, true
	case <-r.Context().Done():
		writeBusy(w, 5, "Another pack is being processed. Try again in a moment.")
		return nil, false
	}
}

type problemSummaryJSON struct {
	ID          int64    `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Difficulty  string   `json:"difficulty"`
	Tags        []string `json:"tags"`
	Origin      string   `json:"origin"`
	Archived    bool     `json:"archived"`
	TestCount   int      `json:"test_count"`
	Points      int      `json:"points"`
	Validated   bool     `json:"validated"`
	ValidatedAt *int64   `json:"validated_at"`
	UpdatedAt   int64    `json:"updated_at"`
	// ShownInPractice: practice after a contest has
	// shown students its hidden tests.
	ShownInPractice bool `json:"shown_in_practice"`
}

type sourceJSON struct {
	Language string `json:"language"`
	Source   string `json:"source"`
}

type subtaskJSON struct {
	Idx    int    `json:"idx,omitempty"`
	Points int    `json:"points"`
	Title  string `json:"title"`
	Tests  string `json:"tests"`
	// GeneratorArgs are the arguments the generator gets for this
	// subtask's planned tests. Left out of a save, they are kept.
	GeneratorArgs []string `json:"generator_args"`
}

// planJSON is one test the generator makes.
type planJSON struct {
	Idx     int      `json:"idx"`
	Subtask int      `json:"subtask"`
	Seed    int64    `json:"seed"`
	Args    []string `json:"args"`
}

type testJSON struct {
	Idx           int    `json:"idx"`
	Sample        bool   `json:"sample"`
	SubtaskIdx    *int   `json:"subtask_idx"`
	InputSize     int64  `json:"input_size"`
	OutputSize    int64  `json:"output_size"`
	InputPreview  string `json:"input_preview"`
	OutputPreview string `json:"output_preview"`
	// OutputMissing marks a test whose expected output is still to be
	// generated from the reference solution.
	OutputMissing bool `json:"output_missing"`
}

type problemJSON struct {
	problemSummaryJSON
	StatementMD   string        `json:"statement_md"`
	TimeLimitMS   int           `json:"time_limit_ms"`
	MemoryLimitMB int           `json:"memory_limit_mb"`
	Checker       string        `json:"checker"`
	Epsilon       float64       `json:"epsilon"`
	CheckerSource string        `json:"checker_source"`
	Reference     sourceJSON    `json:"reference"`
	Brute         *sourceJSON   `json:"brute"`
	Validator     *sourceJSON   `json:"validator"`
	Generator     *sourceJSON   `json:"generator"`
	Plan          []planJSON    `json:"plan"`
	Samples       []int         `json:"samples"`
	Subtasks      []subtaskJSON `json:"subtasks"`
	Tests         []testJSON    `json:"tests"`
	ContentHash   string        `json:"content_hash"`
	ValidatedHash *string       `json:"validated_hash"`
	Problems      []string      `json:"problems"`
	Warnings      []string      `json:"warnings"`
	// Revision is the revision the working copy is. LiveContests are the
	// running contests that pin the problem: while there are any, a save
	// needs a reason.
	Revision     *revisionRefJSON       `json:"revision"`
	LiveContests []problems.LiveContest `json:"live_contests"`
}

type problemEditJSON struct {
	Title         string        `json:"title"`
	Slug          string        `json:"slug"`
	StatementMD   string        `json:"statement_md"`
	TimeLimitMS   int           `json:"time_limit_ms"`
	MemoryLimitMB int           `json:"memory_limit_mb"`
	Checker       string        `json:"checker"`
	Epsilon       float64       `json:"epsilon"`
	CheckerSource string        `json:"checker_source"`
	Reference     sourceJSON    `json:"reference"`
	Brute         *sourceJSON   `json:"brute"`
	Validator     *sourceJSON   `json:"validator"`
	Tags          []string      `json:"tags"`
	Difficulty    string        `json:"difficulty"`
	Samples       []int         `json:"samples"`
	Subtasks      []subtaskJSON `json:"subtasks"`
	// Reason says why the problem changed; required while a live contest
	// pins it.
	Reason string `json:"reason"`
	// Generator and Plan are raw so a save can tell "left out" (keep the
	// generator, the plan and each subtask's arguments) from null or [].
	Generator json.RawMessage `json:"generator"`
	Plan      json.RawMessage `json:"plan"`
}

type validationJSON struct {
	ID            int64                    `json:"id"`
	State         string                   `json:"state"`
	ContentHash   string                   `json:"content_hash"`
	Current       bool                     `json:"current"`
	QueuedAt      int64                    `json:"queued_at"`
	FinishedAt    *int64                   `json:"finished_at"`
	QueuePosition *int                     `json:"queue_position"`
	Compile       []judgeapi.CompileResult `json:"compile"`
	Tests         []judgeapi.TestResult    `json:"tests"`
	Summary       *problems.Summary        `json:"summary"`
	Error         *string                  `json:"error"`
}

func millis(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

func summaryJSON(e problems.BankEntry) problemSummaryJSON {
	return problemSummaryJSON{ID: e.ID, Slug: e.Slug, Title: e.Title, Difficulty: e.Difficulty, Tags: nonNil(e.Tags),
		Origin: e.Origin, Archived: e.Archived, TestCount: e.TestCount, Points: e.Points, Validated: e.Validated,
		ValidatedAt: validatedAt(e.Validated, e.ValidatedAt), UpdatedAt: e.UpdatedAt.UnixMilli(), ShownInPractice: e.ShownInPractice}
}

// validatedAt hides the time of a validation that no longer covers the
// problem, so the editor never shows a stale "Validated" time.
func validatedAt(valid bool, t time.Time) *int64 {
	if !valid {
		return nil
	}
	return millis(t)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func optSource(s *problems.Source) *sourceJSON {
	if s == nil {
		return nil
	}
	return &sourceJSON{s.Language, s.Source}
}

func fromSourceJSON(s *sourceJSON) *problems.Source {
	if s == nil {
		return nil
	}
	return &problems.Source{Language: s.Language, Source: s.Source}
}

// preview returns the start of a blob as valid UTF-8 text.
func (d Deps) preview(sha string) string {
	b, err := d.Problems.Blobs.Read(sha, previewBytes)
	if err != nil {
		return ""
	}
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// problemJSON is Problem in docs/API.md, with its revisions.
func (d Deps) problemJSON(ctx context.Context, p *problems.Problem) (problemJSON, error) {
	points := 0
	for _, st := range p.Subtasks {
		points += st.Points
	}
	out := problemJSON{
		problemSummaryJSON: problemSummaryJSON{ID: p.ID, Slug: p.Slug, Title: p.Title, Difficulty: p.Difficulty,
			Tags: nonNil(p.Tags), Origin: p.Origin, Archived: p.Archived, TestCount: len(p.Tests), Points: points,
			Validated: p.Validated(), ValidatedAt: validatedAt(p.Validated(), p.ValidatedAt), UpdatedAt: p.UpdatedAt.UnixMilli()},
		StatementMD: p.Statement, TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB,
		Checker: p.Checker, Epsilon: p.Epsilon, CheckerSource: p.CheckerSource,
		Reference: sourceJSON{p.Reference.Language, p.Reference.Source},
		Brute:     optSource(p.Brute), Validator: optSource(p.Validator), Generator: optSource(p.Generator),
		Plan:    []planJSON{},
		Samples: p.Samples, Subtasks: []subtaskJSON{}, Tests: []testJSON{},
		ContentHash: p.ContentHash, Problems: nonNil(p.Problems()), Warnings: nonNil(p.Warnings()),
	}
	if out.Samples == nil {
		out.Samples = []int{}
	}
	if p.ValidatedHash != "" {
		out.ValidatedHash = &p.ValidatedHash
	}
	for _, st := range p.Subtasks {
		out.Subtasks = append(out.Subtasks, subtaskJSON{st.Idx, st.Points, st.Title, st.Tests, nonNil(st.GeneratorArgs)})
	}
	for _, t := range p.Plan {
		out.Plan = append(out.Plan, planJSON{t.Test, t.Subtask, t.Seed, nonNil(t.Args)})
	}
	for _, t := range p.Tests {
		tj := testJSON{Idx: t.Idx, Sample: p.IsSample(t.Idx), InputSize: t.InputSize, OutputSize: t.OutputSize,
			InputPreview: d.preview(t.InputSHA), OutputMissing: t.OutputSHA == ""}
		if t.OutputSHA != "" {
			tj.OutputPreview = d.preview(t.OutputSHA)
		}
		if st := p.SubtaskOf(t.Idx); st > 0 {
			tj.SubtaskIdx = &st
		}
		out.Tests = append(out.Tests, tj)
	}
	return out, d.addRevisions(ctx, p, &out)
}

// writeProblem answers with Problem.
func (d Deps) writeProblem(w http.ResponseWriter, r *http.Request, status int, p *problems.Problem, warnings ...string) {
	out, err := d.problemJSON(r.Context(), p)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out.Warnings = append(out.Warnings, warnings...)
	WriteJSON(w, status, out)
}

func validationOut(v *problems.Validation) validationJSON {
	out := validationJSON{ID: v.ID, State: v.State, ContentHash: v.ContentHash, Current: v.Current,
		QueuedAt: v.QueuedAt.UnixMilli(), FinishedAt: millis(v.FinishedAt), Summary: v.Summary,
		Compile: v.Compile, Tests: v.Tests}
	if out.Compile == nil {
		out.Compile = []judgeapi.CompileResult{}
	}
	if out.Tests == nil {
		out.Tests = []judgeapi.TestResult{}
	}
	if v.State == problems.ValidationQueued {
		out.QueuePosition = &v.QueuePosition
	}
	if v.Error != "" {
		out.Error = &v.Error
	}
	return out
}

// problemID reads the {id} path value; it writes a 404 and returns false
// when it is not a number.
func problemID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such problem.")
		return 0, false
	}
	return id, true
}

// problemError maps the problems package's errors to responses.
func (d Deps) problemError(w http.ResponseWriter, r *http.Request, err error) {
	var fields problems.FieldErrors
	var pack *problems.PackError
	var notReady *problems.NotReadyError
	switch {
	case errors.Is(err, problems.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "There is no such problem.")
	case errors.As(err, &fields):
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "bad_request",
			Message: "Some fields need fixing.", Fields: fields}})
	case errors.As(err, &pack):
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "bad_pack",
			Message: "The pack cannot be imported. Fix these and upload it again.", Problems: pack.Problems}})
	case errors.As(err, &notReady):
		WriteJSON(w, http.StatusConflict, map[string]APIError{"error": {Code: "not_ready",
			Message: "The problem is not ready. Fix these first.", Problems: notReady.Problems}})
	default:
		d.internalError(w, r, err)
	}
}

func (d Deps) listProblems(w http.ResponseWriter, r *http.Request) {
	list, err := d.Problems.List(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := make([]problemSummaryJSON, 0, len(list))
	for _, e := range list {
		out = append(out, summaryJSON(e))
	}
	WriteJSON(w, http.StatusOK, map[string]any{"problems": out})
}

func (d Deps) createProblem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p, err := d.Problems.Create(r.Context(), req.Title, req.Slug, userFrom(r.Context()).ID)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("problem created", "by", userFrom(r.Context()).ID, "problem_id", p.ID)
	d.writeProblem(w, r, http.StatusCreated, p)
}

// readZipBody reads an application/zip request body up to the pack limit.
func readZipBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "application/zip" && mt != "application/x-zip-compressed" {
		WriteError(w, http.StatusUnsupportedMediaType, "bad_request", "Upload a .zip file.")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, problems.MaxPackBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "The zip is larger than 32 MB.")
		return nil, false
	}
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "The upload was interrupted. Try again.")
		return nil, false
	}
	return body, true
}

func (d Deps) importProblem(w http.ResponseWriter, r *http.Request) {
	body, ok := readZipBody(w, r)
	if !ok {
		return
	}
	release, ok := withPackSlot(w, r)
	if !ok {
		return
	}
	defer release()
	pk, err := problems.ReadPack(body)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	p, err := d.Problems.Import(r.Context(), pk, userFrom(r.Context()).ID)
	var fields problems.FieldErrors
	if errors.As(err, &fields) && fields["slug"] != "" {
		WriteError(w, http.StatusConflict, "conflict", fmt.Sprintf("A problem with the slug %s already exists. Change slug in problem.yaml, or archive and rename the old problem.", pk.Problem.Slug))
		return
	}
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("problem imported", "by", userFrom(r.Context()).ID, "problem_id", p.ID, "tests", len(p.Tests), "ignored", len(pk.Ignored))
	var ignored []string
	for _, name := range pk.Ignored {
		ignored = append(ignored, fmt.Sprintf("Ignored %s: podium does not use it yet.", name))
	}
	d.writeProblem(w, r, http.StatusCreated, p, ignored...)
}

func (d Deps) getProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	p, err := d.Problems.Get(r.Context(), id)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.writeProblem(w, r, http.StatusOK, p)
}

func (d Deps) updateProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	var req problemEditJSON
	if !decodeJSONLimit(w, r, &req, maxEditBody) {
		return
	}
	e := problems.Edit{Title: req.Title, Slug: req.Slug, Statement: req.StatementMD,
		TimeLimitMS: req.TimeLimitMS, MemoryLimitMB: req.MemoryLimitMB,
		Checker: req.Checker, Epsilon: req.Epsilon, CheckerSource: req.CheckerSource,
		Reference: problems.Source{Language: req.Reference.Language, Source: req.Reference.Source},
		Brute:     fromSourceJSON(req.Brute), Validator: fromSourceJSON(req.Validator),
		Tags: req.Tags, Difficulty: req.Difficulty, Samples: req.Samples,
		By: userFrom(r.Context()).ID, Reason: req.Reason}
	for _, st := range req.Subtasks {
		e.Subtasks = append(e.Subtasks, problems.Subtask{Points: st.Points, Title: st.Title, Tests: st.Tests, GeneratorArgs: st.GeneratorArgs})
	}
	if req.Generator != nil || req.Plan != nil {
		g, err := d.generatorEdit(r, id, req.Generator, req.Plan)
		if err != nil {
			d.problemError(w, r, err)
			return
		}
		e.Generator = g
	}
	p, err := d.Problems.Update(r.Context(), id, e)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.writeProblem(w, r, http.StatusOK, p)
}

// generatorEdit builds the generator part of a save from the raw fields.
// When only one of them was sent, the other keeps its stored value.
func (d Deps) generatorEdit(r *http.Request, id int64, rawGen, rawPlan json.RawMessage) (*problems.GeneratorEdit, error) {
	var cur *problems.Problem
	if rawGen == nil || rawPlan == nil {
		var err error
		if cur, err = d.Problems.Get(r.Context(), id); err != nil {
			return nil, err
		}
	}
	g := &problems.GeneratorEdit{}
	if rawGen != nil {
		var src *sourceJSON
		if err := json.Unmarshal(rawGen, &src); err != nil {
			return nil, problems.FieldErrors{"generator": "Send the generator as {language, source} or null."}
		}
		g.Source = fromSourceJSON(src)
	} else {
		g.Source = cur.Generator
	}
	if rawPlan != nil {
		var plan []planJSON
		if err := json.Unmarshal(rawPlan, &plan); err != nil {
			return nil, problems.FieldErrors{"plan": "Send the plan as a list of {idx, subtask, seed, args}."}
		}
		for _, t := range plan {
			g.Plan = append(g.Plan, problems.PlannedTest{Test: t.Idx, Subtask: t.Subtask, Seed: t.Seed, Args: t.Args})
		}
	} else {
		g.Plan = cur.Plan
	}
	return g, nil
}

func (d Deps) replaceTests(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	body, ok := readZipBody(w, r)
	if !ok {
		return
	}
	release, ok := withPackSlot(w, r)
	if !ok {
		return
	}
	defer release()
	note := problems.RevisionNote{By: userFrom(r.Context()).ID, Reason: r.URL.Query().Get("reason")}
	p, err := d.Problems.ReplaceTests(r.Context(), id, body, note)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("tests replaced", "by", userFrom(r.Context()).ID, "problem_id", id, "tests", len(p.Tests))
	d.writeProblem(w, r, http.StatusOK, p)
}

func (d Deps) downloadTest(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	part := r.PathValue("part")
	if err != nil || (part != "input" && part != "output") {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such test file.")
		return
	}
	sha, err := d.Problems.TestBlob(r.Context(), id, idx, part == "output")
	if errors.Is(err, problems.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such test file.")
		return
	}
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	f, err := d.Problems.Blobs.Open(sha)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	defer f.Close()
	ext := map[string]string{"input": "in", "output": "out"}[part]
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="test-%02d.%s"`, idx, ext))
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, f)
}

func (d Deps) exportProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	p, err := d.Problems.Get(r.Context(), id)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	release, ok := withPackSlot(w, r)
	if !ok {
		return
	}
	defer release()
	// Streamed: only one test file is in memory at a time. An error part
	// way can no longer change the status, so it is logged and the
	// download ends short.
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, p.Slug))
	w.Header().Set("Cache-Control", "no-store")
	err = problems.WritePack(w, p, func(sha string) ([]byte, error) { return d.Problems.Blobs.Read(sha, problems.MaxPackFile+1) })
	if err != nil {
		d.Log.Error("export failed part way", "problem_id", id, "err", err)
	}
}

func (d Deps) archiveProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	if err := d.Problems.Archive(r.Context(), id); err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("problem archived", "by", userFrom(r.Context()).ID, "problem_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) validateProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	v, err := d.Problems.RequestValidation(r.Context(), id, userFrom(r.Context()).ID)
	if err != nil {
		d.problemError(w, r, err)
		return
	}
	d.Log.Info("validation queued", "by", userFrom(r.Context()).ID, "problem_id", id, "validation_id", v.ID)
	WriteJSON(w, http.StatusAccepted, validationOut(v))
}

func (d Deps) getValidation(w http.ResponseWriter, r *http.Request) {
	id, ok := problemID(w, r)
	if !ok {
		return
	}
	v, err := d.Problems.LatestValidation(r.Context(), id)
	if errors.Is(err, problems.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "This problem has not been validated yet.")
		return
	}
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, validationOut(v))
}

// decodeJSONLimit is decodeJSON with a larger body limit.
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return decodeJSONBody(w, r, v)
}
