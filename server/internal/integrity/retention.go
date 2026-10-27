package integrity

import (
	"context"

	"github.com/Sarthaks-24/podium/internal/contests"
)

// RetainSignals deletes the integrity checks (runs) and their signals of
// every contest that ended for everyone more than contests.RetentionPeriod
// ago, the same cutoff contests.Retain uses for the integrity records.
// Decisions are not touched: their reason says what they rested on. It is idempotent
// and returns how many signals and runs it deleted.
func (s *Service) RetainSignals(ctx context.Context) (signals, runs int64, err error) {
	cutoff := s.now().Add(-contests.RetentionPeriod).UnixMilli()
	// The contest's final moment (the contest_final view), as in
	// contests.Retain.
	const old = `SELECT contest_id FROM contest_final WHERE final_at < ?`
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "DELETE FROM integrity_signals WHERE contest_id IN ("+old+")", cutoff)
	if err != nil {
		return 0, 0, err
	}
	signals, _ = res.RowsAffected()
	// A check still running is left alone; it ends in an error or done,
	// and the next pass removes it.
	res, err = tx.ExecContext(ctx, "DELETE FROM integrity_runs WHERE state <> 'running' AND contest_id IN ("+old+")", cutoff)
	if err != nil {
		return 0, 0, err
	}
	runs, _ = res.RowsAffected()
	return signals, runs, tx.Commit()
}
