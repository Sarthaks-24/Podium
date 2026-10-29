package submissions

// The revealed test (D115): Run executes the samples and reveals the first
// failure; a submission that never counts reveals its first failing hidden
// test; a submission that counts never does, in either feedback mode.
// v3 has no open tests (V3-5).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// withThirdSample makes test 3 of a-plus-b a sample (out of subtask 1).
func (r *rig) withThirdSample(t *testing.T) *problems.Problem {
	t.Helper()
	p := r.importPack(t, nil)
	e := editOf(p)
	e.Subtasks[0].Tests = "4"
	e.Samples = append(e.Samples, 3)
	p, err := r.bank.Update(context.Background(), p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Problems(); len(probs) > 0 {
		t.Fatalf("problem not ready: %v", probs)
	}
	return p
}

// report claims the next job of kind and reports the given results.
func (r *rig) report(t *testing.T, kind string, results func(c *judgeapi.Claim) []judgeapi.TestResult) error {
	t.Helper()
	ctx := context.Background()
	c, err := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{kind}, Worker: "fake"})
	if err != nil || c == nil {
		t.Fatalf("claim %s: %v %v", kind, c, err)
	}
	return r.jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}, Results: results(c)})
}

func TestRunRevealsTheFirstFailingSample(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.withThirdSample(t)
	ws, err := r.svc.Workspace(ctx, p.ID, 1, Scope{})
	if err != nil || len(ws.Samples) != 3 {
		t.Fatalf("workspace samples %d %v", len(ws.Samples), err)
	}
	run, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "print(3)"})
	if err != nil {
		t.Fatal(err)
	}
	var pl judgeapi.RunPayload
	if err := r.report(t, judgeapi.KindRun, func(c *judgeapi.Claim) []judgeapi.TestResult {
		json.Unmarshal(c.Payload, &pl)
		return []judgeapi.TestResult{
			{Idx: 1, Role: judgeapi.RoleProgram, Verdict: "AC", Output: "3\n"},
			{Idx: 2, Role: judgeapi.RoleProgram, Verdict: "AC", Output: "7\n"},
			{Idx: 3, Role: judgeapi.RoleProgram, Verdict: "WA", Output: "41\n"},
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(pl.Inputs) != 3 || pl.Inputs[2].Idx != 3 || pl.Inputs[2].Open || pl.Inputs[2].ExpectedSHA == "" {
		t.Fatalf("run payload %+v", pl.Inputs)
	}
	got, _ := r.svc.GetRunWith(ctx, run.ID, StudentView)
	if len(got.Results) != 3 || got.Results[2].Kind != RunSample || got.Results[2].Label != "Example 3" || got.Results[0].Kind != RunSample {
		t.Fatalf("results %+v", got.Results)
	}
	if got.TimeStepMS != 10 {
		t.Errorf("run time step %d, want 10", got.TimeStepMS)
	}
	if graded, _ := r.svc.GetRunWith(ctx, run.ID, ViewOptions{Coarse: true, RoundMS: 100}); graded.TimeStepMS != 100 {
		t.Errorf("graded run time step %d, want 100", graded.TimeStepMS)
	}
	in3, _ := r.bank.Blobs.Read(p.Tests[2].InputSHA, maxSampleTxt+1)
	out3, _ := r.bank.Blobs.Read(p.Tests[2].OutputSHA, maxSampleTxt+1)
	rv := got.Revealed
	if rv == nil || rv.Idx != 3 || rv.Kind != RunSample || rv.Input != clipText(in3, maxSampleTxt) || rv.Expected != clipText(out3, maxSampleTxt) ||
		rv.Output != "41\n" || got.Results[2].Output != "41\n" || got.Results[2].Expected == nil {
		t.Fatalf("revealed %+v", rv)
	}

	// The first failing sample is the one revealed.
	*r.clock = r.clock.Add(3e9)
	run, _ = r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "print(4)"})
	r.report(t, judgeapi.KindRun, func(*judgeapi.Claim) []judgeapi.TestResult {
		return []judgeapi.TestResult{
			{Idx: 1, Role: judgeapi.RoleProgram, Verdict: "WA", Output: "4\n"},
			{Idx: 2, Role: judgeapi.RoleProgram, Verdict: "AC", Output: "7\n"},
			{Idx: 3, Role: judgeapi.RoleProgram, Verdict: "WA", Output: "4\n"},
		}
	})
	got, _ = r.svc.GetRunWith(ctx, run.ID, StudentView)
	if got.Revealed == nil || got.Revealed.Idx != 1 || got.Revealed.Kind != RunSample || got.Results[2].Verdict != "WA" {
		t.Fatalf("after a failing sample: revealed %+v results %+v", got.Revealed, got.Results)
	}

	// Every test passing reveals nothing.
	*r.clock = r.clock.Add(3e9)
	run, _ = r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "print(5)"})
	r.judgeNext(t, judgeapi.KindRun, nil)
	got, _ = r.svc.GetRunWith(ctx, run.ID, StudentView)
	if b, _ := json.Marshal(got); strings.Contains(string(b), `"revealed"`) {
		t.Errorf("a passing run carries revealed: %s", b)
	}
}

