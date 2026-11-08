//go:build sandbox

// On the real judge (v2.0.2 stage B): a function problem
// (problems/array-sum) is judged as the composed file in all four
// languages through Submit, Run, Validate and contest rejudge; compile
// errors name the student's own lines; a composed file of exactly 72 KB
// is read and judged; and student code that redefines main, reads its own
// file or prints the answer itself stays inside the sandbox.
package worker

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// array-sum student parts per language and behaviour. WA overflows a
// 32-bit sum (subtask 1 passes, 40 points) where the language has one,
// and drops the first number otherwise (0 points).
var functionCode = map[string]map[string]string{
	"cpp17": {
		"AC":  "long long arraySum(const vector<int>& a) {\n    long long s = 0;\n    for (int x : a) s += x;\n    return s;\n}\n",
		"WA":  "long long arraySum(const vector<int>& a) {\n    int s = 0;\n    for (int x : a) s += x;\n    return s;\n}\n",
		"CE":  "long long arraySum(const vector<int>& a) {\n    return s;\n}\n",
		"RE":  "long long arraySum(const vector<int>& a) {\n    vector<int> v;\n    return v.at(5);\n}\n",
		"TLE": "long long arraySum(const vector<int>& a) {\n    volatile long long x = 0;\n    for (;;) x++;\n}\n",
	},
	"java21": {
		"AC":  "class Solution {\n    long arraySum(int[] a) {\n        long s = 0;\n        for (int x : a) s += x;\n        return s;\n    }\n}\n",
		"WA":  "class Solution {\n    long arraySum(int[] a) {\n        int s = 0;\n        for (int x : a) s += x;\n        return s;\n    }\n}\n",
		"CE":  "class Solution {\n    long arraySum(int[] a) {\n        return s;\n    }\n}\n",
		"RE":  "class Solution {\n    long arraySum(int[] a) {\n        return a[a.length];\n    }\n}\n",
		"TLE": "class Solution {\n    long arraySum(int[] a) {\n        long x = 0;\n        while (true) x++;\n    }\n}\n",
	},
	"python3": {
		"AC":  "def array_sum(a):\n    return sum(a)\n",
		"WA":  "def array_sum(a):\n    return sum(a[1:])\n",
		"CE":  "def array_sum(a):\n    return sum(a\n",
		"RE":  "def array_sum(a):\n    return a[len(a)]\n",
		"TLE": "def array_sum(a):\n    while True:\n        pass\n",
	},
	"node22": {
		"AC":  "function arraySum(a) {\n  let s = 0;\n  for (const x of a) s += x;\n  return s;\n}\n",
		"WA":  "function arraySum(a) {\n  return a.slice(1).reduce((s, x) => s + x, 0);\n}\n",
		"RE":  "function arraySum(a) {\n  return a.missing.length;\n}\n",
		"TLE": "function arraySum(a) {\n  for (;;) {}\n}\n",
	},
}

func (r *rig) submitAs(t *testing.T, sc submissions.Scope, problemID int64, lang, src, key string) *submissions.SubmissionView {
	t.Helper()
	ctx := context.Background()
	v, _, err := r.subs.Submit(ctx, submissions.SubmitRequest{ProblemID: problemID, UserID: 1, Scope: sc, Language: lang, Source: src, ClientKey: key})
	if err != nil {
		t.Fatalf("%s submit: %v", key, err)
	}
	return r.waitSubmission(t, v.ID, key)
}

