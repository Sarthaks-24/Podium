package worker

import (
	"context"
	"errors"
	"sync"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Report batching (J6). Without it a job sends each test's result and
// waits for the server's answer before the next test starts. With it
// (PODIUM_JUDGE_ASYNC_REPORTS=on) the result goes into a queue, one sender
// goroutine posts whatever has piled up while its last post was in flight,
// and the last results travel in the final report. The protocol and the
// server do not change: a report has always carried a list of results.
//
// Guarantees, each pinned by a test in async_test.go:
//   - results reach the server complete and in test order, because one
//     sender posts them in the order queued and the final report goes only
//     after it is idle;
//   - a post is capped by bytes (batchBytes), and the queue is capped too
//     (pendingBytes): above it the next result waits for the sender, so a
//     slow server slows the job as before and memory stays bounded;
//   - a lost lease or a refused report stops the job at its next result or
//     at the end: the error is kept, the queue is emptied, nothing more is
//     posted;
//   - the sender runs only while there is something to post, and drop waits
//     for it, so no goroutine outlives its job.

const (
	// asyncBatchBytes caps one report of batched results. The server takes
	// 1 MiB of report; a result is at most a few KB of output and stderr.
	asyncBatchBytes = 256 << 10
	// asyncPendingBytes caps the results waiting for a send.
	asyncPendingBytes = 1 << 20
)

type asyncQueue struct {
	// batchBytes and pendingBytes are asyncBatchBytes and
	// asyncPendingBytes; tests set smaller ones on their own queue.
	batchBytes, pendingBytes int

	mu      sync.Mutex
	cond    *sync.Cond
	pending []judgeapi.TestResult
	bytes   int   // size of pending
	sending bool  // a sender goroutine is running
	err     error // first failed send; the job is over once set
	closed  bool  // drop was called
	wg      sync.WaitGroup
}

func newAsyncQueue() *asyncQueue {
	q := &asyncQueue{batchBytes: asyncBatchBytes, pendingBytes: asyncPendingBytes}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// sizeOf estimates the bytes a result adds to a report: its text fields
// plus a fixed amount for the rest.
func sizeOf(tr judgeapi.TestResult) int {
	return 256 + len(tr.Output) + len(tr.Stderr) + len(tr.Message) + len(tr.OutputSHA)
}

// result reports one test. Off, it is sent before result returns, as it
// always was. On, it is queued, and result returns the error of an earlier
// send that failed (ErrLease for a lost lease), or waits while the queue
// is full.
func (r *reporter) result(ctx context.Context, tr judgeapi.TestResult) error {
	q := r.async
	if q == nil {
		return r.send(ctx, judgeapi.Report{Results: []judgeapi.TestResult{tr}})
	}
	stop := context.AfterFunc(ctx, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})
	defer stop()
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.err == nil && !q.closed && q.bytes >= q.pendingBytes && ctx.Err() == nil {
		q.cond.Wait()
	}
	switch {
	case q.err != nil:
		return q.err
	case q.closed:
		return context.Canceled
	case ctx.Err() != nil:
		return ctx.Err()
	}
	q.pending = append(q.pending, tr)
	q.bytes += sizeOf(tr)
	if !q.sending {
		q.sending = true
		q.wg.Add(1)
		go q.run(ctx, r)
	}
	return nil
}

// take removes up to batchBytes of the oldest results, always at least one.
// The caller holds q.mu.
func (q *asyncQueue) take() []judgeapi.TestResult {
	n, size := 0, 0
	for n < len(q.pending) {
		s := sizeOf(q.pending[n])
		if n > 0 && size+s > q.batchBytes {
			break
		}
		n, size = n+1, size+s
	}
	batch := append([]judgeapi.TestResult(nil), q.pending[:n]...)
	q.pending = q.pending[n:]
	q.bytes -= size
	if len(q.pending) == 0 {
		q.pending = nil
	}
	return batch
}

// run is the sender: it posts batches until the queue is empty or a post
// fails, then exits. At most one runs at a time.
func (q *asyncQueue) run(ctx context.Context, r *reporter) {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		if q.err != nil || q.closed || len(q.pending) == 0 {
			q.sending = false
			q.cond.Broadcast()
			q.mu.Unlock()
			return
		}
		batch := q.take()
		q.cond.Broadcast() // room for a waiting result
		q.mu.Unlock()
		if err := r.send(ctx, judgeapi.Report{Results: batch}); err != nil {
			q.mu.Lock()
			q.err, q.pending, q.bytes, q.sending = err, nil, 0, false
			q.cond.Broadcast()
			q.mu.Unlock()
			return
		}
	}
}

// asyncErr is the error of a post that failed after its test ended, or nil.
// A test loop reads it before starting a test, so a lost lease stops the
// job without running a test for nothing.
func (r *reporter) asyncErr() error {
	q := r.async
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

// idle waits until the sender has gone and returns the queue's error. The
// caller holds q.mu on entry and on return.
func (q *asyncQueue) idle(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})
	defer stop()
	for q.sending && ctx.Err() == nil {
		q.cond.Wait()
	}
	if q.err != nil {
		return q.err
	}
	return ctx.Err()
}

// flush sends the results still waiting, without a final flag. A job that
// failed calls it before its fatal report so the tests that ran are shown.
// Only a lost lease is an error: the job is over then.
func (r *reporter) flush(ctx context.Context) error {
	q := r.async
	if q == nil {
		return nil
	}
	q.mu.Lock()
	err := q.idle(ctx)
	rest := q.pending
	q.pending, q.bytes = nil, 0
	q.mu.Unlock()
	if err != nil {
		if errors.Is(err, ErrLease) {
			return err
		}
		return nil
	}
	return r.sendBatches(ctx, rest, false)
}

// finalReport ends the job. Off, it is the final report alone. On, the
// results still waiting travel in the final report, split into reports of
// at most batchBytes when there are more than one fits; only the last has
// Final set.
func (r *reporter) finalReport(ctx context.Context) error {
	q := r.async
	if q == nil {
		return r.send(ctx, judgeapi.Report{Final: true})
	}
	q.mu.Lock()
	err := q.idle(ctx)
	rest := q.pending
	q.pending, q.bytes = nil, 0
	q.mu.Unlock()
	if err != nil {
		return err
	}
	return r.sendBatches(ctx, rest, true)
}

// sendBatches posts results in reports of at most batchBytes. With final
// set the last report carries Final, even when there are no results.
func (r *reporter) sendBatches(ctx context.Context, rest []judgeapi.TestResult, final bool) error {
	if len(rest) == 0 && !final {
		return nil
	}
	q := r.async
	tmp := &asyncQueue{batchBytes: q.batchBytes, pending: rest}
	for _, tr := range rest {
		tmp.bytes += sizeOf(tr)
	}
	for first := true; first || len(tmp.pending) > 0; first = false {
		batch := tmp.take()
		if err := r.send(ctx, judgeapi.Report{Results: batch, Final: final && len(tmp.pending) == 0}); err != nil {
			return err
		}
	}
	return nil
}

// drop ends the batching of a job that is over: it discards what is waiting
// and waits for the sender to exit, which it does as soon as its post
// returns or the job's context is cancelled.
func (r *reporter) drop() {
	q := r.async
	if q == nil {
		return
	}
	q.mu.Lock()
	q.closed = true
	q.pending, q.bytes = nil, 0
	q.cond.Broadcast()
	q.mu.Unlock()
	q.wg.Wait()
}
