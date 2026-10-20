package problems

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// newGenBank is a bank whose queue knows input and output generation,
// chained as in production.
func newGenBank(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "gen.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'setter@college.edu', 'admin', 0)`); err != nil {
		t.Fatal(err)
	}
	blobs := Blobs{Dir: filepath.Join(t.TempDir(), "tests")}
	j := &jobs.Service{DB: d}
	j.Kinds = map[string]jobs.Kind{
		judgeapi.KindValidate:       ValidateKind{},
		judgeapi.KindGenerate:       GenerateKind{Store: blobs, Jobs: j},
		judgeapi.KindGenerateInputs: GenerateInputsKind{Store: blobs, Jobs: j},
	}
	j.Discard = func(shas []string) { blobs.DiscardUnrecorded(ctx, d, shas) }
	return &Service{DB: d, Blobs: blobs, Jobs: j}
}

func sumDraft() Draft {
	return Draft{
		Problem: Problem{
			Title: "Pair sums", Slug: "pair-sums", Statement: "Add.\n\n## Input\n\n## Output\n\n## Constraints\n",
			TimeLimitMS: 1000, MemoryLimitMB: 256, Checker: CheckerTokens,
			Reference: Source{"cpp17", "int main(){}"},
			Brute:     &Source{"python3", "print(1)"}, Validator: &Source{"python3", "pass"},
			Generator: &Source{"python3", "import sys\nprint(sys.argv)"},
			Tags:      []string{"math"}, Difficulty: "easy",
			Subtasks: []Subtask{
				{Idx: 1, Points: 40, Title: "Small", Tests: "3", GeneratorArgs: []string{"v=1000"}},
				{Idx: 2, Points: 60, Title: "Large", Tests: "4", GeneratorArgs: []string{"v=1000000000000000000"}},
			},
			Plan: []PlannedTest{
				{Test: 4, Subtask: 2, Seed: 4, Args: []string{"v=1000000000000000000"}},
				{Test: 3, Subtask: 1, Seed: 3, Args: []string{"v=1000"}},
			},
		},
		Samples: []SampleCase{{"1\n1 2\n", "3\n"}, {"1\n-5 5", "0"}},
	}
}

// claim leases the next job of kind and decodes its payload.
func claim(t *testing.T, s *Service, kind string, payload any) *judgeapi.Claim {
	t.Helper()
	c, err := s.Jobs.Claim(context.Background(), judgeapi.ClaimRequest{Kinds: []string{kind}, Worker: "fake"})
	if err != nil || c == nil {
		t.Fatalf("claim %s: %v %v", kind, c, err)
	}
	json.Unmarshal(c.Payload, payload)
	return c
}

// upload plays the worker's PUT of one file under the lease.
func upload(t *testing.T, s *Service, c *judgeapi.Claim, body string) string {
	t.Helper()
	ok, err := s.Jobs.MayUpload(context.Background(), c.JobID, c.Lease)
	if err != nil || !ok {
		t.Fatalf("upload refused: %v", err)
	}
	sha := SHA([]byte(body))
	if err := s.Blobs.UploadOutput(strings.NewReader(body), sha, MaxPackFile); err != nil {
		t.Fatal(err)
	}
	s.Jobs.RecordUpload(c.JobID, sha)
	return sha
}

