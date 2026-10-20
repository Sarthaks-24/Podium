package integrity

// The clocks of a case (v2 plan B4.3, input 20 defaults): a student has 7
// days to reply from first opening the notice, never more than 21 from
// when it became available, with a reminder on day 5; reviewers have 14
// days to decide once the case is ready; a student has 14 days to appeal
// an outcome or an adjustment, and an appeal is ruled on within 14 days.
// Every limit reads the service's clock, so the tests fix it.

import (
	"context"
	"database/sql"
	"time"

	"github.com/Sarthaks-24/podium/internal/notifications"
)

// The time limits. ResponseWindow (cases.go) is the 7 days to reply.
const (
	// ReplyCap is the longest a student has to reply, from when the
	// notice became available, whether or not they opened it.
	ReplyCap = 21 * 24 * time.Hour
	// ReminderAfter is when the reminder goes, from the first open (or
	// from availability, if never opened).
	ReminderAfter = 5 * 24 * time.Hour
	// DecideWindow is how long reviewers have once a case is ready.
	DecideWindow = 14 * 24 * time.Hour
	// AppealWindow is how long a student has to appeal.
	AppealWindow = 14 * 24 * time.Hour
	// RulingWindow is how long reviewers have to rule on an appeal.
	RulingWindow = 14 * 24 * time.Hour
)

// respondBy is a student's reply deadline for the latest share, or 0 when
// no turn is open. Shares made before v1.4.0 (no availability recorded)
// keep the old rule: 7 days from the share.
func respondBy(asked, available, opened sql.NullInt64) int64 {
	if !asked.Valid {
		return 0
	}
	if !available.Valid {
		return asked.Int64 + ResponseWindow.Milliseconds()
	}
	limit := available.Int64 + ReplyCap.Milliseconds()
	if opened.Valid {
		return min(opened.Int64+ResponseWindow.Milliseconds(), limit)
	}
	return limit
}

// replied reports whether a student explained after the latest share.
func (u *CaseUser) replied() bool {
	return u.AskedAt != nil && u.ExplainedAt != nil && *u.ExplainedAt >= *u.AskedAt
}

// turnsDone reports whether every student replied after the latest share
// or ran out of time, the condition for proposing an outcome.
func turnsDone(c *Case, now int64) bool {
	for _, u := range c.Users {
		if u.AskedAt == nil {
			return false
		}
		if !u.replied() && (u.RespondBy == nil || now < *u.RespondBy) {
			return false
		}
	}
	return len(c.Users) > 0
}

// setClocks fills a shared case's ready_at, decide_by and overdue.
func setClocks(c *Case, now int64) {
	if c.State != AwaitingStudent {
		return
	}
	var ready int64
	for _, u := range c.Users {
		if u.AskedAt == nil {
			return
		}
		at := int64(0)
		if u.replied() {
			at = *u.ExplainedAt
		} else if u.RespondBy != nil {
			at = *u.RespondBy
		}
		ready = max(ready, at)
	}
	decide := ready + DecideWindow.Milliseconds()
	c.ReadyAt, c.DecideBy = &ready, &decide
	c.Overdue = now > decide
}

// RespondBy is one student's reply deadline in a case, or nil when no turn
// is open.
func (s *Service) RespondBy(ctx context.Context, caseID, userID int64) (*int64, error) {
	var asked, available, opened sql.NullInt64
	var state string
	err := s.DB.QueryRowContext(ctx, `SELECT c.state, cu.asked_at, cu.notice_available_at, cu.first_opened_at
		FROM integrity_case_users cu JOIN integrity_cases c ON c.id = cu.case_id WHERE cu.case_id = ? AND cu.user_id = ?`,
		caseID, userID).Scan(&state, &asked, &available, &opened)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state != AwaitingStudent {
		return nil, nil
	}
	if by := respondBy(asked, available, opened); by > 0 {
		return &by, nil
	}
	return nil, nil
}

// NoticeDue is the deadline a notice names, for notifications.Service.Due:
// the reply deadline of a share or reminder, the appeal deadline of an
// outcome for this student or of an adjustment.
func (s *Service) NoticeDue(ctx context.Context, n notifications.Row) (*int64, error) {
	switch n.Kind {
	case notifications.CaseShared, notifications.Reminder:
		return s.RespondBy(ctx, n.CaseID, n.UserID)
	case notifications.CaseDecided:
		var at int64
		var mine bool
		err := s.DB.QueryRowContext(ctx, `SELECT created_at, outcome <> '' AND EXISTS (SELECT 1 FROM json_each(outcome_users) WHERE value = ?)
			FROM integrity_decisions WHERE id = ?`, n.UserID, n.DecisionID).Scan(&at, &mine)
		if err == sql.ErrNoRows || (err == nil && !mine) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		by := at + AppealWindow.Milliseconds()
		return &by, nil
	case notifications.StandingAdjusted:
		var at int64
		err := s.DB.QueryRowContext(ctx, "SELECT at FROM rating_history WHERE id = ? AND user_id = ?", n.RatingHistoryID, n.UserID).Scan(&at)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		by := at + AppealWindow.Milliseconds()
		return &by, nil
	}
	return nil, nil
}

// SweepReminders sends the day-5 reminder to every student with an open
// turn who has not replied, once per share, and returns how many went.
// A reminder after the deadline is never sent.
func (s *Service) SweepReminders(ctx context.Context) (int, error) {
	now := s.now().UnixMilli()
	rows, err := s.DB.QueryContext(ctx, `SELECT cu.case_id, cu.user_id, cu.asked_at, cu.notice_available_at, cu.first_opened_at, c.contest_id
		FROM integrity_case_users cu JOIN integrity_cases c ON c.id = cu.case_id
		WHERE c.state = 'awaiting_student' AND cu.asked_at IS NOT NULL AND cu.notice_available_at IS NOT NULL AND cu.reminded_at IS NULL
		AND coalesce(cu.first_opened_at, cu.notice_available_at) <= ?
		AND NOT EXISTS (SELECT 1 FROM integrity_explanations e WHERE e.case_id = cu.case_id AND e.user_id = cu.user_id AND e.created_at >= cu.asked_at)`,
		now-ReminderAfter.Milliseconds())
	if err != nil {
		return 0, err
	}
	type due struct {
		caseID, userID, contestID int64
		by                        int64
	}
	var list []due
	for rows.Next() {
		var d due
		var asked, available, opened sql.NullInt64
		if err := rows.Scan(&d.caseID, &d.userID, &asked, &available, &opened, &d.contestID); err != nil {
			rows.Close()
			return 0, err
		}
		d.by = respondBy(asked, available, opened)
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	sent := 0
	for _, d := range list {
		if now >= d.by {
			continue
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return sent, err
		}
		res, err := tx.ExecContext(ctx, "UPDATE integrity_case_users SET reminded_at = ? WHERE case_id = ? AND user_id = ? AND reminded_at IS NULL",
			now, d.caseID, d.userID)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 1 {
				err = notifications.InsertTx(ctx, tx, notifications.Row{UserID: d.userID, Kind: notifications.Reminder, CaseID: d.caseID,
					ContestID: d.contestID, CreatedAt: now})
			}
		}
		if err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}
