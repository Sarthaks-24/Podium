//go:build sandbox

// On the real judge: correct, wrong,
// time-limit, memory-limit and compile-error submissions in all four
// languages get the right verdict and score; a run on the samples shows the
// program's output; outputs are generated for a pack shipped without them.
package worker

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// a-plus-b solutions per language and behaviour.
var solutions = map[string]map[string]string{
	"cpp17": {
		"AC":  "#include <cstdio>\nint main(){int t;scanf(\"%d\",&t);while(t--){long long a,b;scanf(\"%lld %lld\",&a,&b);printf(\"%lld\\n\",a+b);}}\n",
		"WA":  "#include <cstdio>\nint main(){int t;scanf(\"%d\",&t);while(t--){int a,b;scanf(\"%d %d\",&a,&b);printf(\"%d\\n\",a+b);}}\n",
		"TLE": "int main(){volatile long x=0;for(;;)x++;}\n",
		"MLE": "#include <vector>\n#include <cstdio>\nint main(){std::vector<long long> v;for(;;)v.push_back(1);}\n",
		"CE":  "int main( { return 0; }\n",
	},
	"java21": {
		"AC":  "import java.util.*;public class Main{public static void main(String[] a){Scanner s=new Scanner(System.in);int t=s.nextInt();StringBuilder b=new StringBuilder();while(t-->0){b.append(s.nextLong()+s.nextLong()).append('\\n');}System.out.print(b);}}\n",
		"WA":  "import java.util.*;public class Main{public static void main(String[] a){Scanner s=new Scanner(System.in);int t=s.nextInt();while(t-->0){System.out.println((int)(s.nextLong()+s.nextLong()));}}}\n",
		"TLE": "public class Main{public static void main(String[] a){long x=0;while(true)x++;}}\n",
		"MLE": "import java.util.*;public class Main{public static void main(String[] a){List<long[]> l=new ArrayList<>();while(true)l.add(new long[1<<20]);}}\n",
		"CE":  "public class Main{ void main( }\n",
	},
	"python3": {
		"AC":  "import sys\nd=sys.stdin.read().split()\nt=int(d[0])\nprint('\\n'.join(str(int(d[1+2*i])+int(d[2+2*i])) for i in range(t)))\n",
		"WA":  "import sys\nd=sys.stdin.read().split()\nt=int(d[0])\ns=[int(d[1+2*i])+int(d[2+2*i]) for i in range(t)]\nprint('\\n'.join(str(x if abs(x)<10**12 else x//10) for x in s))\n",
		"TLE": "while True:\n    pass\n",
		"MLE": "x=[]\nwhile True:\n    x.append(bytearray(1<<20))\n",
		"CE":  "def f(:\n    pass\n",
	},
	"node22": {
		"AC":  "const d=require('fs').readFileSync(0,'utf8').trim().split(/\\s+/);const t=+d[0];const o=[];for(let i=0;i<t;i++)o.push((BigInt(d[1+2*i])+BigInt(d[2+2*i])).toString());console.log(o.join('\\n'));\n",
		"WA":  "const d=require('fs').readFileSync(0,'utf8').trim().split(/\\s+/);const t=+d[0];const o=[];for(let i=0;i<t;i++)o.push(String(Number(d[1+2*i])+Number(d[2+2*i])));console.log(o.join('\\n'));\n",
		"TLE": "for(;;){}\n",
		"MLE": "const a=[];for(;;)a.push(new Array(1e6).fill(1));\n",
		"CE":  "",
	},
}

