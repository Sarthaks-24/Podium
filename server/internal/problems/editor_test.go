package problems

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// The editor asks for no slug: an empty one is made from the title.
func TestCreateDerivesTheSlugFromTheTitle(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	for _, c := range []struct{ title, slug, want string }{
		{"Largest of Three!", "", "largest-of-three"},
		{"Largest of three", "", "largest-of-three-2"},
		{"  ", "", ""},
		{"!!!", "", "problem"},
		{"Kept as given", "kept-slug", "kept-slug"},
	} {
		p, err := s.Create(ctx, c.title, c.slug, 1)
		if c.want == "" {
			var fields FieldErrors
			if !errors.As(err, &fields) || fields["title"] == "" {
				t.Errorf("%q: want a title error, got %v", c.title, err)
			}
			continue
		}
		if err != nil || p.Slug != c.want {
			t.Errorf("%q: slug %v, error %v; want %s", c.title, p, err, c.want)
		}
	}
	if got := SlugFromTitle(strings.Repeat("ab ", 40)); len(got) > 48 || strings.HasSuffix(got, "-") {
		t.Errorf("long title slug %q", got)
	}
}

// Validate on a problem with missing outputs makes them first, then
// validates; a structure that would stop the chain is refused up front.
func TestValidateMakesMissingOutputsFirst(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Pair sums", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceTests(ctx, p.ID, zipEntries(t,
		entry{name: "1.in", body: "1"}, entry{name: "1.out", body: "1"},
		entry{name: "2.in", body: "2"}, entry{name: "3.in", body: "3"}), RevisionNote{}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, p.ID)
	e := editOf(got)
	e.Samples, e.Subtasks = []int{1}, []Subtask{{Points: 100, Tests: "2"}}
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}

	var notReady *NotReadyError
	if _, err := s.RequestGenerationThenValidation(ctx, p.ID, 1); !errors.As(err, &notReady) || !strings.Contains(fmt.Sprint(notReady.Problems), "Test 3 is in no subtask") {
		t.Fatalf("test in no subtask: %v", err)
	}
	// Validation on its own still waits for every output.
	if _, err := s.RequestValidation(ctx, p.ID, 1); !errors.As(err, &notReady) || !strings.Contains(fmt.Sprint(notReady.Problems), "Validate makes them") {
		t.Fatalf("validation with missing outputs: %v", err)
	}

	e.Subtasks = []Subtask{{Points: 100, Tests: "2-3"}}
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	g, err := s.RequestGenerationThenValidation(ctx, p.ID, 1)
	if err != nil || g.State != ValidationQueued {
		t.Fatalf("generation %+v %v", g, err)
	}
	var gp judgeapi.GeneratePayload
	c := claim(t, s, judgeapi.KindGenerate, &gp)
	if len(gp.Inputs) != 2 || gp.Inputs[0].Idx != 2 || !gp.ThenValidate || gp.All {
		t.Fatalf("generate payload %+v", gp)
	}
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, in := range gp.Inputs {
		sha := upload(t, s, c, fmt.Sprintf("%d\n", in.Idx))
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: sha})
	}
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	gen, _ := s.LatestGeneration(ctx, p.ID)
	if gen.State != "done" || gen.Summary == nil || gen.Summary.Applied != 2 || !gen.Summary.Validating {
		t.Fatalf("generation result %+v", gen)
	}
	v, err := s.LatestValidation(ctx, p.ID)
	if err != nil || v.State != ValidationQueued {
		t.Fatalf("validation not queued after the outputs: %+v %v", v, err)
	}
}

// A starter is statement-like content (v4 E5): saving one makes a new
// revision that records it, keeps the content hash (so validation and
// judging are untouched), and an edit that leaves it out keeps it.
func TestStartersMakeARevisionNotAJudgingChange(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Starters", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Get(ctx, p.ID)
	revs, _, total, _ := s.Revisions(ctx, p.ID, 10)
	e := editOf(before)
	starters := map[string]string{"cpp17": "int main() {\n}\n"}
	e.Starters = &starters
	got, err := s.Update(ctx, p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentHash != before.ContentHash || got.Starters["cpp17"] != starters["cpp17"] {
		t.Errorf("hash %s to %s, starters %v", before.ContentHash, got.ContentHash, got.Starters)
	}
	after, _, total2, _ := s.Revisions(ctx, p.ID, 10)
	if total2 != total+1 || after[0].Starters["cpp17"] != starters["cpp17"] || len(revs[0].Starters) != 0 {
		t.Errorf("revisions %d to %d; newest starters %v", total, total2, after[0].Starters)
	}
	// An editor that sends no starters keeps them.
	e = editOf(got)
	e.Starters = nil
	if kept, err := s.Update(ctx, p.ID, e); err != nil || kept.Starters["cpp17"] == "" {
		t.Errorf("save without starters: %v %v", kept.Starters, err)
	}
	bad := map[string]string{"cobol": "x"}
	e.Starters = &bad
	var fields FieldErrors
	if _, err := s.Update(ctx, p.ID, e); !errors.As(err, &fields) || fields["starters"] == "" {
		t.Errorf("unknown language: %v", err)
	}
}
