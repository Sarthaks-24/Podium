package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// job is one corpus request with the figures of its timing line.
type job struct {
	program             string
	heavy, rush, graded bool
	expected, verdict   string
	compileMS, cpuMS    float64
	totalMS, reportMS   float64
	cached              bool
	submittedMS, doneMS int64
}

func mkRun(name string, jobs []job) *Run {
	r := &Run{Name: name, Timing: map[string]Timing{}}
	for i, j := range jobs {
		kind := "run"
		if j.graded {
			kind = "submit"
		}
		exp, ver := j.expected, j.verdict
		if exp == "" {
			exp = "AC"
		}
		if ver == "" {
			ver = exp
		}
		id := int64(i + 1)
		r.Records = append(r.Records, Record{Seq: i, Program: j.program, Kind: kind, Graded: j.graded, Heavy: j.heavy, Rush: j.rush,
			Expected: exp, Verdict: ver, ID: id, SubmittedMS: j.submittedMS, DoneMS: j.doneMS})
		t := Timing{Kind: kind, SubjectID: id, TotalMS: j.totalMS, ReportMS: j.reportMS, SlowestCPUMS: j.cpuMS, Compiles: 1, CompileWallMS: j.compileMS}
		if j.cached {
			t.CompilesCached = 1
		}
		r.Timing[timingKey(kind, id)] = t
	}
	return r
}

func find(t *testing.T, rs []Result, name string) Result {
	t.Helper()
	for _, r := range rs {
		if strings.Contains(r.Name, name) {
			return r
		}
	}
	t.Fatalf("no result named %q in %+v", name, rs)
	return Result{}
}

// heavyArm builds a run of three heavy C++ jobs whose compile wall is ms.
func heavyArm(name string, ms float64) *Run {
	return mkRun(name, []job{
		{program: "h1", heavy: true, compileMS: ms}, {program: "h2", heavy: true, compileMS: ms},
		{program: "h3", heavy: true, compileMS: ms}, {program: "plain", compileMS: 900},
	})
}

func TestJ5Threshold(t *testing.T) {
	base := []*Run{heavyArm("b1", 1000), heavyArm("b2", 1000)}
	cases := []struct {
		name string
		ms   float64
		ship bool
	}{
		{"drops 31 percent", 690, true},
		{"drops exactly 30 percent", 700, true},
		{"drops 29 percent", 710, false},
		{"does not drop", 1000, false},
		{"gets slower", 1200, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			test := []*Run{heavyArm("t1", c.ms), heavyArm("t2", c.ms)}
			rs, err := Change("j5", base, test, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := Ships(rs); got != c.ship {
				t.Errorf("ships = %v, want %v: %+v", got, c.ship, rs)
			}
		})
	}
}

// A drop that the A/A pair shows to be within noise does not ship.
func TestJ5NoiseRaisesTheBar(t *testing.T) {
	base := []*Run{heavyArm("b1", 1000)}
	test := []*Run{heavyArm("t1", 650)} // a 35 percent drop
	if rs, _ := Change("j5", base, test, nil); !Ships(rs) {
		t.Fatalf("without noise a 35%% drop should ship: %+v", rs)
	}
	noisy := []*Run{heavyArm("a1", 1000), heavyArm("a2", 700)} // 35% spread, x2 = 70%
	rs, _ := Change("j5", base, test, noisy)
	if Ships(rs) {
		t.Errorf("a drop inside the A/A noise shipped: %+v", rs)
	}
	quiet := []*Run{heavyArm("a1", 1000), heavyArm("a2", 1010)}
	if rs, _ := Change("j5", base, test, quiet); !Ships(rs) {
		t.Errorf("a drop above a quiet A/A pair did not ship: %+v", rs)
	}
}

func TestJ5IgnoresCachedCompilesAndNeedsTimingLines(t *testing.T) {
	base := []*Run{heavyArm("b1", 1000)}
	// A cached compile reports 0 ms; counting it would fake a huge drop.
	test := heavyArm("t1", 1000)
	for k, v := range test.Timing {
		v.CompilesCached, v.CompileWallMS = 1, 0
		test.Timing[k] = v
	}
	if rs, _ := Change("j5", base, []*Run{test}, nil); Ships(rs) {
		t.Errorf("all-cached test arm shipped: %+v", rs)
	}
	test = heavyArm("t2", 600)
	delete(test.Timing, timingKey("run", 1))
	if rs, _ := Change("j5", base, []*Run{test}, nil); Ships(rs) {
		t.Errorf("a missing timing line shipped: %+v", rs)
	}
}

