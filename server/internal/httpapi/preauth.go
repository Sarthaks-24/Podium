package httpapi

// Limits before sign-in (P0 security review, finding 4, and the pre-auth
// audit's observation 4): a per-address budget on the routes a visitor
// reaches without a session, and single use of each sign-in attempt, so
// a script can neither flood the server with queries nor replay one
// callback into many outbound calls to Google.

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The pre-auth budget per client address: PreAuthBurst requests at once,
// refilled at PreAuthRate per second. A college signs in from few
// addresses behind its NAT, so the budget is sized for a whole class at
// once (700 students each load the config and start one sign-in).
const (
	PreAuthBurst = 600
	PreAuthRate  = 10
	// preAuthMaxKeys bounds the table of addresses; past it, new
	// addresses share one budget until the sweep frees room.
	preAuthMaxKeys = 20000
)

// ipLimiter is a token bucket per client address.
type ipLimiter struct {
	burst, rate float64
	now         func() time.Time

	mu      sync.Mutex
	buckets map[string]*ipBucket
}

type ipBucket struct {
	tokens float64
	at     time.Time
}

func newIPLimiter(burst, rate float64, now func() time.Time) *ipLimiter {
	if now == nil {
		now = time.Now
	}
	return &ipLimiter{burst: burst, rate: rate, now: now, buckets: map[string]*ipBucket{}}
}

// allow takes one token from key's bucket, or says how long until one is
// free.
func (l *ipLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= preAuthMaxKeys {
			l.sweep(now)
		}
		if len(l.buckets) >= preAuthMaxKeys {
			key = "overflow"
			b = l.buckets[key]
		}
		if b == nil {
			b = &ipBucket{tokens: l.burst, at: now}
			l.buckets[key] = b
		}
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// sweep drops buckets that have refilled: they hold nothing to remember.
func (l *ipLimiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.at).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// addressKey is the budget a request counts against: its client address,
// with IPv6 addresses grouped by /64 (one host can hold a whole /64).
func addressKey(r *http.Request) string {
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return "unknown"
	}
	if ip.To4() == nil {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return ip.String()
}

// preAuth wraps a route a visitor reaches without a session. Requests from
// this machine itself are not counted (the load test, install.sh verify).
func (d Deps) preAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !loopbackRequest(r) {
			if ok, wait := d.preAuthLimit.allow(addressKey(r)); !ok {
				secs := max(1, int(wait.Seconds()+0.999))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
					"code": "rate_limited", "message": "Too many requests from your network. Wait a few seconds and try again.",
					"retry_after_s": secs}})
				return
			}
		}
		h(w, r)
	}
}

// usedFlows remembers the state of every sign-in attempt the callback has
// taken, until the attempt would have expired, so each sealed state
// cookie finishes at most one sign-in (finding 4). In memory: a restart
// forgets them, which reopens only attempts started in the 10 minutes
// before it.
type usedFlows struct {
	mu   sync.Mutex
	seen map[string]int64 // state -> expiry, Unix seconds
}

// usedFlowsMax bounds the table. Every entry needs a fresh call to
// google/start, which the pre-auth budget limits; if it still fills, new
// states are let through unrecorded rather than refusing every sign-in.
const usedFlowsMax = 50000

func newUsedFlows() *usedFlows { return &usedFlows{seen: map[string]int64{}} }

// take records state and reports whether it was new. expires is the
// attempt's own expiry; now is the current time in Unix seconds.
func (u *usedFlows) take(state string, expires, now int64) (fresh, recorded bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, ok := u.seen[state]; ok {
		return false, true
	}
	if len(u.seen) >= usedFlowsMax {
		for k, e := range u.seen {
			if e < now {
				delete(u.seen, k)
			}
		}
	}
	if len(u.seen) >= usedFlowsMax {
		return true, false
	}
	u.seen[state] = expires
	return true, true
}
