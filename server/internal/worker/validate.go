package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Limits for the helper programs in a validation (m2-plan section 6).
const (
	validatorTime     = 5 * time.Second
	validatorMemoryMB = 256
	bruteFactor       = 10
	bruteTimeCap      = 10 * time.Second
	maxSourceBytes    = 64 << 10
	heartbeatEvery    = 20 * time.Second
)

// reporter sends results for one job. Every send renews the lease; a
// heartbeat sends an empty report while a long compile or test runs. Once
// the lease is lost every later call returns ErrLease.
type reporter struct {
	client *Client
	jobID  int64
	lease  string

	mu   sync.Mutex
	lost bool
}

// send delivers a report, retrying a server that is briefly unavailable
// (restarting, or a busy database) for as long as the lease can still be
// valid. A refused lease or a refused report is final.
func (r *reporter) send(ctx context.Context, rep judgeapi.Report) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lost {
		return ErrLease
	}
	rep.JobID, rep.Lease = r.jobID, r.lease
	wait := time.Second
	deadline := time.Now().Add(judgeapi.LeaseSeconds * time.Second * 3 / 4)
	for {
		err := r.client.Report(ctx, rep)
		var refused *refusedError
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrLease):
			r.lost = true
			return err
		case errors.As(err, &refused), ctx.Err() != nil, time.Now().Add(wait).After(deadline):
			return err
		}
		sleep(ctx, wait)
		wait *= 2
	}
}

func (r *reporter) heartbeat(ctx context.Context) {
	t := time.NewTicker(heartbeatEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.send(ctx, judgeapi.Report{}); errors.Is(err, ErrLease) {
				return
			}
		}
	}
}

// validate runs one validation job in box (m2-plan section 6): compile
// every program, then for each test run the validator on the input, the
// reference with the problem's limits, and the brute force with a longer
// limit, checking outputs against the expected answer.
func (w *Worker) validate(ctx context.Context, box int, c *judgeapi.Claim) error {
	var pl judgeapi.ValidatePayload
	if err := json.Unmarshal(c.Payload, &pl); err != nil {
		return fmt.Errorf("job %d payload: %w", c.JobID, err)
	}
	rep := &reporter{client: w.Client, jobID: c.JobID, lease: c.Lease}
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	go rep.heartbeat(hbCtx)

	err := w.runValidation(ctx, box, pl, rep, Lease{c.JobID, c.Lease})
	stopHeartbeat()
	if errors.Is(err, ErrLease) || ctx.Err() != nil {
		return err
	}
	if err != nil {
		// The run itself broke (isolate failed, a blob was missing): tell
		// the setter, without internal detail beyond the message.
		w.Log.Error("validation failed to run", "job_id", c.JobID, "err", err)
		return rep.send(ctx, judgeapi.Report{Fatal: "The judge could not run this validation: " + clip(err.Error(), 300)})
	}
	return rep.send(ctx, judgeapi.Report{Final: true})
}

