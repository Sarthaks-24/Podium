package contests

import (
	"context"
	"math"
)

// Percent rounds score of max to one decimal; 0 when max is 0.
func Percent(score, max int) float64 {
	if max <= 0 {
		return 0
	}
	return math.Round(float64(score)*1000/float64(max)) / 10
}

// IsFinal reports whether the contest's results are final now: every
// window closed and nothing waiting for the judge.
func (s *Service) IsFinal(ctx context.Context, c *Contest) (bool, error) {
	if s.now().Before(c.Final()) {
		return false, nil
	}
	var pending int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM submissions WHERE contest_id = ? AND kind = 'submit' AND practice = 0
		AND state IN ('queued', 'running')`, c.ID).Scan(&pending)
	return pending == 0, err
}

// StudentsSeeFinal reports whether students see the final board: the
// results are final and no held reveal is pending.
func (c *Contest) StudentsSeeFinal() bool { return !(c.HoldReveal && c.RevealedAt.IsZero()) }
