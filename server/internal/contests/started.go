package contests

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotParticipant refuses an extension for someone who is neither on
// the roster nor taking part in the contest.
var ErrNotParticipant = errors.New("That person is not on this contest's roster and has not started it, so they cannot be given extra time.")

// Started reports whether the contest's start time has passed.
func (c *Contest) Started(now time.Time) bool { return !now.Before(c.Schedule.StartsAt) }

// IsParticipant reports whether a user may be given extra time: they are on
// the roster, or they started the contest or submitted to it, and they are
// a student (organisers have no clock to extend).
func (s *Service) IsParticipant(ctx context.Context, c *Contest, userID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users u WHERE u.id = ? AND u.role = 'student' AND (
		EXISTS (SELECT 1 FROM contest_roster r WHERE r.contest_id = ? AND r.email = u.email)
		OR EXISTS (SELECT 1 FROM contest_starts cs WHERE cs.contest_id = ? AND cs.user_id = u.id)
		OR EXISTS (SELECT 1 FROM submissions sb WHERE sb.contest_id = ? AND sb.user_id = u.id AND sb.kind = 'submit'))`,
		userID, c.ID, c.ID, c.ID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("participant check: %w", err)
	}
	return n > 0, nil
}
