package contests

import (
	"context"
	"database/sql"
	"time"
)

// QueueHealth is a contest's share of the judge queue, for the console's
// attention strip (contract C10, D103).
type QueueHealth struct {
	// Waiting and Judging count the contest's jobs (Submit and Run) in the
	// queue, paused ones included, and with a judge.
	Waiting, Judging int
	// Errors counts the contest's submissions that ended in a judge error
	// and were not judged again since.
	Errors int
	// OldestWait is how long the oldest waiting job has waited; zero when
	// nothing waits.
	OldestWait time.Duration
	// Unanswered counts clarifications with no answer yet.
	Unanswered int
}

// QueueHealth reads the counts in one statement; each part reads one index
// (jobs_by_contest, submissions_errors, clarifications_unanswered).
func (s *Service) QueueHealth(ctx context.Context, contestID int64) (QueueHealth, error) {
	var q QueueHealth
	var oldest sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM jobs WHERE contest_id = ?1 AND state = 'queued'),
		(SELECT count(*) FROM jobs WHERE contest_id = ?1 AND state = 'leased'),
		(SELECT min(created_at) FROM jobs WHERE contest_id = ?1 AND state = 'queued'),
		(SELECT count(*) FROM submissions WHERE contest_id = ?1 AND state = 'error' AND kind = 'submit'),
		(SELECT count(*) FROM clarifications WHERE contest_id = ?1 AND answered_at IS NULL)`, contestID).
		Scan(&q.Waiting, &q.Judging, &oldest, &q.Errors, &q.Unanswered)
	if err != nil {
		return QueueHealth{}, err
	}
	if oldest.Valid {
		q.OldestWait = max(s.now().Sub(time.UnixMilli(oldest.Int64)), 0)
	}
	return q, nil
}
