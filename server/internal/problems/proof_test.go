package problems

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// secretMessage stands in for a checker message that quotes expected output.
const secretMessage = "expected 7133742 but got 1"

// newProofBank is a bank whose queue knows validation as wired in
// production: the last job of a proof round can queue the validation.
func newProofBank(t *testing.T, wired bool) *Service {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "proof.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES
		(1, 'owner@college.edu', 'owner', 0), (2, 'admin@college.edu', 'admin', 0)`); err != nil {
		t.Fatal(err)
	}
	blobs := Blobs{Dir: filepath.Join(t.TempDir(), "tests")}
	j := &jobs.Service{DB: d}
	if wired {
		j.Kinds = map[string]jobs.Kind{judgeapi.KindValidate: ValidateKind{Store: blobs, Jobs: j},
			judgeapi.KindGenerate: GenerateKind{Store: blobs, Jobs: j}}
	} else {
		j.Kinds = map[string]jobs.Kind{judgeapi.KindValidate: ValidateKind{}, judgeapi.KindGenerate: GenerateKind{}}
	}
	return &Service{DB: d, Blobs: blobs, Jobs: j}
}

// proofProblem is the example function problem (four languages, C++
// reference) with a C++ brute force, as the wizard writes one.
func proofProblem(t *testing.T, s *Service) *Problem {
	t.Helper()
	p := importExample(t, s, "array-sum")
	e := editOf(p)
	e.Brute = &Source{"cpp17", "long long arraySum(vector<int> a) { return 0; }\n"}
	p, err := s.Update(context.Background(), p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Offered()) != 4 || p.Reference.Language != "cpp17" || len(p.Problems()) > 0 {
		t.Fatalf("fixture: offered %v reference %s problems %v", p.Offered(), p.Reference.Language, p.Problems())
	}
	return p
}

// wizardSpec holds a solution for every offered language but the reference.
func wizardSpec(t *testing.T, s *Service, p *Problem) ProofSpec {
	t.Helper()
	spec := ProofSpec{Source: ProofSourceWizard, Solutions: map[string]string{}}
	for _, lang := range p.Offered() {
		if lang == p.Reference.Language {
			continue
		}
		sha, err := s.Blobs.Write([]byte("SOLUTION " + lang + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		spec.Solutions[lang] = sha
	}
	return spec
}

func queueRound(t *testing.T, s *Service, p *Problem, spec ProofSpec) int {
	t.Helper()
	tx, err := s.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	n, err := s.queue().enqueueProofs(context.Background(), tx, p, 1, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return n
}

// proofReport is the report a judge would send for a proof job. how is
// "", "ce" (reference does not compile), "wa", "tle", "ie" (first test), "norep" (compiled,
// no results) or "fatal".
func proofReport(c *judgeapi.Claim, pl proofValidate, how string) judgeapi.Report {
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}
	switch how {
	case "fatal":
		rep.Final, rep.Fatal = false, "isolate failed"
		return rep
	case "ce":
		rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleReference, Verdict: "CE",
			Output: "Main.java:7: error: class Solution is public\n" + strings.Repeat("more output\n", 80)}}
		return rep
	}
	rep.Compile = []judgeapi.CompileResult{{Role: judgeapi.RoleReference, Verdict: "OK"}}
	if pl.Proof.Part == ProofPartStarter || how == "norep" {
		return rep
	}
	for i, test := range pl.Tests {
		v, msg := "AC", ""
		switch {
		case how == "wa":
			v, msg = "WA", secretMessage
		case how == "tle":
			v = "TLE"
		case how == "ie" && i == 0:
			v = "IE"
		}
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: test.Idx, Role: judgeapi.RoleReference, Verdict: v, TimeMS: 5, MemoryKB: 1024, Message: msg})
	}
	return rep
}

// claimProof leases the next validate job and decodes its payload; nil when
// none is waiting.
func claimProof(t *testing.T, s *Service) (*judgeapi.Claim, proofValidate) {
	t.Helper()
	c, err := s.Jobs.Claim(context.Background(), judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindValidate}, Worker: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	var pl proofValidate
	if c != nil {
		if pl, err = decodeProof(c.Payload); err != nil {
			t.Fatal(err)
		}
	}
	return c, pl
}

// runProofs plays the judge over the open proof jobs. behave maps
// "language/part" to a behaviour of proofReport, or "hang" to take the lease
// and never report. It returns the claims left hanging.
func runProofs(t *testing.T, s *Service, behave map[string]string) (hung []*judgeapi.Claim) {
	t.Helper()
	for count(t, s, "SELECT count(*) FROM jobs WHERE state = 'queued' AND priority = ?", ProofPriority) > 0 {
		c, pl := claimProof(t, s)
		if c == nil || pl.Proof == nil {
			t.Fatalf("claimed %v, want a proof job", c)
		}
		how := behave[pl.Proof.Language+"/"+pl.Proof.Part]
		if how == "hang" {
			hung = append(hung, c)
			continue
		}
		if err := s.Jobs.Report(context.Background(), proofReport(c, pl, how)); err != nil {
			t.Fatalf("report %s/%s: %v", pl.Proof.Language, pl.Proof.Part, err)
		}
	}
	return hung
}

// summaries reads every proof job's stored summary by "language/part".
func summaries(t *testing.T, s *Service) map[string]proofSummary {
	t.Helper()
	rows, err := s.DB.Query(`SELECT payload, summary FROM jobs WHERE kind = 'validate' AND priority = ?`, ProofPriority)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]proofSummary{}
	for rows.Next() {
		var payload, summary string
		if err := rows.Scan(&payload, &summary); err != nil {
			t.Fatal(err)
		}
		pl, _ := decodeProof(json.RawMessage(payload))
		var sm proofSummary
		json.Unmarshal([]byte(summary), &sm)
		out[pl.Proof.Language+"/"+pl.Proof.Part] = sm
	}
	return out
}

// applied returns the one Applied record of a round, failing when there is
// not exactly one.
func applied(t *testing.T, s *Service) *Applied {
	t.Helper()
	var found []*Applied
	for _, sm := range summaries(t, s) {
		if sm.Applied != nil {
			found = append(found, sm.Applied)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d jobs applied the round, want exactly 1", len(found))
	}
	return found[0]
}

func validateJobs(t *testing.T, s *Service) int {
	return count(t, s, "SELECT count(*) FROM jobs WHERE kind = 'validate' AND priority = ?", ValidatePriority)
}

func validatedHash(t *testing.T, s *Service, id int64) (h sql.NullString) {
	t.Helper()
	if err := s.DB.QueryRow("SELECT validated_hash FROM problems WHERE id = ?", id).Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func offered(t *testing.T, s *Service, id int64) string {
	t.Helper()
	p, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(p.Offered(), ",")
}

func TestClassify(t *testing.T) {
	mk := func(part string, tests int) proofValidate {
		pl := proofValidate{Proof: &Proof{Language: "java21", Part: part}}
		for i := 1; i <= tests; i++ {
			pl.Tests = append(pl.Tests, judgeapi.Test{Idx: i})
		}
		return pl
	}
	ok := judgeapi.CompileResult{Role: "reference", Verdict: "OK"}
	ref := func(verdicts ...string) []judgeapi.TestResult {
		var out []judgeapi.TestResult
		for i, v := range verdicts {
			out = append(out, judgeapi.TestResult{Idx: i + 1, Role: "reference", Verdict: v, Message: secretMessage})
		}
		return out
	}
	long := strings.Repeat("x", 900) + "\xe2\x82"
	cases := []struct {
		name            string
		pl              proofValidate
		job             jobs.Job
		outcome, reason string
		failed          int
	}{
		{"pass", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("AC", "AC", "AC")}}, ProofPassed, "", 0},
		{"wrong answer", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("AC", "WA", "WA")}}, ProofFailed, "wrong_answer", 2},
		{"runtime error beats a single wrong answer", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("RE", "RE", "WA")}}, ProofFailed, "runtime_error", 3},
		{"time limit", mk("solution", 2), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("AC", "TLE")}}, ProofFailed, "time_limit", 1},
		{"memory limit", mk("solution", 1), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("MLE")}}, ProofFailed, "memory_limit", 1},
		{"output limit", mk("solution", 1), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("OLE")}}, ProofFailed, "output_limit", 1},
		{"a verdict with no name of its own", mk("solution", 1), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("CE")}}, ProofFailed, "ce", 1},
		{"skipped", mk("solution", 1), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("SK")}}, ProofFailed, "skipped", 1},
		{"compile error", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{{Role: "reference", Verdict: "CE", Output: "boom"}}}}, ProofFailed, "compile_error", 0},
		{"starter does not compile", mk("starter", 0), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{{Role: "reference", Verdict: "CE", Output: "boom"}}}}, ProofFailed, "compile_error", 0},
		{"starter compiles", mk("starter", 0), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}}}, ProofPassed, "", 0},
		{"starter with no compile report", mk("starter", 0), jobs.Job{}, ProofUnchecked, "no_report", 0},
		{"internal error", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("AC", "IE", "AC")}}, ProofUnchecked, "internal_error", 0},
		{"fewer results than tests", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("AC", "WA")}}, ProofUnchecked, "no_report", 0},
		{"no results at all", mk("solution", 3), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}}}, ProofUnchecked, "no_report", 0},
		{"checker does not compile", mk("solution", 1), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok, {Role: "checker", Verdict: "CE"}}}}, ProofUnchecked, "internal_error", 0},
		{"failed job", mk("solution", 3), jobs.Job{State: jobs.Failed, Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref("AC", "AC", "AC")}}, ProofUnchecked, "judge_error", 0},
		{"results of other roles are ignored", mk("solution", 1), jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok},
			Tests: append(ref("AC"), judgeapi.TestResult{Idx: 1, Role: "brute", Verdict: "WA"})}}, ProofPassed, "", 0},
	}
	for _, tc := range cases {
		got := classify(tc.pl, &tc.job)
		if got.Outcome != tc.outcome || got.Reason != tc.reason || got.Failed != tc.failed {
			t.Errorf("%s: got %s %q failed %d, want %s %q failed %d", tc.name, got.Outcome, got.Reason, got.Failed, tc.outcome, tc.reason, tc.failed)
		}
		if b, _ := json.Marshal(got); strings.Contains(string(b), secretMessage) {
			t.Errorf("%s: a checker message is in the result: %s", tc.name, b)
		}
	}
	// What a result keeps: counts, the first 60 test verdicts, 10 compiler lines
	// of at most 600 valid UTF-8 bytes.
	var many []string
	for i := 0; i < 100; i++ {
		many = append(many, "WA")
	}
	got := classify(mk("solution", 100), &jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{ok}, Tests: ref(many...)}})
	if got.Failed != 100 || got.Verdicts["WA"] != 100 || len(got.Tests) != 60 || got.Tests[0] != (ProofTest{1, "WA"}) || got.Total != 100 {
		t.Errorf("counts %d %v kept %d total %d", got.Failed, got.Verdicts, len(got.Tests), got.Total)
	}
	got = classify(mk("starter", 0), &jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{{Role: "reference", Verdict: "CE", Output: strings.Repeat("line\n", 30)}}}})
	if n := strings.Count(got.Compiler, "\n") + 1; n != 10 {
		t.Errorf("%d compiler lines kept", n)
	}
	got = classify(mk("starter", 0), &jobs.Job{Result: jobs.Result{Compile: []judgeapi.CompileResult{{Role: "reference", Verdict: "CE", Output: long}}}})
	if len(got.Compiler) > 600 || !strings.HasSuffix(got.Compiler, "x") {
		t.Errorf("compiler text %d bytes, ends %q", len(got.Compiler), got.Compiler[max(0, len(got.Compiler)-3):])
	}
}

// A round queues a solution job for each language but the reference's and a
// starter job for each compiled language; the judge's own fields are the
// same as a validation's, so protocol 3 workers run it unchanged.
func TestEnqueueProofsPayloads(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	spec := wizardSpec(t, s, p)
	if n := queueRound(t, s, p, spec); n != 6 {
		t.Fatalf("%d jobs queued, want 6 (C++ starter; Java, Python solution and starter; Node solution)", n)
	}
	seen := map[string]proofValidate{}
	for {
		c, pl := claimProof(t, s)
		if c == nil {
			break
		}
		seen[pl.Proof.Language+"/"+pl.Proof.Part] = pl
		var strict judgeapi.ValidatePayload
		dec := json.NewDecoder(strings.NewReader(string(c.Payload)))
		if err := dec.Decode(&strict); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"cpp17/starter", "java21/solution", "java21/starter", "node22/solution", "python3/solution", "python3/starter"}
	got := []string{}
	for k := range seen {
		got = append(got, k)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("jobs %v, want %v", got, want)
	}
	round := ""
	for k, pl := range seen {
		if round == "" {
			round = pl.Proof.Round
		}
		pr := pl.Proof
		if len(pr.Round) != 32 || pr.Round != round || pr.Source != ProofSourceWizard || pr.ContentHash != p.ContentHash ||
			pr.StarterSHA != SHA([]byte(p.Starters[pr.Language])) || pr.By != 1 {
			t.Errorf("%s: proof %+v", k, pr)
		}
		if pl.ProblemID != p.ID || pl.ContentHash != p.ContentHash || pl.TimeLimitMS != p.TimeLimitMS || pl.Programs.Brute != nil || pl.Programs.Validator != nil {
			t.Errorf("%s: payload %+v", k, pl.ValidatePayload)
		}
		src, err := s.Blobs.Read(pl.Programs.Reference.SourceSHA, MaxSource)
		if err != nil || pl.Programs.Reference.Language != pr.Language {
			t.Fatalf("%s: program %+v %v", k, pl.Programs.Reference, err)
		}
		middle := "SOLUTION " + pr.Language + "\n"
		if pr.Part == ProofPartStarter {
			middle = p.Starters[pr.Language]
			if len(pl.Tests) != 0 {
				t.Errorf("%s: a starter job has %d tests", k, len(pl.Tests))
			}
		} else if len(pl.Tests) != len(p.Tests) {
			t.Errorf("%s: %d tests, want %d", k, len(pl.Tests), len(p.Tests))
		}
		if want := Compose(p.LockedTop[pr.Language], middle, p.LockedBottom[pr.Language]).Source; string(src) != want {
			t.Errorf("%s: composed source\n%s\nwant\n%s", k, src, want)
		}
	}
	if n := count(t, s, "SELECT count(*) FROM validations"); n != 0 {
		t.Errorf("%d validations rows written by queueing a proof", n)
	}
}

func TestOpenProofFollowsOpenJobs(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	open := func() bool {
		b, err := OpenProof(ctx, s.DB, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if open() {
		t.Fatal("a problem with no job is being checked")
	}
	// A generate job that carries a proof spec marks the draft before any proof job exists.
	tx, _ := s.DB.BeginTx(ctx, nil)
	if _, err := s.Jobs.Enqueue(ctx, tx, judgeapi.KindGenerate, GeneratePriority, map[string]any{"problem_id": p.ID}, 1); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if open() {
		t.Error("a plain generate job marks the problem as being checked")
	}
	tx, _ = s.DB.BeginTx(ctx, nil)
	if _, err := s.Jobs.Enqueue(ctx, tx, judgeapi.KindGenerate, GeneratePriority, map[string]any{"problem_id": p.ID, "proof": map[string]any{"source": "wizard"}}, 1); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if !open() {
		t.Error("a generate job with a proof spec does not mark the problem")
	}
	if b, _ := OpenProof(ctx, s.DB, p.ID+1); b {
		t.Error("another problem is marked")
	}
	s.DB.Exec("DELETE FROM jobs")
	queueRound(t, s, p, wizardSpec(t, s, p))
	if !open() {
		t.Fatal("queued proof jobs do not mark the problem")
	}
	runProofs(t, s, nil)
	if open() {
		t.Error("the problem is still being checked after every job closed")
	}
}

// A passing proof marks nothing validated and writes no validations row for
// itself; one all-pass round saves nothing and queues the validation.
func TestAllLanguagesPassQueuesValidateAndSavesNothing(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	revs := revCount(t, s, p.ID)
	queueRound(t, s, p, wizardSpec(t, s, p))
	runProofs(t, s, nil)
	a := applied(t, s)
	if len(a.Removed) > 0 || a.Kept != "" || a.Discarded != "" || a.Error != "" || !a.Validating {
		t.Errorf("applied %+v", a)
	}
	if got := offered(t, s, p.ID); got != "cpp17,java21,node22,python3" {
		t.Errorf("offered %s", got)
	}
	if n := revCount(t, s, p.ID); n != revs {
		t.Errorf("%d revisions made by an all-pass round", n-revs)
	}
	if h := validatedHash(t, s, p.ID); h.Valid {
		t.Errorf("a proof marked the problem validated: %s", h.String)
	}
	if n := validateJobs(t, s); n != 1 {
		t.Errorf("%d validation jobs queued, want 1", n)
	}
	if n := count(t, s, `SELECT count(*) FROM validations v JOIN jobs j ON j.id = v.job_id WHERE j.priority = ?`, ProofPriority); n != 0 {
		t.Errorf("%d validations rows point at a proof job", n)
	}
	if v, err := s.LatestValidation(ctx, p.ID); err != nil || v.State != ValidationQueued {
		t.Errorf("latest validation %+v %v", v, err)
	}
	for k, sm := range summaries(t, s) {
		if sm.Result.Outcome != ProofPassed {
			t.Errorf("%s: %+v", k, sm.Result)
		}
	}
}

// A language whose solution fails is removed in one ordinary save, and the
// validation is queued; a language that could not be checked goes the same
// way and is reported as such.
func TestFailedAndUncheckedLanguagesAreRemovedInOneSave(t *testing.T) {
	for name, tc := range map[string]struct {
		behave  map[string]string
		outcome string
		reason  string
	}{
		"wrong answer":         {map[string]string{"java21/solution": "wa"}, ProofFailed, "wrong_answer"},
		"time limit":           {map[string]string{"java21/solution": "tle"}, ProofFailed, "time_limit"},
		"compile error":        {map[string]string{"java21/solution": "ce"}, ProofFailed, "compile_error"},
		"internal error":       {map[string]string{"java21/solution": "ie"}, ProofUnchecked, "internal_error"},
		"no report":            {map[string]string{"java21/solution": "norep"}, ProofUnchecked, "no_report"},
		"judge reported fatal": {map[string]string{"java21/solution": "fatal"}, ProofUnchecked, "judge_error"},
		// A bottom that prints more than the answer, or never calls the
		// student's function, shows as wrong answer on the tests.
		"bottom prints extra output":      {map[string]string{"java21/solution": "wa"}, ProofFailed, "wrong_answer"},
		"bottom ignores the function":     {map[string]string{"python3/solution": "wa"}, ProofFailed, "wrong_answer"},
		"starter does not compile (Java)": {map[string]string{"java21/starter": "ce"}, ProofFailed, "compile_error"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProofBank(t, true)
			ctx := context.Background()
			p := proofProblem(t, s)
			revs := revCount(t, s, p.ID)
			queueRound(t, s, p, wizardSpec(t, s, p))
			runProofs(t, s, tc.behave)
			var lang string
			for k := range tc.behave {
				lang = strings.Split(k, "/")[0]
			}
			a := applied(t, s)
			if !slices.Equal(a.Removed, []string{lang}) || !a.Validating || a.Error != "" {
				t.Fatalf("applied %+v", a)
			}
			want := "cpp17,java21,node22,python3"
			want = strings.Replace(want, lang+",", "", 1)
			want = strings.TrimSuffix(strings.Replace(want, ","+lang, "", 1), ",")
			if got := offered(t, s, p.ID); got != want {
				t.Errorf("offered %s, want %s", got, want)
			}
			got, _ := s.Get(ctx, p.ID)
			if got.LockedTop[lang] != "" || got.LockedBottom[lang] != "" || got.Starters[lang] != "" {
				t.Errorf("parts of %s kept", lang)
			}
			if n := revCount(t, s, p.ID); n != revs+1 {
				t.Errorf("%d revisions made, want 1", n-revs)
			}
			rev, _ := s.CurrentRevision(ctx, p.ID)
			if !strings.HasPrefix(rev.Reason, "Languages that did not pass their check were removed: ") {
				t.Errorf("revision reason %q", rev.Reason)
			}
			if n := validateJobs(t, s); n != 1 {
				t.Errorf("%d validation jobs", n)
			}
			if h := validatedHash(t, s, p.ID); h.Valid {
				t.Error("problem marked validated")
			}
			var res *ProofResult
			for k, sm := range summaries(t, s) {
				if strings.HasPrefix(k, lang+"/") && sm.Result.Outcome != ProofPassed {
					r := sm.Result
					res = &r
				}
			}
			if res == nil || res.Outcome != tc.outcome || res.Reason != tc.reason {
				t.Fatalf("result %+v, want %s %s", res, tc.outcome, tc.reason)
			}
			if tc.reason == "compile_error" && !strings.Contains(res.Compiler, "class Solution is public") {
				t.Errorf("compiler lines %q", res.Compiler)
			}
		})
	}
}

// The reference language's own failure keeps every language and queues no
// validation: nothing is deleted.
func TestReferenceLanguageFailureKeepsEverything(t *testing.T) {
	for name, tc := range map[string]struct {
		how  string
		kept string
	}{"does not compile": {"ce", "reference_failed"}, "judge fatal": {"fatal", "reference_unchecked"}} {
		t.Run(name, func(t *testing.T) {
			s := newProofBank(t, true)
			p := proofProblem(t, s)
			revs := revCount(t, s, p.ID)
			queueRound(t, s, p, wizardSpec(t, s, p))
			// Java fails too: with the reference gone it is not removed either.
			runProofs(t, s, map[string]string{"cpp17/starter": tc.how, "java21/solution": "wa"})
			if a := applied(t, s); a.Kept != tc.kept || len(a.Removed) > 0 || a.Validating {
				t.Errorf("applied %+v", a)
			}
			if got := offered(t, s, p.ID); got != "cpp17,java21,node22,python3" {
				t.Errorf("offered %s", got)
			}
			if n := revCount(t, s, p.ID); n != revs {
				t.Errorf("%d revisions made", n-revs)
			}
			if n := validateJobs(t, s); n != 0 {
				t.Errorf("%d validation jobs queued", n)
			}
		})
	}
}

// A job the judge never finishes is failed by Sweep after three leases and
// still counts, so the round ends and applies.
func TestAJobThatFailsThreeTimesDoesNotHangTheRound(t *testing.T) {
	s := newProofBank(t, true)
	now := time.UnixMilli(1_800_000_000_000)
	s.Jobs.Now = func() time.Time { return now }
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	hung := runProofs(t, s, map[string]string{"python3/solution": "hang"})
	if len(hung) != 1 {
		t.Fatalf("%d hanging claims", len(hung))
	}
	for attempt := 1; attempt <= jobs.MaxAttempts; attempt++ {
		if n := count(t, s, "SELECT count(*) FROM jobs WHERE state IN ('queued', 'leased') AND priority = ?", ProofPriority); n != 1 {
			t.Fatalf("attempt %d: %d open jobs", attempt, n)
		}
		if a := countApplied(t, s); a != 0 {
			t.Fatalf("applied before the last job closed")
		}
		now = now.Add(jobs.LeaseTTL + time.Second)
		requeued, failed, err := s.Jobs.Sweep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if attempt < jobs.MaxAttempts {
			if requeued != 1 || failed != 0 {
				t.Fatalf("attempt %d: requeued %d failed %d", attempt, requeued, failed)
			}
			if c, _ := claimProof(t, s); c == nil {
				t.Fatalf("attempt %d: nothing to claim", attempt)
			}
		} else if failed != 1 {
			t.Fatalf("failed %d", failed)
		}
	}
	a := applied(t, s)
	if !slices.Equal(a.Removed, []string{"python3"}) || !a.Validating {
		t.Errorf("applied %+v", a)
	}
	sm := summaries(t, s)["python3/solution"]
	if sm.Result.Outcome != ProofUnchecked || sm.Result.Reason != "judge_error" {
		t.Errorf("result %+v", sm.Result)
	}
}

func countApplied(t *testing.T, s *Service) int {
	n := 0
	for _, sm := range summaries(t, s) {
		if sm.Applied != nil {
			n++
		}
	}
	return n
}

// An edit during a proof wins: the result is dropped, nothing is removed or
// queued, and the round still closes.
func TestAnEditDuringAProofIsNotOverwritten(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	e := editOf(p)
	e.TimeLimitMS = p.TimeLimitMS + 500
	edited, err := s.Update(ctx, p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	revs := revCount(t, s, p.ID)
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	if a := applied(t, s); a.Discarded != "changed" || len(a.Removed) > 0 || a.Validating {
		t.Errorf("applied %+v", a)
	}
	got, _ := s.Get(ctx, p.ID)
	if got.TimeLimitMS != edited.TimeLimitMS || offered(t, s, p.ID) != "cpp17,java21,node22,python3" {
		t.Errorf("the edit or the languages were overwritten: %d %s", got.TimeLimitMS, offered(t, s, p.ID))
	}
	if n := revCount(t, s, p.ID); n != revs || validateJobs(t, s) != 0 {
		t.Errorf("revisions +%d validations %d", n-revs, validateJobs(t, s))
	}
}

// Starting code is outside the content hash, so the round checks it on its
// own: a language whose starter changed is skipped, the rest are applied.
func TestAStarterEditDuringAProofSkipsThatLanguage(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	e := editOf(p)
	starters := map[string]string{}
	for k, v := range p.Starters {
		starters[k] = v
	}
	starters["java21"] += "// edited\n"
	e.Starters = &starters
	edited, err := s.Update(ctx, p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if edited.ContentHash != p.ContentHash {
		t.Fatal("a starter edit changed the content hash: this test needs one that does not")
	}
	runProofs(t, s, map[string]string{"java21/solution": "wa", "python3/solution": "wa"})
	a := applied(t, s)
	if !slices.Equal(a.Stale, []string{"java21"}) || !slices.Equal(a.Removed, []string{"python3"}) || !a.Validating {
		t.Errorf("applied %+v", a)
	}
	if got := offered(t, s, p.ID); got != "cpp17,java21,node22" {
		t.Errorf("offered %s", got)
	}
	if got, _ := s.Get(ctx, p.ID); !strings.HasSuffix(got.Starters["java21"], "// edited\n") {
		t.Error("the starter edit was lost")
	}
}

// Two proofs finishing together: writes take turns, the second sees the
// first as closed, so the round is applied exactly once.
func TestTwoProofsFinishingTogetherApplyOnce(t *testing.T) {
	for run := 0; run < 50; run++ {
		s := newProofBank(t, true)
		p := proofProblem(t, s)
		queueRound(t, s, p, wizardSpec(t, s, p))
		// Lease everything, report all but two, then report the last two at once.
		var claims []*judgeapi.Claim
		var pls []proofValidate
		for {
			c, pl := claimProof(t, s)
			if c == nil {
				break
			}
			claims, pls = append(claims, c), append(pls, pl)
		}
		last := len(claims) - 2
		for i := 0; i < last; i++ {
			if err := s.Jobs.Report(context.Background(), proofReport(claims[i], pls[i], "")); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = s.Jobs.Report(context.Background(), proofReport(claims[last+i], pls[last+i], "wa"))
			}()
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("run %d: report: %v", run, err)
			}
		}
		if n := countApplied(t, s); n != 1 {
			t.Fatalf("run %d: %d jobs applied the round", run, n)
		}
		if n := validateJobs(t, s); n != 1 {
			t.Fatalf("run %d: %d validation jobs", run, n)
		}
	}
}

// A sibling that was cancelled never ran Finish and has no summary: the
// round still closes and counts it as one the judge could not run.
func TestACancelledSiblingCountsAsUnchecked(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	var id int64
	if err := s.DB.QueryRow(`SELECT id FROM jobs WHERE json_extract(payload, '$.proof.language') = 'node22'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tx, _ := s.DB.BeginTx(ctx, nil)
	if err := s.Jobs.Cancel(ctx, tx, id, "stopped"); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	runProofs(t, s, nil)
	if a := applied(t, s); !slices.Equal(a.Removed, []string{"node22"}) || !a.Validating {
		t.Errorf("applied %+v", a)
	}
}

