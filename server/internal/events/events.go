// Package events fans server-sent events out to signed-in browsers: one
// stream per open tab, keyed by user, fed by whatever changes something a
// user is watching (a submission being judged, later a leaderboard or an
// announcement). Publishing never blocks: a client that cannot keep up is
// disconnected and reconnects, so a slow phone never delays judging for
// anyone else. PLAN.md section 6 (realtime) and docs/API.md.
package events

import (
	"encoding/json"
	"errors"
	"sync"
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
	seq    uint64
	once   sync.Once
}

// Subscribe opens a stream for a user. A user's fifth stream replaces
// their oldest. Call Unsubscribe when it ends.
func (h *Hub) Subscribe(userID int64) (*Client, error) {
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
		h.remove(oldest)
	}
	if h.count >= MaxTotal {
		return nil, ErrFull
	}
	h.next++
	c := &Client{C: make(chan Event, buffer), Done: make(chan struct{}), userID: userID, seq: h.next}
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
func (h *Hub) Publish(userID int64, typ string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	ev := Event{Type: typ, Data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients[userID] {
		select {
		case c.C <- ev:
		default:
			h.remove(c)
		}
	}
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