func TestDraftThroughGenerateInputsOutputsAndValidation(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.CreateDraft(ctx, sumDraft(), 1)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, p.ID)
	if got.Origin != OriginAI || got.Tags[len(got.Tags)-1] != AIDraftTag || len(got.Tests) != 2 || len(got.Samples) != 2 {
		t.Fatalf("draft %+v", got)
	}
	if got.Generator == nil || len(got.Plan) != 2 || got.Plan[0].Test != 3 || got.Subtasks[1].GeneratorArgs[0] != "v=1000000000000000000" {
		t.Fatalf("generator and plan did not round-trip: %+v %+v", got.Plan, got.Subtasks)
	}
	if b, _ := s.Blobs.Read(got.Tests[1].OutputSHA, 100); string(b) != "0\n" {
		t.Errorf("sample output %q: a final newline is added", b)
	}
	// A second draft with the same slug gets a suffix.
	p2, err := s.CreateDraft(ctx, sumDraft(), 1)
	if err != nil || p2.Slug != "pair-sums-2" {
		t.Fatalf("second draft slug %v %v", p2, err)
	}

	if _, err := s.RequestGeneration(ctx, p.ID, 1, false); err == nil {
		// Samples already have outputs, so there is nothing to generate yet.
		t.Fatal("output generation with nothing missing")
	}
	g, err := s.RequestInputGeneration(ctx, p.ID, 1)
	if err != nil || g.State != ValidationQueued {
		t.Fatalf("input generation %+v %v", g, err)
	}
	var notReady *NotReadyError
	if _, err := s.RequestGeneration(ctx, p.ID, 1, true); !errors.As(err, &notReady) {
		t.Errorf("output generation while inputs are generated: %v", err)
	}
	if _, err := s.RequestInputGeneration(ctx, p.ID, 1); !errors.As(err, &notReady) {
		t.Errorf("second input generation: %v", err)
	}

	var pl judgeapi.GenerateInputsPayload
	c := claim(t, s, judgeapi.KindGenerateInputs, &pl)
	if len(pl.Tests) != 2 || pl.Tests[0].Idx != 3 || pl.Tests[0].Seed != 3 || pl.Tests[1].Args[0] != "v=1000000000000000000" ||
		pl.ContentHash != got.ContentHash || pl.PlanHash != PlanHash(got) || pl.TimeLimitMS != GeneratorTimeLimitMS {
		t.Fatalf("payload %+v", pl)
	}
	// The worker may fetch the generator and nothing else.
	if ok, _ := s.Jobs.MayFetch(ctx, c.JobID, c.Lease, pl.Generator.SourceSHA); !ok {
		t.Error("generator not fetchable")
	}
	if ok, _ := s.Jobs.MayFetch(ctx, c.JobID, c.Lease, got.Tests[0].InputSHA); ok {
		t.Error("a test is fetchable by an input generation")
	}
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleGenerator, Verdict: "OK"}}}
	for _, pt := range pl.Tests {
		sha := upload(t, s, c, fmt.Sprintf("1\n%d %d\n", pt.Idx, pt.Seed))
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: pt.Idx, Role: judgeapi.RoleGenerator, Verdict: "OK", OutputSHA: sha})
	}
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	g, _ = s.LatestInputGeneration(ctx, p.ID)
	if g.State != "done" || g.Summary == nil || g.Summary.Applied != 2 || !g.Summary.Generating || len(g.Summary.Problems) != 0 {
		t.Fatalf("input generation result %+v %+v", g, g.Summary)
	}
	got, _ = s.Get(ctx, p.ID)
	if len(got.Tests) != 4 || got.Tests[2].OutputSHA != "" || got.Tests[2].InputSize == 0 {
		t.Fatalf("tests after inputs %+v", got.Tests)
	}

	// Output generation was queued for the new tests only, then validation.
	var gp judgeapi.GeneratePayload
	c = claim(t, s, judgeapi.KindGenerate, &gp)
	if len(gp.Inputs) != 2 || gp.Inputs[0].Idx != 3 || !gp.ThenValidate || gp.ContentHash != got.ContentHash {
		t.Fatalf("generate payload %+v", gp)
	}
	rep = judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, in := range gp.Inputs {
		sha := upload(t, s, c, fmt.Sprintf("%d\n", in.Idx))
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: sha})
	}
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	gen, _ := s.LatestGeneration(ctx, p.ID)
	if gen.State != "done" || gen.Summary.Applied != 2 || !gen.Summary.Validating {
		t.Fatalf("generation %+v %+v", gen, gen.Summary)
	}
	v, err := s.LatestValidation(ctx, p.ID)
	if err != nil || v.State != ValidationQueued || !v.Current {
		t.Fatalf("validation not queued: %+v %v", v, err)
	}
	fakeJudge(t, s, 3, nil)
	got, _ = s.Get(ctx, p.ID)
	if !got.Validated() {
		v, _ = s.LatestValidation(ctx, p.ID)
		t.Fatalf("not validated: %+v", v.Summary)
	}

	hasDraftTag := func(p *Problem) bool { return slices.Contains(p.Tags, AIDraftTag) }
	// A save that changes nothing, even one that leaves the tag out, keeps
	// the AI draft tag, the validation and the generator.
	e := editOf(got)
	for i := range e.Subtasks {
		e.Subtasks[i].GeneratorArgs = nil // an editor that does not know them
	}
	e.Tags = nil
	got, err = s.Update(ctx, p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDraftTag(got) || !got.Validated() {
		t.Errorf("a save that changed nothing: tags %v validated=%v; want the AI draft tag kept and still validated", got.Tags, got.Validated())
	}
	if got.Generator == nil || len(got.Plan) != 2 || len(got.Subtasks[0].GeneratorArgs) != 1 {
		t.Errorf("save lost the generator: %+v %+v", got.Plan, got.Subtasks)
	}
	// Editing the statement ends the tag and withdraws the validation: the
	// person's version must pass again. It is not a person's solution.
	e = editOf(got)
	e.Statement += "\n\nChecked by a person."
	got, err = s.Update(ctx, p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if hasDraftTag(got) || got.Validated() {
		t.Errorf("after a statement edit: tags %v validated=%v; want no AI draft tag and not validated", got.Tags, got.Validated())
	}
	var byPerson bool
	s.DB.QueryRowContext(ctx, "SELECT solution_by_person FROM problems WHERE id = ?", p.ID).Scan(&byPerson)
	if byPerson {
		t.Error("a statement edit marked the problem as having a person's solution")
	}
	// Changing the reference marks a person's solution.
	e = editOf(got)
	e.Reference.Source += "\n# checked by a person\n"
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	s.DB.QueryRowContext(ctx, "SELECT solution_by_person FROM problems WHERE id = ?", p.ID).Scan(&byPerson)
	if !byPerson {
		t.Error("a changed reference did not mark a person's solution")
	}
	// Every step above (draft, generated inputs, generated outputs,
	// validation, edits) left the working copy recorded by a revision, and
	// the server's own steps carry a reason and no author.
	assertRevisions(t, s)
	var serverMade int
	s.DB.QueryRowContext(ctx, "SELECT count(*) FROM problem_revisions WHERE problem_id = ? AND created_by IS NULL AND reason <> ''", p.ID).Scan(&serverMade)
	if serverMade != 2 {
		t.Errorf("%d revisions from generated inputs and outputs, want 2", serverMade)
	}
}

func TestGeneratorFailureIsNamedAndNothingApplied(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.CreateDraft(ctx, sumDraft(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestInputGeneration(ctx, p.ID, 1); err != nil {
		t.Fatal(err)
	}
	var pl judgeapi.GenerateInputsPayload
	c := claim(t, s, judgeapi.KindGenerateInputs, &pl)
	sha := upload(t, s, c, "1\n1 1\n")
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleGenerator, Verdict: "OK"}},
		Results: []judgeapi.TestResult{
			{Idx: 3, Role: judgeapi.RoleGenerator, Verdict: "OK", OutputSHA: sha},
			{Idx: 4, Role: judgeapi.RoleGenerator, Verdict: "RE", Message: "ValueError: bad v"},
		}}
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	g, _ := s.LatestInputGeneration(ctx, p.ID)
	want := "The generator failed on test 4 (subtask 2, seed 4): Runtime error. ValueError: bad v"
	if g.Summary == nil || g.Summary.Applied != 0 || g.Summary.Generating || len(g.Summary.Problems) != 1 || g.Summary.Problems[0] != want {
		t.Fatalf("summary %+v", g.Summary)
	}
	got, _ := s.Get(ctx, p.ID)
	if len(got.Tests) != 2 {
		t.Errorf("a failed run applied tests: %+v", got.Tests)
	}
	if c, _ := s.Jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindGenerate}}); c != nil {
		t.Error("output generation queued after a failed input generation")
	}
}

