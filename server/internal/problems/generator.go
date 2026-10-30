package problems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Tests from a generator: a problem may
// carry a generator and a plan of the tests it makes. "Generate inputs"
// runs the generator in isolate once per planned test, stores each output
// as that test's input, then queues output generation from the reference,
// which queues validation.

// Generator limits and the input generation job's priority (the same as
// output generation: behind every contest job, ahead of validation).
const (
	GenerateInputsPriority = 85
	// GeneratorTimeLimitMS is the generator's limit per test before the
	// language factor (a Python generator gets three times as much CPU).
	GeneratorTimeLimitMS = 5000
	GeneratorMemoryMB    = 256
)

// AIDraftTag marks a problem the wizard made until a person first saves
// it.
const AIDraftTag = "ai draft"

// PlannedTest is one test the generator makes: it runs with argv seed
// followed by Args, and its output becomes test Test's input.
type PlannedTest struct {
	Test    int      `json:"test" yaml:"test"`
	Subtask int      `json:"subtask" yaml:"subtask"`
	Seed    int64    `json:"seed" yaml:"seed"`
	Args    []string `json:"args" yaml:"args,flow"`
}

// checkPlan checks the generator arguments of each subtask and the plan.
// Planned tests may not exist yet: generating inputs creates them.
func checkPlan(errs FieldErrors, p *Problem) {
	for i, st := range p.Subtasks {
		if msg := checkArgs(st.GeneratorArgs); msg != "" {
			errs[fmt.Sprintf("subtasks.%d.generator_args", i)] = fmt.Sprintf("Subtask %d: %s", st.Idx, msg)
		}
	}
	if len(p.Plan) == 0 {
		return
	}
	if p.Generator == nil {
		errs["plan"] = "Planned tests need a generator. Add one, or remove the plan."
		return
	}
	if len(p.Plan) > MaxTests {
		errs["plan"] = fmt.Sprintf("Plan at most %d tests.", MaxTests)
		return
	}
	sort.Slice(p.Plan, func(i, j int) bool { return p.Plan[i].Test < p.Plan[j].Test })
	seen := map[int]bool{}
	for _, t := range p.Plan {
		msg := ""
		switch {
		case t.Test < 1 || t.Test > MaxTests:
			msg = "Number planned tests from 1 to 999."
		case seen[t.Test]:
			msg = fmt.Sprintf("Test %d is planned twice.", t.Test)
		case p.IsSample(t.Test):
			msg = fmt.Sprintf("Test %d is a sample. Samples come from the statement, not the generator.", t.Test)
		case t.Subtask < 1 || t.Subtask > len(p.Subtasks):
			msg = fmt.Sprintf("Planned test %d names subtask %d, which does not exist.", t.Test, t.Subtask)
		case t.Seed < 0 || t.Seed > judgeapi.MaxGeneratorSeed:
			msg = fmt.Sprintf("Planned test %d: use a seed from 0 to %d.", t.Test, judgeapi.MaxGeneratorSeed)
		default:
			if m := checkArgs(t.Args); m != "" {
				msg = fmt.Sprintf("Planned test %d: %s", t.Test, m)
			}
		}
		if msg != "" {
			errs["plan"] = msg
			return
		}
		seen[t.Test] = true
	}
}

func checkArgs(args []string) string {
	if len(args) > judgeapi.MaxGeneratorArgs {
		return fmt.Sprintf("use at most %d generator arguments.", judgeapi.MaxGeneratorArgs)
	}
	for _, a := range args {
		if !judgeapi.ValidGeneratorArg(a) {
			return fmt.Sprintf("the generator argument %q may use only letters, digits and _ . , : = + -, up to %d characters.",
				jobs.Clip(a), judgeapi.MaxGeneratorArg)
		}
	}
	return ""
}

