package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// echoKind accepts any report with results for tests 1 to 3 and writes the
// number of results as the summary.
type echoKind struct{ finished int }

func (k *echoKind) CheckReport(_ json.RawMessage, r judgeapi.Report) error {
	for _, t := range r.Results {
		if t.Idx < 1 || t.Idx > 3 {
			return Badf("test %d", t.Idx)
		}
	}
	return nil
}

func (k *echoKind) Blobs(json.RawMessage) ([]string, error) { return []string{"aa"}, nil }

func (k *echoKind) MaxDuration(json.RawMessage) time.Duration { return 10 * time.Minute }

func (k *echoKind) Finish(_ context.Context, _ *sql.Tx, job *Job) error {
	if job.State == Failed {
		return nil
	}
	k.finished++
	job.Result.Summary = json.RawMessage(`{"results":` + string(rune('0'+len(job.Result.Tests))) + `}`)
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(t *testing.T) (*Service, *clock, *echoKind) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	c := &clock{time.UnixMilli(1_800_000_000_000)}
	k := &echoKind{}
	return &Service{DB: d, Kinds: map[string]Kind{judgeapi.KindValidate: k}, Now: c.now}, c, k
}

func claim(worker string) judgeapi.ClaimRequest {
	return judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindValidate}, Worker: worker, Version: "test", Isolate: "2.7"}
}

func enqueue(t *testing.T, s *Service, priority int) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Enqueue(ctx, tx, judgeapi.KindValidate, priority, map[string]int{"p": priority}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestClaimOrderAndLease(t *testing.T) {
	s, _, k := newService(t)
	ctx := context.Background()
	low := enqueue(t, s, 90)
	high := enqueue(t, s, 10)
	if pos, _ := s.QueuePosition(ctx, low); pos != 2 {
		t.Errorf("queue position of the low-priority job = %d, want 2", pos)
	}

	if c, err := s.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{"submit"}, Worker: "w"}); c != nil || err != nil {
		t.Fatalf("unknown kind claimed %v %v", c, err)
	}
	c, err := s.Claim(ctx, claim("w1"))
	if err != nil || c == nil || c.JobID != high || c.Lease == "" || string(c.Payload) != `{"p":10}` {
		t.Fatalf("first claim %+v %v", c, err)
	}
	if pos, _ := s.QueuePosition(ctx, high); pos != 0 {
		t.Errorf("leased job has queue position %d", pos)
	}

	// The lease is the only key: a wrong one is refused.
	if err := s.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: "guess"}); !errors.Is(err, ErrLease) {
		t.Errorf("wrong lease: %v", err)
	}
	if err := s.Report(ctx, judgeapi.Report{JobID: 999, Lease: c.Lease}); !errors.Is(err, ErrLease) {
		t.Errorf("unknown job: %v", err)
	}
	var bad *ReportError
	if err := s.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Results: []judgeapi.TestResult{{Idx: 9}}}); !errors.As(err, &bad) {
		t.Errorf("out-of-range result accepted: %v", err)
	}
	if err := s.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Results: []judgeapi.TestResult{{Idx: 1}, {Idx: 2}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Results: []judgeapi.TestResult{{Idx: 3}}, Final: true}); err != nil {
		t.Fatal(err)
	}
	job, err := s.Get(ctx, c.JobID)
	if err != nil || job.State != Done || len(job.Result.Tests) != 3 || string(job.Result.Summary) != `{"results":3}` || k.finished != 1 {
		t.Errorf("closed job %+v %v", job, err)
	}
	// A closed job takes no more reports.
	if err := s.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}); !errors.Is(err, ErrLease) {
		t.Errorf("report after close: %v", err)
	}
}

func TestExpiredLeasesRequeueThenFail(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	id := enqueue(t, s, 90)
	var leases []string
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		c, err := s.Claim(ctx, claim("w"))
		if err != nil || c == nil || c.JobID != id {
			t.Fatalf("attempt %d: %+v %v", attempt, c, err)
		}
		leases = append(leases, c.Lease)
		s.Report(ctx, judgeapi.Report{JobID: id, Lease: c.Lease, Results: []judgeapi.TestResult{{Idx: 1}}})
		// A report renews the lease; only silence lets it expire.
		clk.t = clk.t.Add(LeaseTTL - time.Second)
		if r, f, _ := s.Sweep(ctx); r != 0 || f != 0 {
			t.Fatalf("attempt %d: swept a live lease", attempt)
		}
		clk.t = clk.t.Add(2 * time.Second)
		r, f, err := s.Sweep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if attempt < MaxAttempts && (r != 1 || f != 0) {
			t.Fatalf("attempt %d: requeued %d failed %d", attempt, r, f)
		}
		if attempt == MaxAttempts && (r != 0 || f != 1) {
			t.Fatalf("last attempt: requeued %d failed %d", r, f)
		}
	}
	job, _ := s.Get(ctx, id)
	if job.State != Failed || job.Error == "" || len(job.Result.Tests) != 1 {
		t.Errorf("job after three expiries: %+v", job)
	}
	// An old worker coming back cannot report.
	if err := s.Report(ctx, judgeapi.Report{JobID: id, Lease: leases[0], Final: true}); !errors.Is(err, ErrLease) {
		t.Errorf("stale lease after failure: %v", err)
	}
}