func (r *rig) waitSubmission(t *testing.T, id int64, key string) *submissions.SubmissionView {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		v, err := r.subs.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if v.State == "done" || v.State == "error" {
			return v
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("%s did not finish", key)
	return nil
}

func (r *rig) runIn(t *testing.T, problemID int64, lang, src string) *submissions.RunView {
	t.Helper()
	ctx := context.Background()
	run, err := r.subs.Run(ctx, submissions.RunRequest{ProblemID: problemID, UserID: 1, Language: lang, Source: src})
	if err != nil {
		t.Fatalf("%s run: %v", lang, err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) && run.State != "done" && run.State != "error" {
		time.Sleep(300 * time.Millisecond)
		run, _ = r.subs.GetRunWith(ctx, run.ID, submissions.ViewOptions{})
	}
	time.Sleep(2100 * time.Millisecond) // one run every 2 seconds
	return run
}

func TestFunctionProblemInEveryLanguage(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "array-sum", nil)
	if v := r.validate(t, p.ID); v.State != problems.ValidationPassed {
		t.Fatalf("array-sum did not validate:\n%s", describe(v))
	}
	want := map[string]struct {
		verdict string
		score   int
	}{"AC": {"AC", 100}, "CE": {"CE", 0}, "RE": {"RE", 0}, "TLE": {"TLE", 0}}
	for lang, byKind := range functionCode {
		for kind, src := range byKind {
			start := time.Now()
			v := r.submitAs(t, submissions.Scope{}, p.ID, lang, src, lang+"-"+kind)
			w, ok := want[kind]
			if kind == "WA" {
				w, ok = struct {
					verdict string
					score   int
				}{"WA", 40}, true
				if lang == "python3" || lang == "node22" {
					w.score = 0
				}
			}
			if !ok || v.Verdict != w.verdict || v.Score != w.score {
				t.Errorf("%s %s: got %s %d/100 (%s), want %s %d\n%s", lang, kind, v.Verdict, v.Score, v.Error, w.verdict, w.score, v.CompileOutput)
				continue
			}
			t.Logf("%s %s: %s %d/100 in %s", lang, kind, v.Verdict, v.Score, time.Since(start).Round(time.Millisecond))
		}
	}
	// Run on the samples: the right answer, and a wrong one.
	for lang, byKind := range functionCode {
		run := r.runIn(t, p.ID, lang, byKind["AC"])
		if run.State != "done" || len(run.Results) != 2 || run.Results[0].Verdict != "AC" || strings.TrimSpace(run.Results[0].Output) != "15" {
			t.Errorf("%s run AC: %+v %s", lang, run.Results, run.CompileOutput)
		}
		run = r.runIn(t, p.ID, lang, byKind["WA"])
		if lang == "python3" || lang == "node22" {
			if run.State != "done" || run.Results[0].Verdict != "WA" {
				t.Errorf("%s run WA: %+v", lang, run.Results)
			}
		}
	}
}

// Compile errors name the student's lines: line 2 of their code, not of
// the composed file; renaming the function points into the locked bottom.
func TestFunctionCompileErrorsNameTheStudentsLines(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "array-sum", nil)
	for lang, c := range map[string]struct{ src, mine, locked string }{
		"cpp17":   {functionCode["cpp17"]["CE"], "main.cpp:2:", "locked code, line"},
		"java21":  {functionCode["java21"]["CE"], "Main.java:3:", "locked code, line"},
		"python3": {functionCode["python3"]["CE"], "", ""},
	} {
		v := r.submitAs(t, submissions.Scope{}, p.ID, lang, c.src, lang+"-ce-lines")
		if v.Verdict != "CE" || (c.mine != "" && !strings.Contains(v.CompileOutput, c.mine)) {
			t.Errorf("%s: %s, compile output:\n%s", lang, v.Verdict, v.CompileOutput)
		}
		if lang == "python3" && !strings.Contains(v.CompileOutput, `"main.py", line 2`) {
			t.Errorf("python compile output:\n%s", v.CompileOutput)
		}
		t.Logf("%s compile output:\n%s", lang, v.CompileOutput)
	}
	renamed := map[string]string{
		"cpp17":  "long long total(const vector<int>& a) {\n    return 0;\n}\n",
		"java21": "class Solution {\n    long total(int[] a) {\n        return 0;\n    }\n}\n",
	}
	for lang, src := range renamed {
		v := r.submitAs(t, submissions.Scope{}, p.ID, lang, src, lang+"-renamed")
		if v.Verdict != "CE" || !strings.Contains(v.CompileOutput, "locked code, line") {
			t.Errorf("%s renamed: %s\n%s", lang, v.Verdict, v.CompileOutput)
		}
		t.Logf("%s renamed:\n%s", lang, v.CompileOutput)
	}
	// Node has no compile step: a syntax error arrives in a run's stderr.
	run := r.runIn(t, p.ID, "node22", "function arraySum(a) {\n  return a.reduce((s, x) => s + x, 0;\n}\n")
	if len(run.Results) == 0 || !strings.Contains(run.Results[0].Stderr, "main.js:2") {
		t.Errorf("node syntax error: %+v", run.Results)
	} else {
		t.Logf("node stderr:\n%s", run.Results[0].Stderr)
	}
}

