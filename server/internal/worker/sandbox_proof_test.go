//go:build sandbox

// Proof jobs on the real judge (v2.1.0 stage F9). A proof is queued the way
// the server queues it for a language the problem does not offer yet:
// Service.RequestLanguageProof with the language's three parts and a
// solution, which runs enqueueProofs and queues a solution job and a
// starting-code job. The real worker runs them in isolate, the jobs service
// checks each report and ValidateKind.Finish classifies it; the tests read
// each job's stored result. The function pack is problems/array-sum, cut down
// to one offered language (the C++ copy uses the reference as its brute force) so the others can be proved as a fix would.
package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/ai/fakeai"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// proofParts are the three locked-around parts of one language.
type proofParts struct{ top, starter, bottom string }

// proofRig is a rig with two cut-down copies of array-sum: one that offers
// only C++ (to prove Java, Python and Node) and one that offers only Python,
// its reference a bare Python function (to prove C++).
type proofRig struct {
	*rig
	cppOnly, pyOnly int64
}

func newProofRig(t *testing.T) *proofRig {
	t.Helper()
	r := newRig(t)
	yaml := mustRead(t, filepath.Join(*examplesDir, "array-sum", "problem.yaml"))
	cpp := r.importPack(t, "array-sum", map[string]string{"problem.yaml": strings.NewReplacer(
		"slug: array-sum", "slug: proof-cpp-only", "brute: brute.py", "brute: solution.cpp",
		"starters: [starter.cpp, starter.java, starter.js, starter.py]", "starters: [starter.cpp]",
		"locked_top: [top.cpp, top.java, top.js, top.py]", "locked_top: [top.cpp]",
		"locked_bottom: [bottom.cpp, bottom.java, bottom.js, bottom.py]", "locked_bottom: [bottom.cpp]").Replace(yaml)})
	py := r.importPack(t, "array-sum", map[string]string{"starter.py": functionCode["python3"]["AC"], "problem.yaml": strings.NewReplacer(
		"slug: array-sum", "slug: proof-py-only",
		"reference: solution.cpp", "reference: starter.py",
		"starters: [starter.cpp, starter.java, starter.js, starter.py]", "starters: [starter.py]",
		"locked_top: [top.cpp, top.java, top.js, top.py]", "locked_top: [top.py]",
		"locked_bottom: [bottom.cpp, bottom.java, bottom.js, bottom.py]", "locked_bottom: [bottom.py]").Replace(yaml)})
	return &proofRig{rig: r, cppOnly: cpp.ID, pyOnly: py.ID}
}

// packParts reads a language's untouched parts from the pack.
func packParts(t *testing.T, lang string) proofParts {
	t.Helper()
	ext := map[string]string{"cpp17": "cpp", "java21": "java", "python3": "py", "node22": "js"}[lang]
	dir := filepath.Join(*examplesDir, "array-sum")
	return proofParts{mustRead(t, filepath.Join(dir, "top."+ext)), mustRead(t, filepath.Join(dir, "starter."+ext)), mustRead(t, filepath.Join(dir, "bottom."+ext))}
}

