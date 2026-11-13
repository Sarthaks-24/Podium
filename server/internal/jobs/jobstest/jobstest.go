// Package jobstest helps tests of other packages check that what they do
// wakes a judge's held claim.
package jobstest

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Hold starts a claim for kinds that waits on s with its own timed look
// switched off, so only a wake can end it, and returns once it is waiting.
// The queue must have nothing claimable of those kinds. The returned
// function waits up to d for the claim's answer and returns it, or nil when
// none came.
func Hold(t testing.TB, s *jobs.Service, kinds ...string) func(d time.Duration) *judgeapi.Claim {
	t.Helper()
	s.LookEvery = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	out := make(chan *judgeapi.Claim, 1)
	before := s.Held()
	go func() {
		c, _ := s.ClaimWait(ctx, nil, judgeapi.ClaimRequest{Kinds: kinds, Worker: "held"}, 30*time.Second)
		out <- c
	}()
	for start := time.Now(); s.Held() == before; time.Sleep(time.Millisecond) {
		select {
		case c := <-out:
			t.Fatalf("the claim answered at once (%+v): the queue was not empty", c)
		default:
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("the claim never started waiting")
		}
	}
	return func(d time.Duration) *judgeapi.Claim {
		select {
		case c := <-out:
			return c
		case <-time.After(d):
			return nil
		}
	}
}
