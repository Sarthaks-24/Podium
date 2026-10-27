package contests

import (
	"context"
	"time"

	"github.com/Sarthaks-24/podium/internal/retention"
)

// RetentionPeriod is how long integrity evidence is kept after a contest
// has ended for everyone (decision D47 default: 180 days).
const RetentionPeriod = retention.IntegrityEvents

// secondSessionQuiet stops one pair of machines from recording a second
// session on every reconnect: at most one record per contest per student
// in this time.
const secondSessionQuiet = 10 * time.Minute

// RecordSecondSession notes, for every contest whose window is open for the
// student now, that they opened a second session from another machine or
// browser while one was already open (D47: evidence for a reviewer, never
// shown to the student as an accusation and never acted on automatically).
func (s *Service) RecordSecondSession(ctx context.Context, userID int64) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT contest_id FROM contest_starts WHERE user_id = ?", userID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := s.now()
	for _, id := range ids {
		c, err := s.Get(ctx, id)
		if err != nil {
			continue
		}
		st, err := s.State(ctx, c, userID)
		if err != nil || !st.Open {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO integrity_events (user_id, problem_id, contest_id, kind, size, created_at)
			SELECT ?, 0, ?, 'second_session', 0, ? WHERE NOT EXISTS (SELECT 1 FROM integrity_events
				WHERE user_id = ? AND contest_id = ? AND kind = 'second_session' AND created_at > ?)`,
			userID, id, now.UnixMilli(), userID, id, now.Add(-secondSessionQuiet).UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

// Retain deletes integrity events and clears the per-submission integrity
// counts (client hash, typed and pasted characters, focus losses) of every
// contest that ended for everyone more than RetentionPeriod ago. It is
// idempotent and returns how many events and submissions it changed.
func (s *Service) Retain(ctx context.Context) (events, subs int64, err error) {
	cutoff := s.now().Add(-RetentionPeriod).UnixMilli()
	const old = `SELECT contest_id FROM contest_final WHERE final_at < ?`
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "DELETE FROM integrity_events WHERE contest_id IN ("+old+")", cutoff)
	if err != nil {
		return 0, 0, err
	}
	events, _ = res.RowsAffected()
	res, err = tx.ExecContext(ctx, `UPDATE submissions SET client_hash = '', typed_chars = 0, pasted_chars = 0, focus_losses = 0
		WHERE contest_id IN (`+old+`) AND (client_hash <> '' OR typed_chars <> 0 OR pasted_chars <> 0 OR focus_losses <> 0)`, cutoff)
	if err != nil {
		return 0, 0, err
	}
	subs, _ = res.RowsAffected()
	return events, subs, tx.Commit()
}
