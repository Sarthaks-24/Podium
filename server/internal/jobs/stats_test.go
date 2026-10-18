package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

func TestStatsCountsQueueAndRecentWork(t *testing.T) {
	s, clk, k := newService(t)
	s.Kinds[judgeapi.KindSubmit] = k
	ctx := context.Background()

	st, err := s.Stats(ctx)
	if err != nil || st.Queued != 0 || st.Leased != 0 || st.OldestQueuedS != 0 || len(st.Kinds) != 2 {
		t.Fatalf("empty queue: %+v %v", st, err)
	}

	enqueue(t, s, 90)
	enqueue(t, s, 90)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Enqueue(ctx, tx, judgeapi.KindSubmit, 10, map[string]int{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	clk.t = clk.t.Add(4 * time.Second)
	c, err := s.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "lane-0"})
	if err != nil || c == nil || c.JobID != sub {
		t.Fatalf("claim submit: %+v %v", c, err)
	}
	st, err = s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Queued != 2 || st.Leased != 1 || st.Kinds["validate"].Queued != 2 || st.Kinds["submit"].Leased != 1 {
		t.Errorf("counts %+v", st)
	}
	if st.OldestQueuedS != 4 {
		t.Errorf("oldest queued %v s, want 4", st.OldestQueuedS)
	}
	if len(st.LeasedJobs) != 1 || st.LeasedJobs[0].Worker != "lane-0" || st.LeasedJobs[0].Attempts != 1 {
		t.Errorf("leased jobs %+v", st.LeasedJobs)
	}

	clk.t = clk.t.Add(6 * time.Second)
	if err := s.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true}); err != nil {
		t.Fatal(err)
	}
	st, err = s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Leased != 0 || st.Kinds["submit"].FinishedLastMinute != 1 || st.Kinds["submit"].FinishedLast5Min != 1 || st.SubmitsJudged != 1 {
		t.Errorf("after close %+v", st)
	}
	if st.SubmitWaitP95MS != 4000 || st.SubmitTotalP95MS != 10000 {
		t.Errorf("latency wait %d total %d, want 4000 and 10000", st.SubmitWaitP95MS, st.SubmitTotalP95MS)
	}

	// Two minutes on, the job leaves the last minute but not the last five.
	clk.t = clk.t.Add(2 * time.Minute)
	st, _ = s.Stats(ctx)
	if st.Kinds["submit"].FinishedLastMinute != 0 || st.Kinds["submit"].FinishedLast5Min != 1 {
		t.Errorf("two minutes later %+v", st.Kinds["submit"])
	}
	// Eleven minutes on, it no longer counts toward the latency figures.
	clk.t = clk.t.Add(9 * time.Minute)
	st, _ = s.Stats(ctx)
	if st.SubmitsJudged != 0 || st.SubmitTotalP95MS != 0 || st.Kinds["submit"].FinishedLast5Min != 0 {
		t.Errorf("eleven minutes later %+v", st)
	}
}

func TestStatsCountsSweptFailures(t *testing.T) {
	s, clk, _ := newService(t)
	ctx := context.Background()
	enqueue(t, s, 90)
	for i := 0; i < MaxAttempts; i++ {
		if c, err := s.Claim(ctx, claim("w")); err != nil || c == nil {
			t.Fatalf("claim %d: %v %v", i, c, err)
		}
		clk.t = clk.t.Add(LeaseTTL + time.Second)
		if _, _, err := s.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.Stats(ctx)
	if err != nil || st.FailedLast5Min != 1 || st.Queued != 0 {
		t.Errorf("after the third expiry %+v %v", st, err)
	}
}

func TestFinishedRingKeepsTheNewest(t *testing.T) {
	s := &Service{}
	base := time.UnixMilli(1_800_000_000_000)
	for i := 0; i < finishedRing+10; i++ {
		s.recordFinished(Job{Kind: "run", CreatedAt: base}, base.Add(time.Duration(i)*time.Millisecond))
	}
	if len(s.done) != finishedRing {
		t.Fatalf("ring holds %d, want %d", len(s.done), finishedRing)
	}
	newest := int64(0)
	for _, f := range s.done {
		newest = max(newest, f.finished)
	}
	if want := base.Add(time.Duration(finishedRing+9) * time.Millisecond).UnixMilli(); newest != want {
		t.Errorf("newest %d, want %d", newest, want)
	}
}

func TestPercentile(t *testing.T) {
	cases := []struct {
		v    []int64
		p    int
		want int64
	}{
		{nil, 95, 0},
		{[]int64{7}, 95, 7},
		{[]int64{5, 1, 4, 2, 3}, 50, 3},
		{[]int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}, 95, 19},
	}
	for _, c := range cases {
		if got := percentile(c.v, c.p); got != c.want {
			t.Errorf("percentile(%v, %d) = %d, want %d", c.v, c.p, got, c.want)
		}
	}
}
