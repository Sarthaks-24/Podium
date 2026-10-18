package problems

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

func newBank(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "bank.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	j := &jobs.Service{DB: d, Kinds: map[string]jobs.Kind{judgeapi.KindValidate: ValidateKind{}}}
	return &Service{DB: d, Blobs: Blobs{Dir: filepath.Join(t.TempDir(), "tests")}, Jobs: j}
}

func importExample(t *testing.T, s *Service, slug string) *Problem {
	t.Helper()
	pk, err := ReadPack(zipDir(t, filepath.Join(examples, slug)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Import(context.Background(), pk, 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeJudge plays the worker: it claims the next job and reports the given
// verdict and time for every program on every test, except where override
// says otherwise ("idx/role" -> verdict, or "skip" to leave it out).
func fakeJudge(t *testing.T, s *Service, timeMS int, override map[string]string) judgeapi.ValidatePayload {
	t.Helper()
	ctx := context.Background()
	c, err := s.Jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindValidate}, Worker: "fake"})
	if err != nil || c == nil {
		t.Fatalf("claim: %v %v", c, err)
	}
	var pl judgeapi.ValidatePayload
	json.Unmarshal(c.Payload, &pl)
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}
	roles := []string{judgeapi.RoleReference}
	rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: judgeapi.RoleReference, Verdict: "OK"})
	if pl.Programs.Brute != nil {
		roles = append(roles, judgeapi.RoleBrute)
		rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: judgeapi.RoleBrute, Verdict: "OK"})
	}
	if pl.Programs.Validator != nil {
		roles = append(roles, judgeapi.RoleValidator)
		rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: judgeapi.RoleValidator, Verdict: "OK"})
	}
	if pl.Checker.Type == CheckerCustom {
		rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: judgeapi.RoleChecker, Verdict: "OK"})
	}
	for _, test := range pl.Tests {
		for _, role := range roles {
			v := "AC"
			if o, ok := override[strconv.Itoa(test.Idx)+"/"+role]; ok {
				v = o
			}
			if v == "skip" {
				continue
			}
			rep.Results = append(rep.Results, judgeapi.TestResult{Idx: test.Idx, Role: role, Verdict: v, TimeMS: timeMS, MemoryKB: 2048})
		}
	}
	if err := s.Jobs.Report(ctx, rep); err != nil {
		t.Fatalf("report: %v", err)
	}
	return pl
}

func TestImportEditAndValidate(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")

	list, err := s.List(ctx, false)
	if err != nil || len(list) != 1 || list[0].Points != 100 || list[0].TestCount != 6 || list[0].Validated {
		t.Fatalf("bank %+v %v", list, err)
	}
	if _, err := s.LatestValidation(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("validation before any: %v", err)
	}

	v, err := s.RequestValidation(ctx, p.ID, 0)
	if err != nil || v.State != ValidationQueued || v.QueuePosition != 1 || !v.Current {
		t.Fatalf("queued %+v %v", v, err)
	}
	pl := fakeJudge(t, s, 3, nil)
	if pl.ContentHash != p.ContentHash || len(pl.Tests) != 6 || !pl.Tests[0].Sample || pl.Tests[2].Sample {
		t.Errorf("payload %+v", pl)
	}
	v, _ = s.LatestValidation(ctx, p.ID)
	if v.State != ValidationPassed || v.Summary.ReferencePassed != 6 || v.Summary.BruteAgreed != 6 || v.Summary.ValidatorPassed != 6 {
		t.Fatalf("validation %+v %+v", v, v.Summary)
	}
	got, _ := s.Get(ctx, p.ID)
	if !got.Validated() {
		t.Fatal("problem not validated after a passing run")
	}

	// A statement edit keeps the problem validated; a limit change does not.
	e := editOf(got)
	e.Statement = "Shorter statement."
	got, err = s.Update(ctx, p.ID, e)
	if err != nil || !got.Validated() {
		t.Fatalf("after statement edit: validated=%v %v", got != nil && got.Validated(), err)
	}
	e.TimeLimitMS = 2000
	got, err = s.Update(ctx, p.ID, e)
	if err != nil || got.Validated() {
		t.Fatalf("after limit edit: validated=%v %v", got != nil && got.Validated(), err)
	}
	if v, _ := s.LatestValidation(ctx, p.ID); v.Current {
		t.Error("old validation still reported as current")
	}
}

func editOf(p *Problem) Edit {
	return Edit{Title: p.Title, Slug: p.Slug, Statement: p.Statement, TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB,
		Checker: p.Checker, Epsilon: p.Epsilon, CheckerSource: p.CheckerSource, Reference: p.Reference,
		Brute: p.Brute, Validator: p.Validator, Tags: p.Tags, Difficulty: p.Difficulty,
		Samples: p.Samples, Subtasks: append([]Subtask{}, p.Subtasks...)}
}

