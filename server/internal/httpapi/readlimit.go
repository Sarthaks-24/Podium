package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Reads that cost several queries (the contest list and page, a
// workspace, the standings) are limited per user with a token bucket: 10
// a second on average, bursts of 20. A page needs a handful; a script
// looping them cannot hold the 4 database connections (m4-security
// finding 10).
const (
	readRate  = 10.0
	readBurst = 20.0
)

type bucket struct {
	tokens float64
	last   time.Time
}

// readLimiter holds one bucket per user, in memory.
type readLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*bucket
	now     func() time.Time
}

func newReadLimiter() *readLimiter {
	return &readLimiter{buckets: map[int64]*bucket{}, now: time.Now}
}

// take spends a token for user, or returns how long until one is free.
func (l *readLimiter) take(user int64) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[user]
	if b == nil {
		b = &bucket{tokens: readBurst, last: now}
		l.buckets[user] = b
	}
	b.tokens = math.Min(readBurst, b.tokens+now.Sub(b.last).Seconds()*readRate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		if len(l.buckets) > 20000 {
			for id, other := range l.buckets {
				if now.Sub(other.last) > time.Minute {
					delete(l.buckets, id)
				}
			}
		}
		return 0, true
	}
	return time.Duration((1 - b.tokens) / readRate * float64(time.Second)), false
}

// limitReads answers 429 with retry_after_s when the signed-in user reads
// faster than the bucket allows. It must sit inside requireRole.
func (d Deps) limitReads(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.reads != nil {
			if wait, ok := d.reads.take(userFrom(r.Context()).ID); !ok {
				secs := max(1, int(math.Ceil(wait.Seconds())))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
					"code": "rate_limited", "message": "Too many requests at once. Try again in " + strconv.Itoa(secs) + " s.", "retry_after_s": secs}})
				return
			}
		}
		h(w, r)
	}
}
