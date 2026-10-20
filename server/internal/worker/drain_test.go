package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/httpapi"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// drainKind is a job kind with no tests to check; it only closes.
type drainKind struct{}

func (drainKind) CheckReport(json.RawMessage, judgeapi.Report) error { return nil }
func (drainKind) Blobs(json.RawMessage) ([]string, error)            { return nil, nil }
func (drainKind) MaxDuration(json.RawMessage) time.Duration          { return 10 * time.Minute }
func (drainKind) Finish(_ context.Context, _ *sql.Tx, job *jobs.Job) error {
	job.Result.Summary = json.RawMessage(`{}`)
	return nil
}

const drainToken = "drain-test-token-drain-test-token-0001"

// drainRig is a real queue behind the real judge API, and a one-lane
// worker whose job blocks until the test lets it finish.
type drainRig struct {
	jobs    *jobs.Service
	now     atomic.Int64 // the queue's clock, Unix ms
	claims  atomic.Int32
	w       *Worker
	started chan int64
	release chan struct{}
	jobCtx  chan context.Context
}

func newDrainRig(t *testing.T) *drainRig {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	r := &drainRig{started: make(chan int64, 4), release: make(chan struct{}), jobCtx: make(chan context.Context, 4)}
	r.now.Store(time.Now().UnixMilli())
	r.jobs = &jobs.Service{DB: d, Kinds: map[string]jobs.Kind{judgeapi.KindValidate: drainKind{}},
		Now: func() time.Time { return time.UnixMilli(r.now.Load()) }}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := httpapi.NewJudge(httpapi.JudgeDeps{Log: log, Jobs: r.jobs, Blobs: problems.Blobs{Dir: t.TempDir()}, Token: drainToken})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/judge/claim" {
			r.claims.Add(1)
		}
		api.ServeHTTP(w, req)
	}))
	t.Cleanup(srv.Close)
	client := &Client{BaseURL: srv.URL, Token: drainToken, HTTP: srv.Client()}
	r.w = &Worker{Client: client, Log: log, Lanes: 1, Name: "drain-test", Version: "test"}
	r.w.handle = func(ctx context.Context, _ int, c *judgeapi.Claim) error {
		r.jobCtx <- ctx
		r.started <- c.JobID
		select {
		case <-r.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return client.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true})
	}
	return r
}

func (r *drainRig) enqueue(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := r.jobs.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id, err := r.jobs.Enqueue(ctx, tx, judgeapi.KindValidate, 10, map[string]int{"p": 1}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func (r *drainRig) state(t *testing.T, id int64) string {
	t.Helper()
	j, err := r.jobs.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j.State
}

// start runs Drain in the background and waits until the lane holds a job.
func (r *drainRig) start(t *testing.T, stop, jobCtx context.Context) (done chan struct{}, job context.Context) {
	t.Helper()
	done = make(chan struct{})
	go func() { r.w.Drain(stop, jobCtx); close(done) }()
	select {
	case <-r.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never started the job")
	}
	return done, <-r.jobCtx
}

// SIGTERM on the judge: no new claims, the running job finishes and is
// reported, and nothing goes back to the queue.
func TestStopFinishesAndReportsTheRunningJob(t *testing.T) {
	r := newDrainRig(t)
	first := r.enqueue(t)
	second := r.enqueue(t)

	stop, stopClaims := context.WithCancel(context.Background())
	jobCtx, abandon := context.WithCancel(context.Background())
	defer abandon()
	done, job := r.start(t, stop, jobCtx)
	claimsAtStop := r.claims.Load()
	stopClaims()

	select {
	case <-done:
		t.Fatal("Drain returned while a job was still running")
	case <-time.After(300 * time.Millisecond):
	}
	if job.Err() != nil {
		t.Fatal("stopping cancelled the running job's context")
	}
	close(r.release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Drain did not return after the job finished")
	}
	if s := r.state(t, first); s != jobs.Done {
		t.Errorf("the job running at the stop is %q, want %q (reported, not requeued)", s, jobs.Done)
	}
	if s := r.state(t, second); s != jobs.Queued {
		t.Errorf("a job waiting at the stop is %q, want %q (never claimed)", s, jobs.Queued)
	}
	if n := r.claims.Load(); n != claimsAtStop {
		t.Errorf("the worker claimed %d more times after the stop", n-claimsAtStop)
	}
	// Nothing for the lease sweep to requeue, however late it runs.
	r.now.Add((2 * jobs.LeaseTTL).Milliseconds())
	if requeued, failed, err := r.jobs.Sweep(context.Background()); err != nil || requeued != 0 || failed != 0 {
		t.Errorf("sweep after a graceful stop: requeued %d failed %d err %v", requeued, failed, err)
	}
}

// A forced stop (second signal, or systemd's SIGKILL after TimeoutStopSec)
// abandons the job without a report; the sweep requeues it once the lease
// runs out, as after a crash.
func TestForcedStopRequeuesAfterTheLease(t *testing.T) {
	r := newDrainRig(t)
	id := r.enqueue(t)

	stop, stopClaims := context.WithCancel(context.Background())
	jobCtx, abandon := context.WithCancel(context.Background())
	done, _ := r.start(t, stop, jobCtx)
	stopClaims()
	abandon()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Drain did not return after a forced stop")
	}
	if s := r.state(t, id); s != jobs.Leased {
		t.Fatalf("after a forced stop the job is %q, want %q until its lease runs out", s, jobs.Leased)
	}
	if requeued, _, _ := r.jobs.Sweep(context.Background()); requeued != 0 {
		t.Fatal("the sweep requeued a job whose lease had not run out")
	}
	r.now.Add((jobs.LeaseTTL + time.Second).Milliseconds())
	if requeued, failed, err := r.jobs.Sweep(context.Background()); err != nil || requeued != 1 || failed != 0 {
		t.Fatalf("sweep after the lease: requeued %d failed %d err %v", requeued, failed, err)
	}
	if s := r.state(t, id); s != jobs.Queued {
		t.Errorf("after the lease the job is %q, want %q", s, jobs.Queued)
	}
}
