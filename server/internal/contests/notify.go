package contests

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// standingsDelay gathers the changes of a busy moment into one refresh and
// one "standings" event per contest, so 700 browsers refetch the board at
// most about once every BoardEvery rather than once per verdict.
const standingsDelay = time.Second

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
	v := s.Version(c.ID)
	s.board.mu.Lock()
	if s.board.notified[c.ID] >= v {
		s.board.mu.Unlock()
		return
	}
	s.board.notified[c.ID] = v
	s.board.mu.Unlock()
	s.publish(ctx, c, c.Feedback == "exam", EventStandings, map[string]any{"contest": c.Slug, "contest_id": c.ID, "version": v})
}

// publish sends an event to the contest's members with an open stream:
// organisers always; students only of a published contest they may enter.
// With organisersOnly set, students are left out.
func (s *Service) publish(ctx context.Context, c *Contest, organisersOnly bool, typ string, v any) {
	if s.Hub == nil {
		return
	}
	users := s.Hub.Users()
	if len(users) == 0 {
		return
	}
	ids, _ := json.Marshal(users)
	students := !organisersOnly && c.Published && !c.Archived
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id FROM users u WHERE u.id IN (SELECT value FROM json_each(?)) AND u.disabled = 0 AND (
		u.role IN ('admin', 'owner')
		OR (? AND (? = 'domain' OR EXISTS (SELECT 1 FROM contest_roster r WHERE r.contest_id = ? AND r.email = u.email))))`,
		string(ids), students, c.Access, c.ID)
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

// OnJobChange invalidates a contest's board when one of its submissions
// finishes judging. It is chained into the jobs service's OnChange hook and
// does not block.
func (s *Service) OnJobChange(jobID int64, kind string) {
	if kind != judgeapi.KindSubmit {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var contest sql.NullInt64
		var state string
		if err := s.DB.QueryRowContext(ctx, "SELECT contest_id, state FROM jobs WHERE id = ?", jobID).Scan(&contest, &state); err != nil {
			return
		}
		if contest.Valid && (state == "done" || state == "failed") {
			s.Invalidate(contest.Int64)
		}
	}()
}
