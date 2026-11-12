//go:build sandbox

// The Java list choice on the real judge (v2.1.0 stage F12): a locked base
// class with two overloads of the function, each converting its argument and
// calling the other, so a student may override the int[] form or the
// ArrayList<Integer> form (or the long pair). The tests compose the locked
// top, the student's class and the locked bottom the way the server composes
// a function submission, and show what the judge says to each kind of
// student: an override of either form is accepted, neither gives one plain
// line, a wrong type is a compile error, and the ArrayList form costs a
// measured amount of time and memory.
package worker

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/submissions"
)

// choiceTop is the locked top of the Java function problem with a list
// choice. The placeholders are the function's name (FN), the primitive
// element type (P: int or long) and its box (B: Integer or Long). The
// function returns long, as arraySum does.
const choiceTop = `import java.util.*;

class Missing extends RuntimeException {}

class Base {
    private boolean inside;

    long FN(P[] a) {
        if (inside) throw new Missing();
        inside = true;
        try {
            ArrayList<B> list = new ArrayList<>(a.length);
            for (P x : a) list.add(x);
            return FN(list);
        } finally {
            inside = false;
        }
    }

    long FN(ArrayList<B> a) {
        if (inside) throw new Missing();
        inside = true;
        try {
            P[] arr = new P[a.size()];
            for (int i = 0; i < arr.length; i++) arr[i] = a.get(i);
            return FN(arr);
        } finally {
            inside = false;
        }
    }
}
`

// choiceBottom is the locked bottom: it reads the numbers into an array and
// calls the array overload. MSG is the plain line.
const choiceBottom = `public class Main {
    public static void main(String[] args) {
        Scanner in = new Scanner(System.in);
        int n = in.nextInt();
        P[] a = new P[n];
        for (int i = 0; i < n; i++) a[i] = in.nextT();
        try {
            System.out.println(new Solution().FN(a));
        } catch (Missing e) {
            System.err.println("Write FN with P[] or ArrayList<B>.");
            System.exit(1);
        }
    }
}
`

// choiceStarter is the stored starting code with both forms, exactly as the
// marker reader of F11 will read it.
const choiceStarter = `// choice: P[]
class Solution extends Base {
    @Override
    long FN(P[] a) {
        // Return the sum of the numbers in a.
        return 0;
    }
}
// choice: ArrayList<B>
class Solution extends Base {
    @Override
    long FN(ArrayList<B> a) {
        // Return the sum of the numbers in a.
        return 0;
    }
}
`

// choiceText fills the placeholders of one of the parts above.
func choiceText(part, prim string) string {
	box, next := "Integer", "nextInt"
	if prim == "long" {
		box, next = "Long", "nextLong"
	}
	return strings.NewReplacer("in.nextT", "in."+next, "FN", "arraySum", "ArrayList<B>", "ArrayList<"+box+">", "new P[", "new "+prim+"[", "P[]", prim+"[]", "P x", prim+" x", "<B>", "<"+box+">").Replace(part)
}

// plainLine is the one line a student sees when neither form is overridden.
func plainLine(prim string) string {
	if prim == "long" {
		return "Write arraySum with long[] or ArrayList<Long>."
	}
	return "Write arraySum with int[] or ArrayList<Integer>."
}

// forms is a student's class for each way of writing the function, per
// element type.
func choiceSolutions(prim string) map[string]string {
	box := "Integer"
	if prim == "long" {
		box = "Long"
	}
	arr := "    @Override\n    long arraySum(" + prim + "[] a) {\n        long s = 0;\n        for (" + prim + " x : a) s += x;\n        return s;\n    }\n"
	list := "    @Override\n    long arraySum(ArrayList<" + box + "> a) {\n        long s = 0;\n        for (" + prim + " x : a) s += x;\n        return s;\n    }\n"
	wrap := func(body string) string { return "class Solution extends Base {\n" + body + "}\n" }
	return map[string]string{
		"array":   wrap(arr),
		"list":    wrap(list),
		"both":    wrap(arr + list),
		"neither": wrap(""),
		// no @Override and a name that is not the function's
		"misspelt": wrap("    long arrySum(" + prim + "[] a) {\n        return 0;\n    }\n"),
		// a third overload with no @Override
		"third": wrap("    long arraySum(List<" + box + "> a) {\n        return 0;\n    }\n"),
		// @Override with a type that is neither form
		"wrongtype": wrap("    @Override\n    long arraySum(List<" + box + "> a) {\n        return 0;\n    }\n"),
		// @Override with a misspelt name
		"wrongname": wrap("    @Override\n    long arrySum(" + prim + "[] a) {\n        return 0;\n    }\n"),
		// @Override with a return type that does not match
		"wrongreturn": wrap("    @Override\n    int arraySum(" + prim + "[] a) {\n        return 0;\n    }\n"),
		// the other form's body called from an override of this one
		"calls": wrap("    @Override\n    long arraySum(" + prim + "[] a) {\n        ArrayList<" + box + "> l = new ArrayList<>();\n        for (" + prim + " x : a) l.add(x);\n        return arraySum(l);\n    }\n"),
	}
}

