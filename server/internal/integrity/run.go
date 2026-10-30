package integrity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Errors the API maps to status codes.
var (
	ErrBusy     = errors.New("an integrity check is already running")
	ErrNotFound = errors.New("not found")
)

// Service runs integrity checks and keeps their results.
type Service struct {
	DB       *sql.DB
	Problems *problems.Service
	// Contests holds notices back during a student's live contest, and
	// rewrites a board and its summaries after a review decision changed
	// it. Nil in tests that need neither.
	Contests *contests.Service
	Log      *slog.Logger
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Settings tune the similarity check; zero means Defaults.
	Settings Settings
	// Hold, when set, runs at the start of every background check. Tests
	// use it to keep a check running; production leaves it nil.
	Hold func()

	mu      sync.Mutex
	running bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) settings() Settings { return s.Settings.withDefaults() }

// Start begins a check of a contest in the background and returns its
// run id. One check runs at a time per server. The caller checks
// that the contest has ended and that the user may review it.
func (s *Service) Start(ctx context.Context, contestID, by int64) (int64, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return 0, ErrBusy
	}
	s.running = true
	s.mu.Unlock()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO integrity_runs (contest_id, started_by, started_at) VALUES (?, ?, ?)`,
		contestID, by, s.now().UnixMilli())
	if err != nil {
		s.done()
		return 0, err
	}
	id, _ := res.LastInsertId()
	go func() {
		defer s.done()
		if s.Hold != nil {
			s.Hold()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.Check(ctx, id, contestID); err != nil {
			if s.Log != nil {
				s.Log.Error("integrity check failed", "run_id", id, "contest_id", contestID, "err", err)
			}
			s.DB.ExecContext(ctx, `UPDATE integrity_runs SET state = 'error', error = ?, finished_at = ? WHERE id = ?`,
				"The check could not finish. Try again; if it fails again, see the server log.", s.now().UnixMilli(), id)
		}
	}()
	return id, nil
}

// Recover marks checks left running by a server that stopped mid-check
// as failed, so the review page does not wait for them forever. main
// calls it once at start-up, before any check can begin.
func (s *Service) Recover(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE integrity_runs SET state = 'error', error = ?, finished_at = ? WHERE state = 'running'`,
		"The server restarted during the check. Run it again.", s.now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Service) done() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

// Signal is one stored finding.
type Signal struct {
	ID          int64           `json:"id"`
	ProblemID   int64           `json:"problem_id"`
	Kind        string          `json:"kind"`
	Strength    string          `json:"strength"`
	Score       float64         `json:"score"`
	UserA       int64           `json:"user_a"`
	UserB       int64           `json:"user_b,omitempty"`
	SubmissionA int64           `json:"submission_a,omitempty"`
	SubmissionB int64           `json:"submission_b,omitempty"`
	Evidence    json.RawMessage `json:"evidence"`
}

// Stats are the counts a finished run shows.
type Stats struct {
	Students    int            `json:"students"`
	Submissions int            `json:"submissions"`
	Problems    int            `json:"problems"`
	Signals     map[string]int `json:"signals"`
	Clusters    int            `json:"clusters"`
}

// submission is what a check reads of one submission.
type submission struct {
	id, user, job int64
	lang, source  string
	score, max    int
	created       int64
}

// Check computes every signal for a contest and replaces the previous
// run's signals in one transaction.
func (s *Service) Check(ctx context.Context, runID, contestID int64) error {
	probs, err := s.contestProblems(ctx, contestID)
	if err != nil {
		return err
	}
	var signals []Signal
	stats := Stats{Signals: map[string]int{}, Problems: len(probs)}
	students := map[int64]bool{}
	for _, pid := range probs {
		p, err := s.Problems.Get(ctx, pid)
		if err != nil {
			return fmt.Errorf("problem %d: %w", pid, err)
		}
		subs, err := s.submissions(ctx, contestID, pid)
		if err != nil {
			return err
		}
		stats.Submissions += len(subs)
		for _, sub := range subs {
			students[sub.user] = true
		}
		found, clusters, err := s.checkProblem(ctx, p, subs)
		if err != nil {
			return err
		}
		stats.Clusters += clusters
		signals = append(signals, found...)
	}
	stats.Students = len(students)
	for _, sg := range signals {
		stats.Signals[sg.Kind]++
	}
	return s.store(ctx, runID, contestID, signals, stats)
}

