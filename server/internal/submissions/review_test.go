package submissions

// Tests for the M3 review fixes (cache/council-2026-10-18/m3-review.md).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

func editOf(p *problems.Problem) problems.Edit {
	return problems.Edit{Title: p.Title, Slug: p.Slug, Statement: p.Statement, TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB,
		Checker: p.Checker, Epsilon: p.Epsilon, CheckerSource: p.CheckerSource, Reference: p.Reference,
		Brute: p.Brute, Validator: p.Validator, Tags: p.Tags, Difficulty: p.Difficulty,
		Samples: p.Samples, Subtasks: append([]problems.Subtask{}, p.Subtasks...)}
}

func TestEmptySubtaskEarnsNothing(t *testing.T) {
	pl := payload()
	pl.Subtasks = append(pl.Subtasks, judgeapi.Subtask{Idx: 3, Points: 50})
	s := ScoreSubmission(pl, compiled, results(map[int]string{1: "AC", 2: "WA", 3: "SK", 4: "SK", 5: "SK", 6: "SK"}), true)
	if s.Score != 0 || s.Subtasks[2].Verdict != "SK" {
		t.Errorf("%+v", s)
	}
}

func TestBrokenProblemIsNotJudged(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	got, _ := r.bank.Get(ctx, p.ID)
	e := editOf(got)
	e.Samples = []int{1, 2, 3} // test 3 is also in subtask 1
	if _, err := r.bank.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	var notReady *NotReadyError
	_, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "x"})
	if !errors.As(err, &notReady) || !strings.Contains(strings.Join(notReady.Problems, " "), "Test 3 is in subtask 1 and in the samples") {
		t.Errorf("broken problem: %v", err)
	}
}

func TestStudentViewIsCoarse(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r.judgeNext(t, judgeapi.KindSubmit, map[int]string{})
	full, _ := r.svc.GetWith(ctx, v.ID, ViewOptions{})
	coarse, _ := r.svc.GetWith(ctx, v.ID, ViewOptions{Coarse: true, RoundMS: 100})
	if full.Subtasks[0].Tests[0].TimeMS != 120 || full.Subtasks[0].Tests[0].MemoryKB == 0 && full.MaxMemoryKB != 0 {
		t.Errorf("full view %+v", full.Subtasks[0].Tests[0])
	}
	if coarse.Subtasks[0].Tests[0].MemoryKB != 0 || coarse.Subtasks[0].Tests[0].TimeMS != 100 || coarse.MaxTimeMS != 100 {
		t.Errorf("coarse view %+v max %d", coarse.Subtasks[0].Tests[0], coarse.MaxTimeMS)
	}
	if coarse.MaxMemoryKB%memoryStepKB != 0 {
		t.Errorf("coarse max memory %d not in 16 MB steps", coarse.MaxMemoryKB)
	}
	// The step travels with the times, so a 0 can say "under 50 ms" (D322).
	if full.TimeStepMS != 10 || coarse.TimeStepMS != 100 {
		t.Errorf("time steps %d and %d, want 10 and 100", full.TimeStepMS, coarse.TimeStepMS)
	}
	hidden, _ := r.svc.GetWith(ctx, v.ID, ViewOptions{Coarse: true, RoundMS: 100, Hide: true})
	if hidden.TimeStepMS != 100 {
		t.Errorf("hidden time step %d", hidden.TimeStepMS)
	}
}

func TestSignatureIgnoresSingleHiddenTests(t *testing.T) {
	v := &SubmissionView{State: "running", Samples: []TestOutcome{{Verdict: "AC"}},
		Subtasks: []SubtaskResult{{Verdict: "", Tests: []TestOutcome{{Verdict: "AC"}, {Verdict: ""}}}}}
	before := signature(v, true)
	v.Subtasks[0].Tests[1].Verdict = "AC" // one more hidden test, subtask not decided yet
	if signature(v, true) != before {
		t.Error("a single hidden test changed the student signature")
	}
	if signature(v, false) == before {
		t.Error("organiser signature should change per test")
	}
	v.Subtasks[0].Verdict = "AC"
	if signature(v, true) == before {
		t.Error("a finished subtask did not change the student signature")
	}
}

func TestClientHashIsKeyed(t *testing.T) {
	a := &Service{HashKey: []byte("one")}
	b := &Service{HashKey: []byte("two")}
	if a.ClientHash("10.0.0.1", "ua") == b.ClientHash("10.0.0.1", "ua") {
		t.Error("the hash does not depend on the key")
	}
	if a.ClientHash("10.0.0.1", "ua") != a.ClientHash("10.0.0.1", "ua") {
		t.Error("the hash is not stable")
	}
}

func TestClientKeyIsPerProblem(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	first := r.importPack(t, nil)
	pk, err := problems.ReadPack(zipPack(t, "../../../problems/a-plus-b", nil))
	if err != nil {
		t.Fatal(err)
	}
	pk.Problem.Slug = "a-plus-b-again"
	second, err := r.bank.Import(ctx, pk, 1)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: first.ID, UserID: 1, Language: "cpp17", Source: "x", ClientKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	*r.clock = r.clock.Add(time.Minute)
	b, dup, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: second.ID, UserID: 1, Language: "cpp17", Source: "x", ClientKey: "k"})
	if err != nil || dup || b.ID == a.ID {
		t.Errorf("same key on another problem: %v dup=%v", err, dup)
	}
}

func TestEventTimesFromTheBrowser(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	now := r.clock.UnixMilli()
	evs := []Event{{Kind: "blur", At: now - 30_000}, {Kind: "blur", At: now + 60_000}, {Kind: "blur", At: now - time.Hour.Milliseconds()}}
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, evs); err != nil {
		t.Fatal(err)
	}
	rows, err := r.svc.DB.Query("SELECT created_at FROM integrity_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var at int64
		rows.Scan(&at)
		got = append(got, at)
	}
	if len(got) != 3 || got[0] != now-30_000 || got[1] != now || got[2] != now {
		t.Errorf("stored times %v, now %d", got, now)
	}
}
