package integrity

// A case is one organiser decision about one student in one contest, with
// a written reason (v3, owner answer B on 2026-10-26). There is no rating,
// no second reviewer, no appeal and no deadline: the college's own process
// handles the rest. "Exclude from this contest" hides the student from the
// board; a later decision of "No action" or "Warning" brings them back.

import (
	"context"
	"strings"

	"github.com/Sarthaks-24/podium/internal/contests"
)

// Decisions an organiser can record.
const (
	DecisionNoAction = "no_action"
	DecisionWarning  = "warning"
	DecisionExcluded = "excluded"
)

// minReason is the shortest reason accepted for a decision.
const minReason = 10

// InputError is a request the caller can fix; its message is shown.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// Case is the latest decision about one student in one contest.
type Case struct {
	ID        int64  `json:"id"`
	ContestID int64  `json:"contest_id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	DecidedBy string `json:"decided_by"`
	DecidedAt int64  `json:"decided_at"`
}

// Decide records (or replaces) the decision about userID in the contest,
// with the audit row in the same transaction, and refreshes the board.
// The student must have taken part.
func (s *Service) Decide(ctx context.Context, contestID, userID int64, by contests.Actor, decision, reason string) (*Case, error) {
	switch decision {
	case DecisionNoAction, DecisionWarning, DecisionExcluded:
	default:
		return nil, &InputError{"Choose No action, Warning or Exclude from this contest."}
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) < minReason {
		return nil, &InputError{"Write the reason in a sentence or two."}
	}
	if len(reason) > 4000 {
		return nil, &InputError{"Keep the reason under 4000 characters."}
	}
	in, err := s.Participant(ctx, contestID, userID)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO integrity_cases (contest_id, user_id, decision, reason, decided_by, decided_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (contest_id, user_id) DO UPDATE SET decision = excluded.decision,
		reason = excluded.reason, decided_by = excluded.decided_by, decided_at = excluded.decided_at`,
		contestID, userID, decision, reason, by.ID, now); err != nil {
		return nil, err
	}
	if err := contests.RecordTx(ctx, tx, contestID, by, contests.AuditDecision, map[string]any{"user_id": userID, "decision": decision}, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if s.Contests != nil {
		s.Contests.Invalidate(contestID)
	}
	return s.caseOf(ctx, contestID, userID)
}

// Cases lists the contest's decisions, newest first.
func (s *Service) Cases(ctx context.Context, contestID int64) ([]Case, error) {
	return s.cases(ctx, "c.contest_id = ?", contestID)
}

func (s *Service) caseOf(ctx context.Context, contestID, userID int64) (*Case, error) {
	cs, err := s.cases(ctx, "c.contest_id = ? AND c.user_id = ?", contestID, userID)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, ErrNotFound
	}
	return &cs[0], nil
}

func (s *Service) cases(ctx context.Context, where string, args ...any) ([]Case, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.contest_id, c.user_id,
		coalesce(nullif(u.name, ''), u.email), u.email, c.decision, c.reason,
		coalesce(b.email, ''), c.decided_at
		FROM integrity_cases c JOIN users u ON u.id = c.user_id
		LEFT JOIN users b ON b.id = c.decided_by
		WHERE `+where+` ORDER BY c.decided_at DESC, c.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Case{}
	for rows.Next() {
		var c Case
		if err := rows.Scan(&c.ID, &c.ContestID, &c.UserID, &c.Name, &c.Email, &c.Decision, &c.Reason, &c.DecidedBy, &c.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