func (s *Service) checkProblem(ctx context.Context, p *problems.Problem, subs []submission) ([]Signal, int, error) {
	var out []Signal
	samples := map[int]bool{}
	for _, idx := range p.Samples {
		samples[idx] = true
	}

	// Similar code: each student's best submission and their last wrong
	// one, against the reference and brute force as shared base.
	var subjects []Subject
	for _, sub := range pickSubjects(subs) {
		subjects = append(subjects, Subject{SubmissionID: sub.id, UserID: sub.user, Language: sub.lang, Source: sub.source})
	}
	base := []Subject{{Language: p.Reference.Language, Source: p.Reference.Source}}
	if p.Brute != nil {
		base = append(base, Subject{Language: p.Brute.Language, Source: p.Brute.Source})
	}
	// Starter code every student was given is shared too, or a long starter
	// would make the whole class look alike (v4 E5).
	for lang, code := range p.Starters {
		base = append(base, Subject{Language: lang, Source: code})
	}
	pairs := Compare(subjects, base, s.settings())
	for _, pr := range pairs {
		ev, _ := json.Marshal(map[string]any{"shared": pr.Shared, "fingerprints": pr.Fingerprints, "tokens": pr.Tokens,
			"spans_a": pr.SpansA, "spans_b": pr.SpansB, "language_a": pr.A.Language, "language_b": pr.B.Language})
		out = append(out, Signal{ProblemID: p.ID, Kind: "similar", Strength: pr.Strength, Score: round2(pr.Score),
			UserA: pr.A.UserID, UserB: pr.B.UserID, SubmissionA: pr.A.SubmissionID, SubmissionB: pr.B.SubmissionID, Evidence: ev})
	}

	// Per-test results of every submission, hidden tests only.
	var outputs []TestOutput
	attempts := map[int64][]Attempt{}
	var hiddenIdx []int
	for _, t := range p.Tests {
		if !samples[t.Idx] {
			hiddenIdx = append(hiddenIdx, t.Idx)
		}
	}
	for _, sub := range subs {
		res, err := s.testResults(ctx, sub.job)
		if err != nil {
			return nil, 0, err
		}
		verdicts := make([]string, len(hiddenIdx))
		for i, idx := range hiddenIdx {
			r, ok := res[idx]
			if !ok {
				verdicts[i] = "SK"
				continue
			}
			verdicts[i] = r.verdict
			outputs = append(outputs, TestOutput{UserID: sub.user, SubmissionID: sub.id, ProblemID: p.ID, TestIdx: idx,
				Verdict: r.verdict, OutputSHA: r.sha})
		}
		attempts[sub.user] = append(attempts[sub.user], Attempt{SubmissionID: sub.id, Language: sub.lang, Source: sub.source, Verdicts: verdicts})
	}
	students := map[int64]bool{}
	for _, sub := range subs {
		students[sub.user] = true
	}
	for _, w := range IdenticalWrong(outputs, len(students)) {
		ev, _ := json.Marshal(map[string]any{"tests": w.Tests})
		out = append(out, Signal{ProblemID: p.ID, Kind: "wrong_output", Strength: w.Strength, Score: float64(len(w.Tests)),
			UserA: w.UserA, UserB: w.UserB, Evidence: ev})
	}

	// Hard-coded hidden outputs.
	var hidden []Expected
	for _, t := range p.Tests {
		if samples[t.Idx] || t.OutputSHA == "" || t.OutputSize > hardcodedMaxOutput {
			continue
		}
		b, err := s.Problems.Blobs.Read(t.OutputSHA, hardcodedMaxOutput)
		if err != nil {
			return nil, 0, fmt.Errorf("expected output of test %d: %w", t.Idx, err)
		}
		hidden = append(hidden, Expected{TestIdx: t.Idx, Output: string(b)})
	}
	if len(hidden) > 0 {
		// Text the problem itself gives everyone: an expected output found
		// there is a fixed word or constant, not a pasted answer.
		shared := []string{p.Reference.Source, p.Statement}
		if p.Brute != nil {
			shared = append(shared, p.Brute.Source)
		}
		progs := make([]Program, len(subs))
		for i, sub := range subs {
			progs[i] = Program{UserID: sub.user, SubmissionID: sub.id, Source: sub.source}
		}
		for _, h := range FindHardcoded(progs, hidden, shared) {
			ev, _ := json.Marshal(map[string]any{"tests": h.Tests})
			out = append(out, Signal{ProblemID: p.ID, Kind: "hardcoded", Strength: h.Strength, Score: float64(len(h.Tests)),
				UserA: h.UserID, SubmissionA: h.SubmissionID, Evidence: ev})
		}
	}

	// Probing.
	probing := map[int64]Signal{}
	for user, as := range attempts {
		n, strength := Probing(as)
		if strength == "" {
			continue
		}
		ev, _ := json.Marshal(map[string]any{"distinct": n, "attempts": len(as)})
		probing[user] = Signal{ProblemID: p.ID, Kind: "probing", Strength: strength, Score: float64(n), UserA: user, Evidence: ev}
	}
	out = append(out, sortedByUser(probing)...)
	return out, len(Clusters(pairs)), nil
}

