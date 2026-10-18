package submissions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// SubmitKind is the jobs.Kind for submissions.
type SubmitKind struct {
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// RunKind is the jobs.Kind for runs.
type RunKind struct {
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// testVerdicts are the verdicts a test of a submission or run can have.
var testVerdicts = map[string]bool{"AC": true, "WA": true, "TLE": true, "MLE": true, "RE": true, "OLE": true, "SK": true, "IE": true}

func nowOr(f func() time.Time) time.Time {
	if f != nil {
		return f()
	}
	return time.Now()
}

func checkCompile(cs []judgeapi.CompileResult, roles ...string) error {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	seen := map[string]bool{}
	for _, c := range cs {
		switch {
		case !allowed[c.Role] || seen[c.Role]:
			return jobs.Badf("compile result for %q", jobs.Clip(c.Role))
		case c.Verdict != string(judge.OK) && c.Verdict != string(judge.CompileError):
			return jobs.Badf("compile verdict %q", jobs.Clip(c.Verdict))
		case len(c.Output) > judgeapi.MaxCompileOutput || !utf8.ValidString(c.Output):
			return jobs.Badf("compile output too long or not UTF-8")
		}
		seen[c.Role] = true
	}
	return nil
}

func checkNumbers(t judgeapi.TestResult) error {
	switch {
	case !judgeapi.Verdicts[t.Verdict]:
		return jobs.Badf("verdict %q", jobs.Clip(t.Verdict))
	case t.TimeMS < 0 || t.TimeMS > 600_000 || t.MemoryKB < 0 || t.MemoryKB > 8<<20:
		return jobs.Badf("impossible time or memory on test %d", t.Idx)
	case len(t.Message) > judgeapi.MaxMessage || !utf8.ValidString(t.Message):
		return jobs.Badf("message too long or not UTF-8")
	case t.OutputSHA != "" && !judgeapi.ValidSHA(t.OutputSHA):
		return jobs.Badf("output hash on test %d", t.Idx)
	}
	return nil
}

func decodeSubmit(raw json.RawMessage) (judgeapi.SubmitPayload, error) {
	var pl judgeapi.SubmitPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		return pl, fmt.Errorf("submit payload: %w", err)
	}
	return pl, nil
}

// CheckReport refuses results for tests the submission does not have, any
// output text (hidden tests stay hidden), and messages on hidden tests (a
// checker comment can quote the expected answer).
func (SubmitKind) CheckReport(raw json.RawMessage, r judgeapi.Report) error {
	pl, err := decodeSubmit(raw)
	if err != nil {
		return err
	}
	if err := checkCompile(r.Compile, judgeapi.RoleProgram, judgeapi.RoleChecker); err != nil {
		return err
	}
	sample := map[int]bool{}
	known := map[int]bool{}
	for _, t := range pl.Tests {
		known[t.Idx] = true
		sample[t.Idx] = t.Sample
	}
	seen := map[int]bool{}
	for _, t := range r.Results {
		switch {
		case !known[t.Idx] || seen[t.Idx]:
			return jobs.Badf("result for test %d", t.Idx)
		case t.Role != judgeapi.RoleProgram:
			return jobs.Badf("result role %q", jobs.Clip(t.Role))
		case t.Output != "":
			return jobs.Badf("submit results carry no output text")
		case t.Message != "" && !sample[t.Idx]:
			return jobs.Badf("message on hidden test %d", t.Idx)
		case !testVerdicts[t.Verdict]:
			return jobs.Badf("test verdict %q", jobs.Clip(t.Verdict))
		}
		if err := checkNumbers(t); err != nil {
			return err
		}
		seen[t.Idx] = true
	}
	return nil
}

// Blobs lists the source, checker and test files.
func (SubmitKind) Blobs(raw json.RawMessage) ([]string, error) {
	pl, err := decodeSubmit(raw)
	if err != nil {
		return nil, err
	}
	out := []string{pl.Program.SourceSHA}
	if pl.Checker.SourceSHA != "" {
		out = append(out, pl.Checker.SourceSHA)
	}
	for _, t := range pl.Tests {
		out = append(out, t.InputSHA, t.OutputSHA)
	}
	return out, nil
}

// MaxDuration is compiles plus every test at its worst, with slack.
func (SubmitKind) MaxDuration(raw json.RawMessage) time.Duration {
	pl, err := decodeSubmit(raw)
	if err != nil {
		return 10 * time.Minute
	}
	perTest := 2*3*time.Duration(pl.TimeLimitMS)*time.Millisecond + 25*time.Second
	return min(2*time.Minute+time.Duration(len(pl.Tests))*perTest, time.Hour)
}

// Finish scores the submission from its results and stores the outcome.
func (k SubmitKind) Finish(ctx context.Context, tx *sql.Tx, job *jobs.Job) error {
	pl, err := decodeSubmit(job.Payload)
	if err != nil {
		return err
	}
	now := nowOr(k.Now).UnixMilli()
	// Every update names the job too: a job a rejudge replaced must never
	// overwrite the submission its successor is judging.
	if job.State == jobs.Failed {
		_, err := tx.ExecContext(ctx, `UPDATE submissions SET state = 'error', error = ?, judged_at = ? WHERE id = ? AND job_id = ?`,
			JudgeFailedMessage, now, pl.SubmissionID, job.ID)
		return err
	}
	for _, c := range job.Result.Compile {
		if c.Role == judgeapi.RoleChecker && c.Verdict == string(judge.CompileError) {
			_, err := tx.ExecContext(ctx, `UPDATE submissions SET state = 'error', error = ?, judged_at = ? WHERE id = ? AND job_id = ?`,
				"The problem's checker does not compile. Tell an organiser; this submission is not counted.", now, pl.SubmissionID, job.ID)
			return err
		}
	}
	var program []judgeapi.CompileResult
	for _, c := range job.Result.Compile {
		if c.Role == judgeapi.RoleProgram {
			program = append(program, c)
		}
	}
	sc := ScoreSubmission(pl, program, job.Result.Tests, true)
	round(&sc)
	stored, err := json.Marshal(storedResult{Subtasks: sc.Subtasks, Samples: sc.Samples, CompileOutput: compileOutput(program)})
	if err != nil {
		return err
	}
	summary, _ := json.Marshal(map[string]any{"verdict": sc.Verdict, "score": sc.Score})
	job.Result.Summary = summary
	earned := ""
	if sc.Verdict != verdictCompile {
		pts := make([]int, len(sc.Subtasks))
		for i, st := range sc.Subtasks {
			pts[i] = st.Earned
		}
		b, _ := json.Marshal(pts)
		earned = string(b)
	}
	_, err = tx.ExecContext(ctx, `UPDATE submissions SET state = 'done', verdict = ?, score = ?, max_score = ?, max_time_ms = ?,
		max_memory_kb = ?, result = ?, earned = ?, judged_at = ? WHERE id = ? AND job_id = ?`,
		sc.Verdict, sc.Score, sc.MaxScore, sc.MaxTimeMS, sc.MaxMemoryKB, string(stored), earned, now, pl.SubmissionID, job.ID)
	return err
}

// JudgeFailedMessage is the error a submission shows when the judge could
// not finish it.
const JudgeFailedMessage = "The judge could not finish this submission. It is not counted; submit again."

func decodeRun(raw json.RawMessage) (judgeapi.RunPayload, error) {
	var pl judgeapi.RunPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		return pl, fmt.Errorf("run payload: %w", err)
	}
	return pl, nil
}

