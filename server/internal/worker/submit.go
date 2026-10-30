package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// startJob decodes a claim, starts the heartbeat, and returns the reporter
// and a scratch directory. done stops the heartbeat and removes the
// directory.
func (w *Worker) startJob(ctx context.Context, c *judgeapi.Claim, payload any) (rep *reporter, work string, done func(), err error) {
	if err := json.Unmarshal(c.Payload, payload); err != nil {
		return nil, "", nil, fmt.Errorf("job %d payload: %w", c.JobID, err)
	}
	work, err = os.MkdirTemp(w.WorkDir, c.Kind+"-*")
	if err != nil {
		return nil, "", nil, err
	}
	rep = &reporter{client: w.Client, jobID: c.JobID, lease: c.Lease}
	hbCtx, stop := context.WithCancel(ctx)
	go rep.heartbeat(hbCtx)
	return rep, work, func() {
		stop()
		os.RemoveAll(work)
		for _, release := range w.held(c.JobID) {
			release()
		}
		w.Compiled.Evict()
		w.Cache.Release(c.JobID)
		w.Cache.Evict()
	}, nil
}

// hold and held keep the compile-cache releases of a running job.
func (w *Worker) hold(jobID int64) *[]func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.holds == nil {
		w.holds = map[int64]*[]func(){}
	}
	h := &[]func(){}
	w.holds[jobID] = h
	return h
}

func (w *Worker) held(jobID int64) []func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	h := w.holds[jobID]
	delete(w.holds, jobID)
	if h == nil {
		return nil
	}
	return *h
}

// finish sends the closing report, or a fatal one when the run broke.
func (w *Worker) finish(ctx context.Context, c *judgeapi.Claim, rep *reporter, err error) error {
	if errors.Is(err, ErrLease) || ctx.Err() != nil {
		return err
	}
	if err != nil {
		w.Log.Error("job failed to run", "job_id", c.JobID, "kind", c.Kind, "err", err)
		return rep.send(ctx, judgeapi.Report{Fatal: "The judge could not run this job: " + clip(err.Error(), 300)})
	}
	return rep.send(ctx, judgeapi.Report{Final: true})
}

// compile builds a program through the compile cache and reports the
// compile under role. ok is false on a compile error. The program stays
// held in the cache until the job's done function runs.
func (w *Worker) compile(ctx context.Context, box int, lease Lease, rep *reporter, role string, p judgeapi.Program, lang judge.Language, held *[]func()) (*judge.Program, bool, error) {
	src, err := w.Cache.Read(ctx, lease, p.SourceSHA, maxSourceBytes)
	if err != nil {
		return nil, false, err
	}
	prog, res, release, err := w.Compiled.Compile(ctx, w.Runner, box, lang, src)
	if err != nil {
		return nil, false, err
	}
	*held = append(*held, release)
	verdict := string(judge.OK)
	if res.Verdict != judge.OK {
		verdict = string(judge.CompileError)
	}
	out := clip(strings.ToValidUTF8(res.Output, ""), judgeapi.MaxCompileOutput)
	if err := rep.send(ctx, judgeapi.Report{Compile: []judgeapi.CompileResult{{Role: role, Verdict: verdict, Output: out, TimedOut: res.Transient}}}); err != nil {
		return nil, false, err
	}
	if verdict != string(judge.OK) {
		return nil, false, nil
	}
	return &prog, true, nil
}

func language(id string) (judge.Language, error) {
	lang, ok := judge.Languages[id]
	if !ok {
		return judge.Language{}, fmt.Errorf("unknown language %q", id)
	}
	return lang, nil
}

// submit judges a submission: samples
// first, stopping everything at a failing sample; then each subtask in
// order, stopping it at its first failure. Skipped tests are reported as
// SK. Only an output hash is reported, never output text, and checker
// comments only for samples: hidden tests stay hidden.
func (w *Worker) submit(ctx context.Context, box int, c *judgeapi.Claim) error {
	var pl judgeapi.SubmitPayload
	rep, work, done, err := w.startJob(ctx, c, &pl)
	if err != nil {
		return err
	}
	defer done()
	return w.finish(ctx, c, rep, w.runSubmit(ctx, box, Lease{c.JobID, c.Lease}, rep, work, pl, w.hold(c.JobID)))
}

