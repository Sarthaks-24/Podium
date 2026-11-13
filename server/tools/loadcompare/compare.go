package main

import (
	"fmt"
	"math"
	"sort"
)

// The thresholds. They were written before J5 and J6 were built so that
// nobody grades their own work afterwards. Change one only with a dated
// amendment in the plan's decision log.
const (
	// J5 ships if compile wall on the heavy C++ programs drops at least this
	// share, and verdicts are identical.
	j5MinCompileDrop = 0.30
	// J6 ships if report time is at least this share of a job's wall time in
	// the rush, and it makes the jobs no slower.
	j6MinReportShare = 0.10
	// J0, J1 and J6 compare a figure that must not get worse. The allowed
	// worsening is the A/A noise of that figure, but never less than this.
	noiseFloor = 0.05
	// J1: p95 CPU time of a program must stay within its A/A noise, but
	// never less than this share, plus cpuSlackMS (the judge reports times
	// in whole milliseconds and a Run in 10 ms steps).
	cpuNoiseFloor = 0.10
	cpuSlackMS    = 20
	// J5 and J6 must beat the A/A noise by this factor as well as the
	// threshold, so a lucky pair cannot ship a change.
	noiseFactor = 2.0
)

// Record is one request of the replay corpus, as the replay driver wrote it.
type Record struct {
	Seq         int    `json:"seq"`
	Program     string `json:"program"`
	Kind        string `json:"kind"` // run or submit
	Language    string `json:"language"`
	Graded      bool   `json:"graded"` // a submission that counts
	Heavy       bool   `json:"heavy"`  // C++ that includes bits/stdc++.h
	NearLimit   bool   `json:"near_limit"`
	Rush        bool   `json:"rush"` // sent in the burst at the end
	Expected    string `json:"expected"`
	Verdict     string `json:"verdict"`
	ID          int64  `json:"id"`
	SubmittedMS int64  `json:"submitted_ms"`
	DoneMS      int64  `json:"done_ms"`
}

// Timing is one "job timing" line of the judge.
type Timing struct {
	Kind           string  `json:"kind"`
	SubjectID      int64   `json:"subject_id"`
	TotalMS        float64 `json:"total_ms"`
	Compiles       int     `json:"compiles"`
	CompilesCached int     `json:"compiles_cached"`
	CompileWallMS  float64 `json:"compile_wall_ms"`
	ReportMS       float64 `json:"report_ms"`
	SlowestCPUMS   float64 `json:"slowest_cpu_ms"`
}

// Run is everything one replay run left behind.
type Run struct {
	Name       string
	Records    []Record
	Timing     map[string]Timing // key: kind/subject_id
	HasSampler bool
	OOMKills   int64 // sandbox out-of-memory kills during the run
}

func timingKey(kind string, id int64) string { return fmt.Sprintf("%s/%d", kind, id) }

// Result is one pass or fail line.
type Result struct {
	Name   string
	Pass   bool
	Skip   bool // not checked; does not fail
	Detail string
}

