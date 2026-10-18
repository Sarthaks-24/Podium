package ai

import (
	"sync"
	"time"
)

// CallTimeout bounds one wizard call, retry included (m5-plan section 2).
const CallTimeout = 3 * time.Minute

// Slots allows one wizard call per admin at a time: a second Generate or
// Solve while one runs is refused rather than queued, so a double click
// cannot spend twice.
type Slots struct {
	mu   sync.Mutex
	busy map[int64]bool
}

// Take reserves the admin's slot. ok is false when a call is already
// running; otherwise release must be called when the call ends.
func (s *Slots) Take(userID int64) (release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[userID] {
		return nil, false
	}
	if s.busy == nil {
		s.busy = map[int64]bool{}
	}
	s.busy[userID] = true
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.busy, userID)
			s.mu.Unlock()
		})
	}, true
}

// ErrBusy is what a refused Take maps to.
var ErrBusy = &Error{Code: "busy", Message: "A wizard request of yours is still running. Wait for it to finish."}