// Contest rejudge composes the stored student part on the real judge.
func TestFunctionContestRejudge(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, "array-sum", nil)
	var rev int64
	if err := r.bank.DB.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ? ORDER BY rev DESC LIMIT 1", p.ID).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	if _, err := r.bank.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'c', 'C', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.bank.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, p.ID, rev); err != nil {
		t.Fatal(err)
	}
	sc := submissions.Scope{ContestID: 1, RevisionID: rev}
	var ids []int64
	for lang, byKind := range functionCode {
		for _, kind := range []string{"AC", "WA"} {
			v := r.submitAs(t, sc, p.ID, lang, byKind[kind], "contest-"+lang+"-"+kind)
			ids = append(ids, v.ID)
		}
	}
	before := map[int64]string{}
	for _, id := range ids {
		v, _ := r.subs.Get(ctx, id)
		before[id] = v.Verdict + " " + strconv.Itoa(v.Score)
	}
	n, err := r.subs.Rejudge(ctx, submissions.RejudgeRequest{ContestID: 1, ProblemID: p.ID, By: 1})
	if err != nil || n != len(ids) {
		t.Fatalf("rejudge: %d %v", n, err)
	}
	for _, id := range ids {
		v := r.waitSubmission(t, id, "rejudge")
		if got := v.Verdict + " " + strconv.Itoa(v.Score); got != before[id] {
			t.Errorf("submission %d: %s after rejudge, %s before", id, got, before[id])
		}
	}
}

// The largest composed files are read by the judge and accepted: a 4 KB
// top, 64 KB of student code and a 4 KB bottom, each ending in a newline
// (exactly 72 KB), and the same with no part ending in one, so Compose adds
// three newlines (72 KB and 3 bytes, the cap).
func TestFunctionLargestComposedFile(t *testing.T) {
	r := newRig(t)
	dir := filepath.Join(*examplesDir, "array-sum")
	for _, nl := range []string{"\n", ""} {
		// One comment line fills the part to size bytes.
		pad := func(code string, size int) string {
			return code + "//" + strings.Repeat("x", size-len(code)-2-len(nl)) + nl
		}
		slug := "array-sum-72k" + map[string]string{"\n": "", "": "-bare"}[nl]
		top := pad(mustRead(t, filepath.Join(dir, "top.cpp")), problems.MaxStarter)
		bottom := pad(mustRead(t, filepath.Join(dir, "bottom.cpp")), problems.MaxStarter)
		p := r.importPack(t, "array-sum", map[string]string{"top.cpp": top, "bottom.cpp": bottom,
			"problem.yaml": strings.Replace(mustRead(t, filepath.Join(dir, "problem.yaml")), "slug: array-sum", "slug: "+slug, 1)})
		code := pad(functionCode["cpp17"]["AC"], submissions.MaxSource)
		want := 72 << 10
		if nl == "" {
			want = judgeapi.MaxProgramSource
		}
		if c := p.ComposeFor("cpp17", code); len(c.Source) != want {
			t.Fatalf("%s: composed file is %d bytes, want %d", slug, len(c.Source), want)
		}
		v := r.submitAs(t, submissions.Scope{}, p.ID, "cpp17", code, slug)
		if v.Verdict != "AC" || v.Score != 100 {
			t.Errorf("%s: %s %d (%s)\n%s", slug, v.Verdict, v.Score, v.Error, v.CompileOutput)
		}
	}
}

