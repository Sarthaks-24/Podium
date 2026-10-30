package submissions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// SubmissionView is a submission as its author sees it: verdicts, times
// and scores, never the input or output of a hidden test,
// except Revealed on a submission that never counts.
type SubmissionView struct {
	ID            int64           `json:"id"`
	ProblemID     int64           `json:"problem_id"`
	Language      string          `json:"language"`
	State         string          `json:"state"`
	Verdict       string          `json:"verdict"`
	Score         int             `json:"score"`
	MaxScore      int             `json:"max_score"`
	MaxTimeMS     int             `json:"max_time_ms"`
	MaxMemoryKB   int             `json:"max_memory_kb"`
	CreatedAt     int64           `json:"created_at"`
	JudgedAt      *int64          `json:"judged_at"`
	QueuePosition *int            `json:"queue_position"`
	CompileOutput string          `json:"compile_output"`
	Subtasks      []SubtaskResult `json:"subtasks"`
	Samples       []TestOutcome   `json:"samples"`
	Error         string          `json:"error,omitempty"`
	// Hidden is set in exam mode while the student's window is open: the
	// submission was received, and nothing about its result is shown.
	Hidden bool `json:"hidden"`
	// Revealed is the first failing hidden test of a submission that never
	// counts (practice, the organisers' workspace),
	// once judged; absent on every submission that counts.
	Revealed *RevealedTest `json:"revealed,omitempty"`
	// TimeStepMS is the step the times were rounded to (to the nearest),
	// so a 0 can be shown as "under step/2 ms".
	TimeStepMS int   `json:"time_step_ms"`
	ServerNow  int64 `json:"server_now"`
}

// RevealedTest is a failing test shown with its input, the expected output
// and the program's output, each at most 4 KB.
type RevealedTest struct {
	Idx        int    `json:"idx"`
	SubtaskIdx int    `json:"subtask_idx"`
	Input      string `json:"input"`
	Expected   string `json:"expected"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated"`
}

