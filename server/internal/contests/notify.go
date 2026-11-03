package contests

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// standingsDelay gathers the changes of a busy moment into one refresh and
// one "standings" event per contest, so 700 browsers refetch the board at
// most about once every BoardEvery rather than once per verdict.
const standingsDelay = time.Second

const refreshRetryDelay = time.Second

// Event names on the stream (docs/API.md, "Live updates").
const (
	EventStandings     = "standings"
	EventAnnouncement  = "announcement"
	EventClarification = "clarification"
	EventContest       = "contest"
)

// notifyStandings sends {contest, contest_id, version} to the contest's
// connected members after a refresh, once per version. The event carries
// no scores: clients refetch the board, which applies the visibility
// rules. In exam mode it goes to organisers only: students have no board
// until their window closes, and an event arriving when judging finished
// would tell them when their hidden submission was judged.
func (s *Service) notifyStandings(ctx context.Context, c *Contest) {
	if s.Hub == nil {
		return
	}
	s.board.mu.Lock()
	s.board.init()
	current := s.board.version[c.ID]
	cached := int64(-1)
	for k, e := range s.board.entries {
		if k.id == c.ID && e.board != nil && (cached < 0 || e.board.Version < cached) {
			cached = e.board.Version
		}
	}
	if cached < 0 || s.board.notified[c.ID] >= cached {
		s.board.mu.Unlock()
		return
	}
	if cached == current {
		s.board.notified[c.ID] = cached
	}
	s.board.mu.Unlock()
	s.publish(ctx, c, false, EventStandings, map[string]any{"contest": c.Slug, "contest_id": c.ID, "version": cached})
}

// publish sends manager events to managers and public events to viewers and
// participants. Membership is checked on every event so removals apply at once.
func (s *Service) publish(ctx context.Context, c *Contest, organisersOnly bool, typ string, v any) {
	if s.Hub == nil {
		return
	}
	users := s.Hub.Users()
	if len(users) == 0 {
		return
	}
	ids, _ := json.Marshal(users)
	publicEvents := !organisersOnly && c.Published && !c.Archived
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id FROM users u
		WHERE u.id IN (SELECT value FROM json_each(?)) AND u.disabled = 0 AND (
			u.role IN ('admin', 'owner')
			OR (u.role = 'organiser' AND EXISTS (SELECT 1 FROM contest_organisers o WHERE o.contest_id = ? AND o.user_id = u.id))
			OR (? AND u.role IN ('organiser', 'student'))
		)`, string(ids), c.ID, publicEvents)
	if err != nil {
		return
	}
	defer rows.Close()
	var to []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			to = append(to, id)
		}
	}
	// A failed read still sends to everyone read so far: events are a
	// hint, and the page reloads the truth on its next poll.
	_ = rows.Err()
	s.Hub.PublishMany(to, typ, v)
}

// publishTo sends an event to particular users with an open stream.
func (s *Service) publishTo(typ string, v any, users ...int64) {
	if s.Hub != nil {
		s.Hub.PublishMany(users, typ, v)
	}
}

// OnJobChange invalidates a contest's board when one of its submission
// jobs changes. The queue supplies the contest id, so this hook does not
// query the jobs table.
func (s *Service) OnJobChange(_ int64, kind string, contestID int64) {
	if kind != judgeapi.KindSubmit || contestID <= 0 {
		return
	}
	s.Invalidate(contestID)
}