func (w *Worker) runSubmit(ctx context.Context, box int, lease Lease, rep *reporter, work string, pl judgeapi.SubmitPayload, held *[]func()) error {
	lang, err := language(pl.Program.Language)
	if err != nil {
		return err
	}
	prog, ok, err := w.compile(ctx, box, lease, rep, judgeapi.RoleProgram, pl.Program, lang, held)
	if err != nil || !ok {
		return err
	}
	var checker *judge.Program
	if pl.Checker.Type == string(judge.CheckCustom) {
		if checker, ok, err = w.compile(ctx, box, lease, rep, judgeapi.RoleChecker,
			judgeapi.Program{Language: "cpp17", SourceSHA: pl.Checker.SourceSHA}, judge.CheckerLanguage(), held); err != nil || !ok {
			return err
		}
	}
	limit := time.Duration(pl.TimeLimitMS) * time.Millisecond
	stopAll, revealed := false, false
	failed := map[int]bool{}
	for i, t := range pl.Tests {
		tr := judgeapi.TestResult{Idx: t.Idx, Role: judgeapi.RoleProgram, Verdict: string(judge.Skipped)}
		if !stopAll && (t.Sample || !failed[t.Subtask]) {
			in, err := w.Cache.Path(ctx, lease, t.InputSHA)
			if err != nil {
				return err
			}
			answer, err := w.Cache.Path(ctx, lease, t.OutputSHA)
			if err != nil {
				return err
			}
			out := filepath.Join(work, fmt.Sprintf("%d.out", i))
			res, err := w.Runner.Execute(ctx, box, *prog, limit, pl.MemoryLimitMB, in, out)
			if err != nil {
				return err
			}
			tr = testResult(t.Idx, judgeapi.RoleProgram, res)
			if res.Verdict == judge.OK {
				if err := w.check(ctx, box, pl.Checker, checker, in, out, answer, &tr); err != nil {
					return err
				}
			}
			tr.OutputSHA = fileSHA(out)
			// A submission that never counts shows its first failing
			// hidden test: only then is output sent.
			if pl.Reveal && !t.Sample && !revealed && revealable(tr.Verdict) {
				tr.Output, revealed = readExcerpt(out, judgeapi.MaxRunOutput), true
			}
			os.Remove(out)
			if !t.Sample {
				tr.Message = ""
			}
			if tr.Verdict != string(judge.Accepted) {
				if t.Sample && pl.StopOnSampleFail {
					stopAll = true
				}
				if !t.Sample {
					failed[t.Subtask] = true
				}
			}
		}
		if err := rep.send(ctx, judgeapi.Report{Results: []judgeapi.TestResult{tr}}); err != nil {
			return err
		}
	}
	return nil
}

// revealable reports whether a hidden test's verdict is one the program
// caused, so its output may be revealed.
func revealable(v string) bool {
	switch judge.Verdict(v) {
	case judge.WrongAnswer, judge.TimeLimit, judge.MemoryLimit, judge.RuntimeError, judge.OutputLimit:
		return true
	}
	return false
}