// RevealedRunTest is the first failing sample of a run.
type RevealedRunTest struct {
	Idx       int    `json:"idx"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Input     string `json:"input"`
	Expected  string `json:"expected"`
	Output    string `json:"output"`
	Stderr    string `json:"stderr,omitempty"`
	Truncated bool   `json:"truncated"`
}

// Run result kinds.
const (
	RunSample = "sample"
	RunCustom = "custom"
)

// RunResult is one input of a run.
type RunResult struct {
	// Kind is sample or custom.
	Kind            string  `json:"kind"`
	Label           string  `json:"label"`
	Verdict         string  `json:"verdict"`
	TimeMS          int     `json:"time_ms"`
	MemoryKB        int     `json:"memory_kb"`
	Output          string  `json:"output"`
	OutputTruncated bool    `json:"output_truncated"`
	Expected        *string `json:"expected"`
	Message         string  `json:"message"`
	// Stderr is the tail of what the program printed to stderr, at most 4 KB.
	Stderr string `json:"stderr,omitempty"`
}

// RunView is a run as its author sees it.
type RunView struct {
	ID            int64       `json:"id"`
	State         string      `json:"state"`
	CompileOutput string      `json:"compile_output"`
	Results       []RunResult `json:"results"`
	// Revealed is the first sample that failed.
	Revealed *RevealedRunTest `json:"revealed,omitempty"`
	Error    string           `json:"error,omitempty"`
	// TimeStepMS is the step the times were rounded to.
	TimeStepMS int   `json:"time_step_ms"`
	ServerNow  int64 `json:"server_now"`
}

// roundTo rounds a displayed time to step ms (10 by default, 100 in graded
// contests): finer figures add noise and help nobody.
func roundTo(ms, step int) int {
	if step <= 1 {
		return ms
	}
	return (ms + step/2) / step * step
}

// ViewOptions say how much a viewer may see of a submission.
type ViewOptions struct {
	// Coarse is for students: no memory figure for hidden
	// tests, the maximum rounded up to 16 MB steps, times rounded to
	// RoundMS. Exact figures on hidden tests let a program leak a test's
	// values a few bits per submission.
	Coarse bool
	// RoundMS rounds displayed times (100 in graded contests); 0 means 10.
	RoundMS int
	// Hide shows only that the submission was received (exam mode while
	// the student's window is open): state "submitted", no verdict, score,
	// tests, times or compile output.
	Hide bool
}

// StudentView is how a student sees a submission outside any contest
// rules.
var StudentView = ViewOptions{Coarse: true}

const memoryStepKB = 16 << 10

// timeStep is the step times are rounded to: RoundMS, or 10.
func (o ViewOptions) timeStep() int {
	if o.RoundMS == 0 {
		return 10
	}
	return o.RoundMS
}

func applyView(s *Score, o ViewOptions) {
	step := o.timeStep()
	s.MaxTimeMS = roundTo(s.MaxTimeMS, step)
	for i := range s.Samples {
		s.Samples[i].TimeMS = roundTo(s.Samples[i].TimeMS, step)
	}
	for i := range s.Subtasks {
		for j := range s.Subtasks[i].Tests {
			t := &s.Subtasks[i].Tests[j]
			t.TimeMS = roundTo(t.TimeMS, step)
			if o.Coarse {
				t.MemoryKB = 0
			}
		}
	}
	if o.Coarse && s.MaxMemoryKB > 0 {
		s.MaxMemoryKB = (s.MaxMemoryKB + memoryStepKB - 1) / memoryStepKB * memoryStepKB
	}
}

// round applies the default (organiser) view.
func round(s *Score) { applyView(s, ViewOptions{}) }

type row struct {
	id, problemID, jobID      int64
	kind, language, state     string
	verdict, result, errorMsg string
	score, maxScore           int
	maxTime, maxMem           int
	createdAt                 int64
	judgedAt                  sql.NullInt64
}

func (s *Service) loadRow(ctx context.Context, id int64) (row, error) {
	var r row
	var job sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, problem_id, job_id, kind, language, state, verdict, result, error,
		score, max_score, max_time_ms, max_memory_kb, created_at, judged_at FROM submissions WHERE id = ?`, id).
		Scan(&r.id, &r.problemID, &job, &r.kind, &r.language, &r.state, &r.verdict, &r.result, &r.errorMsg,
			&r.score, &r.maxScore, &r.maxTime, &r.maxMem, &r.createdAt, &r.judgedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, ErrNotFound
	}
	r.jobID = job.Int64
	return r, err
}

// Get returns a submission with every figure, as organisers see it.
func (s *Service) Get(ctx context.Context, id int64) (*SubmissionView, error) {
	return s.GetWith(ctx, id, ViewOptions{})
}

