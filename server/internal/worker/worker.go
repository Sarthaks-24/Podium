package worker

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Polling and back-off when the queue is empty or the server is away.
const (
	idlePoll   = 2 * time.Second
	maxBackoff = 30 * time.Second
)

// Worker runs judge lanes. Each lane is one isolate box (box id = lane
// number) and handles one job at a time.
type Worker struct {
	Runner *judge.Runner
	Client *Client
	Cache  *Cache
	// Compiled caches compiled programs across jobs.
	Compiled *CompileCache
	Log      *slog.Logger
	Lanes    int
	// WorkDir holds compiled programs and outputs while a job runs.
	WorkDir string
	// Name, Version and Isolate identify this worker in the server's log.
	Name, Version, Isolate string
	// Toolchain is the hash the server returned for this host's
	// toolchain report; each claim carries it.
	Toolchain string

	// handle replaces the job dispatch in tests; nil runs the real kinds.
	handle func(ctx context.Context, box int, c *judgeapi.Claim) error

	mu    sync.Mutex
	holds map[int64]*[]func()
	fatal error
	halt  context.CancelFunc
}

// stopFor ends claiming on every lane for good: the server speaks another
// protocol, which no retry fixes.
func (w *Worker) stopFor(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fatal == nil {
		w.fatal = err
		w.Log.Error("stopping: the server and this judge differ", "err", err)
	}
	if w.halt != nil {
		w.halt()
	}
}

// Run claims and runs jobs on every lane until ctx ends. Ending ctx also
// abandons the jobs that are running; use Drain to let them finish.
func (w *Worker) Run(ctx context.Context) error { return w.Drain(ctx, ctx) }

// Drain claims and runs jobs on every lane until stop ends. Then each lane
// finishes the job it holds, reports it, and Drain returns once every lane
// is idle. This is how an upgrade drains the judge: there is no global
// pause, so stopping the judge is the drain.
//
// Jobs run, heartbeat and report under jobs, never under stop, so stopping
// does not kill isolate or the closing report. Ending jobs abandons the
// running jobs without a final report: the server hands each out again
// once its lease runs out, exactly as after a crash.
//
// Drain returns a *ProtocolError when the server refused this judge's
// protocol; every lane then stopped claiming as if stop had ended.
func (w *Worker) Drain(stop, jobs context.Context) error {
	stop, halt := context.WithCancel(stop)
	defer halt()
	w.mu.Lock()
	w.halt = halt
	w.mu.Unlock()
	var wg sync.WaitGroup
	for lane := 0; lane < w.Lanes; lane++ {
		// A box left behind by a killed worker would make Init fail.
		if w.Runner != nil {
			if err := w.Runner.Iso.Cleanup(jobs, lane); err != nil {
				w.Log.Warn("box cleanup failed", "box", lane, "err", err)
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.lane(stop, jobs, lane)
		}()
	}
	wg.Wait()
	// Leave no box behind: after a forced stop a lane's box may still hold
	// a program's files (tmpfs, so memory) until the next start cleans it.
	// Cleanup runs even when jobs was cancelled (a forced stop).
	if w.Runner != nil {
		for lane := 0; lane < w.Lanes; lane++ {
			if err := w.Runner.Iso.Cleanup(jobs, lane); err != nil {
				w.Log.Warn("box cleanup at stop failed", "box", lane, "err", err)
			}
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fatal
}

func (w *Worker) lane(stop, ctx context.Context, box int) {
	backoff := idlePoll
	for stop.Err() == nil && ctx.Err() == nil {
		// The claim itself runs under the job context: a stop that lands
		// while the request is in flight must not drop a job the server
		// has already leased to this lane.
		claim, err := w.Client.Claim(ctx, judgeapi.ClaimRequest{
			Kinds:  []string{judgeapi.KindRun, judgeapi.KindSubmit, judgeapi.KindGenerate, judgeapi.KindGenerateInputs, judgeapi.KindValidate},
			Worker: w.Name, Version: w.Version, Isolate: w.Isolate, Toolchain: w.Toolchain,
		})
		var protocol *ProtocolError
		switch {
		case errors.As(err, &protocol):
			w.stopFor(protocol)
			return
		case err != nil:
			if ctx.Err() == nil {
				w.Log.Warn("claim failed", "lane", box, "err", err, "retry_in", backoff)
			}
			sleep(stop, backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		case claim == nil:
			backoff = idlePoll
			// Jitter keeps lanes from polling in step.
			sleep(stop, idlePoll+time.Duration(rand.Int64N(int64(time.Second))))
			continue
		}
		backoff = idlePoll
		w.Log.Info("job started", "lane", box, "job_id", claim.JobID, "kind", claim.Kind)
		start := time.Now()
		err = w.dispatch(ctx, box, claim)
		switch {
		case errors.Is(err, ErrLease):
			w.Log.Warn("lease lost; job dropped", "lane", box, "job_id", claim.JobID)
		case ctx.Err() != nil:
			w.Log.Warn("job abandoned by a forced stop; the server requeues it after its lease", "lane", box, "job_id", claim.JobID)
		case err != nil:
			w.Log.Error("job failed", "lane", box, "job_id", claim.JobID, "err", err)
		default:
			w.Log.Info("job finished", "lane", box, "job_id", claim.JobID, "seconds", time.Since(start).Seconds())
		}
	}
}

func (w *Worker) dispatch(ctx context.Context, box int, claim *judgeapi.Claim) error {
	if w.handle != nil {
		return w.handle(ctx, box, claim)
	}
	switch claim.Kind {
	case judgeapi.KindValidate:
		return w.validate(ctx, box, claim)
	case judgeapi.KindSubmit:
		return w.submit(ctx, box, claim)
	case judgeapi.KindRun:
		return w.run(ctx, box, claim)
	case judgeapi.KindGenerate:
		return w.generate(ctx, box, claim)
	case judgeapi.KindGenerateInputs:
		return w.generateInputs(ctx, box, claim)
	}
	return errors.New("unknown job kind " + claim.Kind)
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