// Student code that steps outside "your code" is still only a program in
// the sandbox: redefining main does not compile, reading its own file or
// printing the answer itself and exiting is judged like any program.
func TestFunctionStudentCodeIsContained(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "array-sum", nil)
	for _, c := range []struct{ lang, src, verdict string }{
		{"cpp17", "long long arraySum(const vector<int>& a) { return 0; }\nint main() { return 0; }\n", "CE"},
		{"python3", "print(open(__file__).read())\nraise SystemExit(0)\ndef array_sum(a):\n    return 0\n", "WA"},
		{"python3", "data = sys.stdin.read().split()\nprint(sum(map(int, data[1:])))\nraise SystemExit(0)\ndef array_sum(a):\n    return 0\n", "AC"},
	} {
		v := r.submitAs(t, submissions.Scope{}, p.ID, c.lang, c.src, c.lang+"-"+c.verdict+"-contained")
		if v.Verdict != c.verdict {
			t.Errorf("%s %q: %s (%s)\n%s", c.lang, c.src, v.Verdict, v.Error, v.CompileOutput)
		}
	}
}

// Validate judges the reference and the brute force composed: a correct
// bare-function reference in each language passes, a wrong one fails.
// The pack's stub file stands in as the reference, so no file is added.
func TestFunctionValidateInEveryLanguage(t *testing.T) {
	r := newRig(t)
	yaml := mustRead(t, filepath.Join(*examplesDir, "array-sum", "problem.yaml"))
	ext := map[string]string{"cpp17": "cpp", "java21": "java", "python3": "py", "node22": "js"}
	for lang, byKind := range functionCode {
		for _, kind := range []string{"AC", "WA"} {
			slug := "array-sum-" + ext[lang] + "-" + strings.ToLower(kind)
			p := r.importPack(t, "array-sum", map[string]string{
				"starter." + ext[lang]: byKind[kind],
				"problem.yaml": strings.NewReplacer("slug: array-sum", "slug: "+slug,
					"reference: solution.cpp", "reference: starter."+ext[lang]).Replace(yaml)})
			v := r.validate(t, p.ID)
			want := problems.ValidationPassed
			if kind == "WA" {
				want = problems.ValidationFailed
			}
			if v.State != want {
				t.Errorf("%s: %s, want %s\n%s", slug, v.State, want, describe(v))
				continue
			}
			t.Logf("%s: %s", slug, v.State)
		}
	}
}

// Generate makes the expected outputs by running the composed reference,
// then validation passes.
func TestFunctionGenerateOutputsThenValidate(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, "array-sum", nil)
	if _, err := r.bank.ReplaceTests(ctx, p.ID, inputsOnly(t, filepath.Join(*examplesDir, "array-sum", "tests")), problems.RevisionNote{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.bank.RequestGeneration(ctx, p.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	var g *problems.Generation
	for time.Now().Before(deadline) {
		g, _ = r.bank.LatestGeneration(ctx, p.ID)
		if g != nil && (g.State == "done" || g.State == "error") {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if g == nil || g.State != "done" || g.Summary == nil || g.Summary.Applied != 6 {
		t.Fatalf("generation %+v", g)
	}
	got, _ := r.bank.Get(ctx, p.ID)
	if out, _ := r.bank.Blobs.Read(got.Tests[0].OutputSHA, 100); strings.TrimSpace(string(out)) != "15" {
		t.Errorf("generated output of test 1: %q", out)
	}
	if v := r.validate(t, p.ID); v.State != problems.ValidationPassed {
		t.Errorf("validation after generating outputs:\n%s", describe(v))
	}
}