// prove queues one proof round for lang as a fix would and waits for it to
// close. It returns each part's stored result and how long the round took.
func (r *proofRig) prove(t *testing.T, lang string, parts proofParts, solution string) (map[string]problems.ProofResult, time.Duration) {
	t.Helper()
	ctx := context.Background()
	id := r.cppOnly
	if lang == "cpp17" {
		id = r.pyOnly
	}
	var after int64
	r.bank.DB.QueryRow(`SELECT coalesce(max(id), 0) FROM jobs`).Scan(&after)
	start := time.Now()
	err := r.bank.RequestLanguageProof(ctx, id, 1, problems.ProofRequest{Language: lang, Solution: solution,
		Parts: &problems.LanguageParts{Top: parts.top, Starter: parts.starter, Bottom: parts.bottom}})
	if err != nil {
		t.Fatalf("%s: queue proof: %v", lang, err)
	}
	for deadline := time.Now().Add(4 * time.Minute); ; time.Sleep(300 * time.Millisecond) {
		open, err := problems.OpenProof(ctx, r.bank.DB, id)
		if err != nil {
			t.Fatal(err)
		}
		if !open {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: the proof did not close in 4 minutes", lang)
		}
	}
	took := time.Since(start)
	rows, err := r.bank.DB.Query(`SELECT state, payload, summary FROM jobs WHERE id > ? AND kind = 'validate' AND priority = ?`, after, problems.ProofPriority)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]problems.ProofResult{}
	for rows.Next() {
		var state, payload, summary string
		if err := rows.Scan(&state, &payload, &summary); err != nil {
			t.Fatal(err)
		}
		var pl struct {
			Proof struct{ Language, Part string }
		}
		var sm struct{ Result problems.ProofResult }
		if json.Unmarshal([]byte(payload), &pl) != nil || json.Unmarshal([]byte(summary), &sm) != nil {
			t.Fatalf("%s: unreadable job: %s / %s", lang, payload, summary)
		}
		if state != "done" {
			t.Errorf("%s %s: job state %s, summary %s", lang, pl.Proof.Part, state, summary)
		}
		out[pl.Proof.Part] = sm.Result
		t.Logf("%s %s: %s %s failed %d of %d verdicts %v compiler %q", lang, pl.Proof.Part, sm.Result.Outcome, sm.Result.Reason,
			sm.Result.Failed, sm.Result.Total, sm.Result.Verdicts, sm.Result.Compiler)
	}
	if len(out) != 2 {
		t.Fatalf("%s: want a solution job and a starter job, got %v", lang, out)
	}
	return out, took
}

// want checks one job's outcome, reason and, for a failure, its counts.
func want(t *testing.T, name string, got problems.ProofResult, outcome, reason string, failed, total int) {
	t.Helper()
	if got.Outcome != outcome || got.Reason != reason || got.Failed != failed || got.Total != total {
		t.Errorf("%s: got %s %q failed %d of %d (verdicts %v, compiler %q), want %s %q failed %d of %d",
			name, got.Outcome, got.Reason, got.Failed, got.Total, got.Verdicts, got.Compiler, outcome, reason, failed, total)
	}
}

// All four languages: the solution passes and the untouched starting code
// compiles or loads and gets at least one test wrong. Python and Node
// starters are validate jobs with only a reference program and no compile
// step for Node.
func TestProofEveryLanguage(t *testing.T) {
	r := newProofRig(t)
	for _, lang := range []string{"cpp17", "java21", "python3", "node22"} {
		res, _ := r.prove(t, lang, packParts(t, lang), functionCode[lang]["AC"])
		want(t, lang+" solution", res[problems.ProofPartSolution], problems.ProofPassed, "", 0, 6)
		want(t, lang+" starter", res[problems.ProofPartStarter], problems.ProofPassed, "", 0, 6)
	}
}

// A Java solution that overflows a 32-bit sum gets the large tests wrong.
func TestProofJavaWrongSolution(t *testing.T) {
	r := newProofRig(t)
	res, _ := r.prove(t, "java21", packParts(t, "java21"), functionCode["java21"]["WA"])
	want(t, "java solution", res[problems.ProofPartSolution], problems.ProofFailed, "wrong_answer", 2, 6)
	if got := res[problems.ProofPartSolution].Verdicts["WA"]; got != 2 {
		t.Errorf("verdicts: %v", res[problems.ProofPartSolution].Verdicts)
	}
	want(t, "java starter", res[problems.ProofPartStarter], problems.ProofPassed, "", 0, 6)
}