// A round cannot be queued without a solution for each language it proves.
func TestEnqueueProofsNeedsEverySolution(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	spec := wizardSpec(t, s, p)
	delete(spec.Solutions, "java21")
	tx, _ := s.DB.BeginTx(context.Background(), nil)
	defer tx.Rollback()
	if _, err := s.queue().enqueueProofs(context.Background(), tx, p, 1, spec); err == nil || !strings.Contains(err.Error(), "no solution for java21") {
		t.Errorf("error %v", err)
	}
}

// A second report for a finished job, or one for a lease that is gone, is
// refused and applies nothing again.
func TestARetryOfAFinishedProofJobIsRefused(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	c, pl := claimProof(t, s)
	rep := proofReport(c, pl, "")
	if err := s.Jobs.Report(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	if err := s.Jobs.Report(context.Background(), rep); err != jobs.ErrLease {
		t.Errorf("second report: %v, want %v", err, jobs.ErrLease)
	}
	runProofs(t, s, nil)
	if n := countApplied(t, s); n != 1 {
		t.Errorf("%d applications", n)
	}
}

// A problem row that is gone (not a tombstone) drops the result and still
// closes the job.
func TestAProblemRowThatIsGoneDiscardsTheRound(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, q := range []string{"PRAGMA foreign_keys = OFF", "DELETE FROM problems WHERE id = " + itoa(p.ID)} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	runProofs(t, s, nil)
	if a := applied(t, s); a.Discarded != "gone" || a.Validating {
		t.Errorf("applied %+v", a)
	}
}

// A proof job of a deleted problem closes failed with a "deleted" result and
// applies nothing; deleting the problem cancels its open proof jobs.
func TestDeletedProblemDuringAProof(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	revs := revCount(t, s, p.ID)
	if _, err := s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", p.ID); err != nil {
		t.Fatal(err)
	}
	runProofs(t, s, nil)
	if n := countApplied(t, s); n != 0 {
		t.Errorf("%d jobs applied a round for a deleted problem", n)
	}
	for k, sm := range summaries(t, s) {
		if sm.Result.Reason != "deleted" {
			t.Errorf("%s: %+v", k, sm.Result)
		}
	}
	if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ? AND state = 'failed'", ProofPriority); n != 6 {
		t.Errorf("%d jobs failed", n)
	}
	if revCount(t, s, p.ID) != revs || validateJobs(t, s) != 0 {
		t.Error("something was saved or queued")
	}

	s = newProofBank(t, true)
	p = proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	if err := s.Delete(ctx, p.ID, adminActor); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ? AND state IN ('queued', 'leased')", ProofPriority); n != 0 {
		t.Errorf("%d proof jobs still open after the delete", n)
	}
	if c, _ := claimProof(t, s); c != nil {
		t.Error("a cancelled proof job can be claimed")
	}
}

