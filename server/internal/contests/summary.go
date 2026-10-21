package contests

// Per-student contest summaries (v2 plan B4.7): one row per student on a
// contest's final board, written when the contest turns final (every
// window closed, judging drained) and again after a rejudge of a final
// contest or a review decision that changed its board. Never during a
// live contest. Progress, the students table and the class view read them,
// so none of those recompute a board per request.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/notifications"
)

// ErrNotFinal refuses a summary build before the results are final.
var ErrNotFinal = errors.New("the contest's results are not final yet")

// SubtaskEarned is one subtask's points on the board.
type SubtaskEarned struct {
	Idx    int `json:"idx"`
	Points int `json:"points"`
	Earned int `json:"earned"`
}

// SummaryProblem is one problem of a student's summary.
type SummaryProblem struct {
	Label         string          `json:"label"`
	ProblemID     int64           `json:"problem_id"`
	Score         int             `json:"score"`
	MaxScore      int             `json:"max_score"`
	Attempts      int             `json:"attempts"`
	FullAtMinutes *int            `json:"full_at_minutes"`
	BestVerdict   string          `json:"best_verdict"`
	Subtasks      []SubtaskEarned `json:"subtasks"`
	Zeroed        bool            `json:"zeroed"`
}

// Summary is one student's result in one final contest.
type Summary struct {
	ContestID         int64            `json:"contest_id"`
	UserID            int64            `json:"user_id"`
	Graded            bool             `json:"graded"`
	Feedback          string           `json:"feedback"`
	Score             int              `json:"score"`
	MaxScore          int              `json:"max_score"`
	Percent           float64          `json:"percent"`
	ProblemsAttempted int              `json:"problems_attempted"`
	ProblemsFull      int              `json:"problems_full"`
	Submissions       int              `json:"submissions"`
	Accepted          int              `json:"accepted"`
	Rank              int              `json:"rank"`
	RankedCount       int              `json:"ranked_count"`
	Problems          []SummaryProblem `json:"problems"`
	FinalisedAt       int64            `json:"finalised_at"`
}

// Percent rounds score of max to one decimal; 0 when max is 0.
func Percent(score, max int) float64 {
	if max <= 0 {
		return 0
	}
	return math.Round(float64(score)*1000/float64(max)) / 10
}

