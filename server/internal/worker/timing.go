package worker

import (
	"log/slog"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// jobTiming collects where one judged job spent its time, for the single
// "job timing" log line written when the job ends. It is read and written
// by the one lane that runs the job, so it needs no lock.
type jobTiming struct {
	kind, language string
	lane           int
	started        time.Time
	// Compiles of the program and the checker: how many, how many came from
	// the cache, and the time spent in them.
	compiles, compilesCached   int
	compileWall, compileLock   time.Duration
	setup, cleanup, reportWall time.Duration
	// Every program run, tests and custom checker alike.
	tests, checkerRuns      int
	testsWall, checkerWall  time.Duration
	slowestCPU, slowestWall time.Duration
	slowestLimit            time.Duration
}

func (t *jobTiming) addStep(s judge.StepTiming) {
	t.compileLock += s.LockWait
	t.setup += s.Setup
	t.cleanup += s.Cleanup
}

// compile records one program or checker compile.
func (t *jobTiming) compile(res judge.CompileResult) {
	t.compiles++
	if res.Cached {
		t.compilesCached++
	}
	t.compileWall += res.Wall
	t.addStep(res.Timing)
}

// test records one run of the student's program against limit, the CPU
// limit before language adjustment.
func (t *jobTiming) test(res judge.ExecResult, limit time.Duration) {
	t.tests++
	t.testsWall += res.Meta.WallTime
	t.addStep(res.Timing)
	if res.Meta.Time >= t.slowestCPU {
		t.slowestCPU, t.slowestWall, t.slowestLimit = res.Meta.Time, res.Meta.WallTime, limit
	}
}

// checker records one run of a custom checker.
func (t *jobTiming) checker(res judge.CheckerResult) {
	t.checkerRuns++
	t.checkerWall += res.Meta.WallTime
}

// log writes the job's timing line. Durations are milliseconds.
func (t *jobTiming) log(l *slog.Logger, jobID int64, total time.Duration) {
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	l.Info("job timing", "job_id", jobID, "kind", t.kind, "language", t.language, "lane", t.lane,
		"total_ms", ms(total),
		"compiles", t.compiles, "compiles_cached", t.compilesCached,
		"compile_wall_ms", ms(t.compileWall), "lock_wait_ms", ms(t.compileLock),
		"setup_ms", ms(t.setup), "cleanup_ms", ms(t.cleanup),
		"tests", t.tests, "tests_wall_ms", ms(t.testsWall),
		"slowest_cpu_ms", ms(t.slowestCPU), "slowest_wall_ms", ms(t.slowestWall), "slowest_limit_ms", ms(t.slowestLimit),
		"checker_runs", t.checkerRuns, "checker_wall_ms", ms(t.checkerWall),
		"report_ms", ms(t.reportWall))
}