// fileSHA hashes a program's output for integrity checks, or "" when the
// file is missing.
func fileSHA(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// run runs a program on samples or a custom input and reports a short
// excerpt of each output.
func (w *Worker) run(ctx context.Context, box int, c *judgeapi.Claim) error {
	var pl judgeapi.RunPayload
	rep, work, done, err := w.startJob(ctx, c, &pl)
	if err != nil {
		return err
	}
	defer done()
	return w.finish(ctx, c, rep, w.runRun(ctx, box, Lease{c.JobID, c.Lease}, rep, work, pl, w.hold(c.JobID)))
}

func (w *Worker) runRun(ctx context.Context, box int, lease Lease, rep *reporter, work string, pl judgeapi.RunPayload, held *[]func()) error {
	lang, err := language(pl.Program.Language)
	if err != nil {
		return err
	}
	prog, ok, err := w.compile(ctx, box, lease, rep, judgeapi.RoleProgram, pl.Program, lang, held)
	if err != nil || !ok {
		return err
	}
	var checker *judge.Program
	if pl.Checker.Type == string(judge.CheckCustom) && pl.Checker.SourceSHA != "" {
		if checker, ok, err = w.compile(ctx, box, lease, rep, judgeapi.RoleChecker,
			judgeapi.Program{Language: "cpp17", SourceSHA: pl.Checker.SourceSHA}, judge.CheckerLanguage(), held); err != nil || !ok {
			return err
		}
	}
	limit := time.Duration(pl.TimeLimitMS) * time.Millisecond
	// Open tests run only while every sample and open test before them
	// passed; the rest are reported as skipped.
	stopOpen := false
	for i, input := range pl.Inputs {
		if input.Open && stopOpen {
			if err := rep.send(ctx, judgeapi.Report{Results: []judgeapi.TestResult{
				{Idx: input.Idx, Role: judgeapi.RoleProgram, Verdict: string(judge.Skipped)}}}); err != nil {
				return err
			}
			continue
		}
		in, err := w.Cache.Path(ctx, lease, input.InputSHA)
		if err != nil {
			return err
		}
		out := filepath.Join(work, fmt.Sprintf("%d.out", i))
		res, err := w.Runner.Execute(ctx, box, *prog, limit, pl.MemoryLimitMB, in, out)
		if err != nil {
			return err
		}
		tr := testResult(input.Idx, judgeapi.RoleProgram, res)
		if res.Verdict == judge.OK && input.ExpectedSHA != "" {
			answer, err := w.Cache.Path(ctx, lease, input.ExpectedSHA)
			if err != nil {
				return err
			}
			if err := w.check(ctx, box, pl.Checker, checker, in, out, answer, &tr); err != nil {
				return err
			}
		}
		tr.Output = readExcerpt(out, judgeapi.MaxRunOutput)
		// The student's own input or an example, so the student may see what the program printed to stderr.
		tr.Stderr = tail(strings.ToValidUTF8(string(res.Stderr), ""), judgeapi.MaxRunOutput)
		os.Remove(out)
		if input.ExpectedSHA != "" && tr.Verdict != string(judge.Accepted) {
			stopOpen = true
		}
		if err := rep.send(ctx, judgeapi.Report{Results: []judgeapi.TestResult{tr}}); err != nil {
			return err
		}
	}
	return nil
}

// readExcerpt returns at most n bytes of a file as valid UTF-8.
func readExcerpt(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, int64(n)))
	return strings.ToValidUTF8(string(b), "")
}

// generate runs the reference solution on each input and uploads the
// output, which becomes the test's expected output.
func (w *Worker) generate(ctx context.Context, box int, c *judgeapi.Claim) error {
	var pl judgeapi.GeneratePayload
	rep, work, done, err := w.startJob(ctx, c, &pl)
	if err != nil {
		return err
	}
	defer done()
	return w.finish(ctx, c, rep, w.runGenerate(ctx, box, Lease{c.JobID, c.Lease}, rep, work, pl, w.hold(c.JobID)))
}

func (w *Worker) runGenerate(ctx context.Context, box int, lease Lease, rep *reporter, work string, pl judgeapi.GeneratePayload, held *[]func()) error {
	lang, err := language(pl.Program.Language)
	if err != nil {
		return err
	}
	prog, ok, err := w.compile(ctx, box, lease, rep, judgeapi.RoleProgram, pl.Program, lang, held)
	if err != nil || !ok {
		return err
	}
	limit := time.Duration(pl.TimeLimitMS) * time.Millisecond
	for i, input := range pl.Inputs {
		in, err := w.Cache.Path(ctx, lease, input.InputSHA)
		if err != nil {
			return err
		}
		out := filepath.Join(work, fmt.Sprintf("%d.out", i))
		res, err := w.Runner.Execute(ctx, box, *prog, limit, pl.MemoryLimitMB, in, out)
		if err != nil {
			return err
		}
		tr := testResult(input.Idx, judgeapi.RoleProgram, res)
		if res.Verdict == judge.OK {
			data, err := os.ReadFile(out)
			if err != nil {
				return err
			}
			if tr.OutputSHA, err = w.Client.Upload(ctx, lease, data); err != nil {
				return err
			}
		}
		os.Remove(out)
		if err := rep.send(ctx, judgeapi.Report{Results: []judgeapi.TestResult{tr}}); err != nil {
			return err
		}
	}
	return nil
}