func TestStaleInputGenerationIsNotApplied(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, _ := s.CreateDraft(ctx, sumDraft(), 1)
	if _, err := s.RequestInputGeneration(ctx, p.ID, 1); err != nil {
		t.Fatal(err)
	}
	var pl judgeapi.GenerateInputsPayload
	c := claim(t, s, judgeapi.KindGenerateInputs, &pl)
	// The setter edits the generator while the job runs. The content hash
	// does not cover the generator; the plan hash does.
	got, _ := s.Get(ctx, p.ID)
	e := editOf(got)
	e.Generator = &GeneratorEdit{Source: &Source{"python3", "print(2)"}, Plan: got.Plan}
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}
	for _, pt := range pl.Tests {
		sha := upload(t, s, c, fmt.Sprintf("1\n%d 0\n", pt.Idx))
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: pt.Idx, Role: judgeapi.RoleGenerator, Verdict: "OK", OutputSHA: sha})
	}
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	g, _ := s.LatestInputGeneration(ctx, p.ID)
	if g.Summary == nil || g.Summary.Applied != 0 || !strings.Contains(strings.Join(g.Summary.Problems, " "), "changed while inputs were being generated") {
		t.Fatalf("summary %+v", g.Summary)
	}
	if got, _ := s.Get(ctx, p.ID); len(got.Tests) != 2 {
		t.Errorf("stale inputs applied")
	}
}