func TestValidationNamesTheFailingTest(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.RequestValidation(ctx, p.ID, 0)
	fakeJudge(t, s, 3, map[string]string{"5/reference": "WA", "5/brute": "WA", "6/validator": "WA", "4/brute": "TLE"})
	v, _ := s.LatestValidation(ctx, p.ID)
	if v.State != ValidationFailed {
		t.Fatalf("state %s", v.State)
	}
	all := strings.Join(v.Summary.Problems, "\n")
	for _, want := range []string{"Test 5: reference solution got wrong answer.", "Test 5: the brute force disagrees", "Test 6: the validator rejected"} {
		if !strings.Contains(all, want) {
			t.Errorf("summary misses %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "Test 4") {
		t.Errorf("a slow brute force counted as a failure:\n%s", all)
	}
	if got, _ := s.Get(ctx, p.ID); got.Validated() {
		t.Error("failed validation marked the problem validated")
	}
}

func TestSlowReferenceFails(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.RequestValidation(ctx, p.ID, 0)
	fakeJudge(t, s, 600, nil) // 600 ms of a 1000 ms limit
	v, _ := s.LatestValidation(ctx, p.ID)
	if v.State != ValidationFailed || !strings.Contains(strings.Join(v.Summary.Problems, " "), "more than half the 1000 ms limit") {
		t.Errorf("slow reference: %s %+v", v.State, v.Summary)
	}
	s.RequestValidation(ctx, p.ID, 0)
	fakeJudge(t, s, 400, nil)
	v, _ = s.LatestValidation(ctx, p.ID)
	if v.State != ValidationPassed || len(v.Summary.Warnings) == 0 {
		t.Errorf("400 ms should pass with a warning: %s %+v", v.State, v.Summary)
	}
}

func TestStaleResultDoesNotValidate(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.RequestValidation(ctx, p.ID, 0)
	got, _ := s.Get(ctx, p.ID)
	e := editOf(got)
	e.MemoryLimitMB = 128
	s.Update(ctx, p.ID, e) // edited while queued
	fakeJudge(t, s, 3, nil)
	if got, _ := s.Get(ctx, p.ID); got.Validated() {
		t.Error("a pass for the old version validated the new one")
	}
	v, _ := s.LatestValidation(ctx, p.ID)
	if v.State != ValidationPassed || v.Current {
		t.Errorf("validation %s current=%v", v.State, v.Current)
	}
}

func TestCustomCheckerAndJavaPacks(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	for _, slug := range []string{"average-speed", "pair-with-sum"} {
		p := importExample(t, s, slug)
		if _, err := s.RequestValidation(ctx, p.ID, 0); err != nil {
			t.Fatal(err)
		}
		pl := fakeJudge(t, s, 50, nil)
		if slug == "pair-with-sum" && (pl.Checker.Type != CheckerCustom || !judgeapi.ValidSHA(pl.Checker.SourceSHA)) {
			t.Errorf("checker not sent: %+v", pl.Checker)
		}
		if v, _ := s.LatestValidation(ctx, p.ID); v.State != ValidationPassed {
			t.Errorf("%s: %s %+v", slug, v.State, v.Summary)
		}
	}
}

func TestReportChecks(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "pair-with-sum") // no validator
	s.RequestValidation(ctx, p.ID, 0)
	c, _ := s.Jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindValidate}, Worker: "w"})
	var bad *jobs.ReportError
	for name, rep := range map[string]judgeapi.Report{
		"unknown test":        {Results: []judgeapi.TestResult{{Idx: 99, Role: "reference", Verdict: "AC"}}},
		"absent validator":    {Results: []judgeapi.TestResult{{Idx: 1, Role: "validator", Verdict: "AC"}}},
		"made-up verdict":     {Results: []judgeapi.TestResult{{Idx: 1, Role: "reference", Verdict: "YES"}}},
		"negative time":       {Results: []judgeapi.TestResult{{Idx: 1, Role: "reference", Verdict: "AC", TimeMS: -1}}},
		"long message":        {Results: []judgeapi.TestResult{{Idx: 1, Role: "reference", Verdict: "WA", Message: strings.Repeat("x", 300)}}},
		"compile unknown":     {Compile: []judgeapi.CompileResult{{Role: "validator", Verdict: "OK"}}},
		"duplicate in report": {Results: []judgeapi.TestResult{{Idx: 1, Role: "reference", Verdict: "AC"}, {Idx: 1, Role: "reference", Verdict: "AC"}}},
	} {
		rep.JobID, rep.Lease = c.JobID, c.Lease
		if err := s.Jobs.Report(ctx, rep); !errors.As(err, &bad) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Results: []judgeapi.TestResult{{Idx: 1, Role: "reference", Verdict: "AC"}}}
	if err := s.Jobs.Report(ctx, ok); err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs.Report(ctx, ok); !errors.As(err, &bad) {
		t.Errorf("repeated result accepted: %v", err)
	}
}