// choiceEdits are the Java files of the array-sum pack replaced by the choice
// problem's. The Java starter is the int[] or long[] form alone, so this test
// does not depend on the marker reader; the stored two-form text is composed
// and judged as two separate submissions.
func choiceEdits(prim string) map[string]string {
	return map[string]string{
		"top.java":     choiceText(choiceTop, prim),
		"bottom.java":  choiceText(choiceBottom, prim),
		"starter.java": choiceFormOf(choiceText(choiceStarter, prim), 0),
	}
}

// choiceFormOf returns form i of a two-form starter written like the stored
// text: the lines after the i-th marker line up to the next marker.
func choiceFormOf(src string, i int) string {
	parts := strings.Split(src, "// choice: ")
	body := parts[1+i]
	return body[strings.Index(body, "\n")+1:]
}

// choiceProblem imports the array-sum pack with the choice edits.
func (r *rig) choiceProblem(t *testing.T, slug, prim string) int64 {
	t.Helper()
	e := choiceEdits(prim)
	e["problem.yaml"] = strings.Replace(mustRead(t, filepath.Join(*examplesDir, "array-sum", "problem.yaml")), "slug: array-sum", "slug: "+slug, 1)
	p := r.importPack(t, "array-sum", e)
	if v := r.validate(t, p.ID); v.State != "passed" {
		t.Fatalf("%s did not validate:\n%s", slug, describe(v))
	}
	return p.ID
}

// summary is one line about a submission for the log.
func summary(v *submissions.SubmissionView) string {
	return fmt.Sprintf("%s %d/%d, %d ms, %d KB", v.Verdict, v.Score, v.MaxScore, v.MaxTimeMS, v.MaxMemoryKB)
}

// The base class on the real judge: either form is accepted on every test.
func TestChoiceEitherFormIsAccepted(t *testing.T) {
	r := newRig(t)
	for _, prim := range []string{"int", "long"} {
		id := r.choiceProblem(t, "choice-"+prim, prim)
		sols := choiceSolutions(prim)
		for _, k := range []string{"array", "list", "both"} {
			v := r.submitAs(t, submissions.Scope{}, id, "java21", sols[k], prim+"-"+k)
			if v.Verdict != "AC" || v.Score != 100 {
				t.Errorf("%s %s: %s\n%s", prim, k, summary(v), v.CompileOutput)
				continue
			}
			t.Logf("%s %s: %s", prim, k, summary(v))
		}
	}
}

// A student who overrides neither form (or whose function has another name or
// a third type) gets the plain line, quickly, never a stack overflow or a
// timeout. A Run shows stderr; a Submit is a runtime error.
func TestChoiceNeitherFormGivesThePlainLine(t *testing.T) {
	r := newRig(t)
	for _, prim := range []string{"int", "long"} {
		id := r.choiceProblem(t, "choice-neither-"+prim, prim)
		sols := choiceSolutions(prim)
		for _, k := range []string{"neither", "misspelt", "third", "calls"} {
			run := r.runIn(t, id, "java21", sols[k])
			if run.State != "done" || len(run.Results) == 0 {
				t.Fatalf("%s %s run: %+v %s", prim, k, run, run.CompileOutput)
			}
			for _, res := range run.Results {
				if res.Verdict != "RE" || strings.TrimSpace(res.Stderr) != plainLine(prim) || strings.TrimSpace(res.Output) != "" {
					t.Errorf("%s %s run: verdict %s stderr %q stdout %q", prim, k, res.Verdict, res.Stderr, res.Output)
				}
				if strings.Contains(res.Stderr, "StackOverflow") || res.TimeMS > 1000 {
					t.Errorf("%s %s run: %d ms, stderr %q", prim, k, res.TimeMS, res.Stderr)
				}
			}
			t.Logf("%s %s run: %s in %d ms, stderr %q", prim, k, run.Results[0].Verdict, run.Results[0].TimeMS, strings.TrimSpace(run.Results[0].Stderr))
			v := r.submitAs(t, submissions.Scope{}, id, "java21", sols[k], prim+"-"+k+"-submit")
			if v.Verdict != "RE" || v.Score != 0 {
				t.Errorf("%s %s submit: %s", prim, k, summary(v))
			}
			if v.Revealed != nil {
				t.Logf("%s %s submit revealed: output %q", prim, k, v.Revealed.Output)
			}
			t.Logf("%s %s submit: %s", prim, k, summary(v))
		}
	}
}

