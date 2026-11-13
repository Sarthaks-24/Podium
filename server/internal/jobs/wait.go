package jobs

import (
	"context"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// How often a held claim looks at the queue again without a wake, and how
// many claims may be held at once. The look is the safety net: a wake can
// be missed, and the contest gate opens by the clock with no event, so a
// held claim finds work at most this late, which is what polling cost.
const (
	defaultLook    = 2 * time.Second
	defaultMaxHeld = 64
)

// Wake tells every held claim to look at the queue again. Whatever queues a
// job or frees one (an enqueue, a close, a requeued lease, a resumed
// contest) calls it after its transaction has committed: before the commit
// the claim would find nothing and the signal would be spent, and after a
// rollback there is nothing to find. It never blocks.
func (s *Service) Wake() {
	s.wakeMu.Lock()
	if s.wakeCh != nil {
		close(s.wakeCh)
		s.wakeCh = nil
	}
	s.wakeMu.Unlock()
}

// wakeChan returns the channel the next Wake closes. A claim takes it
// before it looks at the queue, so a wake that lands during the look is not
// lost.
func (s *Service) wakeChan() <-chan struct{} {
	s.wakeMu.Lock()
	defer s.wakeMu.Unlock()
	if s.wakeCh == nil {
		s.wakeCh = make(chan struct{})
	}
	return s.wakeCh
}

// Held is how many claims are waiting in ClaimWait.
func (s *Service) Held() int { return int(s.held.Load()) }

// ClaimWait is Claim that waits up to wait when nothing is claimable, and
// answers as soon as a job is. It holds no transaction and no database
// connection while it waits: each look is an ordinary Claim, so the
// fairness rule, the contest gate and a paused contest decide what is
// claimable exactly as before, and a lease starts when the claim is
// answered. It looks again on a wake and every LookEvery. It returns nil
// when the wait ends or stop is done (server shutdown), and ctx's error when
// the client has gone. A held claim costs one goroutine, one timer and one
// ticker; at most MaxHeld are held, and a claim over that returns at once
// as if its wait had ended.
func (s *Service) ClaimWait(ctx, stop context.Context, req judgeapi.ClaimRequest, wait time.Duration) (*judgeapi.Claim, error) {
	wake := s.wakeChan()
	c, err := s.Claim(ctx, req)
	if c != nil || err != nil || wait <= 0 {
		return c, err
	}
	if n := int(s.held.Add(1)); n > s.maxHeld() {
		s.held.Add(-1)
		return nil, nil
	}
	defer s.held.Add(-1)
	var stopped <-chan struct{}
	if stop != nil {
		stopped = stop.Done()
	}
	end := time.NewTimer(wait)
	defer end.Stop()
	look := time.NewTicker(s.lookEvery())
	defer look.Stop()
	for {
		select {
		case <-wake:
		case <-look.C:
		case <-end.C:
			return nil, nil
		case <-stopped:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if stop != nil && stop.Err() != nil {
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wake = s.wakeChan()
		if c, err = s.Claim(ctx, req); c != nil || err != nil {
			return c, err
		}
	}
}

func (s *Service) lookEvery() time.Duration {
	if s.LookEvery > 0 {
		return s.LookEvery
	}
	return defaultLook
}

func (s *Service) maxHeld() int {
	if s.MaxHeld > 0 {
		return s.MaxHeld
	}
	return defaultMaxHeld
}
