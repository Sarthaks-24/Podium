package auth

import (
	"context"
	"time"

	"github.com/Sarthaks-24/podium/internal/retention"
)

// IdleStudentPeriod is how long a student account may go unseen before
// the semester rollover disables it (v2 plan section 9). Disabled, not
// deleted: an owner can enable it again on People.
const IdleStudentPeriod = 180 * retention.Day

// AuditSemesterRollover is the site audit action of a rollover.
const AuditSemesterRollover = "semester_rollover"

// rolloverEmails bounds the emails one audit row lists (the roster cap).
const rolloverEmails = 5000

// IdleStudents lists the enabled student accounts last seen (signed in,
// or created when never signed in) before seenBefore, by email.
func (s *Service) IdleStudents(ctx context.Context, seenBefore time.Time) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, email, name, role, disabled, last_login_at FROM users
		WHERE role = 'student' AND disabled = 0 AND email <> ? AND coalesce(last_login_at, created_at) < ? ORDER BY email`,
		s.OwnerEmail, seenBefore.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Email, &a.Name, &a.Role, &a.Disabled, &a.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RolloverReport is what DisableIdle did.
type RolloverReport struct {
	Disabled      []Account
	SessionsEnded int64
}

// DisableIdle disables the accounts IdleStudents lists and ends their
// sessions, in one transaction with one site audit row naming the label,
// the counts, the contests archived (archived, from the caller) and the
// emails.
func (s *Service) DisableIdle(ctx context.Context, seenBefore time.Time, label string, archived []map[string]any) (RolloverReport, error) {
	list, err := s.IdleStudents(ctx, seenBefore)
	if err != nil {
		return RolloverReport{}, err
	}
	return s.disableListed(ctx, list, seenBefore, label, archived)
}

// disableListed is DisableIdle after its read: each listed account is
// disabled only if it is still an idle, enabled student inside the
// transaction.
func (s *Service) disableListed(ctx context.Context, list []Account, seenBefore time.Time, label string, archived []map[string]any) (RolloverReport, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return RolloverReport{}, err
	}
	defer tx.Rollback()
	var r RolloverReport
	emails := []string{}
	for _, a := range list {
		// Still idle now: a student who signed in after the list was read
		// keeps their account.
		res, err := tx.ExecContext(ctx, `UPDATE users SET disabled = 1 WHERE id = ? AND disabled = 0 AND role = 'student'
			AND coalesce(last_login_at, created_at) < ?`, a.ID, seenBefore.UnixMilli())
		if err != nil {
			return RolloverReport{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		res, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", a.ID)
		if err != nil {
			return RolloverReport{}, err
		}
		n, _ := res.RowsAffected()
		r.SessionsEnded += n
		a.Disabled = true
		r.Disabled = append(r.Disabled, a)
		if len(emails) < rolloverEmails {
			emails = append(emails, a.Email)
		}
	}
	if archived == nil {
		archived = []map[string]any{}
	}
	if err := s.recordSite(ctx, tx, AuditSemesterRollover, "", map[string]any{"label": label, "accounts_disabled": len(r.Disabled),
		"sessions_ended": r.SessionsEnded, "emails": emails, "contests_archived": archived,
		"seen_before": seenBefore.UnixMilli()}); err != nil {
		return RolloverReport{}, err
	}
	return r, tx.Commit()
}
