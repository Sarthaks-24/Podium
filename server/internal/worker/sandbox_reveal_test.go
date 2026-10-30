//go:build sandbox

// On the real judge: a Run checks the samples and reveals the first
// failure; a practice submission reveals its first failing hidden test
// with the program's output; a counting submission sends no output at all.
package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/submissions"
)

func (r *rig) runAndWait(t *testing.T, problemID int64, src string) *submissions.RunView {
	t.Helper()
	ctx := context.Background()
	run, err := r.subs.Run(ctx, submissions.RunRequest{ProblemID: problemID, UserID: 1, Language: "python3", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) && run.State != "done" && run.State != "error" {
		time.Sleep(300 * time.Millisecond)
		run, _ = r.subs.GetRunWith(ctx, run.ID, submissions.ViewOptions{})
	}
	return run
}

func TestRunRevealsTheFirstFailingSample(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "average-speed", nil)
	if len(p.Samples) != 2 || p.Samples[1] != 7 {
		t.Fatalf("samples %v", p.Samples)
	}
	// Right on sample 1, wrong on sample 7 (whose first distance is 6).
	wrongOnOpen := "import sys\nd=list(map(int,sys.stdin.read().split()))\nn=d[0]\ns=d[1::2][:n]\nh=d[2::2][:n]\nprint(0 if s[0]==6 else sum(s)/sum(h))\n"
	run := r.runAndWait(t, p.ID, wrongOnOpen)
	if run.State != "done" || len(run.Results) != 2 || run.Results[0].Verdict != "AC" || run.Results[1].Kind != submissions.RunSample ||
		run.Results[1].Verdict != "WA" {
		t.Fatalf("run %+v", run)
	}
	if rv := run.Revealed; rv == nil || rv.Idx != 7 || rv.Input != "2\n6 1\n6 3\n" || strings.TrimSpace(rv.Expected) != "3.000000000" ||
		strings.TrimSpace(rv.Output) != "0" {
		t.Fatalf("revealed %+v", run.Revealed)
	}
	// Wrong on both: the first is the one revealed.
	time.Sleep(2100 * time.Millisecond)
	run = r.runAndWait(t, p.ID, "print(0)\n")
	if run.State != "done" || run.Results[0].Verdict != "WA" || run.Revealed == nil || run.Revealed.Idx != 1 {
		t.Fatalf("run with a failing sample %+v %+v", run.Results, run.Revealed)
	}
}

func TestPracticeSubmissionRevealsTheFailingHiddenTest(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, "a-plus-b", nil)
	// The 32-bit program passes the samples and subtask 1 and fails
	// subtask 2, whose first test is 5.
	v, _, err := r.subs.Submit(ctx, submissions.SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17",
		Source: solutions["cpp17"]["WA"], ClientKey: "practice", Scope: submissions.Scope{ContestID: 1, Practice: true, Reveal: true}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) && v.State != "done" && v.State != "error" {
		time.Sleep(300 * time.Millisecond)
		v, _ = r.subs.GetWith(ctx, v.ID, submissions.StudentView)
	}
	if v.State != "done" || v.Score != 40 || v.Revealed == nil || v.Revealed.Idx != 5 || v.Revealed.SubtaskIdx != 2 ||
		v.Revealed.Output == "" || v.Revealed.Input == "" || v.Revealed.Expected == "" {
		t.Fatalf("practice submission %+v revealed %+v", v, v.Revealed)
	}
	// A counting submission of the same program: no output reached the
	// server (the report would have been refused and the job failed).
	c := r.submitAndWait(t, p.ID, "cpp17", solutions["cpp17"]["WA"], "counting")
	if c.State != "done" || c.Score != 40 || c.Revealed != nil {
		t.Fatalf("counting submission %+v", c)
	}
}
