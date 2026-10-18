package submissions

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Minimum intervals per user for the cheap, frequent writes. Each takes
// SQLite's single writer lock, so a script sending them in a loop would
// hold up judging for everyone (m3-security-2 finding 4).
const (
	draftGap  = 2 * time.Second
	eventsGap = 5 * time.Second
)

// rateLimiter remembers when each user last did each limited action. It is
// in memory: a restart forgets it, which costs nothing.
type rateLimiter struct {
	mu   sync.Mutex
	last map[rateKey]time.Time
}

type rateKey struct {
	user   int64
	action string
}

// allow records the action and returns nil, or a RateError saying when to
// try again if the user did it less than gap ago.
func (l *rateLimiter) allow(user int64, action string, gap time.Duration, now time.Time, what string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		l.last = map[rateKey]time.Time{}
	}
	k := rateKey{user, action}
	if prev, ok := l.last[k]; ok {
		if wait := prev.Add(gap).Sub(now); wait > 0 {
			secs := max(1, int(math.Ceil(wait.Seconds())))
			return &RateError{fmt.Sprintf("%s at most once every %d seconds. Try again in %d s.", what, int(gap.Seconds()), secs),
				time.Duration(secs) * time.Second}
		}
	}
	l.last[k] = now
	if len(l.last) > 20000 {
		for key, t := range l.last {
			if now.Sub(t) > time.Minute {
				delete(l.last, key)
			}
		}
	}
	return nil
}