// A starting code that does not compile fails with the compiler's first
// lines, capped at 10 lines and 600 bytes, however many errors there are.
func TestProofStarterThatDoesNotCompile(t *testing.T) {
	r := newProofRig(t)
	var many strings.Builder
	many.WriteString("class Solution {\n    long arraySum(int[] a) {\n")
	for i := 0; i < 30; i++ {
		many.WriteString("        missing" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "();\n")
	}
	many.WriteString("        return 0;\n    }\n}\n")
	for _, c := range []struct{ name, lang, starter string }{
		{"java short", "java21", functionCode["java21"]["CE"]},
		{"java many errors", "java21", many.String()},
		{"cpp", "cpp17", functionCode["cpp17"]["CE"]},
	} {
		parts := packParts(t, c.lang)
		parts.starter = c.starter
		res, _ := r.prove(t, c.lang, parts, functionCode[c.lang]["AC"])
		want(t, c.name+" solution", res[problems.ProofPartSolution], problems.ProofPassed, "", 0, 6)
		st := res[problems.ProofPartStarter]
		want(t, c.name+" starter", st, problems.ProofFailed, "compile_error", 0, 0)
		if !strings.Contains(st.Compiler, "error") || len(st.Compiler) > 600 || strings.Count(st.Compiler, "\n") >= 10 {
			t.Errorf("%s compiler text (%d bytes, %d lines):\n%s", c.name, len(st.Compiler), strings.Count(st.Compiler, "\n")+1, st.Compiler)
		}
	}
}

// A locked bottom that prints one more line makes the solution wrong on every
// test.
func TestProofBottomPrintsExtraOutput(t *testing.T) {
	r := newProofRig(t)
	parts := packParts(t, "python3")
	parts.bottom += "print('done')\n"
	res, _ := r.prove(t, "python3", parts, functionCode["python3"]["AC"])
	want(t, "solution", res[problems.ProofPartSolution], problems.ProofFailed, "wrong_answer", 6, 6)
}

// A locked bottom that computes the answer itself: the solution passes, the
// untouched starting code is accepted on every test.
func TestProofBottomIgnoresTheFunction(t *testing.T) {
	r := newProofRig(t)
	parts := packParts(t, "python3")
	parts.bottom = "data = sys.stdin.buffer.read().split()\nn = int(data[0])\nprint(sum(int(x) for x in data[1:1 + n]))\n"
	res, _ := r.prove(t, "python3", parts, functionCode["python3"]["AC"])
	want(t, "solution", res[problems.ProofPartSolution], problems.ProofPassed, "", 0, 6)
	want(t, "starter", res[problems.ProofPartStarter], problems.ProofFailed, problems.ReasonIgnoresFunction, 0, 6)
}

// A solution that never returns ends with the time limit verdict, and the
// round closes in bounded time.
func TestProofSolutionThatLoopsForever(t *testing.T) {
	r := newProofRig(t)
	res, took := r.prove(t, "cpp17", packParts(t, "cpp17"), functionCode["cpp17"]["TLE"])
	want(t, "solution", res[problems.ProofPartSolution], problems.ProofFailed, "time_limit", 6, 6)
	if took > 2*time.Minute {
		t.Errorf("the round took %s", took)
	}
	t.Logf("round took %s", took.Round(time.Millisecond))
}

// A starting code that loops forever or throws is not accepted on any test,
// which is a pass: the part that proves it is the starter job, not unchecked.
func TestProofStarterThatLoopsOrThrows(t *testing.T) {
	r := newProofRig(t)
	for _, c := range []struct{ name, lang, starter string }{
		{"cpp loop", "cpp17", functionCode["cpp17"]["TLE"]},
		{"java throws", "java21", functionCode["java21"]["RE"]},
		{"python throws", "python3", functionCode["python3"]["RE"]},
		{"node throws", "node22", functionCode["node22"]["RE"]},
	} {
		parts := packParts(t, c.lang)
		parts.starter = c.starter
		res, _ := r.prove(t, c.lang, parts, functionCode[c.lang]["AC"])
		want(t, c.name+" solution", res[problems.ProofPartSolution], problems.ProofPassed, "", 0, 6)
		want(t, c.name+" starter", res[problems.ProofPartStarter], problems.ProofPassed, "", 0, 6)
	}
}

