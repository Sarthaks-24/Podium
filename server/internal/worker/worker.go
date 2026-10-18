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

	mu    sync.Mutex
	holds map[int64]*[]func()
}

// Run claims and runs jobs on every lane until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for lane := 0; lane < w.Lanes; lane++ {
		// A box left behind by a killed worker would make Init fail.
		if err := w.Runner.Iso.Cleanup(ctx, lane); err != nil {
			w.Log.Warn("box cleanup failed", "box", lane, "err", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.lane(ctx, lane)
		}()
	}
	wg.Wait()
}

func (w *Worker) lane(ctx context.Context, box int) {
	backoff := idlePoll
	for ctx.Err() == nil {
		claim, err := w.Client.Claim(ctx, judgeapi.ClaimRequest{
			Kinds:  []string{judgeapi.KindRun, judgeapi.KindSubmit, judgeapi.KindGenerate, judgeapi.KindValidate},
			Worker: w.Name, Version: w.Version, Isolate: w.Isolate,
		})
		switch {
		case err != nil:
			if ctx.Err() == nil {
				w.Log.Warn("claim failed", "lane", box, "err", err, "retry_in", backoff)
			}
			sleep(ctx, backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		case claim == nil:
			backoff = idlePoll
			// Jitter keeps lanes from polling in step.
			sleep(ctx, idlePoll+time.Duration(rand.Int64N(int64(time.Second))))
			continue
		}
		backoff = idlePoll
		w.Log.Info("job started", "lane", box, "job_id", claim.JobID, "kind", claim.Kind)
		start := time.Now()
		switch claim.Kind {
		case judgeapi.KindValidate:
			err = w.validate(ctx, box, claim)
		case judgeapi.KindSubmit:
			err = w.submit(ctx, box, claim)
		case judgeapi.KindRun:
			err = w.run(ctx, box, claim)
		case judgeapi.KindGenerate:
			err = w.generate(ctx, box, claim)
		default:
			err = errors.New("unknown job kind " + claim.Kind)
		}
		switch {
		case errors.Is(err, ErrLease):
			w.Log.Warn("lease lost; job dropped", "lane", box, "job_id", claim.JobID)
		case err != nil && ctx.Err() == nil:
			w.Log.Error("job failed", "lane", box, "job_id", claim.JobID, "err", err)
		default:
			w.Log.Info("job finished", "lane", box, "job_id", claim.JobID, "seconds", time.Since(start).Seconds())
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
