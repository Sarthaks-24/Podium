package submissions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

func resetCompileTimeouts(t *testing.T) {
	t.Cleanup(func() {
		compileTimeouts.mu.Lock()
		compileTimeouts.jobs, compileTimeouts.until = nil, nil
		compileTimeouts.mu.Unlock()
	})
}

func TestCompileTimeoutSlowsDownOnlyThatUser(t *testing.T) {
	resetCompileTimeouts(t)
	ctx := context.Background()
	r := newRig(t)
	p := r.importPack(t, nil)
	run := func(user int64) error {
		_, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: user, Language: "cpp17", Source: "slow"})
		return err
	}
	submit := func(user int64) error {
		_, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: user, Language: "cpp17", Source: "slow"})
		return err
	}
	if err := run(1); err != nil {
		t.Fatal(err)
	}
	c, err := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindRun}, Worker: "fake"})
	if err != nil || c == nil {
		t.Fatalf("claim: %v %v", c, err)
	}
	// A timed-out compile must be a compile error.
	bad := judgeapi.Report{JobID: c.JobID, Lease: c.Lease,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK", TimedOut: true}}}
	var refused *jobs.ReportError
	if err := r.jobs.Report(ctx, bad); !errors.As(err, &refused) {
		t.Fatalf("timed_out on an OK compile: %v", err)
	}
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true, Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram,
		Verdict: "CE", Output: "Compilation took longer than 10 seconds.", TimedOut: true}}}
	if err := r.jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}

	*r.clock = r.clock.Add(5 * time.Second)
	var rate *RateError
	for name, err := range map[string]error{"run": run(1), "submit": submit(1)} {
		if !errors.As(err, &rate) || !strings.Contains(rate.Message, "took too long to compile. Try again in 55 seconds") ||
			rate.RetryAfter != 55*time.Second {
			t.Errorf("%s within 60 s: %v", name, err)
		}
	}
	if err := checkCompileTimeout(1, 99, r.svc.now()); err != nil {
		t.Errorf("another contest refused: %v", err)
	}
	if err := run(2); err != nil {
		t.Errorf("another user's run refused: %v", err)
	}
	if err := submit(2); err != nil {
		t.Errorf("another user's submit refused: %v", err)
	}

	*r.clock = r.clock.Add(56 * time.Second)
	if err := run(1); err != nil {
		t.Errorf("run after 60 s: %v", err)
	}
	if err := submit(1); err != nil {
		t.Errorf("submit after 60 s: %v", err)
	}
}

func TestOrdinaryCompileErrorDoesNotSlowDown(t *testing.T) {
	resetCompileTimeouts(t)
	ctx := context.Background()
	r := newRig(t)
	p := r.importPack(t, nil)
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "x"}); err != nil {
		t.Fatal(err)
	}
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindRun}, Worker: "fake"})
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "CE", Output: "error"}}}
	if err := r.jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(5 * time.Second)
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "x"}); err != nil {
		t.Errorf("run after an ordinary compile error: %v", err)
	}
}