// CheckReport refuses results for inputs the run does not have and output
// beyond the excerpt size.
func (RunKind) CheckReport(raw json.RawMessage, r judgeapi.Report) error {
	pl, err := decodeRun(raw)
	if err != nil {
		return err
	}
	if err := checkCompile(r.Compile, judgeapi.RoleProgram, judgeapi.RoleChecker); err != nil {
		return err
	}
	known := map[int]bool{}
	for _, in := range pl.Inputs {
		known[in.Idx] = true
	}
	seen := map[int]bool{}
	for _, t := range r.Results {
		switch {
		case !known[t.Idx] || seen[t.Idx]:
			return jobs.Badf("result for input %d", t.Idx)
		case t.Role != judgeapi.RoleProgram:
			return jobs.Badf("result role %q", jobs.Clip(t.Role))
		case len(t.Output) > judgeapi.MaxRunOutput || !utf8.ValidString(t.Output):
			return jobs.Badf("run output too long or not UTF-8")
		}
		if err := checkNumbers(t); err != nil {
			return err
		}
		seen[t.Idx] = true
	}
	return nil
}

// Blobs lists the source, checker, inputs and expected outputs.
func (RunKind) Blobs(raw json.RawMessage) ([]string, error) {
	pl, err := decodeRun(raw)
	if err != nil {
		return nil, err
	}
	out := []string{pl.Program.SourceSHA}
	if pl.Checker.SourceSHA != "" {
		out = append(out, pl.Checker.SourceSHA)
	}
	for _, in := range pl.Inputs {
		out = append(out, in.InputSHA)
		if in.ExpectedSHA != "" {
			out = append(out, in.ExpectedSHA)
		}
	}
	return out, nil
}

// MaxDuration bounds a run.
func (RunKind) MaxDuration(raw json.RawMessage) time.Duration {
	pl, err := decodeRun(raw)
	if err != nil {
		return 5 * time.Minute
	}
	return min(2*time.Minute+time.Duration(len(pl.Inputs))*(6*time.Duration(pl.TimeLimitMS)*time.Millisecond+25*time.Second), 30*time.Minute)
}

// Finish marks the run done; its results are read from the job.
func (k RunKind) Finish(ctx context.Context, tx *sql.Tx, job *jobs.Job) error {
	pl, err := decodeRun(job.Payload)
	if err != nil {
		return err
	}
	state, msg := "done", ""
	if job.State == jobs.Failed {
		state, msg = "error", "The judge could not finish this run. Try again."
	}
	for _, c := range job.Result.Compile {
		if c.Role == judgeapi.RoleChecker && c.Verdict == string(judge.CompileError) {
			state, msg = "error", "The problem's checker does not compile, so the samples could not be checked. Tell an organiser."
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE submissions SET state = ?, error = ?, judged_at = ? WHERE id = ?`,
		state, msg, nowOr(k.Now).UnixMilli(), pl.SubmissionID)
	return err
}
