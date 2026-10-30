package httpapi

// The live event stream: one server-sent event stream per signed-in tab,
// ended with its session. It stays here, next to the session middleware,
// because each clock tick checks the session again.

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

// clockEvery is how often the event stream sends the server's clock.
var clockEvery = 25 * time.Second

// streamTag names the device a stream comes from, for the second-session
// signal: the address and the browser. A phone changes address when
// it moves from wifi to cellular, and its old stream lingers until TCP
// gives up, so for a phone or tablet browser the address is left out and
// the same browser on a new network is the same device.
// A desktop keeps the address: identical lab machines differ only by it.
// A phone or tablet that sends ?device= (a random id its browser keeps)
// is told apart by it, so two phones of one model on one account are two
// devices; the id never replaces a desktop's address, where a
// shared id would hide two machines.
func (d Deps) streamTag(r *http.Request) string {
	ua := r.UserAgent()
	if mobileAgent(ua) {
		if dev := r.URL.Query().Get("device"); deviceID(dev) {
			return d.Submissions.ClientHash("device:"+dev, ua)
		}
		return d.Submissions.ClientHash("", ua)
	}
	return d.Submissions.ClientHash(httpx.ClientIP(r), ua)
}

// deviceID reports a well-formed ?device= value: 1 to 64 letters and
// digits. Anything else is ignored, as if it were absent.
func deviceID(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// mobileAgent reports a phone or tablet browser by its user agent.
func mobileAgent(ua string) bool {
	for _, m := range []string{"Mobile", "Android", "iPhone", "iPad"} {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// eventStream is the signed-in user's server-sent event stream: run and
// submission updates as they happen, contest events, and the server's
// clock every 25 s. On each clock tick the session is checked again, so a
// stream ends with its session (logout, expiry, a disabled account). A
// stream displaced by the user's fifth tab gets a final "replaced" event,
// so that tab stops reconnecting. Opening a stream from a second machine
// or browser while another is open is recorded for live contests.
func (d Deps) eventStream(w http.ResponseWriter, r *http.Request) {
	_, ok := w.(http.Flusher)
	if !ok || d.Hub == nil {
		httpx.WriteError(w, http.StatusNotImplemented, "unsupported", "Live updates are not available; the page will poll instead.")
		return
	}
	u := auth.UserFrom(r.Context())
	tag := d.streamTag(r)
	if d.Contests != nil && u.Role == auth.Student {
		for _, other := range d.Hub.Tags(u.ID) {
			if other != tag {
				if err := d.Contests.RecordSecondSession(r.Context(), u.ID); err != nil {
					d.Log.Warn("second session not recorded", "user_id", u.ID, "err", err)
				}
				break
			}
		}
	}
	c, err := d.Hub.SubscribeTagged(u.ID, tag)
	if err != nil {
		w.Header().Set("Retry-After", "30")
		httpx.WriteError(w, http.StatusServiceUnavailable, "stream_limit", "Too many live connections right now; the page will poll instead.")
		return
	}
	defer d.Hub.Unsubscribe(c)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{}) // the stream outlives the server's read timeout
	// Every write has a deadline, so a client that stops reading cannot
	// hold this goroutine and its connection.
	write := func(format string, a ...any) bool {
		rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, format, a...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	clock := func() bool {
		return write("event: clock\ndata: {\"server_now\":%d}\n\n", time.Now().UnixMilli())
	}
	if !clock() {
		return
	}
	t := time.NewTicker(clockEvery)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.Done:
			if c.Replaced() {
				write("event: replaced\ndata: {}\n\n")
			}
			return // too slow (the browser reconnects), or replaced
		case ev := <-c.C:
			if !write("event: %s\ndata: %s\n\n", ev.Type, ev.Data) {
				return
			}
		case <-t.C:
			if !d.sessionLive(r) || !clock() {
				return
			}
		}
	}
}

// sessionLive reports whether the request's session is still valid and its
// account enabled. Contest events need nothing more here: they are
// addressed at send time to the users who may still enter the contest.
func (d Deps) sessionLive(r *http.Request) bool {
	c, err := r.Cookie(d.sessionCookieName())
	if err != nil {
		return false
	}
	u, err := d.authenticate(r.Context(), c.Value)
	return err == nil && u.ID == auth.UserFrom(r.Context()).ID
}
