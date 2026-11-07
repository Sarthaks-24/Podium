package submissions

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Compiler output from a composed file with a 2-line top and 3 lines of
// student code: composed lines 3 to 5 are the student's 1 to 3, lines 6 on
// are the locked bottom's 1 on. One case per compiler shape in
// web/src/lib/compileErrors.ts.
var lineCases = []struct {
	lang, in, want string
}{
	{"cpp17",
		"main.cpp: In function 'long long int twice(long long int)':\n" +
			"main.cpp:4:12: error: 'm' was not declared in this scope\n" +
			"    4 |     return m * 2;\n" +
			"      |            ^\n" +
			"main.cpp: In function 'int main()':\n" +
			"main.cpp:9:13: error: 'twice' was not declared in this scope\n" +
			"    9 |     cout << twice(n) << endl;\n" +
			"      |             ^~~~~\n" +
			"main.cpp:1:10: note: in a header line\n",
		"main.cpp: In function 'long long int twice(long long int)':\n" +
			"main.cpp:2:12: error: 'm' was not declared in this scope\n" +
			"    2 |     return m * 2;\n" +
			"      |            ^\n" +
			"main.cpp: In function 'int main()':\n" +
			"locked code, line 4: error: 'twice' was not declared in this scope\n" +
			"    9 |     cout << twice(n) << endl;\n" +
			"      |             ^~~~~\n" +
			"locked code, line 1: note: in a header line\n"},
	{"java21",
		"Main.java:4: error: cannot find symbol\n" +
			"        return m * 2;\n" +
			"               ^\n" +
			"  symbol:   variable m\n" +
			"  location: class Solution\n" +
			"Main.java:8: error: cannot find symbol\n" +
			"        System.out.println(new Solution().twice(in.nextLong()));\n" +
			"                                         ^\n" +
			"2 errors\n",
		"Main.java:2: error: cannot find symbol\n" +
			"        return m * 2;\n" +
			"               ^\n" +
			"  symbol:   variable m\n" +
			"  location: class Solution\n" +
			"locked code, line 3: error: cannot find symbol\n" +
			"        System.out.println(new Solution().twice(in.nextLong()));\n" +
			"                                         ^\n" +
			"2 errors\n"},
	{"python3",
		"Traceback (most recent call last):\n" +
			"  File \"<string>\", line 1, in <module>\n" +
			"  File \"/usr/lib/python3.12/py_compile.py\", line 150, in compile\n" +
			"    raise py_exc\n" +
			"py_compile.PyCompileError:   File \"main.py\", line 5\n" +
			"    return 2 *\n" +
			"              ^\n" +
			"SyntaxError: invalid syntax\n" +
			"Sorry: IndentationError: unexpected indent (main.py, line 7)\n",
		"Traceback (most recent call last):\n" +
			"  File \"<string>\", line 1, in <module>\n" +
			"  File \"/usr/lib/python3.12/py_compile.py\", line 150, in compile\n" +
			"    raise py_exc\n" +
			"py_compile.PyCompileError:   File \"main.py\", line 3\n" +
			"    return 2 *\n" +
			"              ^\n" +
			"SyntaxError: invalid syntax\n" +
			"Sorry: IndentationError: unexpected indent (locked code, line 2)\n"},
	{"node22",
		"/box/main.js:3\n" +
			"  return 2 * ;\n" +
			"             ^\n" +
			"\n" +
			"SyntaxError: Unexpected token ';'\n" +
			"    at wrapSafe (node:internal/modules/cjs/loader:1469:18)\n",
		"/box/main.js:1\n" +
			"  return 2 * ;\n" +
			"             ^\n" +
			"\n" +
			"SyntaxError: Unexpected token ';'\n" +
			"    at wrapSafe (node:internal/modules/cjs/loader:1469:18)\n"},
	{"node22", "main.js:7\nfoo();\n^\n", "locked code, line 2\nfoo();\n^\n"},
}

func TestCompileLinesCountFromTheStudentsCode(t *testing.T) {
	m := lineMap{top: 2, code: 3}
	for _, c := range lineCases {
		if got := m.rewrite(c.in, c.lang); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.lang, got, c.want)
		}
	}
	// A whole problem's output is never touched.
	for _, c := range lineCases {
		if got := (lineMap{}).rewrite(c.in, c.lang); got != c.in {
			t.Errorf("%s: whole problem output changed", c.lang)
		}
	}
}

