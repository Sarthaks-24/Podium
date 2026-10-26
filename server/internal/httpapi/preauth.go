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

// Budgets per client address before sign-in, each a token bucket: Burst
// requests at once, refilled at Rate per second. A college signs in from
// few addresses behind its NAT, so each is sized for a whole class at
// once: 700 students load the config about three times each on the way
// through a sign-in (the app, the sign-in page, the page after the
// redirect) and make one start and one callback (security review
// 2026-10-20, finding 1).
const (
	// The cheap routes: the config, the health check, sign-out.
	PreAuthBurst = 2500
	PreAuthRate  = 50
	// Session cookies the database did not know, per address: real
	// students hit this once when a session ends, a script sending
	// random cookies hits it on every request.
	AuthFailBurst = 1000
	AuthFailRate  = 10
	// The sign-in routes: Google start and callback, test sign-in. Each
	// callback is an outbound call to Google.
	SignInBurst = 2000
	SignInRate  = 20
	// preAuthMaxKeys bounds each table of addresses.
	preAuthMaxKeys = 20000
)

// ipLimiter is a token bucket per client address.
type ipLimiter struct {
	burst, rate float64
	now         func() time.Time

	mu        sync.Mutex
	buckets   map[string]*ipBucket
	lastSweep time.Time
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
// free. A full table is swept at most once a second; if it is still full,
// one arbitrary bucket makes room, so a stranger never shares a budget
// that someone else is draining (finding 2).
func (l *ipLimiter) allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= preAuthMaxKeys && now.Sub(l.lastSweep) >= time.Second {
			l.lastSweep = now
			l.sweep(now)
		}
		if len(l.buckets) >= preAuthMaxKeys {
			for k := range l.buckets {
				delete(l.buckets, k)
				break
			}
		}
		b = &ipBucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// exhausted reports whether key's bucket is empty, without taking a token.
func (l *ipLimiter) exhausted(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	return b != nil && b.tokens+now.Sub(b.at).Seconds()*l.rate < 1
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

// preAuth wraps a cheap route a visitor reaches without a session, and
// signInLimit a sign-in route. Requests from this machine itself are not
// counted (the load test, install.sh verify). A refused sign-in, being a
// browser navigation, lands on /signin?error=rate_limited rather than a
// page of JSON.
func (d Deps) preAuth(h http.HandlerFunc) http.HandlerFunc {
	return d.limited(d.preAuthLimit, false, h)
}

func (d Deps) signInLimit(redirect bool, h http.HandlerFunc) http.HandlerFunc {
	return d.limited(d.signInLimiter, redirect, h)
}

func (d Deps) limited(l *ipLimiter, redirect bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !loopbackRequest(r) {
			if ok, wait := l.allow(addressKey(r)); !ok {
				secs := max(1, int(wait.Seconds()+0.999))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				if redirect {
					w.Header().Set("Cache-Control", "no-store")
					http.Redirect(w, r, "/signin?error=rate_limited", http.StatusFound)
					return
				}
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
