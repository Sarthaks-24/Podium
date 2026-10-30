// Package schedule runs the server's background jobs from one table and
// lets shutdown wait for them, so no job is still writing when the
// database closes.
package schedule

import (
	"context"
	"sync"
	"time"
)

// Task is one background job.
type Task struct {
	Name string
	// Every is the wait between the end of one run and the start of the
	// next.
	Every time.Duration
	// Delay is the wait before the first run; zero runs it at start.
	Delay time.Duration
	// Run does one pass. Its context ends when the scheduler stops; a pass
	// then finishes the transaction it is in and returns.
	Run func(ctx context.Context)
}

// Scheduler runs each task on its own goroutine until Stop.
type Scheduler struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Start runs the tasks.
func Start(tasks []Task) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Scheduler{cancel: cancel}
	for _, t := range tasks {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			loop(ctx, t)
		}()
	}
	return s
}

func loop(ctx context.Context, t Task) {
	timer := time.NewTimer(t.Delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		t.Run(ctx)
		timer.Reset(t.Every)
	}
}

// Stop ends the runs' context, so no new run starts, and waits up to
// timeout for the runs in progress to return. It reports whether they
// all did.
func (s *Scheduler) Stop(timeout time.Duration) bool {
	s.cancel()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}