// GetWith returns a submission as a viewer with the given options sees
// it. While it is being judged the score so far is computed from the
// results that have arrived.
func (s *Service) GetWith(ctx context.Context, id int64, o ViewOptions) (*SubmissionView, error) {
	r, err := s.loadRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.kind != "submit" {
		return nil, ErrNotFound
	}
	if o.Hide {
		v := &SubmissionView{ID: r.id, ProblemID: r.problemID, Language: r.language, State: "submitted",
			MaxScore: r.maxScore, CreatedAt: r.createdAt, Subtasks: []SubtaskResult{}, Samples: []TestOutcome{},
			Hidden: true, TimeStepMS: o.timeStep(), ServerNow: s.now().UnixMilli()}
		// A submission the judge could not finish is not counted, and the
		// student must know to submit again. The failure is the judge's or
		// the checker's, never something the program chose, so it tells
		// nothing about hidden tests.
		if r.state == "error" {
			v.State, v.Error = "error", r.errorMsg
		}
		return v, nil
	}
	v := &SubmissionView{ID: r.id, ProblemID: r.problemID, Language: r.language, State: r.state, Verdict: r.verdict,
		Score: r.score, MaxScore: r.maxScore, MaxTimeMS: r.maxTime, MaxMemoryKB: r.maxMem, CreatedAt: r.createdAt,
		Error: r.errorMsg, Subtasks: []SubtaskResult{}, Samples: []TestOutcome{}, TimeStepMS: o.timeStep(), ServerNow: s.now().UnixMilli()}
	if r.judgedAt.Valid {
		v.JudgedAt = &r.judgedAt.Int64
	}
	if r.state == "done" && r.result != "" {
		var sc storedResult
		if err := json.Unmarshal([]byte(r.result), &sc); err != nil {
			return nil, fmt.Errorf("submission %d result: %w", id, err)
		}
		view := Score{Samples: sc.Samples, Subtasks: sc.Subtasks, MaxTimeMS: v.MaxTimeMS, MaxMemoryKB: v.MaxMemoryKB}
		applyView(&view, o)
		v.Subtasks, v.Samples, v.CompileOutput, v.MaxTimeMS, v.MaxMemoryKB = view.Subtasks, view.Samples, sc.CompileOutput, view.MaxTimeMS, view.MaxMemoryKB
		if sc.Revealed != nil {
			v.Revealed = s.revealed(sc.Revealed)
		}
		return v, nil
	}
	if r.jobID == 0 || r.state == "error" {
		// A failed submission is not counted: no partial score.
		return v, nil
	}
	job, err := s.Jobs.Get(ctx, r.jobID)
	if err != nil {
		return nil, err
	}
	switch job.State {
	case jobs.Queued:
		pos, _ := s.Jobs.QueuePosition(ctx, r.jobID)
		v.QueuePosition = &pos
	case jobs.Leased:
		v.State = "running"
	}
	var pl judgeapi.SubmitPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, err
	}
	var program []judgeapi.CompileResult
	for _, c := range job.Result.Compile {
		if c.Role == judgeapi.RoleProgram {
			program = append(program, c)
		}
	}
	sc := ScoreSubmission(pl, program, job.Result.Tests, false)
	applyView(&sc, o)
	if o.Coarse {
		maskUnfinished(&sc)
	}
	v.Subtasks, v.Samples, v.Score, v.MaxTimeMS, v.MaxMemoryKB = sc.Subtasks, sc.Samples, sc.Score, sc.MaxTimeMS, sc.MaxMemoryKB
	v.CompileOutput = compileOutput(program)
	return v, nil
}

// maskUnfinished hides per-test progress inside a block that is still
// being judged (the samples, or a subtask), so polling a submission shows
// as much as the live stream does and no more: when a whole block
// finishes, never when one hidden test does.
func maskUnfinished(sc *Score) {
	blank := func(tests []TestOutcome) {
		for i := range tests {
			tests[i] = TestOutcome{Idx: tests[i].Idx}
		}
	}
	for _, t := range sc.Samples {
		if t.Verdict == "" {
			blank(sc.Samples)
			break
		}
	}
	for i := range sc.Subtasks {
		if sc.Subtasks[i].Verdict == "" {
			blank(sc.Subtasks[i].Tests)
		}
	}
	// The maximum time and memory so far would reveal the same progress.
	sc.MaxTimeMS, sc.MaxMemoryKB = 0, 0
}

func compileOutput(cs []judgeapi.CompileResult) string {
	for _, c := range cs {
		if c.Role == judgeapi.RoleProgram {
			return c.Output
		}
	}
	return ""
}

// storedResult is what a finished submission keeps in submissions.result.
type storedResult struct {
	Subtasks      []SubtaskResult `json:"subtasks"`
	Samples       []TestOutcome   `json:"samples"`
	CompileOutput string          `json:"compile_output"`
	// Revealed is set only when the job's payload asked for it.
	Revealed *storedReveal `json:"revealed,omitempty"`
}

// storedReveal names the revealed test by its files and keeps the
// program's output excerpt the judge sent.
type storedReveal struct {
	Idx         int    `json:"idx"`
	Subtask     int    `json:"subtask"`
	InputSHA    string `json:"input_sha"`
	ExpectedSHA string `json:"expected_sha"`
	Output      string `json:"output"`
}

