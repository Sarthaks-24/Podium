package contests

// AI problems in graded contests (security council S10). The wizard's
// reference solution is the model's code: tests generated from it agree
// with it by construction, so validation cannot catch a wrong reference.
// A graded contest takes such a problem only once a person has written a
// solution (problems.solution_by_person, set when an editor save changes
// the reference or the brute force) or an admin has given a reason, which
// the audit log keeps.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// minAIReason is the shortest reason accepted for grading an AI problem
// as it is.
const minAIReason = 10

// AIProblemError refuses AI problems no person has checked in a graded
// contest. Labels are the problems concerned. Grading is set when the
// refused change was turning grading on, rather than adding problems.
type AIProblemError struct {
	Labels  []string
	Grading bool
}

func (e *AIProblemError) Error() string {
	which := "Problem " + e.Labels[0]
	if len(e.Labels) > 1 {
		which = "Problems " + strings.Join(e.Labels, ", ")
	}
	if e.Grading {
		return fmt.Sprintf("%s came from the AI wizard and no person has written a solution for it. Write a solution of your own and validate it, or remove the problem, turn grading on, and add it again with a reason.", which)
	}
	return fmt.Sprintf("%s came from the AI wizard and no person has written a solution for it. Write a solution of your own and validate it, or give a reason of at least %d characters to grade it as it is.", which, minAIReason)
}

// aiConfirmedIn returns the reason an admin gave to grade an AI problem in
// a contest, or "" when there is none.
func (s *Service) aiConfirmedIn(ctx context.Context, contestID, problemID int64) (string, error) {
	var reason sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT json_extract(details, '$.reason') FROM contest_audit
		WHERE contest_id = ? AND action = 'ai_problem' AND json_extract(details, '$.problem_id') = ? ORDER BY id DESC LIMIT 1`,
		contestID, problemID).Scan(&reason)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return reason.String, err
}

// unconfirmedAI reports whether a problem came from the AI wizard, no
// person has written a solution for it, and no admin has confirmed it for
// this contest.
func unconfirmedAI(ctx context.Context, tx *sql.Tx, contestID, problemID int64) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM problems p WHERE p.id = ? AND p.origin = 'ai' AND p.solution_by_person = 0
		AND NOT EXISTS (SELECT 1 FROM contest_audit a WHERE a.contest_id = ? AND a.action = 'ai_problem'
			AND json_extract(a.details, '$.problem_id') = p.id)`, problemID, contestID).Scan(&n)
	return n > 0, err
}