// IsFinal reports whether the contest's results are final now: every
// window closed and nothing waiting for the judge.
func (s *Service) IsFinal(ctx context.Context, c *Contest) (bool, error) {
	if s.now().Before(c.Final()) {
		return false, nil
	}
	var pending int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM submissions WHERE contest_id = ? AND kind = 'submit' AND practice = 0
		AND state IN ('queued', 'running')`, c.ID).Scan(&pending)
	return pending == 0, err
}

// StudentsSeeFinal reports whether students see the final board: the
// results are final and no held reveal is pending.
func (c *Contest) StudentsSeeFinal() bool { return !(c.HoldReveal && c.RevealedAt.IsZero()) }

// BuildSummaries writes every summary of a final contest in one
// transaction, replacing the previous ones, and records when. Erased
// accounts get none. When students may see the results and nobody was
// told yet, each participant gets a results_final notice. It returns how
// many rows it wrote, or ErrNotFinal.
func (s *Service) BuildSummaries(ctx context.Context, c *Contest) (int, error) {
	// summarised_at is the time before the final check and the compute:
	// a rejudge or a decision recorded while they run is newer than it,
	// so the next sweep builds again (council C1-006).
	return s.buildSummaries(ctx, c, s.now().UnixMilli())
}

// buildSummaries is BuildSummaries with the start time given: c must have
// been read at or after start, so every change older than start is in it.
func (s *Service) buildSummaries(ctx context.Context, c *Contest, start int64) (int, error) {
	// A contest past retention has lost its submissions and summaries;
	// building again would give every participant a zero.
	var expired int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM contest_results WHERE contest_id = ?", c.ID).Scan(&expired); err != nil {
		return 0, err
	}
	if expired > 0 {
		return 0, nil
	}
	final, err := s.IsFinal(ctx, c)
	if err != nil {
		return 0, err
	}
	if !final {
		return 0, ErrNotFinal
	}
	list, err := s.computeSummaries(ctx, c)
	if err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM student_contest_summary WHERE contest_id = ?", c.ID); err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO student_contest_summary (contest_id, user_id, graded, feedback, score, max_score,
		percent, problems_attempted, problems_full, submissions, accepted, rank, ranked_count, problems, finalised_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, sm := range list {
		js, _ := json.Marshal(sm.Problems)
		if _, err := stmt.ExecContext(ctx, sm.ContestID, sm.UserID, sm.Graded, sm.Feedback, sm.Score, sm.MaxScore, sm.Percent,
			sm.ProblemsAttempted, sm.ProblemsFull, sm.Submissions, sm.Accepted, sm.Rank, sm.RankedCount, string(js), now); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE contests SET summarised_at = ? WHERE id = ?", start, c.ID); err != nil {
		return 0, err
	}
	if err := s.notifyResultsTx(ctx, tx, c, now); err != nil {
		return 0, err
	}
	return len(list), tx.Commit()
}

// notifyResultsTx sends results_final to every summarised participant
// once, when students may see the final board.
func (s *Service) notifyResultsTx(ctx context.Context, tx *sql.Tx, c *Contest, now int64) error {
	if !c.StudentsSeeFinal() {
		return nil
	}
	var notified sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT results_notified_at FROM contests WHERE id = ?", c.ID).Scan(&notified); err != nil {
		return err
	}
	if notified.Valid {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT user_id FROM student_contest_summary WHERE contest_id = ?", c.ID)
	if err != nil {
		return err
	}
	var users []int64
	for rows.Next() {
		var u int64
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		users = append(users, u)
	}
	rows.Close()
	// A short list would still mark the contest as told.
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range users {
		if err := notifications.InsertTx(ctx, tx, notifications.Row{UserID: u, Kind: notifications.ResultsFinal, ContestID: c.ID,
			CreatedAt: now}); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE contests SET results_notified_at = ? WHERE id = ?", now, c.ID)
	return err
}

// RebuildSummaries rewrites a contest's summaries if it was summarised
// before (after a review decision changed its board); otherwise the sweep
// writes them when the contest turns final.
func (s *Service) RebuildSummaries(ctx context.Context, contestID int64) error {
	c, err := s.Get(ctx, contestID)
	if err != nil {
		return err
	}
	if c.SummarisedAt.IsZero() {
		return nil
	}
	_, err = s.BuildSummaries(ctx, c)
	if errors.Is(err, ErrNotFinal) {
		return nil
	}
	return err
}

// summaryTriggers are the audit actions after which a final contest's
// summaries are written again: a rejudge, a pin moved to a new revision,
// a change of problems, extra time (which can make a final contest live
// again until it ends), a review decision or appeal that changed the
// board, and a settings change (graded, scoring and feedback are stored
// in each summary).
var summaryTriggers = []string{AuditRejudge, "revision", AuditProblems, AuditExtension, AuditExtend, AuditBoardAdjusted, AuditSettings}

// SweepSummaries writes the summaries of every contest that turned final
// since the last pass, or was rejudged or changed after its summaries
// were written, and tells students of a held contest once it is
// revealed. It returns how many contests it summarised.
func (s *Service) SweepSummaries(ctx context.Context) (int, error) {
	now := s.now().UnixMilli()
	marks := strings.TrimSuffix(strings.Repeat("?,", len(summaryTriggers)), ",")
	args := []any{now}
	for _, a := range summaryTriggers {
		args = append(args, a)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id FROM contests c WHERE c.ends_at < ? AND (c.summarised_at IS NULL
		OR EXISTS (SELECT 1 FROM contest_audit a WHERE a.contest_id = c.id AND a.created_at >= c.summarised_at AND a.action IN (`+marks+`))
		OR (c.results_notified_at IS NULL AND (c.hold_reveal = 0 OR c.revealed_at IS NOT NULL)))
		AND (EXISTS (SELECT 1 FROM contest_starts cs WHERE cs.contest_id = c.id) OR c.summarised_at IS NOT NULL)
		AND NOT EXISTS (SELECT 1 FROM contest_results r WHERE r.contest_id = c.id)
		ORDER BY c.id`, args...)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	built := 0
	var errs []error
	for _, id := range ids {
		// The build's start is taken before the contest is read, so a
		// change committed in between is in the build or newer than its
		// summarised_at, never lost (C2-035).
		start := s.now().UnixMilli()
		c, err := s.Get(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var stale bool
		if !c.SummarisedAt.IsZero() {
			if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM contest_audit WHERE contest_id = ? AND created_at >= ?
				AND action IN (`+marks+`))`, append([]any{c.ID, c.SummarisedAt.UnixMilli()}, args[1:]...)...).Scan(&stale); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if c.SummarisedAt.IsZero() || stale {
			if _, err := s.buildSummaries(ctx, c, start); err == nil {
				built++
			} else if !errors.Is(err, ErrNotFinal) {
				errs = append(errs, err)
			}
			continue
		}
		// Summarised and current: only a reveal may be left to announce.
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		err = s.notifyResultsTx(ctx, tx, c, now)
		if err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return built, errors.Join(errs...)
}

// summaryEntry is one judged submission as the summary reads it.
type summaryEntry struct {
	userID, problemID int64
	at                int64
	verdict           string
	score             int
}

// computeSummaries scores the full board, review decisions applied, and
// derives each participant's summary.
func (s *Service) computeSummaries(ctx context.Context, c *Contest) ([]Summary, error) {
	b := s.scoringBoard(c)
	var err error
	if b.Zeroed, err = s.Adjustments(ctx, c.ID); err != nil {
		return nil, err
	}
	people, err := s.participants(ctx, c)
	if err != nil {
		return nil, err
	}
	forgotten := map[int64]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM users WHERE email LIKE '%@removed.invalid' AND id IN (
		SELECT user_id FROM contest_starts WHERE contest_id = ? UNION SELECT user_id FROM submissions WHERE contest_id = ?)`, c.ID, c.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		forgotten[id] = true
	}
	rows.Close()
	entries, _, err := s.entries(ctx, c, 0)
	if err != nil {
		return nil, err
	}
	board := Compute(b, people, entries, false)
	starts := map[int64]time.Time{}
	for _, p := range people {
		starts[p.UserID] = p.Started
	}
	// Verdicts and attempts: every judged, counting submission.
	srows, err := s.DB.QueryContext(ctx, `SELECT user_id, problem_id, created_at, verdict, score FROM submissions
		WHERE contest_id = ? AND kind = 'submit' AND practice = 0 AND state = 'done' ORDER BY created_at, id`, c.ID)
	if err != nil {
		return nil, err
	}
	byUser := map[int64][]summaryEntry{}
	for srows.Next() {
		var e summaryEntry
		if err := srows.Scan(&e.userID, &e.problemID, &e.at, &e.verdict, &e.score); err != nil {
			srows.Close()
			return nil, err
		}
		byUser[e.userID] = append(byUser[e.userID], e)
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}
	// The subtask breakdown: best per subtask, or with best_submission
	// scoring the subtasks of the best submission.
	earned := map[ZeroKey][]int{}
	bestScore := map[ZeroKey]int{}
	for _, e := range entries {
		if !e.Done || b.Zeroed[ZeroKey{e.UserID, e.ProblemID}] {
			continue
		}
		k := ZeroKey{e.UserID, e.ProblemID}
		if c.Scoring == BestSubmission {
			if prev, seen := bestScore[k]; !seen || e.Score > prev {
				bestScore[k], earned[k] = e.Score, append([]int{}, e.Earned...)
			}
			continue
		}
		for i, v := range e.Earned {
			if len(earned[k]) <= i {
				earned[k] = append(earned[k], make([]int, i+1-len(earned[k]))...)
			}
			if v > earned[k][i] {
				earned[k][i] = v
			}
		}
	}
	out := make([]Summary, 0, len(board))
	for _, row := range board {
		if forgotten[row.UserID] {
			continue
		}
		sm := Summary{ContestID: c.ID, UserID: row.UserID, Graded: c.Graded, Feedback: c.Feedback, Score: row.Total,
			Rank: row.Rank, RankedCount: len(board), Problems: make([]SummaryProblem, 0, len(c.Problems))}
		mine := byUser[row.UserID]
		for _, e := range mine {
			sm.Submissions++
			if e.verdict == "AC" {
				sm.Accepted++
			}
		}
		for i, cp := range c.Problems {
			cell := row.Cells[i]
			sp := SummaryProblem{Label: cp.Label, ProblemID: cp.ProblemID, Score: cell.Score, MaxScore: cell.Max,
				Subtasks: make([]SubtaskEarned, 0, len(cp.Points))}
			k := ZeroKey{row.UserID, cp.ProblemID}
			sp.Zeroed = b.Zeroed[k]
			best := -1
			for _, e := range mine {
				if e.problemID != cp.ProblemID {
					continue
				}
				sp.Attempts++
				if !sp.Zeroed && e.score > best {
					best, sp.BestVerdict = e.score, e.verdict
				}
				if !sp.Zeroed && sp.FullAtMinutes == nil && cell.Max > 0 && e.score >= cell.Max {
					m := int(time.UnixMilli(e.at).Sub(starts[row.UserID]) / time.Minute)
					sp.FullAtMinutes = &m
				}
			}
			for j, pts := range cp.Points {
				got := 0
				if !sp.Zeroed && j < len(earned[k]) {
					got = min(max(earned[k][j], 0), pts)
				}
				sp.Subtasks = append(sp.Subtasks, SubtaskEarned{Idx: j, Points: pts, Earned: got})
			}
			if cell.Tried {
				sm.ProblemsAttempted++
			}
			if cell.Max > 0 && cell.Score >= cell.Max {
				sm.ProblemsFull++
			}
			sm.MaxScore += cell.Max
			sm.Problems = append(sm.Problems, sp)
		}
		sm.Percent = Percent(sm.Score, sm.MaxScore)
		out = append(out, sm)
	}
	return out, nil
}