// F14: a wizard round for a Java starting code with two forms, on the real
// judge. The fake provider's canned "either" pack (the base class, both
// starting forms, the bottom and a solution for each form) becomes a draft and
// goes through the real server-side chain: inputs, outputs, then nine proof
// jobs (two solution and two starting-code jobs for Java, a solution and a
// starting-code job for Python and JavaScript, a starting-code job for C++).
// The canned variants prove their defects are real.
func TestProofEitherWizardRound(t *testing.T) {
	const listForm = "ArrayList<Integer>"
	for _, tc := range []struct {
		name, variant string
		// failed is the "part/form" of the one Java job that must not pass, with its reason.
		failed, reason string
	}{
		{"both forms pass", "", "", ""},
		{"the ArrayList solution is wrong", ai.VariantEitherListWrong, "solution/" + listForm, "wrong_answer"},
		{"the ArrayList form does not compile", ai.VariantEitherFormBroken, "starter/" + listForm, "compile_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			topic := "Sum of an array"
			if tc.variant != "" {
				topic += " variant:" + tc.variant
			}
			pack, err := ai.GenerateFunction(ctx, &fakeai.Fake{}, ai.Request{Brief: &ai.Brief{Topic: topic, Difficulty: "easy"}, JavaLists: ai.JavaListsEither})
			if err != nil {
				t.Fatal(err)
			}
			p, err := r.bank.CreateDraft(ctx, ai.ToDraftFunction(pack), 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.bank.RequestInputGenerationProof(ctx, p.ID, 1, pack.Solutions()); err != nil {
				t.Fatal(err)
			}
			// The rig's validate kind has no queue, so the round ends without a
			// validation: wait for the chain (inputs, outputs, proofs) to close.
			for deadline := time.Now().Add(6 * time.Minute); ; time.Sleep(300 * time.Millisecond) {
				open, err := problems.OpenProof(ctx, r.bank.DB, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !open {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the chain did not close in 6 minutes")
				}
			}
			rows, err := r.bank.DB.Query(`SELECT state, payload, summary FROM jobs WHERE kind = 'validate' AND priority = ?`, problems.ProofPriority)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			results := map[string]problems.ProofResult{}
			for rows.Next() {
				var state, payload, summary string
				if err := rows.Scan(&state, &payload, &summary); err != nil {
					t.Fatal(err)
				}
				var pl struct {
					Proof struct{ Language, Part, Variant string }
				}
				var sm struct{ Result problems.ProofResult }
				if json.Unmarshal([]byte(payload), &pl) != nil || json.Unmarshal([]byte(summary), &sm) != nil || state != "done" {
					t.Fatalf("unreadable or open job (%s): %s / %s", state, payload, summary)
				}
				key := pl.Proof.Language + "/" + pl.Proof.Part
				if pl.Proof.Variant != "" {
					key += "/" + pl.Proof.Variant
				}
				results[key] = sm.Result
				t.Logf("%s: %s %s failed %d of %d compiler %q", key, sm.Result.Outcome, sm.Result.Reason, sm.Result.Failed, sm.Result.Total, sm.Result.Compiler)
			}
			if len(results) != 9 {
				t.Fatalf("%d proof jobs, want 9: %v", len(results), results)
			}
			for _, k := range []string{"java21/solution/int[]", "java21/solution/" + listForm, "java21/starter/int[]", "java21/starter/" + listForm} {
				res, ok := results[k]
				if !ok {
					t.Fatalf("no job %s", k)
				}
				if k == "java21/"+tc.failed {
					if res.Outcome != problems.ProofFailed || res.Reason != tc.reason {
						t.Errorf("%s: %s %q, want failed %q", k, res.Outcome, res.Reason, tc.reason)
					}
				} else if res.Outcome != problems.ProofPassed {
					t.Errorf("%s: %s %q (failed %d of %d, compiler %q)", k, res.Outcome, res.Reason, res.Failed, res.Total, res.Compiler)
				}
			}
			for k, res := range results {
				if !strings.HasPrefix(k, "java21/") && res.Outcome != problems.ProofPassed {
					t.Errorf("%s: %s %q", k, res.Outcome, res.Reason)
				}
			}
			got, err := r.bank.Get(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if kept := got.Offers("java21"); kept != (tc.failed == "") {
				t.Errorf("Java offered %v after the round", kept)
			}
		})
	}
}