// revealed reads a stored reveal's files, each cut at 4 KB.
func (s *Service) revealed(r *storedReveal) *RevealedTest {
	in, _ := s.Problems.Blobs.Read(r.InputSHA, maxSampleTxt+1)
	exp, _ := s.Problems.Blobs.Read(r.ExpectedSHA, maxSampleTxt+1)
	return &RevealedTest{Idx: r.Idx, SubtaskIdx: r.Subtask, Input: clipText(in, maxSampleTxt), Expected: clipText(exp, maxSampleTxt),
		Output:    clipText([]byte(r.Output), maxSampleTxt),
		Truncated: len(in) > maxSampleTxt || len(exp) > maxSampleTxt || len(r.Output) >= judgeapi.MaxRunOutput-3}
}

// GetRunWith returns a run with times rounded as the options say. A run
// uses the samples or the student's own input, so nothing else is hidden.
func (s *Service) GetRunWith(ctx context.Context, id int64, o ViewOptions) (*RunView, error) {
	step := o.timeStep()
	r, err := s.loadRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.kind != "run" {
		return nil, ErrNotFound
	}
	v := &RunView{ID: r.id, State: r.state, Results: []RunResult{}, Error: r.errorMsg, TimeStepMS: step, ServerNow: s.now().UnixMilli()}
	if r.state == "error" {
		return v, nil
	}
	if r.jobID == 0 {
		return v, nil
	}
	job, err := s.Jobs.Get(ctx, r.jobID)
	if err != nil {
		return nil, err
	}
	if job.State == jobs.Leased {
		v.State = "running"
	}
	var pl judgeapi.RunPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil {
		return nil, err
	}
	v.CompileOutput = compileOutput(job.Result.Compile)
	got := map[int]judgeapi.TestResult{}
	for _, t := range job.Result.Tests {
		got[t.Idx] = t
	}
	samples := 0
	for _, in := range pl.Inputs {
		res := RunResult{Kind: RunCustom, Label: "Your input"}
		if in.Idx > 0 {
			samples++
			res.Kind, res.Label = RunSample, fmt.Sprintf("Example %d", samples)
		}
		var expected string
		if in.ExpectedSHA != "" {
			exp, _ := s.Problems.Blobs.Read(in.ExpectedSHA, maxSampleTxt+1)
			expected = clipText(exp, maxSampleTxt)
			res.Expected = &expected
		}
		t, ok := got[in.Idx]
		if ok {
			res.Verdict, res.TimeMS, res.MemoryKB, res.Message = t.Verdict, roundTo(t.TimeMS, step), t.MemoryKB, t.Message
			res.Output = t.Output
			res.Stderr = clipText([]byte(t.Stderr), judgeapi.MaxRunOutput)
			// The worker cuts at MaxRunOutput and drops a split rune (at
			// most 3 bytes), so anything that close to the cap was cut.
			res.OutputTruncated = len(t.Output) >= judgeapi.MaxRunOutput-3
		}
		// The first failing sample is shown in full.
		if ok && in.ExpectedSHA != "" && v.Revealed == nil && failed(t.Verdict) {
			raw, _ := s.Problems.Blobs.Read(in.InputSHA, maxSampleTxt+1)
			v.Revealed = &RevealedRunTest{Idx: in.Idx, Kind: res.Kind, Label: res.Label, Input: clipText(raw, maxSampleTxt),
				Expected: expected, Output: t.Output, Stderr: res.Stderr,
				Truncated: len(raw) > maxSampleTxt || len(expected) >= maxSampleTxt || len(t.Output) >= judgeapi.MaxRunOutput-3}
		}
		v.Results = append(v.Results, res)
	}
	return v, nil
}

// failed reports whether a test verdict is a failure the program caused:
// not passed, not skipped, not a judge error.
func failed(verdict string) bool {
	switch verdict {
	case "WA", "TLE", "MLE", "RE", "OLE":
		return true
	}
	return false
}

// listMine returns a user's newest submissions on a problem as a viewer
// with the given options sees them.
func (s *Service) listMine(ctx context.Context, userID, problemID, contestID int64, o ViewOptions) ([]SubmissionView, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM submissions WHERE user_id = ? AND problem_id = ? AND coalesce(contest_id, 0) = ?
		AND kind = 'submit' ORDER BY id DESC LIMIT ?`, userID, problemID, contestID, listLimit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []SubmissionView{}
	for _, id := range ids {
		v, err := s.GetWith(ctx, id, o)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}