// Summaries lists one student's summaries, newest contest first.
func (s *Service) Summaries(ctx context.Context, userID int64) ([]Summary, error) {
	return s.querySummaries(ctx, `SELECT s.contest_id, s.user_id, s.graded, s.feedback, s.score, s.max_score, s.percent,
		s.problems_attempted, s.problems_full, s.submissions, s.accepted, s.rank, s.ranked_count, s.problems, s.finalised_at
		FROM student_contest_summary s JOIN contests c ON c.id = s.contest_id WHERE s.user_id = ?
		ORDER BY c.ends_at DESC, c.id DESC`, userID)
}

// ContestSummaries lists a contest's summaries in rank order.
func (s *Service) ContestSummaries(ctx context.Context, contestID int64) ([]Summary, error) {
	return s.querySummaries(ctx, `SELECT contest_id, user_id, graded, feedback, score, max_score, percent, problems_attempted,
		problems_full, submissions, accepted, rank, ranked_count, problems, finalised_at
		FROM student_contest_summary WHERE contest_id = ? ORDER BY rank, user_id`, contestID)
}

func (s *Service) querySummaries(ctx context.Context, q string, args ...any) ([]Summary, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var sm Summary
		var probs string
		if err := rows.Scan(&sm.ContestID, &sm.UserID, &sm.Graded, &sm.Feedback, &sm.Score, &sm.MaxScore, &sm.Percent,
			&sm.ProblemsAttempted, &sm.ProblemsFull, &sm.Submissions, &sm.Accepted, &sm.Rank, &sm.RankedCount, &probs,
			&sm.FinalisedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(probs), &sm.Problems)
		out = append(out, sm)
	}
	return out, rows.Err()
}