func TestGenerateInputsBudgetAndReportChecks(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, _ := s.CreateDraft(ctx, sumDraft(), 1)
	s.RequestInputGeneration(ctx, p.ID, 1)
	var pl judgeapi.GenerateInputsPayload
	c := claim(t, s, judgeapi.KindGenerateInputs, &pl)
	for i := 0; i < len(pl.Tests)+2; i++ {
		upload(t, s, c, fmt.Sprintf("input %d\n", i))
	}
	if ok, _ := s.Jobs.MayUpload(ctx, c.JobID, c.Lease); ok {
		t.Error("upload budget is planned tests plus 2")
	}
	var bad *jobs.ReportError
	for name, r := range map[string]judgeapi.TestResult{
		"unknown test": {Idx: 9, Role: judgeapi.RoleGenerator, Verdict: "OK"},
		"wrong role":   {Idx: 3, Role: judgeapi.RoleProgram, Verdict: "OK"},
		"output text":  {Idx: 3, Role: judgeapi.RoleGenerator, Verdict: "OK", Output: "x"},
		"not uploaded": {Idx: 3, Role: judgeapi.RoleGenerator, Verdict: "OK", OutputSHA: SHA([]byte("never sent"))},
	} {
		err := s.Jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Results: []judgeapi.TestResult{r}})
		if !errors.As(err, &bad) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestPlanRules(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	for name, edit := range map[string]func(d *Draft){
		"sample planned":  func(d *Draft) { d.Problem.Plan[0].Test = 1 },
		"twice":           func(d *Draft) { d.Problem.Plan[0].Test = 3 },
		"no subtask":      func(d *Draft) { d.Problem.Plan[0].Subtask = 7 },
		"bad seed":        func(d *Draft) { d.Problem.Plan[0].Seed = -1 },
		"bad arg":         func(d *Draft) { d.Problem.Plan[0].Args = []string{"n = 5"} },
		"brace arg":       func(d *Draft) { d.Problem.Plan[0].Args = []string{"{mem}"} },
		"subtask arg":     func(d *Draft) { d.Problem.Subtasks[0].GeneratorArgs = []string{"a;b"} },
		"no generator":    func(d *Draft) { d.Problem.Generator = nil },
		"too many args":   func(d *Draft) { d.Problem.Plan[0].Args = strings.Split(strings.Repeat("a,", 17)+"a", ",") },
		"bad gen lang":    func(d *Draft) { d.Problem.Generator.Language = "cobol" },
		"oversized input": func(d *Draft) { d.Samples[0].Input = strings.Repeat("x", MaxPackFile+1) },
	} {
		d := sumDraft()
		edit(&d)
		var fe FieldErrors
		if _, err := s.CreateDraft(ctx, d, 1); !errors.As(err, &fe) {
			t.Errorf("%s: %v", name, err)
		}
		if name != "oversized input" && CheckDraft(d) == nil {
			t.Errorf("%s: CheckDraft found nothing", name)
		}
	}
	if errs := CheckDraft(sumDraft()); errs != nil {
		t.Errorf("good draft: %v", errs)
	}
}

func TestPackCarriesGeneratorAndPlan(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, _ := s.CreateDraft(ctx, sumDraft(), 1)
	got, _ := s.Get(ctx, p.ID)
	var buf bytes.Buffer
	if err := WritePack(&buf, got, func(sha string) ([]byte, error) { return s.Blobs.Read(sha, MaxPackFile) }); err != nil {
		t.Fatal(err)
	}
	// Planned tests have no files yet; the pack still imports.
	pk, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	q := pk.Problem
	if q.Generator == nil || q.Generator.Source != got.Generator.Source || len(q.Plan) != 2 || q.Plan[1].Args[0] != "v=1000000000000000000" ||
		q.Subtasks[0].GeneratorArgs[0] != "v=1000" || len(q.Tests) != 2 {
		t.Fatalf("round trip lost the generator: %+v %+v", q.Plan, q.Subtasks)
	}
	if !strings.Contains(strings.Join(q.Warnings(), " "), "2 planned tests have no input yet") {
		t.Errorf("warnings %v", q.Warnings())
	}
	pk.Problem.Slug = "pair-sums-copy"
	if _, err := s.Import(ctx, pk, 1); err != nil {
		t.Fatal(err)
	}
}
