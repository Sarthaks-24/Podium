package problems

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs/jobstest"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Each place that queues setter work signals after its transaction commits,
// so a judge holding a claim starts at once (J2, V203-69).
func TestSetterJobsWakeAHeldClaim(t *testing.T) {
	ctx := context.Background()
	const within = 100 * time.Millisecond
	t.Run("validation", func(t *testing.T) {
		s := newBank(t)
		p := importExample(t, s, "a-plus-b")
		answer := jobstest.Hold(t, s.Jobs, judgeapi.KindValidate)
		if _, err := s.RequestValidation(ctx, p.ID, 0); err != nil {
			t.Fatal(err)
		}
		if answer(within) == nil {
			t.Error("a validation request did not wake a held claim")
		}
	})
	t.Run("output generation", func(t *testing.T) {
		s := newGenBank(t)
		p := importExample(t, s, "a-plus-b")
		answer := jobstest.Hold(t, s.Jobs, judgeapi.KindGenerate)
		if _, err := s.RequestGeneration(ctx, p.ID, 1, true); err != nil {
			t.Fatal(err)
		}
		if answer(within) == nil {
			t.Error("an output generation request did not wake a held claim")
		}
	})
	t.Run("input generation", func(t *testing.T) {
		s := newGenBank(t)
		p, err := s.CreateDraft(ctx, sumDraft(), 1)
		if err != nil {
			t.Fatal(err)
		}
		answer := jobstest.Hold(t, s.Jobs, judgeapi.KindGenerateInputs)
		if _, err := s.RequestInputGeneration(ctx, p.ID, 1); err != nil {
			t.Fatal(err)
		}
		if answer(within) == nil {
			t.Error("an input generation request did not wake a held claim")
		}
	})
	t.Run("language proof", func(t *testing.T) {
		s := newProofBank(t, true)
		p := proofProblem(t, s)
		answer := jobstest.Hold(t, s.Jobs, judgeapi.KindValidate)
		if err := s.RequestLanguageProof(ctx, p.ID, 1, ProofRequest{Language: "java21", Solution: "class Solution {}"}); err != nil {
			t.Fatal(err)
		}
		if answer(within) == nil {
			t.Error("a language check did not wake a held claim")
		}
	})
}

// Closing a generation job queues the validation behind it from inside the
// closing transaction; the close signals after it commits, so a held claim
// takes the validation at once.
func TestAClosingGenerationWakesAHeldClaimForTheValidationItQueues(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Pair sums", "", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceTests(ctx, p.ID, zipEntries(t, entry{name: "1.in", body: "1"}, entry{name: "1.out", body: "1"},
		entry{name: "2.in", body: "2"}), RevisionNote{}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, p.ID)
	e := editOf(got)
	e.Samples, e.Subtasks = []int{1}, []Subtask{{Points: 100, Tests: "2"}}
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestGenerationThenValidation(ctx, p.ID, 1); err != nil {
		t.Fatal(err)
	}
	var gp judgeapi.GeneratePayload
	c := claim(t, s, judgeapi.KindGenerate, &gp)
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, in := range gp.Inputs {
		sha := upload(t, s, c, "2\n")
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: sha})
	}
	answer := jobstest.Hold(t, s.Jobs, judgeapi.KindValidate)
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if answer(100*time.Millisecond) == nil {
		t.Error("the validation queued by a closing generation did not wake a held claim")
	}
}
