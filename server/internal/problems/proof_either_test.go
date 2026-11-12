package problems

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Stage F14: a wizard round for a Java starting code that holds two forms
// (an array and an ArrayList) proves four Java jobs: both solutions and both
// forms on their own. Java stays only if all four pass.

const (
	formArray = "int[]"
	formList  = "ArrayList<Integer>"
	// twoFormStarter is a Java starting code with text shared by both forms.
	twoFormStarter = "// shared line\n// choice: int[]\nclass Solution extends Base {\n    @Override\n    long solve(int[] a) { return 0; }\n}\n" +
		"// choice: ArrayList<Integer>\nclass Solution extends Base {\n    @Override\n    long solve(ArrayList<Integer> a) { return 0; }\n}\n"
)

// eitherProblem is the example function problem with a two-form Java starting
// code.
func eitherProblem(t *testing.T, s *Service) *Problem {
	t.Helper()
	p := proofProblem(t, s)
	starters := map[string]string{}
	for k, v := range p.Starters {
		starters[k] = v
	}
	starters["java21"] = twoFormStarter
	e := editOf(p)
	e.Starters = &starters
	p, err := s.Update(context.Background(), p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// eitherSpec is wizardSpec plus the ArrayList solution of Java.
func eitherSpec(t *testing.T, s *Service, p *Problem) ProofSpec {
	t.Helper()
	spec := wizardSpec(t, s, p)
	sha, err := s.Blobs.Write([]byte("SOLUTION arraylist\n"))
	if err != nil {
		t.Fatal(err)
	}
	spec.Solutions[ProofJavaArrayListKey] = sha
	return spec
}

// runEither plays the judge like runProofs, with behaviours keyed by
// "language/part" or, for a two-form language, "language/part/form".
func runEither(t *testing.T, s *Service, behave map[string]string) {
	t.Helper()
	for count(t, s, "SELECT count(*) FROM jobs WHERE state = 'queued' AND priority = ?", ProofPriority) > 0 {
		c, pl := claimProof(t, s)
		if c == nil || pl.Proof == nil {
			t.Fatalf("claimed %v, want a proof job", c)
		}
		key := pl.Proof.Language + "/" + pl.Proof.Part
		if pl.Proof.Variant != "" {
			key += "/" + pl.Proof.Variant
		}
		if err := s.Jobs.Report(context.Background(), proofReport(c, pl, behave[key])); err != nil {
			t.Fatalf("report %s: %v", key, err)
		}
	}
}

// eitherSummaries reads the stored result of every proof job by
// "language/part" or "language/part/form".
func eitherSummaries(t *testing.T, s *Service) map[string]proofSummary {
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
		key := pl.Proof.Language + "/" + pl.Proof.Part
		if pl.Proof.Variant != "" {
			key += "/" + pl.Proof.Variant
		}
		out[key] = sm
	}
	return out
}

func TestEnqueueProofsQueuesFourJavaJobsForTwoForms(t *testing.T) {
	s := newProofBank(t, true)
	p := eitherProblem(t, s)
	if n := queueRound(t, s, p, eitherSpec(t, s, p)); n != 9 {
		t.Fatalf("%d jobs queued, want 9 (7 and two more for the second Java form)", n)
	}
	got := map[string]string{}
	for {
		c, pl := claimProof(t, s)
		if c == nil {
			break
		}
		src, err := s.Blobs.Read(pl.Programs.Reference.SourceSHA, MaxSource)
		if err != nil {
			t.Fatal(err)
		}
		key := pl.Proof.Language + "/" + pl.Proof.Part
		if pl.Proof.Variant != "" {
			key += "/" + pl.Proof.Variant
		}
		got[key] = string(src)
		if pl.Proof.StarterSHA != SHA([]byte(p.Starters[pl.Proof.Language])) {
			t.Errorf("%s: starter hash is not the stored starting code's", key)
		}
	}
	top, bottom := p.LockedTop["java21"], p.LockedBottom["java21"]
	want := map[string]string{
		"java21/solution/" + formArray: Compose(top, "SOLUTION java21\n", bottom).Source,
		"java21/solution/" + formList:  Compose(top, "SOLUTION arraylist\n", bottom).Source,
		"java21/starter/" + formArray:  Compose(top, "// shared line\nclass Solution extends Base {\n    @Override\n    long solve(int[] a) { return 0; }\n}\n", bottom).Source,
		"java21/starter/" + formList:   Compose(top, "// shared line\nclass Solution extends Base {\n    @Override\n    long solve(ArrayList<Integer> a) { return 0; }\n}\n", bottom).Source,
	}
	for k, src := range want {
		if got[k] != src {
			t.Errorf("%s: composed source\n%s\nwant\n%s", k, got[k], src)
		}
		delete(got, k)
	}
	var rest []string
	for k := range got {
		rest = append(rest, k)
	}
	slices.Sort(rest)
	if wantRest := []string{"cpp17/starter", "node22/solution", "node22/starter", "python3/solution", "python3/starter"}; !slices.Equal(rest, wantRest) {
		t.Errorf("other jobs %v, want %v", rest, wantRest)
	}
}

func TestEnqueueProofsTwoFormsNeedTheArrayListSolution(t *testing.T) {
	s := newProofBank(t, true)
	p := eitherProblem(t, s)
	spec := wizardSpec(t, s, p)
	if err := enqueueErr(t, s, p, spec); err == nil || !strings.Contains(err.Error(), "no ArrayList solution for java21") {
		t.Errorf("error %v", err)
	}
}

// A check has no starting-code job and one solution job whatever the problem
// holds: the pasted class is composed between the locked parts as a student's
// submission is, which works for either form because the base class accepts
// both.
func TestCheckOnATwoFormProblemRunsThePastedClassOnce(t *testing.T) {
	s := newProofBank(t, true)
	p := eitherProblem(t, s)
	sol, _ := s.Blobs.Write([]byte("class Solution extends Base { }\n"))
	if n := queueRound(t, s, p, ProofSpec{Source: ProofSourceCheck, Language: "java21", Solutions: map[string]string{"java21": sol}}); n != 1 {
		t.Fatalf("%d jobs, want 1", n)
	}
	c, pl := claimProof(t, s)
	src, _ := s.Blobs.Read(pl.Programs.Reference.SourceSHA, MaxSource)
	if want := Compose(p.LockedTop["java21"], "class Solution extends Base { }\n", p.LockedBottom["java21"]).Source; c == nil || string(src) != want || pl.Proof.Variant != "" {
		t.Errorf("composed %q variant %q", src, pl.Proof.Variant)
	}
}

// Java is offered only if all four of its jobs pass, and the result names the
// form that failed.
func TestJavaWithTwoFormsNeedsAllFourJobs(t *testing.T) {
	for name, tc := range map[string]struct {
		behave  map[string]string
		removed bool
		key     string // the job whose result is the language's worst
		reason  string
	}{
		"all pass":                            {map[string]string{}, false, "", ""},
		"the ArrayList solution is wrong":     {map[string]string{"java21/solution/" + formList: "wa"}, true, "java21/solution/" + formList, "wrong_answer"},
		"the array solution is wrong":         {map[string]string{"java21/solution/" + formArray: "wa"}, true, "java21/solution/" + formArray, "wrong_answer"},
		"the array form does not compile":     {map[string]string{"java21/starter/" + formArray: "ce"}, true, "java21/starter/" + formArray, "compile_error"},
		"the ArrayList form does not compile": {map[string]string{"java21/starter/" + formList: "ce"}, true, "java21/starter/" + formList, "compile_error"},
		"one form is accepted on every test":  {map[string]string{"java21/starter/" + formList: "accepted"}, true, "java21/starter/" + formList, ReasonIgnoresFunction},
		"one form could not be checked":       {map[string]string{"java21/solution/" + formList: "norep"}, true, "java21/solution/" + formList, "no_report"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProofBank(t, true)
			ctx := context.Background()
			p := eitherProblem(t, s)
			queueRound(t, s, p, eitherSpec(t, s, p))
			runEither(t, s, tc.behave)
			var found []*Applied
			sums := eitherSummaries(t, s)
			for _, sm := range sums {
				if sm.Applied != nil {
					found = append(found, sm.Applied)
				}
			}
			if len(found) != 1 {
				t.Fatalf("%d jobs applied the round, want 1", len(found))
			}
			a := found[0]
			want := "cpp17,java21,node22,python3"
			if tc.removed {
				want = "cpp17,node22,python3"
				if !slices.Equal(a.Removed, []string{"java21"}) {
					t.Errorf("removed %v", a.Removed)
				}
			} else if len(a.Removed) > 0 {
				t.Errorf("removed %v", a.Removed)
			}
			if got := offered(t, s, p.ID); got != want || !a.Validating {
				t.Errorf("offered %s validating %v, want %s", got, a.Validating, want)
			}
			if tc.removed {
				if got, _ := s.Get(ctx, p.ID); got.Starters["java21"] != "" {
					t.Error("the two-form starting code was kept")
				}
				res := sums[tc.key].Result
				if res.Reason != tc.reason || res.Variant != strings.Split(tc.key, "/")[2] {
					t.Errorf("result %+v", res)
				}
				// The state shows the worst of the four jobs and its form.
				st, err := s.ProofState(ctx, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, l := range st.Languages {
					if l.Language == "java21" && (l.Reason != tc.reason || l.Variant != res.Variant || l.Result == ProofPassed) {
						t.Errorf("state %+v", l)
					}
				}
			}
		})
	}
}

// While a two-form round runs, the progress counts all of its jobs.
func TestProgressCountsTheJobsOfTwoForms(t *testing.T) {
	s := newProofBank(t, true)
	p := eitherProblem(t, s)
	queueRound(t, s, p, eitherSpec(t, s, p))
	st, err := s.ProofState(context.Background(), p.ID)
	if err != nil || st.Progress == nil || st.Progress.Total != 9 || st.Progress.Done != 0 {
		t.Fatalf("progress %+v %v", st.Progress, err)
	}
}

// An edit of the two-form starting code during the round leaves Java alone,
// as for any one-form starting code.
func TestEditingATwoFormStarterDuringAProofSkipsJava(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := eitherProblem(t, s)
	queueRound(t, s, p, eitherSpec(t, s, p))
	starters := map[string]string{}
	for k, v := range p.Starters {
		starters[k] = v
	}
	starters["java21"] += "// edited\n"
	e := editOf(p)
	e.Starters = &starters
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	runEither(t, s, map[string]string{"java21/solution/" + formList: "wa"})
	var applied *Applied
	for _, sm := range eitherSummaries(t, s) {
		if sm.Applied != nil {
			applied = sm.Applied
		}
	}
	if applied == nil || !slices.Equal(applied.Stale, []string{"java21"}) || len(applied.Removed) > 0 {
		t.Errorf("applied %+v", applied)
	}
	if got := offered(t, s, p.ID); got != "cpp17,java21,node22,python3" {
		t.Errorf("offered %s", got)
	}
}

// The wizard chain needs both Java solutions for a two-form starting code and
// takes them as two blobs.
func TestRequestInputGenerationProofNeedsBothJavaSolutions(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	d := funcDraft(t, s)
	d.Problem.Starters["java21"] = twoFormStarter
	p, err := s.CreateDraft(ctx, d, 1)
	if err != nil {
		t.Fatal(err)
	}
	solutions := solutionTexts("java21", "python3", "node22")
	if _, err := s.RequestInputGenerationProof(ctx, p.ID, 1, solutions); err == nil || !strings.Contains(err.Error(), "ArrayList") {
		t.Fatalf("error %v", err)
	}
	solutions[ProofJavaArrayListKey] = "SOLUTION arraylist\n"
	if g, err := s.RequestInputGenerationProof(ctx, p.ID, 1, solutions); err != nil || g.State != ValidationQueued {
		t.Fatalf("queue: %v %v", g, err)
	}
}