func TestEditRulesAndStructure(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Two sum", "two-sum", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "Again", "two-sum", 0); err == nil || !strings.Contains(err.Error(), "already uses") {
		t.Errorf("duplicate slug: %v", err)
	}
	var notReady *NotReadyError
	if _, err := s.RequestValidation(ctx, p.ID, 0); !errors.As(err, &notReady) {
		t.Errorf("validated a problem with no tests: %v", err)
	}
	e := editOf(p)
	e.Slug, e.TimeLimitMS, e.Checker = "Bad Slug", 50, "fuzzy"
	e.Subtasks = []Subtask{{Points: 0, Tests: "5-3"}}
	_, err = s.Update(ctx, p.ID, e)
	var fields FieldErrors
	if !errors.As(err, &fields) {
		t.Fatalf("got %v", err)
	}
	for _, k := range []string{"slug", "time_limit_ms", "checker", "subtasks.0.points", "subtasks.0.tests"} {
		if fields[k] == "" {
			t.Errorf("no error for %s: %v", k, fields)
		}
	}

	// Tests replaced: ranges kept as written, mismatches listed.
	e = editOf(p)
	e.Samples, e.Subtasks = []int{1}, []Subtask{{Points: 100, Tests: "2-4"}}
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReplaceTests(ctx, p.ID, zipEntries(t,
		entry{name: "01.in", body: "1"}, entry{name: "01.out", body: "1"},
		entry{name: "tests/2.in", body: "2"}, entry{name: "tests/2.out", body: "2"},
		entry{name: "5.in", body: "5"}, entry{name: "5.out", body: "5"}))
	if err != nil {
		t.Fatal(err)
	}
	probs := strings.Join(got.Problems(), "\n")
	for _, want := range []string{"lists test 3, which does not exist", "Test 5 is in no subtask"} {
		if !strings.Contains(probs, want) {
			t.Errorf("problems miss %q:\n%s", want, probs)
		}
	}
	if _, err := s.ReplaceTests(ctx, p.ID, zipEntries(t, entry{name: "1.in", body: "1"})); err == nil {
		t.Error("input without output accepted")
	}
	if err := s.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, false); len(list) != 0 {
		t.Error("archived problem listed")
	}
	if list, _ := s.List(ctx, true); len(list) != 1 {
		t.Error("archived problem missing from the archive list")
	}
}

func TestMissingResultsDoNotPass(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.RequestValidation(ctx, p.ID, 0)
	fakeJudge(t, s, 3, map[string]string{"3/validator": "skip", "4/brute": "skip"})
	v, _ := s.LatestValidation(ctx, p.ID)
	all := strings.Join(v.Summary.Problems, "\n")
	if v.State != ValidationFailed || !strings.Contains(all, "Test 3: the judge did not report the validator.") ||
		!strings.Contains(all, "Test 4: the judge did not report the brute force.") {
		t.Errorf("missing results passed: %s\n%s", v.State, all)
	}
}

func TestFailingRerunWithdrawsValidation(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.RequestValidation(ctx, p.ID, 0)
	fakeJudge(t, s, 3, nil)
	if got, _ := s.Get(ctx, p.ID); !got.Validated() {
		t.Fatal("first run did not validate")
	}
	s.RequestValidation(ctx, p.ID, 0)
	fakeJudge(t, s, 3, map[string]string{"2/reference": "TLE"})
	if got, _ := s.Get(ctx, p.ID); got.Validated() {
		t.Error("a failing re-run of the same content left it validated")
	}
}

func TestOneOpenValidationPerProblem(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	first, _ := s.RequestValidation(ctx, p.ID, 0)
	again, _ := s.RequestValidation(ctx, p.ID, 0)
	if again.ID != first.ID {
		t.Errorf("same content queued twice: %d and %d", first.ID, again.ID)
	}
	got, _ := s.Get(ctx, p.ID)
	e := editOf(got)
	e.TimeLimitMS = 1500
	s.Update(ctx, p.ID, e)
	newer, _ := s.RequestValidation(ctx, p.ID, 0)
	if newer.ID == first.ID || newer.QueuePosition != 1 {
		t.Errorf("new content: %+v", newer)
	}
	old, _ := s.Jobs.Get(ctx, firstJob(t, s, first.ID))
	if old.State != jobs.Failed || !strings.Contains(old.Error, "newer version") {
		t.Errorf("older queued run not superseded: %+v", old)
	}
}

func firstJob(t *testing.T, s *Service, validationID int64) int64 {
	t.Helper()
	var id int64
	if err := s.DB.QueryRow("SELECT job_id FROM validations WHERE id = ?", validationID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
