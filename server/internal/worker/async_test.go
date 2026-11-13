package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// reportServer records every report it gets. answer decides each one: it
// may block (a slow server) and returns the HTTP status.
type reportServer struct {
	*httptest.Server
	mu      sync.Mutex
	reports []judgeapi.Report
	sizes   []int
}

func newReportServer(t *testing.T, answer func(n int, rep judgeapi.Report) int) *reportServer {
	t.Helper()
	s := &reportServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rep judgeapi.Report
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			t.Errorf("bad report body: %v", err)
		}
		s.mu.Lock()
		s.reports = append(s.reports, rep)
		n := len(s.reports)
		s.sizes = append(s.sizes, int(r.ContentLength))
		s.mu.Unlock()
		status := http.StatusOK
		if answer != nil {
			status = answer(n, rep)
		}
		w.WriteHeader(status)
		w.Write([]byte(`{"lease_seconds":60}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *reportServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reports)
}

func (s *reportServer) snapshot() []judgeapi.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]judgeapi.Report(nil), s.reports...)
}

// results lists the test indexes of every report, in arrival order.
func (s *reportServer) results() []int {
	var idx []int
	for _, r := range s.snapshot() {
		for _, tr := range r.Results {
			idx = append(idx, tr.Idx)
		}
	}
	return idx
}

func newTestReporter(s *reportServer, batched bool) *reporter {
	c := &Client{BaseURL: s.URL, Token: "t", HTTP: s.Client()}
	r := &reporter{client: c, jobID: 1, lease: "l"}
	if batched {
		r.async = newAsyncQueue()
	}
	return r
}

func testResultOf(idx, outputBytes int) judgeapi.TestResult {
	return judgeapi.TestResult{Idx: idx, Role: judgeapi.RoleProgram, Verdict: "AC", Output: strings.Repeat("x", outputBytes)}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// noLeak fails the test when goroutines the test started are still running
// once it is over (the report server's own are closed first).
func noLeak(t *testing.T, s *reportServer) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		s.Close()
		s.Client().CloseIdleConnections()
		for i := 0; i < 200; i++ {
			if runtime.NumGoroutine() <= before {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		buf := make([]byte, 1<<16)
		t.Errorf("goroutines %d -> %d:\n%s", before, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
	})
}

func TestBatchedResultsAreCompleteAndInOrder(t *testing.T) {
	s := newReportServer(t, func(int, judgeapi.Report) int { time.Sleep(4 * time.Millisecond); return 200 })
	noLeak(t, s)
	r := newTestReporter(s, true)
	ctx := context.Background()
	const n = 200
	for i := 0; i < n; i++ {
		if err := r.result(ctx, testResultOf(i, 50)); err != nil {
			t.Fatalf("result %d: %v", i, err)
		}
		time.Sleep(time.Millisecond) // a test running
	}
	if err := r.finalReport(ctx); err != nil {
		t.Fatal(err)
	}
	r.drop()
	idx := s.results()
	if len(idx) != n {
		t.Fatalf("%d results arrived, want %d", len(idx), n)
	}
	for i, v := range idx {
		if v != i {
			t.Fatalf("result %d arrived as test %d: out of order", i, v)
		}
	}
	reports := s.snapshot()
	if len(reports) >= n || len(reports) < 2 {
		t.Errorf("%d reports for %d results: not batched", len(reports), n)
	}
	for i, rep := range reports {
		if rep.Final != (i == len(reports)-1) {
			t.Errorf("report %d final = %v", i, rep.Final)
		}
	}
}

// While one send is in flight the results pile up and go as one report.
func TestBatchedResultsGoTogetherWhileASendIsInFlight(t *testing.T) {
	release := make(chan struct{})
	s := newReportServer(t, func(n int, _ judgeapi.Report) int {
		if n == 1 {
			<-release
		}
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if err := r.result(ctx, testResultOf(i, 10)); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			// Result 0 is the one in flight; the rest pile up behind it.
			waitFor(t, "the first report to arrive", func() bool { return s.count() == 1 })
		}
	}
	close(release)
	if err := r.finalReport(ctx); err != nil {
		t.Fatal(err)
	}
	r.drop()
	reports := s.snapshot()
	if len(reports) != 3 || len(reports[0].Results) != 1 || len(reports[1].Results) != 9 || !reports[2].Final || len(reports[2].Results) != 0 {
		var shape []int
		for _, rep := range reports {
			shape = append(shape, len(rep.Results))
		}
		t.Errorf("reports hold %v results; want 1, 9 and a final one", shape)
	}
}

func TestBatchIsCappedByBytes(t *testing.T) {
	release := make(chan struct{})
	s := newReportServer(t, func(n int, _ judgeapi.Report) int {
		if n == 1 {
			<-release
		}
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	r.async.batchBytes = 10_000
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		if err := r.result(ctx, testResultOf(i, 4000)); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			waitFor(t, "the first report", func() bool { return s.count() == 1 })
		}
	}
	close(release)
	if err := r.finalReport(ctx); err != nil {
		t.Fatal(err)
	}
	r.drop()
	for i, rep := range s.snapshot() {
		if len(rep.Results) > 2 {
			t.Errorf("report %d holds %d results of 4000 bytes under a cap of 10000", i, len(rep.Results))
		}
	}
	if got := len(s.results()); got != 8 {
		t.Errorf("%d results arrived, want 8", got)
	}
}

func TestBatchedLostLeaseDropsCleanly(t *testing.T) {
	s := newReportServer(t, func(n int, _ judgeapi.Report) int {
		if n >= 2 {
			return http.StatusConflict
		}
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	ctx := context.Background()
	if err := r.result(ctx, testResultOf(0, 10)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first report", func() bool { return s.count() == 1 })
	_ = r.result(ctx, testResultOf(1, 10)) // this one is refused with 409
	waitFor(t, "the lease loss to be noticed", func() bool { return errors.Is(r.asyncErr(), ErrLease) })
	at := s.count()
	if err := r.result(ctx, testResultOf(2, 10)); !errors.Is(err, ErrLease) {
		t.Errorf("a result after the loss returned %v, want ErrLease", err)
	}
	if err := r.finalReport(ctx); !errors.Is(err, ErrLease) {
		t.Errorf("the final report returned %v, want ErrLease", err)
	}
	if err := r.send(ctx, judgeapi.Report{}); !errors.Is(err, ErrLease) {
		t.Errorf("a heartbeat after the loss returned %v", err)
	}
	r.drop()
	time.Sleep(50 * time.Millisecond)
	if s.count() != at {
		t.Errorf("%d reports were sent after the lease was lost", s.count()-at)
	}
}

func TestBatchedRefusedReportStopsTheJob(t *testing.T) {
	s := newReportServer(t, func(n int, _ judgeapi.Report) int {
		if n == 2 {
			return http.StatusBadRequest
		}
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	ctx := context.Background()
	_ = r.result(ctx, testResultOf(0, 10))
	waitFor(t, "first report", func() bool { return s.count() == 1 })
	_ = r.result(ctx, testResultOf(1, 10))
	waitFor(t, "the refusal", func() bool { return r.asyncErr() != nil })
	var refused *refusedError
	if err := r.result(ctx, testResultOf(2, 10)); !errors.As(err, &refused) {
		t.Errorf("result after a refusal returned %v", err)
	}
	r.async.mu.Lock()
	held := r.async.bytes
	r.async.mu.Unlock()
	if held != 0 {
		t.Errorf("%d bytes still held after the job failed", held)
	}
	if err := r.finalReport(ctx); !errors.As(err, &refused) {
		t.Errorf("final report returned %v", err)
	}
	r.drop()
}

// A heartbeat still renews the lease while a test runs and while a send is
// in flight.
func TestBatchedHeartbeatStillRenewsTheLease(t *testing.T) {
	s := newReportServer(t, func(_ int, rep judgeapi.Report) int {
		if len(rep.Results) > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	r.beat = 30 * time.Millisecond
	ctx, stop := context.WithCancel(context.Background())
	hbDone := make(chan struct{})
	go func() { r.heartbeat(ctx); close(hbDone) }()
	if err := r.result(ctx, testResultOf(0, 10)); err != nil { // a slow send starts
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond) // the next test runs
	stop()
	<-hbDone
	beats := 0
	for _, rep := range s.snapshot() {
		if len(rep.Results) == 0 && !rep.Final {
			beats++
		}
	}
	if beats < 4 {
		t.Errorf("%d heartbeats in 500 ms at a 30 ms interval", beats)
	}
	r.drop()
}

// Progress is what the server counts from the results it has: each report
// must continue where the last stopped.
func TestBatchedProgressNeverMovesBack(t *testing.T) {
	var last atomic.Int64
	last.Store(-1)
	var bad atomic.Int32
	s := newReportServer(t, func(_ int, rep judgeapi.Report) int {
		for _, tr := range rep.Results {
			if int64(tr.Idx) != last.Load()+1 {
				bad.Add(1)
			}
			last.Store(int64(tr.Idx))
		}
		time.Sleep(2 * time.Millisecond)
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		if err := r.result(ctx, testResultOf(i, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.finalReport(ctx); err != nil {
		t.Fatal(err)
	}
	r.drop()
	if bad.Load() != 0 || last.Load() != 99 {
		t.Errorf("%d results were not the next one; progress ended at %d", bad.Load(), last.Load())
	}
}

// A server that answers slowly makes the next test wait instead of the
// judge holding results without bound; nothing is lost.
func TestBatchedSlowServerBoundsWhatIsHeld(t *testing.T) {
	release := make(chan struct{})
	s := newReportServer(t, func(int, judgeapi.Report) int { <-release; return 200 })
	noLeak(t, s)
	r := newTestReporter(s, true)
	r.async.batchBytes, r.async.pendingBytes = 4000, 8000
	ctx := context.Background()
	var queued atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			if err := r.result(ctx, testResultOf(i, 1000)); err != nil {
				t.Error(err)
				return
			}
			queued.Add(1)
		}
	}()
	time.Sleep(300 * time.Millisecond)
	r.async.mu.Lock()
	held := r.async.bytes
	r.async.mu.Unlock()
	if q := queued.Load(); q >= 100 || held > 8000+1100 {
		t.Errorf("%d results accepted with %d bytes held under a cap of 8000: the queue is unbounded", q, held)
	}
	close(release)
	<-done
	if err := r.finalReport(ctx); err != nil {
		t.Fatal(err)
	}
	r.drop()
	idx := s.results()
	if len(idx) != 100 {
		t.Fatalf("%d results arrived, want 100", len(idx))
	}
	for i, v := range idx {
		if v != i {
			t.Fatalf("result %d arrived as test %d", i, v)
		}
	}
}

// With the setting off a result is delivered before result returns, one
// report each, as before.
func TestUnbatchedResultsAreSentOneByOne(t *testing.T) {
	s := newReportServer(t, nil)
	r := newTestReporter(s, false)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := r.result(ctx, testResultOf(i, 10)); err != nil {
			t.Fatal(err)
		}
		if s.count() != i+1 || len(s.snapshot()[i].Results) != 1 {
			t.Fatalf("after result %d the server has %d reports", i, s.count())
		}
	}
	if err := r.asyncErr(); err != nil {
		t.Error(err)
	}
	if err := r.finalReport(ctx); err != nil {
		t.Fatal(err)
	}
	last := s.snapshot()[5]
	if !last.Final || len(last.Results) != 0 || s.count() != 6 {
		t.Errorf("final report %+v", last)
	}
	r.drop()
}

// A cancelled job ends the sender at once, even in the middle of a send.
func TestBatchedCancelEndsTheSender(t *testing.T) {
	release := make(chan struct{})
	s := newReportServer(t, func(int, judgeapi.Report) int { <-release; return 200 })
	noLeak(t, s)
	t.Cleanup(func() { close(release) })
	r := newTestReporter(s, true)
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.result(ctx, testResultOf(0, 10)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the send", func() bool { return s.count() == 1 })
	cancel()
	done := make(chan struct{})
	go func() { r.drop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("drop did not return after the job was cancelled")
	}
}

// flush sends what is waiting, so a job that fails still shows the tests
// that ran; a lost lease reports ErrLease.
func TestBatchedFlush(t *testing.T) {
	release := make(chan struct{})
	s := newReportServer(t, func(n int, _ judgeapi.Report) int {
		if n == 1 {
			<-release
		}
		return 200
	})
	noLeak(t, s)
	r := newTestReporter(s, true)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if err := r.result(ctx, testResultOf(i, 10)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the first send", func() bool { return s.count() == 1 })
	close(release)
	if err := r.flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(s.results()); got != 4 {
		t.Errorf("%d results delivered by flush, want 4", got)
	}
	for _, rep := range s.snapshot() {
		if rep.Final {
			t.Error("flush sent a final report")
		}
	}
	r.drop()
}

func TestBatchedFinalReportSplitsWhatIsLeft(t *testing.T) {
	s := newReportServer(t, nil)
	noLeak(t, s)
	r := newTestReporter(s, true)
	r.async.batchBytes = 5000
	// Results are put in the queue directly: nothing is sending yet.
	r.async.mu.Lock()
	for i := 0; i < 6; i++ {
		tr := testResultOf(i, 2000)
		r.async.pending = append(r.async.pending, tr)
		r.async.bytes += sizeOf(tr)
	}
	r.async.mu.Unlock()
	if err := r.finalReport(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.drop()
	reports := s.snapshot()
	if len(reports) != 3 || !reports[2].Final || reports[0].Final || reports[1].Final {
		t.Fatalf("%d reports; the last one alone must be final: %+v", len(reports), reports)
	}
	if got := s.results(); len(got) != 6 {
		t.Errorf("%d results, want 6", len(got))
	}
}

func batchedWorker(t *testing.T, s *reportServer) *Worker {
	t.Helper()
	c := &Client{BaseURL: s.URL, Token: "t", HTTP: s.Client()}
	return &Worker{Client: c, Cache: &Cache{Dir: t.TempDir(), Client: c}, Compiled: &CompileCache{Dir: t.TempDir()},
		WorkDir: t.TempDir(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)), AsyncReports: true}
}

func startBatchedJob(t *testing.T, w *Worker) (*judgeapi.Claim, *reporter, func()) {
	t.Helper()
	claim := &judgeapi.Claim{JobID: 9, Kind: judgeapi.KindRun, Lease: "l", Payload: json.RawMessage(`{}`)}
	rep, _, done, err := w.startJob(context.Background(), claim, &judgeapi.RunPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.async == nil {
		t.Fatal("AsyncReports did not give the job a queue")
	}
	return claim, rep, done
}

// The worker's own path: startJob makes the queue, finish puts the last
// results in the final report, done leaves no sender.
func TestBatchedJobEndsWithTheFinalReport(t *testing.T) {
	s := newReportServer(t, func(int, judgeapi.Report) int { time.Sleep(3 * time.Millisecond); return 200 })
	noLeak(t, s)
	w := batchedWorker(t, s)
	claim, rep, done := startBatchedJob(t, w)
	for i := 0; i < 30; i++ {
		if err := rep.result(context.Background(), testResultOf(i, 20)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.finish(context.Background(), claim, rep, nil); err != nil {
		t.Fatal(err)
	}
	done()
	if idx := s.results(); len(idx) != 30 {
		t.Fatalf("%d results, want 30", len(idx))
	}
	reports := s.snapshot()
	if !reports[len(reports)-1].Final {
		t.Error("the last report is not final")
	}
	for _, rep := range reports[:len(reports)-1] {
		if rep.Final {
			t.Error("a report before the last is final")
		}
	}
}

// A job that breaks still shows the tests that ran: they are posted before
// the fatal report, and the fatal report is last.
func TestBatchedFatalReportComesAfterTheResults(t *testing.T) {
	release := make(chan struct{})
	s := newReportServer(t, func(n int, _ judgeapi.Report) int {
		if n == 1 {
			<-release
		}
		return 200
	})
	noLeak(t, s)
	w := batchedWorker(t, s)
	claim, rep, done := startBatchedJob(t, w)
	for i := 0; i < 5; i++ {
		if err := rep.result(context.Background(), testResultOf(i, 20)); err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	if err := w.finish(context.Background(), claim, rep, errors.New("isolate broke")); err != nil {
		t.Fatal(err)
	}
	done()
	reports := s.snapshot()
	last := reports[len(reports)-1]
	if last.Fatal == "" || last.Final || len(s.results()) != 5 {
		t.Errorf("last report %+v, %d results", last, len(s.results()))
	}
	for _, r := range reports[:len(reports)-1] {
		if r.Fatal != "" {
			t.Error("a fatal report before the end")
		}
	}
}

// A post that fails after its test ended is found by finish: the lease is
// gone, so no final report is sent.
func TestBatchedLeaseLostBeforeTheEndSendsNoFinalReport(t *testing.T) {
	s := newReportServer(t, func(int, judgeapi.Report) int { return http.StatusConflict })
	noLeak(t, s)
	w := batchedWorker(t, s)
	claim, rep, done := startBatchedJob(t, w)
	if err := rep.result(context.Background(), testResultOf(0, 20)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the refused post", func() bool { return rep.asyncErr() != nil })
	if err := w.finish(context.Background(), claim, rep, nil); !errors.Is(err, ErrLease) {
		t.Errorf("finish returned %v, want ErrLease", err)
	}
	done()
	for _, r := range s.snapshot() {
		if r.Final {
			t.Error("a final report was sent after the lease was lost")
		}
	}
}