func (w *Worker) runValidation(ctx context.Context, box int, pl judgeapi.ValidatePayload, rep *reporter, lease Lease) error {
	work, err := os.MkdirTemp(w.WorkDir, "validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	type prog struct {
		role string
		spec *judgeapi.Program
	}
	specs := []prog{{judgeapi.RoleReference, &pl.Programs.Reference}, {judgeapi.RoleBrute, pl.Programs.Brute},
		{judgeapi.RoleValidator, pl.Programs.Validator}}
	if pl.Checker.Type == string(judge.CheckCustom) {
		specs = append(specs, prog{judgeapi.RoleChecker, &judgeapi.Program{Language: "cpp17", SourceSHA: pl.Checker.SourceSHA}})
	}
	programs := map[string]*judge.Program{}
	for _, s := range specs {
		if s.spec == nil {
			continue
		}
		lang, ok := judge.Languages[s.spec.Language]
		if !ok {
			return fmt.Errorf("%s: unknown language %q", s.role, s.spec.Language)
		}
		if s.role == judgeapi.RoleChecker {
			lang = judge.CheckerLanguage()
		}
		src, err := w.Cache.Read(ctx, lease, s.spec.SourceSHA, maxSourceBytes)
		if err != nil {
			return err
		}
		dir := filepath.Join(work, s.role)
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
		p, res, err := w.Runner.Compile(ctx, box, lang, src, dir)
		if err != nil {
			return err
		}
		verdict := string(judge.OK)
		if res.Verdict != judge.OK {
			verdict = string(judge.CompileError)
		} else {
			programs[s.role] = &p
		}
		out := clip(strings.ToValidUTF8(res.Output, ""), judgeapi.MaxCompileOutput)
		if err := rep.send(ctx, judgeapi.Report{Compile: []judgeapi.CompileResult{{Role: s.role, Verdict: verdict, Output: out}}}); err != nil {
			return err
		}
	}
	if programs[judgeapi.RoleReference] == nil || (pl.Checker.Type == string(judge.CheckCustom) && programs[judgeapi.RoleChecker] == nil) {
		return nil // the server's summary explains the compile error
	}

	limit := time.Duration(pl.TimeLimitMS) * time.Millisecond
	bruteLimit := min(limit*bruteFactor, bruteTimeCap)
	for i, t := range pl.Tests {
		in, err := w.Cache.Path(ctx, lease, t.InputSHA)
		if err != nil {
			return err
		}
		answer, err := w.Cache.Path(ctx, lease, t.OutputSHA)
		if err != nil {
			return err
		}
		var results []judgeapi.TestResult
		if v := programs[judgeapi.RoleValidator]; v != nil {
			res, err := w.Runner.Execute(ctx, box, *v, helperLimit(validatorTime, v.Lang), validatorMemoryMB, in, filepath.Join(work, "validator.out"))
			if err != nil {
				return err
			}
			tr := testResult(t.Idx, judgeapi.RoleValidator, res)
			switch res.Verdict {
			case judge.OK:
				tr.Verdict = string(judge.Accepted)
			case judge.RuntimeError:
				// A validator says no by exiting non-zero.
				tr.Verdict, tr.Message = string(judge.WrongAnswer), clip(strings.ToValidUTF8(strings.TrimSpace(string(res.Stderr)), ""), judgeapi.MaxMessage)
			}
			results = append(results, tr)
		}
		for _, role := range []string{judgeapi.RoleReference, judgeapi.RoleBrute} {
			p := programs[role]
			if p == nil {
				continue
			}
			tl := limit
			if role == judgeapi.RoleBrute {
				tl = helperLimit(bruteLimit, p.Lang)
			}
			out := filepath.Join(work, fmt.Sprintf("%s-%d.out", role, i))
			res, err := w.Runner.Execute(ctx, box, *p, tl, pl.MemoryLimitMB, in, out)
			if err != nil {
				return err
			}
			tr := testResult(t.Idx, role, res)
			if res.Verdict == judge.OK {
				if err := w.check(ctx, box, pl.Checker, programs[judgeapi.RoleChecker], in, out, answer, &tr); err != nil {
					return err
				}
			} else if role == judgeapi.RoleBrute && res.Verdict == judge.TimeLimit {
				tr.Verdict = string(judge.Skipped)
			}
			os.Remove(out)
			results = append(results, tr)
		}
		if err := rep.send(ctx, judgeapi.Report{Results: results}); err != nil {
			return err
		}
	}
	return nil
}

// check judges an output with the problem's checker.
func (w *Worker) check(ctx context.Context, box int, c judgeapi.Checker, custom *judge.Program, in, out, answer string, tr *judgeapi.TestResult) error {
	if c.Type == string(judge.CheckCustom) {
		res, err := w.Runner.RunChecker(ctx, box, *custom, in, out, answer)
		if err != nil {
			return err
		}
		tr.Verdict, tr.Message = string(res.Verdict), clip(res.Message, judgeapi.MaxMessage)
		return nil
	}
	exp, err := os.Open(answer)
	if err != nil {
		return err
	}
	defer exp.Close()
	act, err := os.Open(out)
	if err != nil {
		return err
	}
	defer act.Close()
	res, err := judge.Check(judge.CheckerType(c.Type), exp, act, c.Epsilon)
	if err != nil {
		return err
	}
	tr.Verdict = string(judge.WrongAnswer)
	if res.OK {
		tr.Verdict = string(judge.Accepted)
	}
	tr.Message = clip(strings.ToValidUTF8(res.Message, ""), judgeapi.MaxMessage)
	return nil
}

// helperLimit converts a CPU budget for a helper program (validator or
// brute force) into the limit Execute expects, which multiplies by the
// language's time factor: a Python validator gets 5 s of CPU, not 15.
func helperLimit(budget time.Duration, lang judge.Language) time.Duration {
	if lang.TimeFactor <= 0 {
		return budget
	}
	return time.Duration(float64(budget) / lang.TimeFactor)
}

func testResult(idx int, role string, res judge.ExecResult) judgeapi.TestResult {
	return judgeapi.TestResult{Idx: idx, Role: role, Verdict: string(res.Verdict),
		TimeMS: int(res.Meta.Time / time.Millisecond), MemoryKB: res.Meta.MemoryKB()}
}

// clip shortens s to at most n bytes without splitting a UTF-8 sequence.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
