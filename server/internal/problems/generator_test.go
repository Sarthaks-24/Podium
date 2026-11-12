package problems

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'setter@college.edu', 'organiser', 0)`); err != nil {
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

// funcDraft is the wizard's draft of a function problem: the array-sum
// example's locked parts and starters in four languages, a C++ reference
// and brute force, and the generator and plan of sumDraft.
func funcDraft(t *testing.T, s *Service) Draft {
	t.Helper()
	ex := importExample(t, s, "array-sum")
	d := sumDraft()
	d.Problem.Mode, d.Problem.LockedTop, d.Problem.LockedBottom, d.Problem.Starters = ex.Mode, ex.LockedTop, ex.LockedBottom, ex.Starters
	d.Problem.Reference = ex.Reference
	d.Problem.Brute = &Source{"cpp17", ex.Reference.Source}
	return d
}

func solutionTexts(langs ...string) map[string]string {
	out := map[string]string{}
	for _, l := range langs {
		out[l] = "SOLUTION " + l + "\n"
	}
	return out
}

// runInputs plays the judge over the open input generation: every planned
// test gets an input.
func runInputs(t *testing.T, s *Service) {
	t.Helper()
	var pl judgeapi.GenerateInputsPayload
	c := claim(t, s, judgeapi.KindGenerateInputs, &pl)
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleGenerator, Verdict: "OK"}}}
	for _, pt := range pl.Tests {
		sha := upload(t, s, c, fmt.Sprintf("1\n%d %d\n", pt.Idx, pt.Seed))
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: pt.Idx, Role: judgeapi.RoleGenerator, Verdict: "OK", OutputSHA: sha})
	}
	if err := s.Jobs.Report(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
}

// runOutputs plays the judge over the open output generation; the reference
// fails on test failIdx when that is not 0.
func runOutputs(t *testing.T, s *Service, failIdx int) {
	t.Helper()
	var pl judgeapi.GeneratePayload
	c := claim(t, s, judgeapi.KindGenerate, &pl)
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, in := range pl.Inputs {
		if in.Idx == failIdx {
			rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "RE"})
			continue
		}
		sha := upload(t, s, c, fmt.Sprintf("%d\n", in.Idx))
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: sha})
	}
	if err := s.Jobs.Report(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
}

// payloadProof is the proof spec on the newest job of a kind, nil when its
// payload has none.
func payloadProof(t *testing.T, s *Service, kind string) *ProofSpec {
	t.Helper()
	var raw string
	if err := s.DB.QueryRow(`SELECT payload FROM jobs WHERE kind = ? ORDER BY id DESC LIMIT 1`, kind).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var pl struct {
		Proof *ProofSpec `json:"proof"`
	}
	if err := json.Unmarshal([]byte(raw), &pl); err != nil {
		t.Fatal(err)
	}
	return pl.Proof
}

func openProof(t *testing.T, s *Service, id int64) bool {
	t.Helper()
	open, err := OpenProof(context.Background(), s.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	return open
}

func proofJobs(t *testing.T, s *Service) int {
	return count(t, s, "SELECT count(*) FROM jobs WHERE kind = 'validate' AND priority = ?", ProofPriority)
}

// solutionsOnDisk fails unless every solution blob of spec is stored with
// the text that was given.
func solutionsOnDisk(t *testing.T, s *Service, spec *ProofSpec, texts map[string]string) {
	t.Helper()
	for lang, sha := range spec.Solutions {
		b, err := s.Blobs.Read(sha, 1000)
		if err != nil || string(b) != texts[lang] {
			t.Errorf("solution %s: %q %v", lang, b, err)
		}
	}
}

func TestWizardFunctionDraftChainQueuesProofsNotValidation(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	s.Jobs.Kinds[judgeapi.KindValidate] = ValidateKind{Store: s.Blobs, Jobs: s.Jobs}
	p, err := s.CreateDraft(ctx, funcDraft(t, s), 1)
	if err != nil {
		t.Fatal(err)
	}
	texts := solutionTexts("java21", "python3", "node22")
	if g, err := s.RequestInputGenerationProof(ctx, p.ID, 1, texts); err != nil || g.State != ValidationQueued {
		t.Fatalf("request %+v %v", g, err)
	}
	spec := payloadProof(t, s, judgeapi.KindGenerateInputs)
	if spec == nil || spec.Source != ProofSourceWizard || len(spec.Solutions) != 3 {
		t.Fatalf("generate_inputs payload spec %+v", spec)
	}
	solutionsOnDisk(t, s, spec, texts)
	if !openProof(t, s, p.ID) {
		t.Error("a draft with a proof ahead of it is not marked as being checked")
	}
	if _, err := s.RequestGeneration(ctx, p.ID, 1, true); !errors.Is(err, ErrChecking) {
		t.Errorf("output generation during the chain: %v", err)
	}
	if _, err := s.RequestInputGeneration(ctx, p.ID, 1); !errors.Is(err, ErrChecking) {
		t.Errorf("input generation during the chain: %v", err)
	}
	// The solutions live only in the open jobs' payloads: a collection with
	// every blob long out of its grace period still keeps them.
	gcKeeps := func(when string) {
		t.Helper()
		age(t, s, time.Now().Add(-30*24*time.Hour))
		if _, err := s.Blobs.GC(ctx, s.DB, time.Now()); err != nil {
			t.Fatal(err)
		}
		for lang, sha := range spec.Solutions {
			if !onDisk(s, sha) || !hasRow(t, s, sha) {
				t.Errorf("%s: GC removed the %s solution", when, lang)
			}
		}
	}
	gcKeeps("while inputs are generated")

	runInputs(t, s)
	if got := payloadProof(t, s, judgeapi.KindGenerate); got == nil || !maps.Equal(got.Solutions, spec.Solutions) || got.Source != ProofSourceWizard {
		t.Fatalf("generate payload spec %+v, want %+v", got, spec)
	}
	if !openProof(t, s, p.ID) {
		t.Error("not marked as being checked between inputs and outputs")
	}
	gcKeeps("while outputs are generated")

	runOutputs(t, s, 0)
	gen, _ := s.LatestGeneration(ctx, p.ID)
	if gen.State != "done" || gen.Summary.Applied != 2 || gen.Summary.Validating || gen.Summary.Proofs != 6 {
		t.Fatalf("generation %+v %+v", gen, gen.Summary)
	}
	if validateJobs(t, s) != 0 || proofJobs(t, s) != 6 || !openProof(t, s, p.ID) {
		t.Fatalf("validate jobs %d, proof jobs %d, open %v: want 0, 6, true", validateJobs(t, s), proofJobs(t, s), openProof(t, s, p.ID))
	}
	gcKeeps("while the languages are proved")

	// The round passes: nothing is removed and Validate follows.
	runProofs(t, s, nil)
	if validateJobs(t, s) != 1 || openProof(t, s, p.ID) {
		t.Errorf("after the proofs: validate jobs %d, open %v", validateJobs(t, s), openProof(t, s, p.ID))
	}
	if got, _ := s.Get(ctx, p.ID); len(got.Offered()) != 4 {
		t.Errorf("offered %v", got.Offered())
	}
}

func TestAnOldPayloadWithNoProofGoesToValidation(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.CreateDraft(ctx, funcDraft(t, s), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestInputGeneration(ctx, p.ID, 1); err != nil {
		t.Fatal(err)
	}
	if payloadProof(t, s, judgeapi.KindGenerateInputs) != nil || openProof(t, s, p.ID) {
		t.Fatal("a plain input generation carries or shows a proof")
	}
	runInputs(t, s)
	if payloadProof(t, s, judgeapi.KindGenerate) != nil {
		t.Error("the generate payload gained a proof")
	}
	runOutputs(t, s, 0)
	gen, _ := s.LatestGeneration(ctx, p.ID)
	if !gen.Summary.Validating || gen.Summary.Proofs != 0 || validateJobs(t, s) != 1 || proofJobs(t, s) != 0 {
		t.Errorf("summary %+v, validate jobs %d, proof jobs %d", gen.Summary, validateJobs(t, s), proofJobs(t, s))
	}
}

func TestAWholeProgramDraftWithASpecStillValidates(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.CreateDraft(ctx, sumDraft(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, nil); err != nil {
		t.Fatal(err)
	}
	runInputs(t, s)
	runOutputs(t, s, 0)
	gen, _ := s.LatestGeneration(ctx, p.ID)
	if !gen.Summary.Validating || gen.Summary.Proofs != 0 || validateJobs(t, s) != 1 || proofJobs(t, s) != 0 {
		t.Errorf("summary %+v, validate jobs %d, proof jobs %d", gen.Summary, validateJobs(t, s), proofJobs(t, s))
	}
}

func TestAReferenceWithNoCompileStepAloneValidatesDirectly(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	d := funcDraft(t, s)
	for _, m := range []map[string]string{d.Problem.LockedTop, d.Problem.LockedBottom, d.Problem.Starters} {
		for _, lang := range []string{"cpp17", "java21", "python3"} {
			delete(m, lang)
		}
	}
	d.Problem.Reference = Source{"node22", "function arraySum(a) { return a.reduce((x, y) => x + y, 0); }\n"}
	d.Problem.Brute = &Source{"node22", d.Problem.Reference.Source}
	p, err := s.CreateDraft(ctx, d, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Offered(); len(got) != 1 || got[0] != "node22" {
		t.Fatalf("offered %v", got)
	}
	if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, solutionTexts("node22")); err != nil {
		t.Fatal(err)
	}
	if spec := payloadProof(t, s, judgeapi.KindGenerateInputs); spec == nil || len(spec.Solutions) != 0 {
		t.Fatalf("spec %+v: the reference language needs no solution", spec)
	}
	runInputs(t, s)
	runOutputs(t, s, 0)
	gen, _ := s.LatestGeneration(ctx, p.ID)
	if !gen.Summary.Validating || gen.Summary.Proofs != 0 || validateJobs(t, s) != 1 || proofJobs(t, s) != 0 || openProof(t, s, p.ID) {
		t.Errorf("summary %+v, validate jobs %d, proof jobs %d", gen.Summary, validateJobs(t, s), proofJobs(t, s))
	}
}

func TestAChainThatStopsEarlyQueuesNoProof(t *testing.T) {
	ctx := context.Background()
	t.Run("generator fails", func(t *testing.T) {
		s := newGenBank(t)
		p, err := s.CreateDraft(ctx, funcDraft(t, s), 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, solutionTexts("java21", "python3", "node22")); err != nil {
			t.Fatal(err)
		}
		var pl judgeapi.GenerateInputsPayload
		c := claim(t, s, judgeapi.KindGenerateInputs, &pl)
		rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
			Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleGenerator, Verdict: "OK"}},
			Results: []judgeapi.TestResult{{Idx: 3, Role: judgeapi.RoleGenerator, Verdict: "RE"}, {Idx: 4, Role: judgeapi.RoleGenerator, Verdict: "RE"}}}
		if err := s.Jobs.Report(ctx, rep); err != nil {
			t.Fatal(err)
		}
		if count(t, s, "SELECT count(*) FROM jobs WHERE kind = ?", judgeapi.KindGenerate) != 0 || proofJobs(t, s) != 0 || validateJobs(t, s) != 0 || openProof(t, s, p.ID) {
			t.Error("a failed generator left a job behind or the draft marked as being checked")
		}
	})
	t.Run("an output fails", func(t *testing.T) {
		s := newGenBank(t)
		p, err := s.CreateDraft(ctx, funcDraft(t, s), 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, solutionTexts("java21", "python3", "node22")); err != nil {
			t.Fatal(err)
		}
		runInputs(t, s)
		runOutputs(t, s, 4)
		gen, _ := s.LatestGeneration(ctx, p.ID)
		if gen.Summary.Applied != 1 || len(gen.Summary.Problems) != 1 || gen.Summary.Proofs != 0 {
			t.Fatalf("summary %+v", gen.Summary)
		}
		if proofJobs(t, s) != 0 || validateJobs(t, s) != 0 || openProof(t, s, p.ID) {
			t.Error("a failed output left a job behind or the draft marked as being checked")
		}
	})
}

func TestRequestInputGenerationProofTakesTheSolutionsItNeeds(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	p, err := s.CreateDraft(ctx, funcDraft(t, s), 1)
	if err != nil {
		t.Fatal(err)
	}
	var notReady *NotReadyError
	if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, solutionTexts("java21", "python3")); !errors.As(err, &notReady) ||
		!strings.Contains(strings.Join(notReady.Problems, " "), "node22") {
		t.Fatalf("a missing solution: %v", err)
	}
	if count(t, s, "SELECT count(*) FROM jobs") != 0 || count(t, s, "SELECT count(*) FROM input_generations") != 0 {
		t.Error("a refused request queued a job")
	}
	// The reference language's solution and unknown languages are ignored.
	texts := solutionTexts("java21", "python3", "node22", "cpp17", "cobol")
	if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, texts); err != nil {
		t.Fatal(err)
	}
	spec := payloadProof(t, s, judgeapi.KindGenerateInputs)
	if len(spec.Solutions) != 3 || spec.Solutions["cpp17"] != "" || spec.Solutions["cobol"] != "" {
		t.Errorf("spec %+v", spec)
	}
	solutionsOnDisk(t, s, spec, texts)
	// A second request while the first is open is refused, as a plain one is.
	if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, texts); !errors.Is(err, ErrChecking) {
		t.Errorf("second request: %v", err)
	}
}
