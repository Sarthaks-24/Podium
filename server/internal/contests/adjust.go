package contests

// Zero for problem (D77 amended): a confirmed review decision sets one
// student's score on one problem to zero on the board. The decision lives
// in the integrity tables; this file keeps the board's side, the
// outcome_adjustments rows that Compute reads on every call. It is not the
// rejudge path (submissions.Rejudge rewrites verdicts), so a later rejudge
// cannot bring the score back. An upheld appeal removes the row.

import (
	"context"
	"database/sql"
)

// Adjustments returns the contest's zeroed problems, by student.
func (s *Service) Adjustments(ctx context.Context, contestID int64) (map[ZeroKey]bool, error) {
	return s.adjustmentsSince(ctx, contestID, 0)
}

// adjustmentsSince is Adjustments for decisions made at or after since.
func (s *Service) adjustmentsSince(ctx context.Context, contestID, since int64) (map[ZeroKey]bool, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT user_id, problem_id FROM outcome_adjustments WHERE contest_id = ? AND created_at >= ?", contestID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out map[ZeroKey]bool
	for rows.Next() {
		var k ZeroKey
		if err := rows.Scan(&k.UserID, &k.ProblemID); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[ZeroKey]bool{}
		}
		out[k] = true
	}
	return out, rows.Err()
}

// ZeroedFor returns, for one student in one contest, each zeroed problem
// with the case whose decision zeroed it: the student's own contest page
// says "Zero on this problem by decision. See your case".
func (s *Service) ZeroedFor(ctx context.Context, contestID, userID int64) (map[int64]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.problem_id, d.case_id FROM outcome_adjustments a
		JOIN integrity_decisions d ON d.id = a.decision_id WHERE a.contest_id = ? AND a.user_id = ?`, contestID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var p, c int64
		if err := rows.Scan(&p, &c); err != nil {
			return nil, err
		}
		out[p] = c
	}
	return out, rows.Err()
}

// AddZeroTx records, inside the decision's transaction, that the decision
// zeroes the student's problem, and marks when the board changed. The
// caller calls Invalidate and RebuildSummaries after the commit.
func AddZeroTx(ctx context.Context, tx *sql.Tx, contestID, userID, problemID, decisionID, now int64) error {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO outcome_adjustments (contest_id, user_id, problem_id, decision_id, created_at)
		VALUES (?, ?, ?, ?, ?)`, contestID, userID, problemID, decisionID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE contests SET board_adjusted_at = ? WHERE id = ?", now, contestID)
	return err
}

// RemoveZeroTx deletes the zero one decision put on one student's board
// (an upheld appeal) and reports whether there was one.
func RemoveZeroTx(ctx context.Context, tx *sql.Tx, decisionID, userID, now int64) (contestID int64, removed bool, err error) {
	err = tx.QueryRowContext(ctx, "SELECT contest_id FROM outcome_adjustments WHERE decision_id = ? AND user_id = ? LIMIT 1",
		decisionID, userID).Scan(&contestID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM outcome_adjustments WHERE decision_id = ? AND user_id = ?", decisionID, userID); err != nil {
		return 0, false, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE contests SET board_adjusted_at = ? WHERE id = ?", now, contestID)
	return contestID, true, err
}
