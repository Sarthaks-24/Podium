//go:build sandbox

// M5 acceptance on the real judge (cache/v1/m5-plan.md section 5): the
// fake provider's two packs, one from a brief and one from pasted text,
// become drafts; their inputs are generated in isolate, outputs are
// generated from the reference, and validation passes, with no manual
// step. Then the model's solution ("Can a model solve this?") is judged
// like a submission. A broken generator is named on the test it failed.
package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// waitPipeline follows input generation, output generation and validation
// until validation finishes, failing at the first step that reports a
// problem.
func (r *rig) waitPipeline(t *testing.T, id int64) *problems.Validation {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		if ig, _ := r.bank.LatestInputGeneration(ctx, id); ig != nil {
			if ig.State == problems.ValidationError {
				t.Fatalf("input generation failed: %s", ig.Error)
			}
			if ig.State == "done" && ig.Summary != nil && len(ig.Summary.Problems) > 0 {
				t.Fatalf("input generation: %s", strings.Join(ig.Summary.Problems, " | "))
			}
		}
		if g, _ := r.bank.LatestGeneration(ctx, id); g != nil {
			if g.State == problems.ValidationError {
				t.Fatalf("output generation failed: %s", g.Error)
			}
			if g.State == "done" && g.Summary != nil && len(g.Summary.Problems) > 0 {
				t.Fatalf("output generation: %s", strings.Join(g.Summary.Problems, " | "))
			}
		}
		if v, _ := r.bank.LatestValidation(ctx, id); v != nil && v.Current {
			switch v.State {
			case problems.ValidationPassed, problems.ValidationFailed, problems.ValidationError:
				return v
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("the pipeline did not finish")
	return nil
}

func TestM5FakeWizardDraftsReachValidated(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	fake := &ai.Fake{}
	for name, req := range map[string]ai.Request{
		"brief":  {Brief: &ai.Brief{Topic: "Maximum subarray sum", Difficulty: "easy", Constraints: "n up to 200000"}},
		"pasted": {Pasted: "Given two integers a and b, return their sum. Example: a = 1, b = 2, output 3."},
	} {
		start := time.Now()
		pack, err := ai.Generate(ctx, fake, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		p, err := r.bank.CreateDraft(ctx, ai.ToDraft(pack), 1)
		if err != nil {
			t.Fatalf("%s draft: %v", name, err)
		}
		if _, err := r.bank.RequestInputGeneration(ctx, p.ID, 1); err != nil {
			t.Fatalf("%s generate inputs: %v", name, err)
		}
		v := r.waitPipeline(t, p.ID)
		if v.State != problems.ValidationPassed {
			t.Fatalf("%s (%s) validation:\n%s", name, p.Slug, describe(v))
		}
		got, _ := r.bank.Get(ctx, p.ID)
		// Samples, open tests (D115), then four generated tests.
		if !got.Validated() || len(got.Tests) != len(got.Samples)+len(got.Open)+4 {
			t.Fatalf("%s: validated=%v tests=%d", name, got.Validated(), len(got.Tests))
		}
		t.Logf("%s: %s validated in %s (reference %d/%d, brute agreed %d/%d, validator %d/%d, slowest %d ms)", name, p.Slug,
			time.Since(start).Round(time.Second), v.Summary.ReferencePassed, v.Summary.ReferenceTotal, v.Summary.BruteAgreed,
			v.Summary.BruteTotal, v.Summary.ValidatorPassed, v.Summary.ValidatorTotal, v.Summary.MaxTimeMS)

		sub := r.submitAndWait(t, p.ID, "cpp17", pack.Reference, "reference-"+name)
		if sub.Verdict != "AC" || sub.Score != 100 {
			t.Errorf("%s: the reference got %s %d/100 (%s)", name, sub.Verdict, sub.Score, sub.Error)
		}
	}
}

func TestM5BrokenGeneratorIsNamed(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	pack := ai.FakePairSums()
	// Fails on the second subtask's arguments only.
	pack.Generator = "import sys\nif 'v=1000' not in sys.argv:\n    raise ValueError('v too large')\n" + pack.Generator
	p, err := r.bank.CreateDraft(ctx, ai.ToDraft(pack), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.bank.RequestInputGeneration(ctx, p.ID, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	var ig *problems.InputGeneration
	for time.Now().Before(deadline) {
		ig, _ = r.bank.LatestInputGeneration(ctx, p.ID)
		if ig != nil && (ig.State == "done" || ig.State == problems.ValidationError) {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if ig == nil || ig.Summary == nil {
		t.Fatalf("input generation %+v", ig)
	}
	probs := strings.Join(ig.Summary.Problems, " | ")
	// Two samples and one open test come first, so subtask 2's tests are 6 and 7.
	for _, want := range []string{"The generator failed on test 6 (subtask 2, seed 6): Runtime error.", "v too large",
		"The generator failed on test 7 (subtask 2, seed 7)"} {
		if !strings.Contains(probs, want) {
			t.Errorf("missing %q in %s", want, probs)
		}
	}
	if ig.Summary.Applied != 0 {
		t.Error("inputs applied after a failure")
	}
}
