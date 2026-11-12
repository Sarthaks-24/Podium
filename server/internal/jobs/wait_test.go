package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

const within = 100 * time.Millisecond

// held starts a claim that waits on s (30 s, timed look off) and returns
// once it is waiting; answer reports what it was given within d.
func held(t *testing.T, s *Service, ctx, stop context.Context, kinds ...string) (answer func(d time.Duration) (*judgeapi.Claim, bool)) {
	t.Helper()
	type result struct {
		c *judgeapi.Claim
	}
	out := make(chan result, 1)
	n := s.Held()
	go func() {
		c, _ := s.ClaimWait(ctx, stop, judgeapi.ClaimRequest{Kinds: kinds, Worker: "w"}, 30*time.Second)
		out <- result{c}
	}()
	for start := time.Now(); s.Held() == n; time.Sleep(time.Millisecond) {
		if time.Since(start) > 2*time.Second {
			t.Fatal("the claim never started waiting")
		}
	}
	return func(d time.Duration) (*judgeapi.Claim, bool) {
		select {
		case r := <-out:
			return r.c, true
		case <-time.After(d):
			return nil, false
		}
	}
}

// commit queues a job in its own transaction and signals after the commit,
// the way every enqueue site does.
func commit(t *testing.T, s *Service, kind string, priority int, contest int64) int64 {
	t.Helper()
	id := add(t, s, kind, priority, contest)
	s.Wake()
	return id
}

func TestHeldClaimAnswersWithin100msOfACommittedEnqueue(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = time.Hour
	answer := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	id := commit(t, s, judgeapi.KindSubmit, 20, 0)
	c, ok := answer(within)
	if !ok || c == nil || c.JobID != id {
		t.Fatalf("answer %+v (answered %v), want job %d within %v", c, ok, id, within)
	}
	if s.Held() != 0 {
		t.Errorf("held = %d after the answer", s.Held())
	}
}