// MedianPercent is the median of the summaries' percents (0 for none).
func MedianPercent(list []Summary) float64 {
	p := make([]float64, len(list))
	for i, sm := range list {
		p[i] = sm.Percent
	}
	return medianOf(p)
}

// ClassMedian is MedianPercent of a contest's summaries, read from the
// percent column alone: the problems JSON is never decoded for it
// (council C1-043).
func (s *Service) ClassMedian(ctx context.Context, contestID int64) (float64, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT percent FROM student_contest_summary WHERE contest_id = ?", contestID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var p []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return 0, err
		}
		p = append(p, v)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return medianOf(p), nil
}

// medianOf sorts p in place and returns its median, rounded to 0.1.
func medianOf(p []float64) float64 {
	if len(p) == 0 {
		return 0
	}
	sort.Float64s(p)
	if len(p)%2 == 1 {
		return p[len(p)/2]
	}
	return math.Round((p[len(p)/2-1]+p[len(p)/2])*5) / 10
}

// Upsolved returns, for one student and contest, the problems they first
// brought to full score in practice after the contest (the per-user
// submissions index of 0013).
func (s *Service) Upsolved(ctx context.Context, contestID, userID int64) (map[int64]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT problem_id FROM submissions WHERE user_id = ? AND kind = 'submit' AND practice = 1
		AND contest_id = ? AND state = 'done' AND max_score > 0 AND score >= max_score`, userID, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// DeleteSummariesTx removes a contest's summaries (the retention job, with
// its submissions).
func DeleteSummariesTx(ctx context.Context, ex execer, contestID int64) error {
	_, err := ex.ExecContext(ctx, "DELETE FROM student_contest_summary WHERE contest_id = ?", contestID)
	return err
}
