package contests

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotParticipant refuses an extension for someone not taking part in
// the contest.
var ErrNotParticipant = errors.New("That person has not started this contest, so they cannot be given extra time.")

// IsParticipant reports whether a user may be given extra time: they
// started the contest or submitted to it, and they are a student
// (organisers have no clock to extend).
func (s *Service) IsParticipant(ctx context.Context, c *Contest, userID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users u WHERE u.id = ? AND u.role = 'student' AND (
		EXISTS (SELECT 1 FROM contest_starts cs WHERE cs.contest_id = ? AND cs.user_id = u.id)
		OR EXISTS (SELECT 1 FROM submissions sb WHERE sb.contest_id = ? AND sb.user_id = u.id AND sb.kind = 'submit'))`,
		userID, c.ID, c.ID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("participant check: %w", err)
	}
	return n > 0, nil
}
