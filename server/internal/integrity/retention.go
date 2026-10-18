package integrity

import (
	"context"
	"time"
)

// CaseRetention is how long a closed case keeps its evidence and the
// students' explanations (integrity plan section 3). After it both are
// emptied; the case row and its decision log stay, so the outcome and the
// reasons for it remain on record.
const CaseRetention = 2 * 365 * 24 * time.Hour

// Retain empties the evidence and explanations of cases closed more than
// CaseRetention ago. Open and awaiting_student cases are never touched. It
// is idempotent and returns how many cases it emptied.
func (s *Service) Retain(ctx context.Context) (int64, error) {
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
	res, err := tx.ExecContext(ctx, `UPDATE integrity_cases SET evidence = '[]' WHERE evidence <> '[]' AND id IN (`+old+`)`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