// An expected refusal of the save is recorded, not returned: a returned
// error would leave the job leased.
func TestARefusedSaveStillClosesTheJob(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	if _, err := s.DB.Exec("UPDATE problems SET slug = 'Not a slug' WHERE id = ?", p.ID); err != nil { // outside the content hash
		t.Fatal(err)
	}
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	a := applied(t, s)
	if !strings.Contains(a.Error, "lowercase letters") || a.Validating {
		t.Errorf("applied %+v", a)
	}
	if n := count(t, s, "SELECT count(*) FROM jobs WHERE priority = ? AND state = 'done'", ProofPriority); n != 6 {
		t.Errorf("%d jobs closed done, want 6", n)
	}
	if got := offered(t, s, p.ID); got != "cpp17,java21,node22,python3" {
		t.Errorf("offered %s", got)
	}
}

// A student who began a language while the round ran keeps it: the save
// refuses its removal, which is recorded and the job still closes.
func TestALanguageAStudentUsedIsNotRemoved(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	if _, err := s.DB.Exec(`INSERT INTO drafts (user_id, problem_id, contest_id, language, source, updated_at) VALUES (1, ?, 0, 'java21', 'x', 0)`, p.ID); err != nil {
		t.Fatal(err)
	}
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	a := applied(t, s)
	if !strings.Contains(a.Error, "already has submissions in Java 21") || a.Validating || len(a.Removed) != 0 {
		t.Errorf("applied %+v", a)
	}
	if got := offered(t, s, p.ID); got != "cpp17,java21,node22,python3" {
		t.Errorf("offered %s", got)
	}
}