// hardcodedMaxOutput: longer expected outputs are not looked for in
// sources; nobody pastes 4 KB of answers into a program unnoticed.
const hardcodedMaxOutput = 4 << 10

func sortedByUser(m map[int64]Signal) []Signal {
	out := make([]Signal, 0, len(m))
	for _, sg := range m {
		out = append(out, sg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserA < out[j].UserA })
	return out
}

func round2(x float64) float64 { return float64(int(x*100+0.5)) / 100 }

// pickSubjects keeps each student's best submission (highest score,
// earliest on ties) and their last one below full marks, if different.
func pickSubjects(subs []submission) []submission {
	best := map[int64]submission{}
	lastWrong := map[int64]submission{}
	for _, sub := range subs {
		if b, ok := best[sub.user]; !ok || sub.score > b.score {
			best[sub.user] = sub
		}
		if sub.score < sub.max {
			lastWrong[sub.user] = sub
		}
	}
	var out []submission
	for user, b := range best {
		out = append(out, b)
		if w, ok := lastWrong[user]; ok && w.id != b.id {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func (s *Service) contestProblems(ctx context.Context, contestID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT problem_id FROM contest_problems WHERE contest_id = ? ORDER BY ord", contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// submissions are a problem's judged contest submissions by students
// (organisers' test submissions are not evidence), practice excluded.
func (s *Service) submissions(ctx context.Context, contestID, problemID int64) ([]submission, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id, s.user_id, coalesce(s.job_id, 0), s.language, s.source, s.score, s.max_score, s.created_at
		FROM submissions s JOIN users u ON u.id = s.user_id
		WHERE s.contest_id = ? AND s.problem_id = ? AND s.kind = 'submit' AND s.practice = 0 AND s.state = 'done'
		AND u.role = 'student'
		ORDER BY s.id`, contestID, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []submission
	for rows.Next() {
		var sub submission
		if err := rows.Scan(&sub.id, &sub.user, &sub.job, &sub.lang, &sub.source, &sub.score, &sub.max, &sub.created); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

type testResult struct {
	verdict, sha string
}

func (s *Service) testResults(ctx context.Context, jobID int64) (map[int]testResult, error) {
	out := map[int]testResult{}
	if jobID == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT idx, verdict, output_sha FROM job_results
		WHERE job_id = ? AND kind = 'test' AND role = 'program'`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var idx int
		var r testResult
		if err := rows.Scan(&idx, &r.verdict, &r.sha); err != nil {
			return nil, err
		}
		out[idx] = r
	}
	return out, rows.Err()
}

func (s *Service) store(ctx context.Context, runID, contestID int64, signals []Signal, stats Stats) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM integrity_signals WHERE contest_id = ?", contestID); err != nil {
		return err
	}
	for _, sg := range signals {
		if _, err := tx.ExecContext(ctx, `INSERT INTO integrity_signals (run_id, contest_id, problem_id, kind, strength, score,
			user_a, user_b, submission_a, submission_b, evidence) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID, contestID, sg.ProblemID, sg.Kind, sg.Strength, sg.Score, sg.UserA, nullID(sg.UserB),
			nullID(sg.SubmissionA), nullID(sg.SubmissionB), string(sg.Evidence)); err != nil {
			return err
		}
	}
	js, _ := json.Marshal(stats)
	if _, err := tx.ExecContext(ctx, `UPDATE integrity_runs SET state = 'done', stats = ?, finished_at = ? WHERE id = ?`,
		string(js), s.now().UnixMilli(), runID); err != nil {
		return err
	}
	return tx.Commit()
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