// Enqueue only writes inside the caller's transaction; it must not signal,
// because a held claim looking at that moment would find nothing and the
// signal would be spent. A rolled-back enqueue then wakes nothing.
func TestAnEnqueueThatRollsBackWakesNothing(t *testing.T) {
	s, clk := newMixed(t)
	s.LookEvery = time.Hour
	answer := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	seen, _ := s.LastClaimAt()
	clk.t = clk.t.Add(time.Second)
	ctx := context.Background()
	tx, _ := s.DB.BeginTx(ctx, nil)
	if _, err := s.Enqueue(ctx, tx, judgeapi.KindSubmit, 20, map[string]int{}, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(within) // a premature wake would make the claim look again now
	if looked, _ := s.LastClaimAt(); !looked.Equal(seen) {
		t.Fatal("the claim looked again before the enqueue committed")
	}
	tx.Rollback()
	if c, ok := answer(within); ok {
		t.Fatalf("a rolled-back enqueue answered a held claim: %+v", c)
	}
	if looked, _ := s.LastClaimAt(); !looked.Equal(seen) {
		t.Error("a rolled-back enqueue woke the held claim")
	}
	// The same claim still wakes on a real, committed job.
	commit(t, s, judgeapi.KindSubmit, 20, 0)
	if c, ok := answer(within); !ok || c == nil {
		t.Fatalf("the held claim missed a committed job: %+v", c)
	}
}

func TestAMissedSignalIsCaughtByTheLook(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = 20 * time.Millisecond
	answer := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	id := add(t, s, judgeapi.KindSubmit, 20, 0) // committed, but nobody called Wake
	c, ok := answer(2 * time.Second)
	if !ok || c == nil || c.JobID != id {
		t.Fatalf("answer %+v (answered %v), want job %d from the timed look", c, ok, id)
	}
}

// A paused contest's job is not claimable, so waking does not hand it out;
// un-pausing does.
func TestAPausedContestsJobDoesNotWakeItButUnpausingDoes(t *testing.T) {
	s, clk := newMixed(t)
	s.LookEvery = time.Hour
	paused := contest(t, s, clk, true)
	answer := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	commit(t, s, judgeapi.KindSubmit, 20, paused)
	if c, ok := answer(within); ok {
		t.Fatalf("a paused contest's job answered a held claim: %+v", c)
	}
	if _, err := s.DB.Exec("UPDATE contests SET paused = 0 WHERE id = ?", paused); err != nil {
		t.Fatal(err)
	}
	s.Wake()
	if c, ok := answer(within); !ok || c == nil {
		t.Fatalf("un-pausing did not release the held claim: %+v", c)
	}
}

// Waking changes nothing about what is claimable: after a Run the next
// claim still takes a waiting submission first.
func TestHeldClaimsKeepTheFairnessRule(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = time.Hour
	add(t, s, judgeapi.KindRun, 10, 0)
	if got := claimKinds(t, s, judgeapi.KindRun); got == nil {
		t.Fatal("no first run")
	} // the last claim was a run
	answer := held(t, s, context.Background(), nil, judgeapi.KindRun, judgeapi.KindSubmit)
	run := add(t, s, judgeapi.KindRun, 10, 0)
	sub := add(t, s, judgeapi.KindSubmit, 20, 0)
	s.Wake()
	c, ok := answer(within)
	if !ok || c == nil || c.JobID != sub {
		t.Fatalf("after a run the held claim took %+v, want the submission %d before run %d", c, sub, run)
	}
}

// The setter gate still decides: a second validation waits while one is
// leased in a live contest, and the close of the first wakes the claim.
func TestHeldClaimsKeepTheContestGateAndAFinishedJobWakesThem(t *testing.T) {
	s, clk := newMixed(t)
	s.LookEvery = time.Hour
	contest(t, s, clk, false)
	first := add(t, s, judgeapi.KindValidate, 90, 0)
	c1 := claimKinds(t, s, judgeapi.KindValidate)
	if c1 == nil || c1.JobID != first {
		t.Fatalf("first validation %+v", c1)
	}
	answer := held(t, s, context.Background(), nil, judgeapi.KindValidate)
	second := commit(t, s, judgeapi.KindValidate, 90, 0)
	if c, ok := answer(within); ok {
		t.Fatalf("the gate let a second validation through: %+v", c)
	}
	if err := s.Report(context.Background(), judgeapi.Report{JobID: c1.JobID, Lease: c1.Lease, Final: true}); err != nil {
		t.Fatal(err)
	}
	if c, ok := answer(within); !ok || c == nil || c.JobID != second {
		t.Fatalf("closing the first job did not release the second: %+v (answered %v)", c, ok)
	}
}

func TestARequeuedExpiredLeaseWakesAHeldClaim(t *testing.T) {
	s, clk := newMixed(t)
	s.LookEvery = time.Hour
	id := add(t, s, judgeapi.KindSubmit, 20, 0)
	if c := claimKinds(t, s, judgeapi.KindSubmit); c == nil || c.JobID != id {
		t.Fatalf("claim %+v", c)
	}
	answer := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	clk.t = clk.t.Add(LeaseTTL + time.Second)
	if _, _, err := s.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c, ok := answer(within); !ok || c == nil || c.JobID != id {
		t.Fatalf("the sweep's requeue did not wake the held claim: %+v (answered %v)", c, ok)
	}
}

func TestShutdownReleasesHeldClaimsAtOnce(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = time.Hour
	stop, shutdown := context.WithCancel(context.Background())
	a := held(t, s, context.Background(), stop, judgeapi.KindSubmit)
	b := held(t, s, context.Background(), stop, judgeapi.KindRun)
	shutdown()
	for _, answer := range []func(time.Duration) (*judgeapi.Claim, bool){a, b} {
		if c, ok := answer(within); !ok || c != nil {
			t.Fatalf("after shutdown began: answered %v with %+v, want an empty answer at once", ok, c)
		}
	}
	if s.Held() != 0 {
		t.Errorf("held = %d after shutdown", s.Held())
	}
}

// A client that goes away frees its place; nothing is leased for it.
func TestADisconnectedClientFreesItsHold(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = time.Hour
	ctx, gone := context.WithCancel(context.Background())
	answer := held(t, s, ctx, nil, judgeapi.KindSubmit)
	gone()
	if _, ok := answer(within); !ok {
		t.Fatal("the held claim outlived its client")
	}
	if s.Held() != 0 {
		t.Errorf("held = %d after the client left", s.Held())
	}
	id := commit(t, s, judgeapi.KindSubmit, 20, 0)
	if j, _ := s.Get(context.Background(), id); j.State != Queued {
		t.Errorf("the job is %q: it was leased to a client that had left", j.State)
	}
}

// Two lanes hold at once; one enqueue gives the job to exactly one and the
// other keeps holding until the next.
func TestTwoHeldClaimsGetOneJobEach(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = time.Hour
	a := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	b := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	first := commit(t, s, judgeapi.KindSubmit, 20, 0)
	var got []*judgeapi.Claim
	deadline := time.After(within)
	for len(got) < 1 {
		select {
		case <-deadline:
			t.Fatal("no held claim answered")
		default:
		}
		for _, answer := range []func(time.Duration) (*judgeapi.Claim, bool){a, b} {
			if c, ok := answer(time.Millisecond); ok && c != nil {
				got = append(got, c)
			}
		}
	}
	time.Sleep(within) // the loser re-looked, found nothing, keeps holding
	if len(got) != 1 || got[0].JobID != first || s.Held() != 1 {
		t.Fatalf("after one job: answers %+v, held %d; want one answer for job %d and one claim holding", got, s.Held(), first)
	}
	second := commit(t, s, judgeapi.KindSubmit, 21, 0)
	var rest []*judgeapi.Claim
	for _, answer := range []func(time.Duration) (*judgeapi.Claim, bool){a, b} {
		if c, ok := answer(within); ok && c != nil {
			rest = append(rest, c)
		}
	}
	// The lane that answered first has nothing left to deliver and times out;
	// only the other lane can answer.
	if len(rest) != 1 || rest[0].JobID != second {
		t.Fatalf("the second job went to %+v, want job %d", rest, second)
	}
}

// A claim answered after a hold starts its lease at the answer, not at the
// request.
func TestALeaseStartsWhenAHeldClaimIsAnswered(t *testing.T) {
	s, clk := newMixed(t)
	s.LookEvery = time.Hour
	answer := held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	clk.t = clk.t.Add(15 * time.Second) // time passes while it holds
	id := commit(t, s, judgeapi.KindSubmit, 20, 0)
	if c, ok := answer(within); !ok || c == nil || c.JobID != id {
		t.Fatalf("answer %+v", c)
	}
	var expires int64
	if err := s.DB.QueryRow("SELECT lease_expires_at FROM jobs WHERE id = ?", id).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if want := clk.t.Add(LeaseTTL).UnixMilli(); expires != want {
		t.Errorf("lease expires at %d, want %d (answer time plus the TTL)", expires, want)
	}
}

func TestHoldsAreBoundedAndZeroWaitDoesNotHold(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = time.Hour
	s.MaxHeld = 1
	held(t, s, context.Background(), nil, judgeapi.KindSubmit)
	start := time.Now()
	c, err := s.ClaimWait(context.Background(), nil, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "w"}, 30*time.Second)
	if c != nil || err != nil || time.Since(start) > within {
		t.Fatalf("a claim over the cap: %+v %v after %v, want an empty answer at once", c, err, time.Since(start))
	}
	s.MaxHeld = 64
	start = time.Now()
	c, err = s.ClaimWait(context.Background(), nil, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "w"}, 0)
	if c != nil || err != nil || time.Since(start) > within {
		t.Fatalf("wait 0: %+v %v after %v, want the old immediate answer", c, err, time.Since(start))
	}
}

func TestTheWaitEnds(t *testing.T) {
	s, _ := newMixed(t)
	s.LookEvery = 5 * time.Millisecond
	start := time.Now()
	c, err := s.ClaimWait(context.Background(), nil, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "w"}, 60*time.Millisecond)
	if c != nil || err != nil || time.Since(start) < 50*time.Millisecond || time.Since(start) > time.Second {
		t.Fatalf("%+v %v after %v, want an empty answer when the wait ends", c, err, time.Since(start))
	}
}
