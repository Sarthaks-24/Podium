package submissions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// RejudgeRequest names what to judge again: every submission of one
// problem in a contest, or one submission. Both are limited to the contest.
type RejudgeRequest struct {
	ContestID    int64
	ProblemID    int64 // all of this problem's submissions in the contest
	SubmissionID int64 // or just this one
	By           int64
	// NoStopOnSample matches the contest's setting.
	NoStopOnSample bool
}

// rejudgeReason is the error a replaced job is closed with.
const rejudgeReason = "Replaced by a rejudge."

// Rejudge queues the submissions again with the problem's current tests,
// at RejudgePriority, and returns how many were queued. Each new job and
// its submission record the revision the contest pins now (B2.10); the
// old job keeps its own. Practice submissions and those of an ungraded
// contest still reveal their first failing hidden test (D115). Each submission
// goes back to "queued" with its old result cleared, so standings count it
// only once the new result is in.
//
// A submission still waiting or being judged is included: its job carries
// the old tests, so it is cancelled and replaced (the old job's Finish can
// no longer touch the row, since Finish matches the job id). A submission
// whose waiting job already carries exactly the payload a rejudge would
// queue is left alone, so a double click or two organisers at once never
// double the work. Everything happens in one write transaction.
func (s *Service) Rejudge(ctx context.Context, r RejudgeRequest) (int, error) {
	if r.ContestID <= 0 || (r.ProblemID <= 0) == (r.SubmissionID <= 0) {
		return 0, &InputError{"Choose a problem or one submission to rejudge."}
	}
	// Problems are loaded before the transaction: inside it, a second
	// connection would wait behind writers holding the others.
	var problemIDs []int64
	if r.SubmissionID > 0 {
		var pid int64
		err := s.DB.QueryRowContext(ctx, "SELECT problem_id FROM submissions WHERE id = ? AND contest_id = ? AND kind = 'submit'",
			r.SubmissionID, r.ContestID).Scan(&pid)
		if err != nil {
			return 0, ErrNotFound
		}
		problemIDs = []int64{pid}
	} else {
		problemIDs = []int64{r.ProblemID}
	}
	loaded := map[int64]*problems.Problem{}
	for _, pid := range problemIDs {
		p, err := s.problemForJudging(ctx, pid)
		if err != nil {
			return 0, err
		}
		loaded[pid] = p
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := `SELECT s.id, s.problem_id, s.language, s.source_sha, s.state, coalesce(s.job_id, 0), coalesce(j.state, ''), coalesce(j.payload, ''),
		s.practice = 1 OR coalesce(c.graded, 1) = 0, coalesce(cp.revision_id, 0)
		FROM submissions s LEFT JOIN jobs j ON j.id = s.job_id LEFT JOIN contests c ON c.id = s.contest_id
		LEFT JOIN contest_problems cp ON cp.contest_id = s.contest_id AND cp.problem_id = s.problem_id
		WHERE s.contest_id = ? AND s.kind = 'submit'`
	args := []any{r.ContestID}
	if r.SubmissionID > 0 {
		q += " AND s.id = ?"
		args = append(args, r.SubmissionID)
	} else {
		q += " AND s.problem_id = ?"
		args = append(args, r.ProblemID)
	}
	rows, err := tx.QueryContext(ctx, q+" ORDER BY s.id", args...)
	if err != nil {
		return 0, err
	}
	type target struct {
		id, problem, job         int64
		language, source         string
		state, jobState, payload string
		reveal                   bool
		revision                 int64
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.problem, &t.language, &t.source, &t.state, &t.job, &t.jobState, &t.payload, &t.reveal, &t.revision); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	queued := 0
	for _, t := range targets {
		p := loaded[t.problem]
		if p == nil {
			continue
		}
		pl := submitPayload(p, t.id, judgeapi.Program{Language: t.language, SourceSHA: t.source})
		pl.StopOnSampleFail = !r.NoStopOnSample
		pl.Reveal = t.reveal
		if p.Checker == problems.CheckerCustom {
			if pl.Checker.SourceSHA, err = s.Problems.Blobs.Put(ctx, tx, []byte(p.CheckerSource)); err != nil {
				return 0, err
			}
		}
		unfinished := t.state == "queued" || t.state == "running"
		if unfinished && (t.jobState == "queued" || t.jobState == "leased") {
			if body, _ := json.Marshal(pl); string(body) == t.payload {
				continue // already being judged on exactly these tests
			}
			if err := s.Jobs.Cancel(ctx, tx, t.job, rejudgeReason); err != nil {
				return 0, err
			}
		}
		jobID, err := s.Jobs.Enqueue(ctx, tx, judgeapi.KindSubmit, RejudgePriority, pl, r.By)
		if err != nil {
			return 0, err
		}
		if err := tagJob(ctx, tx, jobID, Scope{ContestID: r.ContestID, RevisionID: t.revision}); err != nil {
			return 0, err
		}
		maxScore := 0
		for _, st := range p.Subtasks {
			maxScore += st.Points
		}
		if _, err := tx.ExecContext(ctx, `UPDATE submissions SET job_id = ?, state = 'queued', verdict = '', score = 0, max_score = ?,
			max_time_ms = 0, max_memory_kb = 0, result = '', earned = '', error = '', judged_at = NULL, rejudged_at = ?,
			revision_id = ? WHERE id = ?`,
			jobID, maxScore, now, nullID(t.revision), t.id); err != nil {
			return 0, fmt.Errorf("rejudge submission %d: %w", t.id, err)
		}
		queued++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return queued, nil
}
