package integrity

import (
	"context"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
)

// CaseRetention is how long a closed case keeps its evidence and the
// students' explanations (integrity plan section 3). After it both are
// emptied, every explanation version included; the case row and its
// decision log stay, so the outcome and the reasons for it remain on
// record.
const CaseRetention = 2 * 365 * 24 * time.Hour

// Retain applies both integrity retention rules and returns how many
// cases it emptied:
//
//   - RetainSignals: a contest's checks and signals go once its integrity
//     records pass contests.RetentionPeriod.
//   - Cases closed more than CaseRetention ago lose their evidence and
//     explanations. Open and awaiting_student cases are never touched.
//
// It is idempotent. Signal and run deletions are logged when Log is set.
func (s *Service) Retain(ctx context.Context) (int64, error) {
	signals, runs, err := s.RetainSignals(ctx)
	if err != nil {
		return 0, err
	}
	if (signals > 0 || runs > 0) && s.Log != nil {
		s.Log.Info("integrity checks past retention removed", "signals", signals, "runs", runs)
	}
	cutoff := s.now().Add(-CaseRetention).UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	const old = `SELECT id FROM integrity_cases WHERE state IN ('cleared', 'actioned') AND closed_at IS NOT NULL AND closed_at < ?`
	if _, err := tx.ExecContext(ctx, `UPDATE integrity_case_users SET explanation = '' WHERE explanation <> '' AND case_id IN (`+old+`)`,
		cutoff); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM integrity_explanations WHERE case_id IN (`+old+`)`, cutoff); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE integrity_cases SET evidence = '[]' WHERE evidence <> '[]' AND id IN (`+old+`)`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}

// RetainSignals deletes the integrity checks (runs) and their signals of
// every contest that ended for everyone more than contests.RetentionPeriod
// ago, the same cutoff contests.Retain uses for the integrity records.
// Cases are not touched: each keeps its own copy of the signals it was
// opened from, so an open case still has its evidence. It is idempotent
// and returns how many signals and runs it deleted.
func (s *Service) RetainSignals(ctx context.Context) (signals, runs int64, err error) {
	cutoff := s.now().Add(-contests.RetentionPeriod).UnixMilli()
	// The contest's final moment: its close plus the longest extension, as
	// in contests.Retain.
	const old = `SELECT c.id FROM contests c WHERE c.ends_at + 60000 * (c.extended_minutes +
		coalesce((SELECT max(minutes) FROM contest_extensions e WHERE e.contest_id = c.id), 0)) < ?`
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
