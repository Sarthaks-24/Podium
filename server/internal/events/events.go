// Package events fans server-sent events out to signed-in browsers: one
// stream per open tab, keyed by user, fed by whatever changes something a
// user is watching (a submission being judged, a contest's standings, an
// announcement). Contest-wide events go to the set of connected users who
// belong to the contest (PublishMany), so there is no separate topic
// registry to keep in step with access rules. Publishing never blocks: a
// client that cannot keep up is disconnected and reconnects, so a slow
// phone never delays judging for anyone else. The events are listed in
// docs/API.md.
package events

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// buffer is how many events wait for a slow client before it is dropped.
const buffer = 16

// Limits on open streams: a few tabs per person, and a ceiling for the
// server (700 students with a tab or two each fit easily).
const (
	MaxPerUser = 4
	MaxTotal   = 5000
)

// ErrFull means the server holds MaxTotal streams already.
var ErrFull = errors.New("too many event streams")

// Event is one message: a type name and its JSON data.
type Event struct {
	Type string
	Data json.RawMessage
}

// Hub holds the open streams.
type Hub struct {
	mu      sync.Mutex
	clients map[int64]map[*Client]struct{}
	count   int
	next    uint64
}

// Client is one open stream. C delivers events; Done closes when the hub
// drops the client (too slow, or replaced by a newer tab of the same user).
type Client struct {
	C      chan Event
	Done   chan struct{}
	userID int64
	tag    string
	seq    uint64
	once   sync.Once
	// replaced is set when a newer stream of the same user took this
	// one's place.
	replaced atomic.Bool
}

// Replaced reports whether the hub closed this stream because the user
// opened a newer one (MaxPerUser). The handler then sends a final
// "replaced" event so the browser stops reconnecting instead of evicting
// the next tab in turn.
func (c *Client) Replaced() bool { return c.replaced.Load() }

// SubscribeTagged opens a stream for a user, with an opaque tag kept on
// the stream (the hashed IP and browser), so a second session on another machine can be
// noticed.
func (h *Hub) SubscribeTagged(userID int64, tag string) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients == nil {
		h.clients = map[int64]map[*Client]struct{}{}
	}
	if h.clients[userID] == nil {
		h.clients[userID] = map[*Client]struct{}{}
	}
	if len(h.clients[userID]) >= MaxPerUser {
		var oldest *Client
		for c := range h.clients[userID] {
			if oldest == nil || c.seq < oldest.seq {
				oldest = c
			}
		}
		oldest.replaced.Store(true)
		h.remove(oldest)
	}
	if h.count >= MaxTotal {
		return nil, ErrFull
	}
	h.next++
	c := &Client{C: make(chan Event, buffer), Done: make(chan struct{}), userID: userID, tag: tag, seq: h.next}
	if h.clients[userID] == nil {
		h.clients[userID] = map[*Client]struct{}{}
	}
	h.clients[userID][c] = struct{}{}
	h.count++
	return c, nil
}

// Unsubscribe closes a stream.
func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove(c)
}

func (h *Hub) remove(c *Client) {
	set := h.clients[c.userID]
	if _, ok := set[c]; !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(h.clients, c.userID)
	}
	h.count--
	c.once.Do(func() { close(c.Done) })
}

// Publish sends an event to every stream of a user. A stream whose buffer
// is full is dropped instead of waited for.
func (h *Hub) Publish(userID int64, typ string, v any) { h.PublishMany([]int64{userID}, typ, v) }

// PublishMany sends one event to every stream of each of the users. The
// data is encoded once.
func (h *Hub) PublishMany(userIDs []int64, typ string, v any) {
	if len(userIDs) == 0 {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	ev := Event{Type: typ, Data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range userIDs {
		for c := range h.clients[id] {
			select {
			case c.C <- ev:
			default:
				h.remove(c)
			}
		}
	}
}

// Users returns the ids of users with at least one open stream.
func (h *Hub) Users() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]int64, 0, len(h.clients))
	for id := range h.clients {
		out = append(out, id)
	}
	return out
}

// Tags returns the tags of a user's open streams.
func (h *Hub) Tags(userID int64) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.clients[userID]))
	for c := range h.clients[userID] {
		out = append(out, c.tag)
	}
	return out
}

// Has reports whether a user has any open stream.
func (h *Hub) Has(userID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients[userID]) > 0
}

// Connections is the number of open streams, for /metrics and tests.
func (h *Hub) Connections() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}
