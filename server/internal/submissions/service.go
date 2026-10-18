package submissions

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Limits on what a student sends (PLAN.md 7.5).
const (
	MaxSource    = 64 << 10
	MaxInput     = 64 << 10
	maxSampleTxt = 4 << 10
	listLimit    = 20
	maxEvents    = 100
)

// Priorities (lower runs first; validation is 90).
const (
	RunPriority      = 10
	SubmitPriority   = 20 // plus up to 10 for each earlier judged submission
	RejudgePriority  = 50
	PracticePriority = 60
	// runGap is the shortest time between two runs by one user.
	runGap = 2 * time.Second
	// maxEventsPerScope bounds integrity events per user, problem and
	// contest.
	maxEventsPerScope = 5000
)

// Errors the API maps to status codes.
var (
	ErrNotFound = errors.New("not found")
)

// InputError is a request the student can fix (400).
type InputError struct{ Message string }

// Error returns the message.
func (e *InputError) Error() string { return e.Message }

// RateError is a request refused by a rate limit (429).
type RateError struct {
	Message    string
	RetryAfter time.Duration
}

// Error returns the message.
func (e *RateError) Error() string { return e.Message }

// Service runs the student side of judging.
type Service struct {
	DB       *sql.DB
	Jobs     *jobs.Service
	Problems *problems.Service
	Hub      *events.Hub
	// Now is the clock; tests replace it.
	Now func() time.Time
	// HashKey keys ClientHash (the server's session secret).
	HashKey []byte

	pub publisher
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Scope says whose rules apply: outside a contest (organisers trying a
// problem in M3) or inside one (M4).
type Scope struct {
	ContestID int64
	// MinGap is the shortest time between two submits by one user.
	MinGap time.Duration
	// MaxPerProblem caps submits per problem; 0 means no cap.
	MaxPerProblem int
	// Practice marks submissions made after the contest ended.
	Practice bool
	// NoStopOnSample keeps judging after a failing sample.
	NoStopOnSample bool
}

// Sample is a sample test shown with the statement.
type Sample struct {
	Idx       int    `json:"idx"`
	Input     string `json:"input"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated"`
}

// Draft is the code last saved from the editor.
type Draft struct {
	Language  string `json:"language"`
	Source    string `json:"source"`
	UpdatedAt int64  `json:"updated_at"`
}

// Workspace is what the student's editor page needs.
type Workspace struct {
	ProblemID     int64            `json:"problem_id"`
	Title         string           `json:"title"`
	StatementMD   string           `json:"statement_md"`
	TimeLimitMS   int              `json:"time_limit_ms"`
	MemoryLimitMB int              `json:"memory_limit_mb"`
	Languages     []Language       `json:"languages"`
	Samples       []Sample         `json:"samples"`
	Draft         *Draft           `json:"draft"`
	Submissions   []SubmissionView `json:"submissions"`
	ServerNow     int64            `json:"server_now"`
}

// Language is a choice in the editor's language select.
type Language struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// languages is the editor's list, in display order.
var languages = []Language{{"cpp17", "C++17"}, {"java21", "Java 21"}, {"python3", "Python 3"}, {"node22", "JavaScript (Node 22)"}}

// Workspace loads a problem for the editor. Organisers may try a problem
// before it is validated; students reach problems only through contests,
// which accept validated problems only (M4).
func (s *Service) Workspace(ctx context.Context, problemID, userID int64, sc Scope) (*Workspace, error) {
	p, err := s.Problems.Get(ctx, problemID)
	if errors.Is(err, problems.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w := &Workspace{ProblemID: p.ID, Title: p.Title, StatementMD: p.Statement, TimeLimitMS: p.TimeLimitMS,
		MemoryLimitMB: p.MemoryLimitMB, Languages: languages, Samples: []Sample{}, Submissions: []SubmissionView{},
		ServerNow: s.now().UnixMilli()}
	for _, t := range p.Tests {
		if !p.IsSample(t.Idx) || t.OutputSHA == "" {
			continue
		}
		in, _ := s.Problems.Blobs.Read(t.InputSHA, maxSampleTxt+1)
		out, _ := s.Problems.Blobs.Read(t.OutputSHA, maxSampleTxt+1)
		w.Samples = append(w.Samples, Sample{Idx: t.Idx, Input: clipText(in, maxSampleTxt), Output: clipText(out, maxSampleTxt),
			Truncated: len(in) > maxSampleTxt || len(out) > maxSampleTxt})
	}
	var d Draft
	err = s.DB.QueryRowContext(ctx, `SELECT language, source, updated_at FROM drafts WHERE user_id = ? AND problem_id = ? AND contest_id = ?`,
		userID, problemID, sc.ContestID).Scan(&d.Language, &d.Source, &d.UpdatedAt)
	if err == nil {
		w.Draft = &d
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	w.Submissions, err = s.listMine(ctx, userID, problemID, sc.ContestID)
	return w, err
}

func clipText(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

func checkCode(language, source string) error {
	if _, ok := judge.Languages[language]; !ok {
		return &InputError{"Choose C++17, Java 21, Python 3 or JavaScript."}
	}
	if len(source) > MaxSource {
		return &InputError{"The code is longer than 64 KB."}
	}
	if !utf8.ValidString(source) {
		return &InputError{"The code is not valid UTF-8 text."}
	}
	return nil
}

// SaveDraft stores the editor's code.
func (s *Service) SaveDraft(ctx context.Context, problemID, userID int64, sc Scope, language, source string) error {
	if err := checkCode(language, source); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO drafts (user_id, problem_id, contest_id, language, source, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, problem_id, contest_id) DO UPDATE SET language = excluded.language, source = excluded.source, updated_at = excluded.updated_at`,
		userID, problemID, sc.ContestID, language, source, s.now().UnixMilli())
	if err != nil && strings.Contains(err.Error(), "FOREIGN KEY") {
		return ErrNotFound
	}
	return err
}

// SubmitRequest is one press of Submit.
type SubmitRequest struct {
	ProblemID, UserID   int64
	Scope               Scope
	Language, Source    string
	ClientKey           string
	Typed, Pasted, Blur int
	ClientHash          string
}

// ClientHash turns an IP address and user agent into the opaque value
// stored with a submission (D47): enough to notice two people on one
// browser, not enough to recover either. It is keyed (HMAC with HashKey),
// so campus IP ranges cannot be brute-forced back out of it.
func (s *Service) ClientHash(ip, userAgent string) string {
	mac := hmac.New(sha256.New, s.HashKey)
	mac.Write([]byte(ip + "\x00" + userAgent))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// Submit queues a submission for judging. A repeated client key for the
// same problem returns the first submission instead of counting again;
// dup reports that case.
func (s *Service) Submit(ctx context.Context, r SubmitRequest) (v *SubmissionView, dup bool, err error) {
	if err := checkCode(r.Language, r.Source); err != nil {
		return nil, false, err
	}
	if len(r.ClientKey) > 64 {
		return nil, false, &InputError{"The request key is too long."}
	}
	// Loaded before the transaction: inside it, a second connection would
	// wait behind writers holding the others.
	p, err := s.problemForJudging(ctx, r.ProblemID)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if r.ClientKey != "" {
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM submissions WHERE user_id = ? AND client_key = ? AND problem_id = ?",
			r.UserID, r.ClientKey, r.ProblemID).Scan(&id)
		if err == nil {
			tx.Rollback()
			v, err := s.Get(ctx, id)
			return v, true, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	}
	now := s.now()
	var pending, judged, count int
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(state IN ('queued', 'running')), 0), coalesce(sum(state = 'done'), 0), count(*)
		FROM submissions WHERE user_id = ? AND problem_id = ? AND coalesce(contest_id, 0) = ? AND kind = 'submit'`,
		r.UserID, r.ProblemID, r.Scope.ContestID).Scan(&pending, &judged, &count); err != nil {
		return nil, false, err
	}
	if pending > 0 {
		return nil, false, &RateError{"Your last submission for this problem is still being judged. Wait for its result.", 5 * time.Second}
	}
	if r.Scope.MaxPerProblem > 0 && count >= r.Scope.MaxPerProblem {
		return nil, false, &InputError{fmt.Sprintf("You have used all %d submissions for this problem.", r.Scope.MaxPerProblem)}
	}
	// The gap is per person, across problems (PLAN.md 7.5).
	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(created_at) FROM submissions WHERE user_id = ? AND coalesce(contest_id, 0) = ? AND kind = 'submit'`,
		r.UserID, r.Scope.ContestID).Scan(&last); err != nil {
		return nil, false, err
	}
	if last.Valid && r.Scope.MinGap > 0 {
		wait := time.UnixMilli(last.Int64).Add(r.Scope.MinGap).Sub(now)
		if wait > 0 {
			secs := int(math.Ceil(wait.Seconds()))
			return nil, false, &RateError{fmt.Sprintf("Submissions are limited to one every %d seconds. Try again in %d s.", int(r.Scope.MinGap.Seconds()), secs),
				time.Duration(secs) * time.Second}
		}
	}
	srcSHA, err := s.Problems.Blobs.Put(ctx, tx, []byte(r.Source))
	if err != nil {
		return nil, false, err
	}
	var contest any
	if r.Scope.ContestID > 0 {
		contest = r.Scope.ContestID
	}
	maxScore := 0
	for _, st := range p.Subtasks {
		maxScore += st.Points
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha,
		max_score, client_key, typed_chars, pasted_chars, focus_losses, client_hash, practice, created_at)
		VALUES (?, ?, ?, 'submit', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.ProblemID, contest, r.UserID, r.Language, r.Source, srcSHA, maxScore, r.ClientKey,
		clamp(r.Typed), clamp(r.Pasted), clamp(r.Blur), r.ClientHash, r.Scope.Practice, now.UnixMilli()).Scan(&id)
	if err != nil {
		return nil, false, err
	}
	pl := submitPayload(p, id, judgeapi.Program{Language: r.Language, SourceSHA: srcSHA})
	pl.StopOnSampleFail = !r.Scope.NoStopOnSample
	if p.Checker == problems.CheckerCustom {
		if pl.Checker.SourceSHA, err = s.Problems.Blobs.Put(ctx, tx, []byte(p.CheckerSource)); err != nil {
			return nil, false, err
		}
	}
	// Fair queue (PLAN.md 4.1 lever 5): a student's first submission on a
	// problem goes ahead of someone's tenth.
	priority := SubmitPriority + min(judged, 10)
	if r.Scope.Practice {
		priority = PracticePriority
	}
	jobID, err := s.Jobs.Enqueue(ctx, tx, judgeapi.KindSubmit, priority, pl, r.UserID)
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE submissions SET job_id = ? WHERE id = ?", jobID, id); err != nil {
		return nil, false, err
	}
	if r.Scope.ContestID > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE jobs SET contest_id = ? WHERE id = ?", r.Scope.ContestID, jobID); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	v, err = s.Get(ctx, id)
	return v, false, err
}

func clamp(n int) int { return max(0, min(n, 10_000_000)) }

// NotReadyError says why nobody can submit to a problem yet.
type NotReadyError struct{ Problems []string }

// Error joins the reasons.
func (e *NotReadyError) Error() string { return strings.Join(e.Problems, " ") }

// problemForJudging loads a problem that can be judged: tests, samples
// and subtasks fit together and every test has its expected output.
// (Contests also require validation, M4; organisers may try an
// unvalidated problem.)
func (s *Service) problemForJudging(ctx context.Context, id int64) (*problems.Problem, error) {
	p, err := s.Problems.Get(ctx, id)
	if errors.Is(err, problems.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if probs := p.Problems(); len(probs) > 0 {
		return nil, &NotReadyError{probs}
	}
	return p, nil
}

// submitPayload lists tests samples first, then each subtask's tests in
// subtask order.
func submitPayload(p *problems.Problem, submissionID int64, prog judgeapi.Program) judgeapi.SubmitPayload {
	pl := judgeapi.SubmitPayload{SubmissionID: submissionID, Program: prog, TimeLimitMS: p.TimeLimitMS,
		MemoryLimitMB: p.MemoryLimitMB, Checker: judgeapi.Checker{Type: p.Checker, Epsilon: p.Epsilon},
		StopOnSampleFail: true}
	byIdx := map[int]problems.Test{}
	for _, t := range p.Tests {
		byIdx[t.Idx] = t
	}
	for _, t := range p.Tests {
		if p.IsSample(t.Idx) {
			pl.Tests = append(pl.Tests, judgeapi.SubmitTest{Idx: t.Idx, Sample: true, InputSHA: t.InputSHA, OutputSHA: t.OutputSHA})
		}
	}
	for _, st := range p.Subtasks {
		pl.Subtasks = append(pl.Subtasks, judgeapi.Subtask{Idx: st.Idx, Points: st.Points})
		nums, _ := problems.ParseRanges(st.Tests)
		for _, n := range nums {
			if t, ok := byIdx[n]; ok {
				pl.Tests = append(pl.Tests, judgeapi.SubmitTest{Idx: n, Subtask: st.Idx, InputSHA: t.InputSHA, OutputSHA: t.OutputSHA})
			}
		}
	}
	return pl
}

// RunRequest is one press of Run: on the samples, or on a custom input.
type RunRequest struct {
	ProblemID, UserID int64
	Scope             Scope
	Language, Source  string
	// Input is the custom input; nil runs the samples.
	Input *string
}

// Run queues a run. One run per user is pending at a time.
func (s *Service) Run(ctx context.Context, r RunRequest) (*RunView, error) {
	if err := checkCode(r.Language, r.Source); err != nil {
		return nil, err
	}
	if r.Input != nil && len(*r.Input) > MaxInput {
		return nil, &InputError{"The input is longer than 64 KB."}
	}
	p, err := s.problemForJudging(ctx, r.ProblemID)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM submissions WHERE user_id = ? AND kind = 'run' AND state IN ('queued', 'running')`,
		r.UserID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending > 0 {
		return nil, &RateError{"Your last run is still going. Wait for it to finish.", 3 * time.Second}
	}
	var lastRun sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(created_at) FROM submissions WHERE user_id = ? AND kind = 'run'`, r.UserID).Scan(&lastRun); err != nil {
		return nil, err
	}
	if lastRun.Valid && time.UnixMilli(lastRun.Int64).Add(runGap).After(s.now()) {
		return nil, &RateError{"Runs are limited to one every 2 seconds.", runGap}
	}
	srcSHA, err := s.Problems.Blobs.Put(ctx, tx, []byte(r.Source))
	if err != nil {
		return nil, err
	}
	pl := judgeapi.RunPayload{Program: judgeapi.Program{Language: r.Language, SourceSHA: srcSHA},
		TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB, Checker: judgeapi.Checker{Type: p.Checker, Epsilon: p.Epsilon}}
	input := ""
	if r.Input != nil {
		input = *r.Input
		sha, err := s.Problems.Blobs.Put(ctx, tx, []byte(input))
		if err != nil {
			return nil, err
		}
		pl.Inputs = []judgeapi.RunInput{{Idx: 0, InputSHA: sha}}
	} else {
		for _, t := range p.Tests {
			if p.IsSample(t.Idx) {
				pl.Inputs = append(pl.Inputs, judgeapi.RunInput{Idx: t.Idx, InputSHA: t.InputSHA, ExpectedSHA: t.OutputSHA})
			}
		}
		if len(pl.Inputs) == 0 {
			return nil, &InputError{"This problem has no samples. Run it on your own input instead."}
		}
		if p.Checker == problems.CheckerCustom {
			if pl.Checker.SourceSHA, err = s.Problems.Blobs.Put(ctx, tx, []byte(p.CheckerSource)); err != nil {
				return nil, err
			}
		}
	}
	var contest any
	if r.Scope.ContestID > 0 {
		contest = r.Scope.ContestID
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, input, created_at)
		VALUES (?, ?, ?, 'run', ?, ?, ?, ?, ?) RETURNING id`, r.ProblemID, contest, r.UserID, r.Language, r.Source, srcSHA, input,
		s.now().UnixMilli()).Scan(&id); err != nil {
		return nil, err
	}
	pl.SubmissionID = id
	jobID, err := s.Jobs.Enqueue(ctx, tx, judgeapi.KindRun, RunPriority, pl, r.UserID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE submissions SET job_id = ? WHERE id = ?", jobID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRun(ctx, id)
}

// Event is one integrity event from the editor (D47). At is when it
// happened, in server Unix ms as the browser estimates it; events arrive
// in batches, so it is kept when plausible.
type Event struct {
	Kind string `json:"kind"`
	Size int    `json:"size"`
	At   int64  `json:"at"`
}

// eventAgeLimit is how old a batched event's time may be before the
// server's own clock is used instead.
const eventAgeLimit = 10 * time.Minute

// RecordEvents stores paste, blur and focus events. Unknown kinds are
// refused; sizes are clamped.
func (s *Service) RecordEvents(ctx context.Context, problemID, userID int64, sc Scope, evs []Event) error {
	if len(evs) > maxEvents {
		return &InputError{"Too many events in one request."}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var have, exists int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM integrity_events WHERE user_id = ? AND problem_id = ? AND contest_id = ?),
		(SELECT count(*) FROM problems WHERE id = ?)`, userID, problemID, sc.ContestID, problemID).Scan(&have, &exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	if have+len(evs) > maxEventsPerScope {
		return nil // enough recorded; dropping more loses nothing a reviewer needs
	}
	now := s.now().UnixMilli()
	for _, e := range evs {
		switch e.Kind {
		case "paste", "blur", "focus":
		default:
			return &InputError{"Unknown event kind."}
		}
		at := e.At
		if at > now || at < now-eventAgeLimit.Milliseconds() {
			at = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO integrity_events (user_id, problem_id, contest_id, kind, size, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			userID, problemID, sc.ContestID, e.Kind, clamp(e.Size), at); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Owner returns who made a submission or run, for access checks.
func (s *Service) Owner(ctx context.Context, id int64) (userID int64, kind string, err error) {
	err = s.DB.QueryRowContext(ctx, "SELECT user_id, kind FROM submissions WHERE id = ?", id).Scan(&userID, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	return userID, kind, err
}