// A wrong type, a misspelt name or a wrong return type with @Override is an
// ordinary compile error at the student's line.
func TestChoiceWrongOverrideIsACompileError(t *testing.T) {
	r := newRig(t)
	id := r.choiceProblem(t, "choice-ce", "int")
	sols := choiceSolutions("int")
	// javac points at the @Override line of the student's own code and does not
	// name the method, except for a wrong return type; none of it is in locked code.
	for _, k := range []string{"wrongtype", "wrongname", "wrongreturn"} {
		v := r.submitAs(t, submissions.Scope{}, id, "java21", sols[k], "ce-"+k)
		if v.Verdict != "CE" || !strings.Contains(v.CompileOutput, "Main.java:2: error: method does not override or implement a method from a supertype") || strings.Contains(v.CompileOutput, "locked code, line") || (k == "wrongreturn" && !strings.Contains(v.CompileOutput, "arraySum(int[])")) {
			t.Errorf("%s: %s\n%s", k, summary(v), v.CompileOutput)
		}
		t.Logf("%s compile output:\n%s", k, v.CompileOutput)
	}
}

// The stored two-form starting code: each form, composed untouched, compiles
// and runs (it returns 0, so it is wrong, never a runtime error).
func TestChoiceStartingFormsCompileAndRun(t *testing.T) {
	r := newRig(t)
	id := r.choiceProblem(t, "choice-starters", "int")
	for i, name := range []string{"int[]", "ArrayList<Integer>"} {
		v := r.submitAs(t, submissions.Scope{}, id, "java21", choiceFormOf(choiceText(choiceStarter, "int"), i), "starter-"+strconv.Itoa(i))
		if v.Verdict != "WA" {
			t.Errorf("starting form %s: %s\n%s", name, summary(v), v.CompileOutput)
		}
		t.Logf("starting form %s: %s", name, summary(v))
	}
}

// bigPack makes a copy of array-sum whose test 6 holds 200000 numbers.
func (r *rig) bigPack(t *testing.T, slug string, edits map[string]string) int64 {
	t.Helper()
	const n = 200000
	var in strings.Builder
	in.WriteString(strconv.Itoa(n) + "\n")
	var sum int64
	x := int64(12345)
	for i := 0; i < n; i++ {
		x = (x*1103515245 + 12345) % 2147483648
		v := x%2000000001 - 1000000000
		sum += v
		if i > 0 {
			in.WriteByte(' ')
		}
		in.WriteString(strconv.FormatInt(v, 10))
	}
	in.WriteByte('\n')
	yaml := mustRead(t, filepath.Join(*examplesDir, "array-sum", "problem.yaml"))
	e := map[string]string{
		"problem.yaml": strings.NewReplacer("slug: array-sum", "slug: "+slug, "time_limit_ms: 1000", "time_limit_ms: 5000").Replace(yaml),
		"tests/06.in":  in.String(),
		"tests/06.out": strconv.FormatInt(sum, 10) + "\n",
		"validator.py": strings.Replace(mustRead(t, filepath.Join(*examplesDir, "array-sum", "validator.py")), "20000", "200000", 1),
	}
	for k, v := range edits {
		e[k] = v
	}
	p := r.importPack(t, "array-sum", e)
	if v := r.validate(t, p.ID); v.State != "passed" {
		t.Fatalf("%s did not validate:\n%s", slug, describe(v))
	}
	return p.ID
}

// The cost of the ArrayList form against the int[] form, and of the base class
// itself against today's plain class, on a test of 200000 numbers (boxing
// 200000 integers). Three submissions of each; the figures are the judge's
// own report of the largest test.
func TestChoiceCost(t *testing.T) {
	r := newRig(t)
	plain := r.bigPack(t, "cost-plain", nil)
	base := r.bigPack(t, "cost-base", choiceEdits("int"))
	type variant struct {
		name, src string
		id        int64
	}
	sols := choiceSolutions("int")
	for _, vr := range []variant{
		{"plain class", functionCode["java21"]["AC"], plain},
		{"base, int[] form", sols["array"], base},
		{"base, ArrayList form", sols["list"], base},
	} {
		for rep := 1; rep <= 3; rep++ {
			v := r.submitAs(t, submissions.Scope{}, vr.id, "java21", vr.src, fmt.Sprintf("cost-%s-%d", vr.name, rep))
			if v.Verdict != "AC" {
				t.Fatalf("%s: %s\n%s", vr.name, summary(v), v.CompileOutput)
			}
			var big submissions.TestOutcome
			for _, st := range v.Subtasks {
				for _, to := range st.Tests {
					if to.Idx == 5 {
						big = to
					}
				}
			}
			t.Logf("COST %s rep %d: test 6 (200000 numbers) %d ms, %d KB; submission max %d ms, %d KB", vr.name, rep, big.TimeMS, big.MemoryKB, v.MaxTimeMS, v.MaxMemoryKB)
		}
	}
}