// rushArm builds a run with graded rush jobs; reportMS of totalMS each.
func rushArm(name string, totalMS, reportMS float64) *Run {
	return mkRun(name, []job{
		{program: "g1", rush: true, graded: true, totalMS: totalMS, reportMS: reportMS, submittedMS: 0, doneMS: 5000},
		{program: "g2", rush: true, graded: true, totalMS: totalMS, reportMS: reportMS, submittedMS: 1000, doneMS: 8000},
		{program: "r1", rush: true, totalMS: totalMS, reportMS: reportMS, submittedMS: 500, doneMS: 9000},
	})
}

func TestJ6Threshold(t *testing.T) {
	for _, c := range []struct {
		name   string
		report float64
		ship   bool
	}{{"11 percent", 110, true}, {"exactly 10 percent", 100, true}, {"9 percent", 90, false}, {"none", 0, false}} {
		t.Run(c.name, func(t *testing.T) {
			rs, err := Change("j6", []*Run{rushArm("b1", 1000, c.report)}, []*Run{rushArm("t1", 1000, 0)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := Ships(rs); got != c.ship {
				t.Errorf("ships = %v, want %v: %+v", got, c.ship, rs)
			}
		})
	}
}

func TestJ6MustNotSlowTheJobs(t *testing.T) {
	base := []*Run{rushArm("b1", 1000, 200)}
	if rs, _ := Change("j6", base, []*Run{rushArm("t1", 1040, 0)}, nil); !Ships(rs) {
		t.Errorf("4%% slower is inside the floor: %+v", rs)
	}
	rs, _ := Change("j6", base, []*Run{rushArm("t1", 1100, 0)}, nil)
	if Ships(rs) || find(t, rs, "J6 job wall").Pass {
		t.Errorf("10%% slower shipped: %+v", rs)
	}
	// A noisy A/A pair widens the allowance.
	aa := []*Run{rushArm("a1", 1000, 0), rushArm("a2", 1200, 0)}
	if rs, _ := Change("j6", base, []*Run{rushArm("t1", 1100, 0)}, aa); !Ships(rs) {
		t.Errorf("10%% slower is inside an 18%% A/A spread: %+v", rs)
	}
}

func TestJ0Order(t *testing.T) {
	// drain 8000 ms, p95 wait 7000 ms.
	base := []*Run{rushArm("b1", 1000, 0)}
	faster := rushArm("t1", 1000, 0)
	faster.Records[1].DoneMS = 6000 // drain 6000, p95 5000
	slower := rushArm("t2", 1000, 0)
	slower.Records[1].DoneMS = 9000 // drain 9000
	for _, c := range []struct {
		name string
		run  *Run
		ship bool
	}{{"faster", faster, true}, {"same", rushArm("t3", 1000, 0), true}, {"slower drain", slower, false}} {
		t.Run(c.name, func(t *testing.T) {
			rs, _ := Change("j0", base, []*Run{c.run}, nil)
			if Ships(rs) != c.ship {
				t.Errorf("ships = %v, want %v: %+v", Ships(rs), c.ship, rs)
			}
		})
	}
	// Drain equal but the p95 wait worse.
	worse := rushArm("t4", 1000, 0)
	worse.Records[1].SubmittedMS = -2000 // wait 10000 instead of 7000, drain from -2000 is 10000 too
	rs, _ := Change("j0", base, []*Run{worse}, nil)
	if Ships(rs) {
		t.Errorf("a worse wait shipped: %+v", rs)
	}
	if find(t, rs, "p95").Pass {
		t.Errorf("the p95 check passed a worse wait: %+v", rs)
	}
}

func TestJ0NeedsGradedJobs(t *testing.T) {
	none := mkRun("t", []job{{program: "r1", rush: true}})
	rs, _ := Change("j0", []*Run{rushArm("b1", 1, 0)}, []*Run{none}, nil)
	if Ships(rs) {
		t.Errorf("no graded submissions shipped: %+v", rs)
	}
}

func j1Arm(name string, cpu float64, tle int, kills int64, sampler bool) *Run {
	jobs := []job{
		{program: "near-java", cpuMS: cpu}, {program: "near-java", cpuMS: cpu},
		{program: "plain", cpuMS: 100}, {program: "plain", cpuMS: 100},
	}
	for i := 0; i < tle; i++ {
		jobs = append(jobs, job{program: "brute", expected: "TLE", cpuMS: 3000})
	}
	r := mkRun(name, jobs)
	r.HasSampler, r.OOMKills = sampler, kills
	return r
}

func TestJ1(t *testing.T) {
	base := []*Run{j1Arm("b1", 500, 2, 0, true)}
	ok := func(test *Run, aa []*Run) bool {
		rs, _ := Change("j1", base, []*Run{test}, aa)
		return Ships(rs)
	}
	if !ok(j1Arm("t", 500, 2, 0, true), nil) {
		t.Error("identical run did not ship")
	}
	if !ok(j1Arm("t", 540, 2, 0, true), nil) { // +8%, inside the 10% floor
		t.Error("CPU inside the floor did not ship")
	}
	if ok(j1Arm("t", 600, 2, 0, true), nil) { // +20%
		t.Error("CPU 20% up shipped")
	}
	if !ok(j1Arm("t", 600, 2, 0, true), []*Run{j1Arm("a1", 500, 2, 0, true), j1Arm("a2", 680, 2, 0, true)}) {
		t.Error("CPU change inside an A/A spread did not ship")
	}
	if ok(j1Arm("t", 500, 1, 0, true), nil) {
		t.Error("a missing TLE verdict shipped")
	}
	if ok(j1Arm("t", 500, 3, 0, true), nil) {
		t.Error("an extra TLE verdict shipped")
	}
	if ok(j1Arm("t", 500, 2, 1, true), nil) {
		t.Error("an out-of-memory kill shipped")
	}
	rs, _ := Change("j1", base, []*Run{j1Arm("t", 500, 2, 0, false)}, nil)
	if r := find(t, rs, "out-of-memory"); !r.Skip || !Ships(rs) {
		t.Errorf("no sampler should be skipped, not failed: %+v", rs)
	}
}

// A moved verdict fails every change, whatever its own numbers say.
func TestAMovedVerdictFailsEverything(t *testing.T) {
	b5, t5 := []*Run{heavyArm("b", 1000)}, []*Run{heavyArm("t", 100)}
	if rs, _ := Change("j5", b5, t5, nil); !Ships(rs) {
		t.Fatalf("j5: the clean arm should ship: %+v", rs)
	}
	t5[0].Records[0].Verdict = "WA"
	rs, _ := Change("j5", b5, t5, nil)
	if Ships(rs) || find(t, rs, "verdicts").Pass {
		t.Errorf("j5: a moved verdict shipped: %+v", rs)
	}
	for _, change := range []string{"j0", "j6", "j1"} {
		b, tst := []*Run{rushArm("b", 1000, 200)}, []*Run{rushArm("t", 900, 0)}
		if change == "j1" {
			b, tst = []*Run{j1Arm("b", 500, 2, 0, true)}, []*Run{j1Arm("t", 500, 2, 0, true)}
		}
		if rs, _ := Change(change, b, tst, nil); !Ships(rs) {
			t.Fatalf("%s: the clean arm should ship: %+v", change, rs)
		}
		tst[0].Records[0].Verdict = "TLE"
		if rs, _ := Change(change, b, tst, nil); Ships(rs) {
			t.Errorf("%s: a moved verdict shipped", change)
		}
	}
}

func TestMissingAndExtraPrograms(t *testing.T) {
	base := []*Run{heavyArm("b", 1000)}
	missing := heavyArm("t", 100)
	missing.Records = missing.Records[:3] // the plain program was not judged
	if rs, _ := Change("j5", base, []*Run{missing}, nil); Ships(rs) {
		t.Errorf("a missing program shipped: %+v", rs)
	}
	extra := heavyArm("t", 100)
	extra.Records = append(extra.Records, Record{Program: "new", Expected: "AC", Verdict: "AC"})
	if rs, _ := Change("j5", base, []*Run{extra}, nil); Ships(rs) {
		t.Errorf("an extra program shipped: %+v", rs)
	}
	unjudged := heavyArm("t", 100)
	unjudged.Records[3].Verdict = ""
	if rs, _ := Change("j5", base, []*Run{unjudged}, nil); Ships(rs) {
		t.Errorf("a program without a verdict shipped: %+v", rs)
	}
}

func TestUnknownChange(t *testing.T) {
	if _, err := Change("j9", nil, nil, nil); err == nil {
		t.Error("unknown change accepted")
	}
}

func TestStats(t *testing.T) {
	if median([]float64{3, 1, 2}) != 2 || median([]float64{1, 2, 3, 4}) != 2.5 || median(nil) != 0 {
		t.Error("median")
	}
	if p95([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) != 10 || p95(nil) != 0 {
		t.Error("p95")
	}
	if got := spread([]float64{90, 110}); got < 0.199 || got > 0.201 {
		t.Errorf("spread = %v", got)
	}
	if spread([]float64{5}) != 0 || spread([]float64{0, 0}) != 0 {
		t.Error("spread of one or zeros")
	}
}

func TestLoadRunAndPrint(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("verdicts.jsonl", `{"seq":1,"program":"p","kind":"submit","graded":true,"expected":"AC","verdict":"AC","id":9}`+"\n")
	write("judge.log", "Nov 13 judge[1]: "+`{"msg":"job timing","kind":"submit","subject_id":9,"compile_wall_ms":800,"compiles":1,"slowest_cpu_ms":40}`+"\n"+
		`{"msg":"other"}`+"\nnot json\n")
	write("sampler.log", `{"slice_oom_kills":2}`+"\n"+`{"slice_oom_kills":5}`+"\n")
	r, err := loadRun(dir)
	if err != nil {
		t.Fatal(err)
	}
	tm, ok := r.timingOf(r.Records[0])
	if !ok || tm.CompileWallMS != 800 || tm.SlowestCPUMS != 40 || !r.HasSampler || r.OOMKills != 3 {
		t.Errorf("run %+v timing %+v", r, tm)
	}
	if _, err := loadRun(t.TempDir()); err == nil {
		t.Error("an empty directory loaded")
	}
	var out bytes.Buffer
	printResults(&out, "J5", []Result{pass("a", "x"), fail("b", "y"), {Name: "c", Skip: true}})
	for _, want := range []string{"PASS  a: x", "FAIL  b: y", "SKIP  c", "RESULT J5: does not ship"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// A sampler counter that restarts must not hide kills: only rises count.
func TestLoadRunOOMKillsSurviveACounterReset(t *testing.T) {
	dir := writeRunDir(t, "", "")
	os.WriteFile(filepath.Join(dir, "sampler.log"), []byte(`{"slice_oom_kills":2}`+"\n"+`{"slice_oom_kills":5}`+"\n"+`{"slice_oom_kills":0}`+"\n"+`{"slice_oom_kills":4}`+"\n"), 0o644)
	r, err := loadRun(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.OOMKills != 7 {
		t.Errorf("kills %d, want 7 (3 + 4 after the reset)", r.OOMKills)
	}
}

func TestJ1NeedsTheSamplerOnEveryTestRun(t *testing.T) {
	base := []*Run{j1Arm("b1", 500, 2, 0, true)}
	for name, test := range map[string][]*Run{
		"kills in the second run": {j1Arm("t1", 500, 2, 0, false), j1Arm("t2", 500, 2, 1, true)},
	} {
		rs, _ := Change("j1", base, test, nil)
		if Ships(rs) {
			t.Errorf("%s shipped: %+v", name, rs)
		}
	}
	rs, _ := Change("j1", base, []*Run{j1Arm("t1", 500, 2, 0, true), j1Arm("t2", 500, 2, 0, false)}, nil)
	if r := find(t, rs, "out-of-memory"); !r.Skip {
		t.Errorf("a test run without sampler lines was counted as checked: %+v", r)
	}
}

func TestLoadRunRefusesBadInput(t *testing.T) {
	good := `{"seq":1,"program":"p","kind":"submit","verdict":"AC","id":9}` + "\n"
	tm := `{"msg":"job timing","kind":"submit","subject_id":9,"compile_wall_ms":800}` + "\n"
	if _, err := loadRun(writeRunDir(t, good+"{broken\n", tm)); err == nil || !strings.Contains(err.Error(), "verdicts.jsonl") {
		t.Errorf("an unparsable verdict line: %v", err)
	}
	if _, err := loadRun(writeRunDir(t, good, tm+tm)); err == nil || !strings.Contains(err.Error(), "judge.log") || !strings.Contains(err.Error(), "submit/9") {
		t.Errorf("a repeated timing key: %v", err)
	}
	if _, err := loadRun(writeRunDir(t, good, tm)); err != nil {
		t.Errorf("good run: %v", err)
	}
}

func TestArmsNeedRuns(t *testing.T) {
	r := mkRun("r", []job{{program: "p"}})
	if err := checkArms([]*Run{r}, []*Run{r}); err != nil {
		t.Errorf("both arms present: %v", err)
	}
	if err := checkArms(nil, []*Run{r}); err == nil || !strings.Contains(err.Error(), "-base") {
		t.Errorf("no base runs: %v", err)
	}
	if err := checkArms([]*Run{r}, nil); err == nil || !strings.Contains(err.Error(), "-test") {
		t.Errorf("no test runs: %v", err)
	}
}

func writeRunDir(t *testing.T, verdicts, judge string) string {
	t.Helper()
	dir := t.TempDir()
	if verdicts == "" {
		verdicts = `{"seq":1,"program":"p","kind":"submit","verdict":"AC","id":9}` + "\n"
	}
	os.WriteFile(filepath.Join(dir, "verdicts.jsonl"), []byte(verdicts), 0o644)
	os.WriteFile(filepath.Join(dir, "judge.log"), []byte(judge), 0o644)
	return dir
}