// The zero ValidateKind (five test banks build it) records a round's result
// and queues nothing.
func TestZeroValidateKindRecordsAndQueuesNothing(t *testing.T) {
	s := newProofBank(t, false)
	p := proofProblem(t, s)
	revs := revCount(t, s, p.ID)
	queueRound(t, s, p, wizardSpec(t, s, p))
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	a := applied(t, s)
	if !slices.Equal(a.Removed, []string{"java21"}) || a.Validating {
		t.Errorf("applied %+v", a)
	}
	if n := revCount(t, s, p.ID); n != revs+1 || validateJobs(t, s) != 0 {
		t.Errorf("revisions +%d, validation jobs %d", n-revs, validateJobs(t, s))
	}
}

// A fix adds its language only when every job passed, only when nothing
// changed, and queues the validation; a check never changes the problem.
func TestFixAddsALanguageOnlyOnAPass(t *testing.T) {
	type tc struct {
		behave map[string]string
		edit   bool
		added  string
		kept   string
	}
	for name, c := range map[string]tc{
		"pass":                     {nil, false, "java21", ""},
		"solution fails":           {map[string]string{"java21/solution": "wa"}, false, "", "not_added"},
		"starter does not compile": {map[string]string{"java21/starter": "ce"}, false, "", "not_added"},
		"problem edited":           {nil, true, "", ""},
		"parts removed from disk":  {nil, false, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProofBank(t, true)
			ctx := context.Background()
			p := proofProblem(t, s)
			// Java was removed earlier; the AI writes it again.
			full := map[string]string{"top": p.LockedTop["java21"], "starter": p.Starters["java21"], "bottom": p.LockedBottom["java21"]}
			for _, m := range []*map[string]string{&p.LockedTop, &p.LockedBottom, &p.Starters} {
				delete(*m, "java21")
			}
			e := editOf(p)
			e.LockedTop, e.LockedBottom, e.Starters = &p.LockedTop, &p.LockedBottom, &p.Starters
			p, err := s.Update(ctx, p.ID, e)
			if err != nil {
				t.Fatal(err)
			}
			var parts ProofParts
			for name, dst := range map[string]*string{"top": &parts.Top, "starter": &parts.Starter, "bottom": &parts.Bottom} {
				if *dst, err = s.Blobs.Write([]byte(full[name])); err != nil {
					t.Fatal(err)
				}
			}
			sol, _ := s.Blobs.Write([]byte("SOLUTION java21\n"))
			n := queueRound(t, s, p, ProofSpec{Source: ProofSourceFix, Language: "java21", Solutions: map[string]string{"java21": sol}, Parts: &parts})
			if n != 2 {
				t.Fatalf("%d jobs, want a solution and a starter job", n)
			}
			if c.edit {
				e := editOf(p)
				e.TimeLimitMS++
				if _, err := s.Update(ctx, p.ID, e); err != nil {
					t.Fatal(err)
				}
			}
			if name == "parts removed from disk" {
				os.Remove(s.Blobs.path(parts.Top))
			}
			revs := revCount(t, s, p.ID)
			runProofs(t, s, c.behave)
			a := applied(t, s)
			if (name == "parts removed from disk") != (a.Error != "") {
				t.Fatalf("applied %+v", a)
			}
			if a.Added != c.added || a.Kept != c.kept || (c.added != "") != a.Validating {
				t.Fatalf("applied %+v", a)
			}
			got, _ := s.Get(ctx, p.ID)
			if c.added != "" {
				if got.LockedTop["java21"] != full["top"] || got.Starters["java21"] != full["starter"] || got.LockedBottom["java21"] != full["bottom"] {
					t.Errorf("parts not added: %+v", got.LockedTop)
				}
				if rev, _ := s.CurrentRevision(ctx, p.ID); rev.Reason != "Java 21 added after the AI's version passed every test." || rev.CreatedBy != 1 {
					t.Errorf("revision %q by %d", rev.Reason, rev.CreatedBy)
				}
				if n := revCount(t, s, p.ID); n != revs+1 {
					t.Errorf("%d revisions", n-revs)
				}
			} else if got.Offers("java21") || revCount(t, s, p.ID) != revs || validateJobs(t, s) != 0 {
				t.Errorf("java offered %v, revisions +%d, validation jobs %d", got.Offers("java21"), revCount(t, s, p.ID)-revs, validateJobs(t, s))
			}
		})
	}
}