// Text from the student's own code that compilers echo (the line after a
// location, or a g++ gutter line) is never rewritten, even when it looks
// exactly like a location. The seed is fixed, so a failure repeats.
func TestLineRewriteLeavesEchoedStudentTextAlone(t *testing.T) {
	fakes := []string{"main.cpp:4:2", "Main.java:9", "File \"main.py\", line 8", "(main.py, line 6)", "/box/main.js:7",
		"main.js:8", " ", "x = ", "// ", "\"", "In file included from main.cpp:5:", "from main.cpp:5"}
	rng := rand.New(rand.NewPCG(7, 11))
	echo := func() string {
		var b strings.Builder
		for range 1 + rng.IntN(5) {
			b.WriteString(fakes[rng.IntN(len(fakes))])
		}
		return strings.TrimLeft(b.String(), " ")
	}
	m := lineMap{top: 2, code: 3}
	for range 2000 {
		e := echo()
		for _, c := range []struct{ lang, out, kept string }{
			{"cpp17", "main.cpp:4:1: error: x\n    4 | " + e + "\n", "    2 | " + e + "\n"},
			{"java21", "Main.java:4: error: x\n" + e + "\n", e + "\n"},
			{"python3", "  File \"main.py\", line 4\n    " + e + "\n", "    " + e + "\n"},
			{"node22", "/box/main.js:4\n" + e + "\n", e + "\n"},
		} {
			if got := m.rewrite(c.out, c.lang); !strings.HasSuffix(got, c.kept) {
				t.Fatalf("%s: echoed %q became %q", c.lang, e, got)
			}
		}
	}
}

// End to end: a submission's compile output is shown with the student's
// line numbers, using the top_lines stored when it was judged, so a later
// edit of the top (practice has no pinned revision) does not shift it.
func TestCompileOutputMapsWithTheStoredTop(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: Scope{Practice: true, Reveal: true}, Language: "cpp17", Source: fnCode})
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "fake"})
	if err != nil || c == nil {
		t.Fatalf("claim: %v %v", c, err)
	}
	out := "main.cpp:4:5: error: x\nmain.cpp:7:1: error: y\n"
	if err := r.jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "CE", Output: out}}}); err != nil {
		t.Fatal(err)
	}
	want := "main.cpp:2:5: error: x\nlocked code, line 2: error: y\n"
	got, _ := r.svc.GetWith(ctx, v.ID, StudentView)
	if got.Verdict != "CE" || got.CompileOutput != want {
		t.Fatalf("compile output %q (%s)", got.CompileOutput, got.Verdict)
	}
	// The organiser adds a line to the top; the old submission still maps
	// with the top it was judged with.
	cur, _ := r.bank.Get(ctx, p.ID)
	top := map[string]string{"cpp17": "#include <bits/stdc++.h>\n#include <cmath>\nusing namespace std;\n", "python3": fnTopPy}
	if _, err := r.bank.Update(ctx, p.ID, problems.Edit{Title: cur.Title, Slug: cur.Slug, Statement: cur.Statement,
		TimeLimitMS: cur.TimeLimitMS, MemoryLimitMB: cur.MemoryLimitMB, Checker: cur.Checker, Epsilon: cur.Epsilon,
		Reference: cur.Reference, Brute: cur.Brute, Validator: cur.Validator, Tags: cur.Tags, Samples: cur.Samples,
		Subtasks: cur.Subtasks, LockedTop: &top}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.svc.Get(ctx, v.ID); got.CompileOutput != want {
		t.Errorf("after a top edit: %q", got.CompileOutput)
	}
}

// A run's compile output maps too, and so does a JavaScript run's stderr,
// where Node reports a syntax error (it has no compile step).
func TestRunOutputMapsLines(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	run, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: fnCode})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindRun}, Worker: "fake"})
	if err := r.jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "CE", Output: "main.cpp:3:1: error: x\n"}}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.svc.GetRunWith(ctx, run.ID, ViewOptions{}); got.CompileOutput != "main.cpp:1:1: error: x\n" {
		t.Errorf("run compile output %q", got.CompileOutput)
	}
	stderr := "/box/main.js:9\nfoo(\n^\n\nSyntaxError: missing ) after argument list\n"
	m := lineMap{top: 1, code: 3}
	if got := m.rewriteRunStderr(stderr, "node22"); got != "locked code, line 5\nfoo(\n^\n\nSyntaxError: missing ) after argument list\n" {
		t.Errorf("node stderr %q", got)
	}
	py := "Traceback (most recent call last):\n  File \"/box/main.py\", line 9, in <module>\n"
	if got := m.rewriteRunStderr(py, "python3"); got != py {
		t.Errorf("a Python runtime trace was remapped: %q", got)
	}
}