func (r *rig) submitAndWait(t *testing.T, problemID int64, lang, src, key string) *submissions.SubmissionView {
	t.Helper()
	ctx := context.Background()
	v, _, err := r.subs.Submit(ctx, submissions.SubmitRequest{ProblemID: problemID, UserID: 1, Language: lang, Source: src, ClientKey: key})
	if err != nil {
		t.Fatalf("%s submit: %v", key, err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		v, err = r.subs.Get(ctx, v.ID)
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

func TestVerdictsInEveryLanguage(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "a-plus-b", nil)
	// The memory-limit programs run against a copy with a 4 s time limit.
	// They still die at the same 256 MB, but filling it costs CPU (page
	// faults, and V8 collecting garbage near its heap cap): node22 measured
	// up to 1.6 s of its 2 s and cpp17 0.78 s of its 1 s, so on a loaded
	// host they could cross the time limit first and be judged TLE (C2-011).
	// The verdict under test is MLE, not the race between the two limits.
	yaml := mustRead(t, filepath.Join(*examplesDir, "a-plus-b", "problem.yaml"))
	if !strings.Contains(yaml, "time_limit_ms: 1000") {
		t.Fatal("a-plus-b/problem.yaml no longer says time_limit_ms: 1000; update the MLE copy")
	}
	mle := r.importPack(t, "a-plus-b", map[string]string{"problem.yaml": strings.NewReplacer(
		"slug: a-plus-b", "slug: a-plus-b-mle", "time_limit_ms: 1000", "time_limit_ms: 4000").Replace(yaml)})
	want := map[string]struct {
		verdict string
		score   int
	}{
		"AC": {"AC", 100}, "WA": {"WA", 40}, "TLE": {"TLE", 0}, "MLE": {"MLE", 0}, "CE": {"CE", 0},
	}
	for lang, byKind := range solutions {
		for kind, src := range byKind {
			if src == "" {
				continue // JavaScript has no compile step, so no compile error
			}
			id := p.ID
			if kind == "MLE" {
				id = mle.ID
			}
			start := time.Now()
			v := r.submitAndWait(t, id, lang, src, lang+"-"+kind)
			w := want[kind]
			// A wrong answer that overflows only on large numbers passes
			// the samples and subtask 1, and fails subtask 2: 40 points.
			if v.Verdict != w.verdict || v.Score != w.score {
				t.Errorf("%s %s: got %s %d/100 (%s), want %s %d", lang, kind, v.Verdict, v.Score, v.Error, w.verdict, w.score)
				continue
			}
			t.Logf("%s %s: %s %d/100 in %s, slowest %d ms", lang, kind, v.Verdict, v.Score, time.Since(start).Round(time.Millisecond), v.MaxTimeMS)
		}
	}
}

func TestRunOnSamples(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, "a-plus-b", nil)
	run, err := r.subs.Run(ctx, submissions.RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: solutions["python3"]["AC"]})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) && run.State != "done" && run.State != "error" {
		time.Sleep(300 * time.Millisecond)
		run, _ = r.subs.GetRunWith(ctx, run.ID, submissions.ViewOptions{})
	}
	if run.State != "done" || len(run.Results) != 2 || run.Results[0].Verdict != "AC" || run.Results[0].Output != "3\n7\n" {
		t.Errorf("run %+v", run)
	}
}

func TestGenerateOutputsThenValidate(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, "a-plus-b", nil)
	// Replace the tests with inputs only, then generate.
	if _, err := r.bank.ReplaceTests(ctx, p.ID, inputsOnly(t, *examplesDir+"/a-plus-b/tests"), problems.RevisionNote{}); err != nil {
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
	v := r.validate(t, p.ID)
	if v.State != problems.ValidationPassed {
		t.Errorf("validation after generating outputs:\n%s", describe(v))
	}
}

// inputsOnly zips a tests folder's .in files.
func inputsOnly(t *testing.T, dir string) []byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".in") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		w, _ := zw.Create(e.Name())
		w.Write(b)
	}
	zw.Close()
	return buf.Bytes()
}

// TestJobTimingLineOnTheRealJudge judges one accepted C++ submission and
// reads the worker's timing line for it: a real compile, real boxes.
func TestJobTimingLineOnTheRealJudge(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "a-plus-b", nil)
	v := r.submitAndWait(t, p.ID, "cpp17", solutions["cpp17"]["AC"], "timing")
	if v.State != "done" {
		t.Fatalf("submission %+v", v)
	}
	var line map[string]any
	for _, l := range strings.Split(r.timing.String(), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["msg"] == "job timing" && m["kind"] == judgeapi.KindSubmit {
			line = m
		}
	}
	if line == nil {
		t.Fatalf("no job timing line in %s", r.timing.String())
	}
	num := func(k string) float64 { f, _ := line[k].(float64); return f }
	if line["language"] != "cpp17" || num("compiles") != 1 || num("compile_wall_ms") <= 0 || num("setup_ms") <= 0 ||
		num("cleanup_ms") <= 0 || num("subject_id") != float64(v.ID) || num("tests") < 1 || num("tests_wall_ms") <= 0 || num("slowest_limit_ms") <= 0 {
		t.Errorf("timing line %v", line)
	}
}

// S9: the checker's comment on a hidden test never leaves the judge. The
// server refuses a report that carries one, which would turn the whole
// submission into an error, so a wrong answer on a hidden test that still
// finishes as judged proves the worker blanked the message.
func TestHiddenTestMessageNeverReachesTheServer(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, "a-plus-b", nil)
	v := r.submitAndWait(t, p.ID, "cpp17", solutions["cpp17"]["WA"], "hidden-message")
	if v.State != "done" || v.Error != "" || v.Verdict != "WA" || v.Score != 40 {
		t.Fatalf("a wrong answer on a hidden test: state %q, error %q, verdict %s, score %d", v.State, v.Error, v.Verdict, v.Score)
	}
}