func TestRequeuedJobStartsClean(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	id := enqueue(t, s, 90)
	first, _ := s.Claim(ctx, claim("w1"))
	s.Report(ctx, judgeapi.Report{JobID: id, Lease: first.Lease, Results: []judgeapi.TestResult{{Idx: 1}}})
	clk.t = clk.t.Add(LeaseTTL + time.Second)
	s.Sweep(ctx)
	second, _ := s.Claim(ctx, claim("w2"))
	if second == nil || second.Lease == first.Lease {
		t.Fatal("job was not handed out again with a new lease")
	}
	if err := s.Report(ctx, judgeapi.Report{JobID: id, Lease: first.Lease}); !errors.Is(err, ErrLease) {
		t.Errorf("first worker still accepted: %v", err)
	}
	job, _ := s.Get(ctx, id)
	if len(job.Result.Tests) != 0 {
		t.Errorf("results from the abandoned attempt kept: %+v", job.Result.Tests)
	}
}

func TestFatalReportFailsJob(t *testing.T) {
	s, _, k := newService(t)
	ctx := context.Background()
	id := enqueue(t, s, 90)
	c, _ := s.Claim(ctx, claim("w"))
	if err := s.Report(ctx, judgeapi.Report{JobID: id, Lease: c.Lease, Fatal: "isolate is not installed"}); err != nil {
		t.Fatal(err)
	}
	job, _ := s.Get(ctx, id)
	if job.State != Failed || job.Error != "isolate is not installed" || k.finished != 0 {
		t.Errorf("job %+v finished %d", job, k.finished)
	}
}

func TestBlobsOnlyForTheLeasedJob(t *testing.T) {
	s, _, _ := newService(t)
	ctx := context.Background()
	id := enqueue(t, s, 90)
	c, _ := s.Claim(ctx, claim("w"))
	if ok, err := s.MayFetch(ctx, id, c.Lease, "aa"); !ok || err != nil {
		t.Errorf("own blob refused: %v %v", ok, err)
	}
	if ok, _ := s.MayFetch(ctx, id, c.Lease, "bb"); ok {
		t.Error("a blob outside the job was served")
	}
	if ok, _ := s.MayFetch(ctx, id, "guess", "aa"); ok {
		t.Error("a blob was served without the lease")
	}
	s.Report(ctx, judgeapi.Report{JobID: id, Lease: c.Lease, Final: true})
	if ok, _ := s.MayFetch(ctx, id, c.Lease, "aa"); ok {
		t.Error("a blob was served after the job closed")
	}
}

func TestDeadlineStopsAHeartbeatingJob(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	id := enqueue(t, s, 90)
	c, _ := s.Claim(ctx, claim("w"))
	for i := 0; i < 15; i++ { // 15 heartbeats a minute apart: past the 10 minute deadline
		clk.t = clk.t.Add(time.Minute)
		if err := s.Report(ctx, judgeapi.Report{JobID: id, Lease: c.Lease}); err != nil {
			break
		}
		s.Sweep(ctx)
	}
	job, _ := s.Get(ctx, id)
	if job.State != Failed || !strings.Contains(job.Error, "took too long") || job.WorkerVersion != "test / isolate 2.7" {
		t.Errorf("job %+v", job)
	}
}

// A job requeued after its judge went away, then held past its first
// deadline, runs on its next claim instead of failing at once (C1-050).
func TestRequeuedJobGetsAFreshDeadline(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	id := enqueue(t, s, 90)
	if c, _ := s.Claim(ctx, claim("w1")); c == nil {
		t.Fatal("no claim")
	}
	clk.t = clk.t.Add(LeaseTTL + time.Second)
	if requeued, _, _ := s.Sweep(ctx); requeued != 1 {
		t.Fatalf("requeued %d", requeued)
	}
	clk.t = clk.t.Add(time.Hour) // held in the queue past the first deadline
	c, _ := s.Claim(ctx, claim("w2"))
	if c == nil {
		t.Fatal("not handed out again")
	}
	if _, failed, _ := s.Sweep(ctx); failed != 0 {
		t.Errorf("failed %d at once on the new claim", failed)
	}
	if err := s.Report(ctx, judgeapi.Report{JobID: id, Lease: c.Lease, Final: true}); err != nil {
		t.Errorf("report on the new claim: %v", err)
	}
	job, _ := s.Get(ctx, id)
	if job.State == Failed {
		t.Errorf("job failed: %s", job.Error)
	}
}