func TestPracticeRevealsAndContestNever(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.withThirdSample(t)
	failing := func(withOutput bool) func(c *judgeapi.Claim) []judgeapi.TestResult {
		return func(c *judgeapi.Claim) []judgeapi.TestResult {
			var pl judgeapi.SubmitPayload
			json.Unmarshal(c.Payload, &pl)
			var out []judgeapi.TestResult
			for _, test := range pl.Tests {
				res := judgeapi.TestResult{Idx: test.Idx, Role: judgeapi.RoleProgram, Verdict: "AC"}
				if test.Idx == 5 {
					res.Verdict = "WA"
					if withOutput {
						res.Output = "-1\n"
					}
				}
				if test.Idx == 6 {
					res.Verdict = "SK"
				}
				out = append(out, res)
			}
			return out
		}
	}

	// Practice: the payload asks to reveal; the first failing hidden test
	// is shown to the author, never to a co-organiser or while hidden.
	sub, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "practice",
		Scope: Scope{ContestID: 9, Practice: true, Reveal: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.report(t, judgeapi.KindSubmit, failing(true)); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetWith(ctx, sub.ID, StudentView)
	in5, _ := r.bank.Blobs.Read(p.Tests[4].InputSHA, 1<<20)
	out5, _ := r.bank.Blobs.Read(p.Tests[4].OutputSHA, 1<<20)
	rv := got.Revealed
	if rv == nil || rv.Idx != 5 || rv.SubtaskIdx != 2 || rv.Output != "-1\n" {
		t.Fatalf("practice revealed %+v", rv)
	}
	// Each field is capped at 4 KB like the samples.
	if len(rv.Input) > 4<<10 || !strings.HasPrefix(string(in5), rv.Input) || rv.Expected != clipText(out5, 4<<10) ||
		rv.Truncated != (len(in5) > 4<<10 || len(out5) > 4<<10) {
		t.Fatalf("practice revealed fields: input %d of %d bytes, truncated %v", len(rv.Input), len(in5), rv.Truncated)
	}
	for name, o := range map[string]ViewOptions{"co-organiser": {Coarse: true, NoReveal: true}, "exam hidden": {Coarse: true, Hide: true}} {
		v, _ := r.svc.GetWith(ctx, sub.ID, o)
		if v.Revealed != nil {
			t.Errorf("%s sees the revealed test", name)
		}
	}

	// A submission that counts: the judge may not send output, and no
	// view carries revealed, in either feedback mode.
	for _, mode := range []ViewOptions{{Coarse: true, RoundMS: 100}, {Coarse: true, RoundMS: 100, Hide: true}} {
		*r.clock = r.clock.Add(10e9)
		sub, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 2, Language: "cpp17", Source: "contest" + strings.Repeat("x", int(r.clock.Unix()%7)),
			Scope: Scope{ContestID: 9, View: mode}})
		if err != nil {
			t.Fatal(err)
		}
		c, err := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "fake"})
		if err != nil || c == nil {
			t.Fatalf("claim: %v %v", c, err)
		}
		var pl judgeapi.SubmitPayload
		json.Unmarshal(c.Payload, &pl)
		if pl.Reveal {
			t.Fatal("a counting submission's payload asks to reveal")
		}
		rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
			Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}, Results: failing(true)(c)}
		var bad *jobs.ReportError
		if err := r.jobs.Report(ctx, rep); !errors.As(err, &bad) {
			t.Fatalf("output on a counting submission was accepted: %v", err)
		}
		rep.Results = failing(false)(c)
		if err := r.jobs.Report(ctx, rep); err != nil {
			t.Fatal(err)
		}
		for _, o := range []ViewOptions{mode, {}} {
			v, _ := r.svc.GetWith(ctx, sub.ID, o)
			if v.State != "done" && !o.Hide {
				t.Fatalf("not judged: %+v", v)
			}
			if b, _ := json.Marshal(v); strings.Contains(string(b), `"revealed"`) {
				t.Errorf("a counting submission (hide=%v) carries revealed: %s", mode.Hide, b)
			}
		}
	}
}