// planWarnings are advice about the plan that does not block validation.
func (p *Problem) planWarnings() []string {
	if p.Generator == nil || len(p.Plan) == 0 {
		return nil
	}
	var out []string
	exists := map[int]bool{}
	for _, t := range p.Tests {
		exists[t.Idx] = true
	}
	missing := 0
	for _, t := range p.Plan {
		if st := p.SubtaskOf(t.Test); st != t.Subtask {
			out = append(out, fmt.Sprintf("Planned test %d is for subtask %d, but subtask %d does not list it.", t.Test, t.Subtask, t.Subtask))
		}
		if !exists[t.Test] {
			missing++
		}
	}
	if missing > 0 {
		out = append(out, fmt.Sprintf("%d planned tests have no input yet. Use Generate inputs.", missing))
	}
	return out
}

// PlanHash covers the generator and the plan, which ContentHash leaves
// out: editing the generator does not unvalidate the tests it already
// made, but a generation queued before the edit must not be applied.
func PlanHash(p *Problem) string {
	m := struct {
		Language string        `json:"language"`
		SHA      string        `json:"sha"`
		Plan     []PlannedTest `json:"plan"`
	}{Plan: nonNilPlan(p.Plan)}
	if p.Generator != nil {
		m.Language, m.SHA = p.Generator.Language, SHA([]byte(p.Generator.Source))
	}
	b, _ := json.Marshal(m)
	return SHA(b)
}

func nonNilPlan(p []PlannedTest) []PlannedTest {
	if p == nil {
		return []PlannedTest{}
	}
	return p
}

func withoutAIDraftTag(tags []string) []string {
	out := tags[:0:0]
	for _, t := range tags {
		if strings.ToLower(strings.TrimSpace(t)) != AIDraftTag {
			out = append(out, t)
		}
	}
	return out
}

// InputGeneration is a generate inputs run as the editor shows it.
type InputGeneration struct {
	ID        int64
	ProblemID int64
	// Planned is how many tests the run was asked to make.
	Planned    int
	State      string // queued, running, done, error
	QueuedAt   time.Time
	FinishedAt time.Time
	Compile    []judgeapi.CompileResult
	Tests      []judgeapi.TestResult
	Summary    *InputSummary
	Error      string
}

// InputSummary is what a finished input generation changed. Nothing is
// applied unless every planned test got an input.
type InputSummary struct {
	Applied  int      `json:"applied"`
	Problems []string `json:"problems"`
	// Generating is set when output generation was queued next.
	Generating bool `json:"generating,omitempty"`
}