func TestCheckChangesNothing(t *testing.T) {
	for _, how := range []string{"", "wa"} {
		s := newProofBank(t, true)
		p := proofProblem(t, s)
		sol, _ := s.Blobs.Write([]byte("SOLUTION java21\n"))
		if n := queueRound(t, s, p, ProofSpec{Source: ProofSourceCheck, Language: "java21", Solutions: map[string]string{"java21": sol}}); n != 1 {
			t.Fatalf("%d jobs, want one solution job", n)
		}
		revs := revCount(t, s, p.ID)
		runProofs(t, s, map[string]string{"java21/solution": how})
		a := applied(t, s)
		if a.Validating || a.Added != "" || len(a.Removed) > 0 || revCount(t, s, p.ID) != revs || validateJobs(t, s) != 0 {
			t.Errorf("how %q: applied %+v", how, a)
		}
		if got := summaries(t, s)["java21/solution"].Result.Outcome; (how == "") != (got == ProofPassed) {
			t.Errorf("how %q: outcome %s", how, got)
		}
	}
}

// No message, input or output of a test is stored in a proof job's summary.
func TestProofSummariesHoldNoMessages(t *testing.T) {
	s := newProofBank(t, true)
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))
	runProofs(t, s, map[string]string{"java21/solution": "wa"})
	rows, err := s.DB.Query("SELECT summary FROM jobs WHERE priority = ?", ProofPriority)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sum string
		rows.Scan(&sum)
		if strings.Contains(sum, secretMessage) || strings.Contains(sum, "message") {
			t.Errorf("summary holds a message: %s", sum)
		}
	}
}

// The three queries start from open or closed proof jobs through the
// (state, priority, id) index, not a scan of every job.
func TestProofQueriesUseTheStateIndex(t *testing.T) {
	s := newProofBank(t, true)
	for name, q := range map[string]struct {
		sql  string
		args []any
	}{
		"open proof":    {openProofSQL, []any{1}},
		"open siblings": {openSiblingsSQL, []any{ProofPriority, 1, 1, "r"}},
		"round jobs":    {roundJobsSQL, []any{ProofPriority, 1, "r"}},
	} {
		rows, err := s.DB.Query("EXPLAIN QUERY PLAN "+q.sql, q.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			rows.Scan(&id, &parent, &unused, &detail)
			plan = append(plan, detail)
		}
		rows.Close()
		if !strings.Contains(strings.Join(plan, "\n"), "jobs_by_state") {
			t.Errorf("%s: plan %v does not use jobs_by_state", name, plan)
		}
	}
}
