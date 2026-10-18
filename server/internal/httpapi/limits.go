package httpapi

import (
	"crypto/sha256"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SlowBodyTimeout is how long a route that takes a large upload (a pack,
// a test zip, a roster) may spend reading its body. Every other request
// must arrive within the server's ReadTimeout (15 s), so a client sending
// a body a byte at a time cannot hold a connection and its buffer for long.
const SlowBodyTimeout = 120 * time.Second

// slowBody lets one route read its body for SlowBodyTimeout instead of the
// server-wide ReadTimeout.
func slowBody(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// An error means the connection cannot change its deadline (an
		// HTTP/2 stream through a proxy that does not support it); the
		// server-wide timeout then applies, which is the safe direction.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(SlowBodyTimeout))
		h(w, r)
	}
}

// MaxInFlight is how many requests one session may have open at once,
// event streams aside (they have their own cap, events.MaxPerUser). The web
// app needs a handful; a script opening hundreds of slow requests to hold
// memory is refused instead.
const MaxInFlight = 8

// inFlight counts open requests per session cookie. Keying by the cookie,
// not the user, means the count happens before authentication, so a
// refused request costs no database read; a forged cookie only limits
// itself.
type inFlight struct {
	mu   sync.Mutex
	open map[[32]byte]int
}

func newInFlight() *inFlight { return &inFlight{open: map[[32]byte]int{}} }

// limit wraps the API: requests carrying a session cookie beyond
// MaxInFlight open at once get 429 with Retry-After: 1. Requests without a
// session (sign-in, the public config) and the event stream are not
// counted.
func (f *inFlight) limit(cookieName string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/events" {
			next.ServeHTTP(w, r)
			return
		}
		key := sha256.Sum256([]byte(c.Value))
		f.mu.Lock()
		if f.open[key] >= MaxInFlight {
			f.mu.Unlock()
			w.Header().Set("Retry-After", "1")
			WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{
				"code": "rate_limited", "message": "Too many requests at once from this browser. Try again in a second.", "retry_after_s": 1}})
			return
		}
		f.open[key]++
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			if f.open[key]--; f.open[key] <= 0 {
				delete(f.open, key)
			}
			f.mu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}