// RequestInputGeneration queues a run of the generator for every planned
// test. When it succeeds, output generation and then validation follow.
func (s *Service) RequestInputGeneration(ctx context.Context, id, userID int64) (*InputGeneration, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	switch {
	case p.Generator == nil:
		return nil, &NotReadyError{[]string{"Add a generator before generating inputs."}}
	case len(p.Plan) == 0:
		return nil, &NotReadyError{[]string{"Plan the generator's tests before generating inputs."}}
	case strings.TrimSpace(p.Reference.Source) == "":
		return nil, &NotReadyError{[]string{"Write the reference solution first: outputs are generated from it once the inputs exist."}}
	}
	if err := pendingGeneration(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := s.queue().enqueueInputGeneration(ctx, tx, p, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.LatestInputGeneration(ctx, id)
}

func (q queue) enqueueInputGeneration(ctx context.Context, tx *sql.Tx, p *Problem, userID int64) error {
	sha, err := q.blobs.Put(ctx, tx, []byte(p.Generator.Source))
	if err != nil {
		return err
	}
	pl := judgeapi.GenerateInputsPayload{ProblemID: p.ID, ContentHash: p.ContentHash, PlanHash: PlanHash(p),
		Generator:   judgeapi.Program{Language: p.Generator.Language, SourceSHA: sha},
		TimeLimitMS: GeneratorTimeLimitMS, MemoryLimitMB: GeneratorMemoryMB}
	for _, t := range p.Plan {
		pl.Tests = append(pl.Tests, judgeapi.PlannedInput{Idx: t.Test, Subtask: t.Subtask, Seed: t.Seed, Args: nonNil(t.Args)})
	}
	jobID, err := q.jobs.Enqueue(ctx, tx, judgeapi.KindGenerateInputs, GenerateInputsPriority, pl, userID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO input_generations (problem_id, job_id, requested_by, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, jobID, nullUser(userID), q.at())
	return err
}

// LatestInputGeneration returns the problem's most recent input
// generation, or ErrNotFound.
func (s *Service) LatestInputGeneration(ctx context.Context, id int64) (*InputGeneration, error) {
	var g InputGeneration
	var jobID, created int64
	err := s.DB.QueryRowContext(ctx, `SELECT id, job_id, created_at FROM input_generations WHERE problem_id = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&g.ID, &jobID, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.ProblemID, g.QueuedAt = id, time.UnixMilli(created)
	job, err := s.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if pl, err := decodeGenerateInputs(job.Payload); err == nil {
		g.Planned = len(pl.Tests)
	}
	g.Compile, g.Tests, g.FinishedAt = job.Result.Compile, job.Result.Tests, job.FinishedAt
	switch job.State {
	case jobs.Queued:
		g.State = ValidationQueued
	case jobs.Leased:
		g.State = ValidationRunning
	case jobs.Failed:
		g.State, g.Error = ValidationError, job.Error
	case jobs.Done:
		g.State = "done"
		var sm InputSummary
		if json.Unmarshal(job.Result.Summary, &sm) == nil {
			g.Summary = &sm
		}
	}
	return &g, nil
}

// InputGenerationJob returns the problem and the requester of the input
// generation that job jobID runs, or ErrNotFound. The requester is 0 when
// their account is gone.
func (s *Service) InputGenerationJob(ctx context.Context, jobID int64) (problemID, requestedBy int64, err error) {
	var by sql.NullInt64
	err = s.DB.QueryRowContext(ctx, "SELECT problem_id, requested_by FROM input_generations WHERE job_id = ?", jobID).Scan(&problemID, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	return problemID, by.Int64, err
}

// GenerateInputsKind is the jobs.Kind for input generation. The worker
// uploads each input through the judge API's PUT route, like output
// generation.
type GenerateInputsKind struct {
	Store Blobs
	// Jobs queues output generation once the inputs are stored. When nil,
	// nothing is chained.
	Jobs *jobs.Service
	Now  func() time.Time
}

func decodeGenerateInputs(raw json.RawMessage) (judgeapi.GenerateInputsPayload, error) {
	var pl judgeapi.GenerateInputsPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		return pl, fmt.Errorf("generate_inputs payload: %w", err)
	}
	return pl, nil
}

// MaxUploads is one input per planned test plus two retried uploads.
func (GenerateInputsKind) MaxUploads(raw json.RawMessage) int {
	pl, err := decodeGenerateInputs(raw)
	if err != nil {
		return 0
	}
	return len(pl.Tests) + 2
}

// CheckReport refuses results for tests the plan does not have, roles
// other than the generator, and hashes that are not hashes.
func (GenerateInputsKind) CheckReport(raw json.RawMessage, r judgeapi.Report) error {
	pl, err := decodeGenerateInputs(raw)
	if err != nil {
		return err
	}
	planned := map[int]bool{}
	for _, t := range pl.Tests {
		planned[t.Idx] = true
	}
	for _, c := range r.Compile {
		if c.Role != judgeapi.RoleGenerator || (c.Verdict != string(judge.OK) && c.Verdict != string(judge.CompileError)) || len(c.Output) > judgeapi.MaxCompileOutput {
			return jobs.Badf("compile result %q %q", jobs.Clip(c.Role), jobs.Clip(c.Verdict))
		}
	}
	seen := map[int]bool{}
	for _, t := range r.Results {
		switch {
		case !planned[t.Idx] || seen[t.Idx]:
			return jobs.Badf("result for planned test %d", t.Idx)
		case t.Role != judgeapi.RoleGenerator || !judgeapi.Verdicts[t.Verdict]:
			return jobs.Badf("result role %q verdict %q", jobs.Clip(t.Role), jobs.Clip(t.Verdict))
		case t.OutputSHA != "" && !judgeapi.ValidSHA(t.OutputSHA):
			return jobs.Badf("input hash on test %d", t.Idx)
		case t.Output != "" || len(t.Message) > judgeapi.MaxMessage:
			return jobs.Badf("generate_inputs results carry no output text")
		case t.TimeMS < 0 || t.TimeMS > 600_000 || t.MemoryKB < 0 || t.MemoryKB > 8<<20:
			return jobs.Badf("impossible time or memory on test %d", t.Idx)
		}
		seen[t.Idx] = true
	}
	return nil
}

// Blobs lists the generator source: the generator reads no files.
func (GenerateInputsKind) Blobs(raw json.RawMessage) ([]string, error) {
	pl, err := decodeGenerateInputs(raw)
	if err != nil {
		return nil, err
	}
	return []string{pl.Generator.SourceSHA}, nil
}

// MaxDuration is a compile plus each test at its limit with the slowest
// language factor and wall-clock slack.
func (GenerateInputsKind) MaxDuration(raw json.RawMessage) time.Duration {
	pl, err := decodeGenerateInputs(raw)
	if err != nil {
		return 10 * time.Minute
	}
	d := 2*time.Minute + time.Duration(len(pl.Tests))*(7*time.Duration(pl.TimeLimitMS)*time.Millisecond+30*time.Second)
	return min(d, time.Hour)
}

// Finish stores the generated inputs as the planned tests, replacing any
// test with the same number (its output is cleared), and queues output
// generation. Nothing is applied when the problem, the generator or the
// plan changed since the job was queued, or when any planned test failed.
func (k GenerateInputsKind) Finish(ctx context.Context, tx *sql.Tx, job *jobs.Job) error {
	if job.State == jobs.Failed {
		return nil
	}
	pl, err := decodeGenerateInputs(job.Payload)
	if err != nil {
		return err
	}
	sm := InputSummary{Problems: []string{}}
	defer func() {
		job.Result.Summary, _ = json.Marshal(sm)
	}()
	for _, c := range job.Result.Compile {
		if c.Verdict == string(judge.CompileError) {
			sm.Problems = append(sm.Problems, "The generator does not compile, so no inputs were made.")
			return nil
		}
	}
	p, err := load(ctx, tx, pl.ProblemID)
	if errors.Is(err, ErrNotFound) {
		sm.Problems = append(sm.Problems, "The problem no longer exists.")
		return nil
	}
	if err != nil {
		return err
	}
	if p.ContentHash != pl.ContentHash || PlanHash(p) != pl.PlanHash {
		sm.Problems = append(sm.Problems, "The problem changed while inputs were being generated. Generate them again.")
		return nil
	}
	got := map[int]judgeapi.TestResult{}
	for _, t := range job.Result.Tests {
		got[t.Idx] = t
	}
	made := make([]Test, 0, len(pl.Tests))
	for _, t := range pl.Tests {
		where := fmt.Sprintf("test %d (subtask %d, seed %d)", t.Idx, t.Subtask, t.Seed)
		r, ok := got[t.Idx]
		switch {
		case !ok:
			sm.Problems = append(sm.Problems, fmt.Sprintf("The judge did not report the generator on %s.", where))
			continue
		case r.Verdict != string(judge.OK) && r.Verdict != string(judge.Accepted):
			sm.Problems = append(sm.Problems, fmt.Sprintf("The generator failed on %s: %s.%s", where, upperFirst(verdictWords[r.Verdict]), detail(r.Message)))
			continue
		case r.OutputSHA == "":
			sm.Problems = append(sm.Problems, fmt.Sprintf("The generator wrote nothing on %s.", where))
			continue
		}
		f, err := k.Store.Open(r.OutputSHA)
		if err != nil {
			sm.Problems = append(sm.Problems, fmt.Sprintf("The input for %s did not arrive.", where))
			continue
		}
		st, err := f.Stat()
		f.Close()
		switch {
		case err != nil || !st.Mode().IsRegular():
			sm.Problems = append(sm.Problems, fmt.Sprintf("The input for %s did not arrive.", where))
		case st.Size() == 0:
			sm.Problems = append(sm.Problems, fmt.Sprintf("The generator wrote nothing on %s.", where))
		default:
			made = append(made, Test{Idx: t.Idx, InputSHA: r.OutputSHA, InputSize: st.Size()})
		}
	}
	if len(sm.Problems) > 0 {
		return nil
	}
	byIdx := map[int]int{}
	for i, t := range p.Tests {
		byIdx[t.Idx] = i
	}
	for _, t := range made {
		if err := k.Store.Record(ctx, tx, t.InputSHA, t.InputSize); err != nil {
			return err
		}
		if i, ok := byIdx[t.Idx]; ok {
			p.Tests[i] = t
		} else {
			p.Tests = append(p.Tests, t)
		}
	}
	sort.Slice(p.Tests, func(i, j int) bool { return p.Tests[i].Idx < p.Tests[j].Idx })
	sm.Applied = len(made)
	svc := Service{Now: k.Now}
	if err := svc.save(ctx, tx, p, RevisionNote{Reason: "Inputs generated by the generator."}); err != nil {
		return err
	}
	if k.Jobs == nil || strings.TrimSpace(p.Reference.Source) == "" {
		return nil
	}
	by, err := jobCreator(ctx, tx, job.ID)
	if err != nil {
		return err
	}
	var notReady *NotReadyError
	err = queue{k.Jobs, k.Store, k.Now}.enqueueGeneration(ctx, tx, p, by, false, true)
	switch {
	case errors.As(err, &notReady):
		return nil
	case err != nil:
		return err
	}
	sm.Generating = true
	return nil
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// SampleCase is a sample from a statement: its input and expected output.
type SampleCase struct {
	Input  string
	Output string
}

// Draft is a new problem from the AI wizard. Samples become tests 1 to
// len(Samples) and the problem's samples; the plan's tests come later,
// from the generator. Problem.Tests and Problem.Samples are ignored.
type Draft struct {
	Problem Problem
	Samples []SampleCase
}

// CreateDraft adds a wizard draft to the bank with origin ai and the AI
// draft tag. A taken slug gets a numeric suffix. Field errors come back as
// FieldErrors, so the wizard can send them to the model for one retry.
func (s *Service) CreateDraft(ctx context.Context, d Draft, authorID int64) (*Problem, error) {
	p, ins, outs, err := prepareDraft(d)
	if err != nil {
		return nil, err
	}
	p.AuthorID = authorID
	if p.Slug, err = s.freeSlug(ctx, p.Slug); err != nil {
		return nil, err
	}
	if err := s.writeTests(p.Tests, ins, outs); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.recordTests(ctx, tx, p.Tests); err != nil {
		return nil, err
	}
	if err := s.insert(ctx, tx, p, RevisionNote{By: authorID, Reason: "Made by the AI wizard."}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// CheckDraft applies the editor's field rules to a draft without storing
// anything, so the wizard can name what is wrong with a model's answer.
func CheckDraft(d Draft) FieldErrors {
	_, _, _, err := prepareDraft(d)
	var errs FieldErrors
	if errors.As(err, &errs) {
		return errs
	}
	return nil
}

// prepareDraft builds the problem a draft becomes and the sample files,
// and checks its fields.
func prepareDraft(d Draft) (*Problem, map[int][]byte, map[int][]byte, error) {
	p := d.Problem
	p.ID, p.Origin, p.Archived = 0, OriginAI, false
	p.ContentHash, p.ValidatedHash = "", ""
	p.Tags = withoutAIDraftTag(append([]string{}, p.Tags...))
	if len(p.Tags) >= MaxTags {
		p.Tags = p.Tags[:MaxTags-1]
	}
	p.Tags = append(p.Tags, AIDraftTag)
	if p.Checker == "" {
		p.Checker = CheckerTokens
	}
	if p.Epsilon == 0 {
		p.Epsilon = judge.DefaultEpsilon
	}
	ins, outs := map[int][]byte{}, map[int][]byte{}
	p.Tests, p.Samples = nil, []int{}
	for i, sc := range d.Samples {
		n := i + 1
		in, out := withNewline(sc.Input), withNewline(sc.Output)
		if len(in) > MaxPackFile || len(out) > MaxPackFile {
			return nil, nil, nil, FieldErrors{"samples": fmt.Sprintf("Test %d is larger than 16 MB.", n)}
		}
		ins[n], outs[n] = []byte(in), []byte(out)
		p.Tests = append(p.Tests, Test{Idx: n})
		p.Samples = append(p.Samples, n)
	}
	if errs := checkFields(&p); errs != nil {
		return nil, nil, nil, errs
	}
	return &p, ins, outs, nil
}

func withNewline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