func fail(name, f string, a ...any) Result { return Result{Name: name, Detail: fmt.Sprintf(f, a...)} }
func pass(name, f string, a ...any) Result {
	return Result{Name: name, Pass: true, Detail: fmt.Sprintf(f, a...)}
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// p95 is the nearest-rank 95th percentile.
func p95(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[int(math.Ceil(0.95*float64(len(s))))-1]
}

// spread is how far the runs of an A/A pair (or triple) differ, as a share
// of their mean: the noise of a figure. Fewer than two runs give 0.
func spread(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	lo, hi, sum := v[0], v[0], 0.0
	for _, x := range v {
		lo, hi, sum = math.Min(lo, x), math.Max(hi, x), sum+x
	}
	if sum == 0 {
		return 0
	}
	return (hi - lo) / (sum / float64(len(v)))
}

// perRun applies stat to each run and returns the figures in run order.
func perRun(runs []*Run, stat func(*Run) float64) []float64 {
	out := make([]float64, len(runs))
	for i, r := range runs {
		out[i] = stat(r)
	}
	return out
}

// timingOf joins a record to its job's timing line.
func (r *Run) timingOf(rec Record) (Timing, bool) {
	t, ok := r.Timing[timingKey(rec.Kind, rec.ID)]
	return t, ok
}

// Verdicts checks the rule for every arm: no verdict of any corpus program
// moved, and no program is missing or extra.
func Verdicts(base, test []*Run) []Result {
	const name = "verdicts"
	var bad []string
	ref := map[string]int{}
	for _, rec := range base[0].Records {
		ref[rec.Program]++
	}
	for _, r := range append(append([]*Run(nil), base...), test...) {
		got := map[string]int{}
		for _, rec := range r.Records {
			got[rec.Program]++
			if rec.Verdict != rec.Expected {
				bad = append(bad, fmt.Sprintf("%s: %s seq %d expected %q got %q", r.Name, rec.Program, rec.Seq, rec.Expected, rec.Verdict))
			}
		}
		for p, n := range ref {
			if got[p] != n {
				bad = append(bad, fmt.Sprintf("%s: %s judged %d times, the reference run %d (a missing program)", r.Name, p, got[p], n))
			}
		}
		for p := range got {
			if _, ok := ref[p]; !ok {
				bad = append(bad, fmt.Sprintf("%s: %s is not in the reference run", r.Name, p))
			}
		}
	}
	if len(bad) > 0 {
		return []Result{fail(name, "%d moved or missing, first: %s", len(bad), bad[0])}
	}
	return []Result{pass(name, "every program in every run has its expected verdict, same counts in all runs")}
}

// heavyCompile returns the compile wall of the heavy C++ jobs that really
// built (a cache hit adds nothing). missing counts records without a
// timing line.
func heavyCompile(r *Run) (walls []float64, missing int) {
	for _, rec := range r.Records {
		if !rec.Heavy {
			continue
		}
		t, ok := r.timingOf(rec)
		if !ok {
			missing++
			continue
		}
		if t.Compiles > t.CompilesCached {
			walls = append(walls, t.CompileWallMS)
		}
	}
	return
}

// J5 ships if compile wall on the heavy C++ programs drops at least 30
// percent, beyond the A/A noise, and verdicts are identical.
func J5(base, test, aa []*Run) []Result {
	const name = "J5 compile wall of heavy C++"
	stat := func(r *Run) float64 { w, _ := heavyCompile(r); return median(w) }
	for _, r := range append(append([]*Run(nil), base...), test...) {
		w, missing := heavyCompile(r)
		if missing > 0 {
			return []Result{fail(name, "%s: %d heavy records have no timing line", r.Name, missing)}
		}
		if len(w) == 0 {
			return []Result{fail(name, "%s: no heavy C++ job compiled (all cached, or none in the corpus)", r.Name)}
		}
	}
	b, t := median(perRun(base, stat)), median(perRun(test, stat))
	noise := spread(perRun(aa, stat))
	need := math.Max(j5MinCompileDrop, noiseFactor*noise)
	drop := 1 - t/b
	d := fmt.Sprintf("median %.0f ms -> %.0f ms, drop %.1f%%, needs %.1f%% (threshold %.0f%%, A/A noise %.1f%% x %.0f)",
		b, t, 100*drop, 100*need, 100*j5MinCompileDrop, 100*noise, noiseFactor)
	if drop >= need {
		return []Result{pass(name, "%s", d)}
	}
	return []Result{fail(name, "%s", d)}
}

// rushJobs lists the timing lines of the jobs sent in the final burst.
func rushJobs(r *Run) (jobs []Timing, missing int) {
	for _, rec := range r.Records {
		if !rec.Rush {
			continue
		}
		if t, ok := r.timingOf(rec); ok {
			jobs = append(jobs, t)
		} else {
			missing++
		}
	}
	return
}

// J6 ships if report time is at least 10 percent of a job's wall time in
// the rush (read on the base arm, where J6 is off) and the jobs of the test
// arm are no slower.
func J6(base, test, aa []*Run) []Result {
	const share, slower = "J6 report share of job wall in the rush", "J6 job wall in the rush"
	var rep, tot float64
	for _, r := range base {
		jobs, missing := rushJobs(r)
		if missing > 0 || len(jobs) == 0 {
			return []Result{fail(share, "%s: %d rush jobs, %d without a timing line", r.Name, len(jobs), missing)}
		}
		for _, j := range jobs {
			rep += j.ReportMS
			tot += j.TotalMS
		}
	}
	var out []Result
	frac := rep / tot
	d := fmt.Sprintf("%.1f%% of job wall is report time, needs %.0f%%", 100*frac, 100*j6MinReportShare)
	if frac >= j6MinReportShare {
		out = append(out, pass(share, "%s", d))
	} else {
		out = append(out, fail(share, "%s", d))
	}
	stat := func(r *Run) float64 {
		jobs, _ := rushJobs(r)
		var v []float64
		for _, j := range jobs {
			v = append(v, j.TotalMS)
		}
		return median(v)
	}
	for _, r := range test {
		if jobs, missing := rushJobs(r); missing > 0 || len(jobs) == 0 {
			return append(out, fail(slower, "%s: %d rush jobs, %d without a timing line", r.Name, len(jobs), missing))
		}
	}
	b, t := median(perRun(base, stat)), median(perRun(test, stat))
	tol := math.Max(noiseFloor, spread(perRun(aa, stat)))
	d = fmt.Sprintf("median %.0f ms -> %.0f ms, may grow %.1f%%", b, t, 100*tol)
	if t <= b*(1+tol) {
		return append(out, pass(slower, "%s", d))
	}
	return append(out, fail(slower, "%s", d))
}

// drain is the time from the first rush request to the last graded verdict.
func drain(r *Run) float64 {
	var start, end int64 = math.MaxInt64, 0
	for _, rec := range r.Records {
		if rec.Rush && rec.SubmittedMS < start {
			start = rec.SubmittedMS
		}
		if rec.Graded && rec.Rush && rec.DoneMS > end {
			end = rec.DoneMS
		}
	}
	if end == 0 || start == math.MaxInt64 {
		return 0
	}
	return float64(end - start)
}

// gradedWait is the p95 time from request to verdict of the graded
// submissions sent in the rush.
func gradedWait(r *Run) float64 {
	var v []float64
	for _, rec := range r.Records {
		if rec.Graded && rec.Rush {
			v = append(v, float64(rec.DoneMS-rec.SubmittedMS))
		}
	}
	return p95(v)
}

// J0: graded submissions drain no slower, and the p95 time to a verdict
// of the graded ones is not worse. "Not worse" means within the A/A noise.
func J0(base, test, aa []*Run) []Result {
	var out []Result
	for _, c := range []struct {
		name string
		stat func(*Run) float64
	}{{"J0 drain of graded submissions", drain}, {"J0 p95 time to a verdict, graded", gradedWait}} {
		b, t := median(perRun(base, c.stat)), median(perRun(test, c.stat))
		tol := math.Max(noiseFloor, spread(perRun(aa, c.stat)))
		d := fmt.Sprintf("median %.0f ms -> %.0f ms, may grow %.1f%%", b, t, 100*tol)
		switch {
		case b == 0 || t == 0:
			out = append(out, fail(c.name, "no graded submission in the rush"))
		case t <= b*(1+tol):
			out = append(out, pass(c.name, "%s", d))
		default:
			out = append(out, fail(c.name, "%s", d))
		}
	}
	return out
}

// tleCounts counts TLE verdicts per program.
func tleCounts(r *Run) map[string]int {
	m := map[string]int{}
	for _, rec := range r.Records {
		if rec.Verdict == "TLE" {
			m[rec.Program]++
		}
	}
	return m
}

// cpuP95 is the p95 of the slowest-test CPU time of each program's jobs.
func cpuP95(r *Run) (map[string]float64, int) {
	by := map[string][]float64{}
	missing := 0
	for _, rec := range r.Records {
		t, ok := r.timingOf(rec)
		if !ok {
			missing++
			continue
		}
		by[rec.Program] = append(by[rec.Program], t.SlowestCPUMS)
	}
	out := map[string]float64{}
	for p, v := range by {
		out[p] = p95(v)
	}
	return out, missing
}

// J1 on the resized machine: the time-limit verdicts are the same set, the
// p95 CPU time of each program is within the A/A noise, and no sandbox was
// killed for memory.
func J1(base, test, aa []*Run) []Result {
	var out []Result
	const tle, cpu, oom = "J1 time-limit verdicts", "J1 p95 CPU time per program", "J1 sandbox out-of-memory kills"
	// The same set of TLE verdicts, run by run: the first base run is the reference.
	ref := tleCounts(base[0])
	bad := ""
	for _, r := range test {
		got := tleCounts(r)
		for p, n := range ref {
			if got[p] != n && bad == "" {
				bad = fmt.Sprintf("%s: %s has %d TLE, the base run %d", r.Name, p, got[p], n)
			}
		}
		for p, n := range got {
			if ref[p] != n && bad == "" {
				bad = fmt.Sprintf("%s: %s has %d TLE, the base run %d", r.Name, p, n, ref[p])
			}
		}
	}
	if bad != "" {
		out = append(out, fail(tle, "%s", bad))
	} else {
		out = append(out, pass(tle, "%d programs with a TLE verdict, identical in every run", len(ref)))
	}

	armCPU := func(runs []*Run) (map[string]float64, int) {
		per := map[string][]float64{}
		miss := 0
		for _, r := range runs {
			m, n := cpuP95(r)
			miss += n
			for p, v := range m {
				per[p] = append(per[p], v)
			}
		}
		out := map[string]float64{}
		for p, v := range per {
			out[p] = median(v)
		}
		return out, miss
	}
	b, mb := armCPU(base)
	t, mt := armCPU(test)
	if mb+mt > 0 {
		out = append(out, fail(cpu, "%d records have no timing line", mb+mt))
	} else {
		noise := map[string]float64{}
		aaPer := map[string][]float64{}
		for _, r := range aa {
			m, _ := cpuP95(r)
			for p, v := range m {
				aaPer[p] = append(aaPer[p], v)
			}
		}
		for p, v := range aaPer {
			noise[p] = spread(v)
		}
		var progs []string
		for p := range b {
			progs = append(progs, p)
		}
		sort.Strings(progs)
		worst := ""
		for _, p := range progs {
			tol := math.Max(cpuNoiseFloor, noise[p])
			if math.Abs(t[p]-b[p]) > tol*b[p]+cpuSlackMS && worst == "" {
				worst = fmt.Sprintf("%s: p95 CPU %.0f ms -> %.0f ms, allowed %.1f%% + %d ms", p, b[p], t[p], 100*tol, cpuSlackMS)
			}
		}
		if worst != "" {
			out = append(out, fail(cpu, "%s", worst))
		} else {
			out = append(out, pass(cpu, "%d programs within their A/A noise (floor %.0f%% + %d ms)", len(progs), 100*cpuNoiseFloor, cpuSlackMS))
		}
	}

	switch {
	case !test[0].HasSampler:
		out = append(out, Result{Name: oom, Skip: true, Detail: "no sampler lines: not checked here (an out-of-memory kill still shows as a moved verdict)"})
	default:
		var kills int64
		for _, r := range test {
			kills += r.OOMKills
		}
		if kills > 0 {
			out = append(out, fail(oom, "%d kills in the test runs", kills))
		} else {
			out = append(out, pass(oom, "none in the test runs"))
		}
	}
	return out
}

// Change evaluates one conditional change: its own rules, then the verdict
// rule that holds for every arm.
func Change(change string, base, test, aa []*Run) ([]Result, error) {
	var out []Result
	switch change {
	case "j0":
		out = J0(base, test, aa)
	case "j1":
		out = J1(base, test, aa)
	case "j5":
		out = J5(base, test, aa)
	case "j6":
		out = J6(base, test, aa)
	default:
		return nil, fmt.Errorf("unknown change %q: use j0, j1, j5 or j6", change)
	}
	return append(out, Verdicts(base, test)...), nil
}

// Ships is true when no result failed.
func Ships(rs []Result) bool {
	for _, r := range rs {
		if !r.Pass && !r.Skip {
			return false
		}
	}
	return true
}
