package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// generateInputs runs the problem's generator once per planned test, with
// argv seed and the test's arguments and an empty stdin, and uploads each
// output, which becomes that test's input.
func (w *Worker) generateInputs(ctx context.Context, box int, c *judgeapi.Claim) error {
	var pl judgeapi.GenerateInputsPayload
	rep, work, done, err := w.startJob(ctx, c, &pl)
	if err != nil {
		return err
	}
	defer done()
	return w.finish(ctx, c, rep, w.runGenerateInputs(ctx, box, Lease{c.JobID, c.Lease}, rep, work, pl, w.hold(c.JobID)))
}

func (w *Worker) runGenerateInputs(ctx context.Context, box int, lease Lease, rep *reporter, work string, pl judgeapi.GenerateInputsPayload, held *[]func()) error {
	// The server checks arguments when the plan is saved; checking again
	// here keeps a bad payload from ever reaching a command line.
	for _, t := range pl.Tests {
		if len(t.Args) > judgeapi.MaxGeneratorArgs || t.Seed < 0 || t.Seed > judgeapi.MaxGeneratorSeed {
			return fmt.Errorf("planned test %d: bad seed or too many arguments", t.Idx)
		}
		for _, a := range t.Args {
			if !judgeapi.ValidGeneratorArg(a) {
				return fmt.Errorf("planned test %d: argument %q is not allowed", t.Idx, clip(a, 64))
			}
		}
	}
	lang, err := language(pl.Generator.Language)
	if err != nil {
		return err
	}
	prog, ok, err := w.compile(ctx, box, lease, rep, judgeapi.RoleGenerator, pl.Generator, lang, held)
	if err != nil || !ok {
		return err
	}
	empty := filepath.Join(work, "empty.in")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		return err
	}
	limit := time.Duration(pl.TimeLimitMS) * time.Millisecond
	for i, t := range pl.Tests {
		gen := withArgs(*prog, append([]string{strconv.FormatInt(t.Seed, 10)}, t.Args...))
		out := filepath.Join(work, fmt.Sprintf("%d.in", i))
		res, err := w.Runner.Execute(ctx, box, gen, limit, pl.MemoryLimitMB, empty, out)
		if err != nil {
			return err
		}
		tr := testResult(t.Idx, judgeapi.RoleGenerator, res)
		if res.Verdict == judge.OK {
			data, err := os.ReadFile(out)
			if err != nil {
				return err
			}
			// An empty input is reported without a hash; the server names
			// the test.
			if len(data) > 0 {
				if tr.OutputSHA, err = w.Client.Upload(ctx, lease, data); err != nil {
					return err
				}
			}
		} else {
			// The generator is setter code: the end of its stderr (a
			// Python traceback's last line) helps the setter fix it.
			tr.Message = tail(strings.TrimSpace(strings.ToValidUTF8(string(res.Stderr), "")), judgeapi.MaxMessage)
		}
		os.Remove(out)
		if err := rep.send(ctx, judgeapi.Report{Results: []judgeapi.TestResult{tr}}); err != nil {
			return err
		}
	}
	return nil
}

// withArgs returns prog with args appended to its run command. The
// arguments go after isolate's "--" and reach the program as argv, never
// through a shell.
func withArgs(prog judge.Program, args []string) judge.Program {
	prog.Lang.Run = append(slices.Clone(prog.Lang.Run), args...)
	return prog
}

// tail returns at most the last n bytes of s without splitting a UTF-8
// sequence.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[len(s)-n:], "")
}
