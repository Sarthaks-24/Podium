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
	// ErrPinMoved refuses a submit, run or rejudge whose problem changed
	// after the contest's Ready check, so the judged tests would not be
	// the pinned revision's (council C1-045). The words are contests'
	// ErrNotReady's, for a student.
	ErrPinMoved = errors.New("This problem is being updated by the organisers. Try again in a few minutes.")
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
	// ContestView, when set, says how a student sees a submission made in
	// a contest (rounding in graded contests, exam mode hiding results). It
	// is used for live updates; handlers pass the same options directly.
	ContestView func(ctx context.Context, contestID, userID int64) ViewOptions

	pub    publisher
	limits rateLimiter
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
	// Languages limits the languages allowed; empty allows all four.
	Languages []string
	// View is how the viewer sees the submissions listed in the workspace
	// and returned by Submit.
	View ViewOptions
	// RevisionID is the problem revision the contest pins (B2.3): the
	// workspace serves its title, statement and samples, and submissions
	// and runs record it. 0 outside contests.
	RevisionID int64
	// LogView records the first time this viewer is served RevisionID
	// (the served log, B2.4): set for students, not for organisers.
	LogView bool
	// Reveal marks submissions that never count (practice after the
	// window, an ungraded contest, the organisers' workspace): their
	// first failing hidden test is revealed to the author (D115).
	Reveal bool
}

// allows reports whether the scope accepts a language.
func (sc Scope) allows(language string) bool {
	if len(sc.Languages) == 0 {
		return true
	}
	for _, l := range sc.Languages {
		if l == language {
			return true
		}
	}
	return false
}

// checkLanguage refuses a language the contest does not allow.
func (sc Scope) checkLanguage(language string) error {
	if sc.allows(language) {
		return nil
	}
	names := make([]string, 0, len(sc.Languages))
	for _, l := range languages {
		if sc.allows(l.ID) {
			names = append(names, l.Name)
		}
	}
	return &InputError{"This contest allows " + joinWords(names) + "."}
}

func joinWords(w []string) string {
	switch len(w) {
	case 0:
		return "no languages"
	case 1:
		return w[0]
	}
	return strings.Join(w[:len(w)-1], ", ") + " and " + w[len(w)-1]
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
	ProblemID     int64      `json:"problem_id"`
	Title         string     `json:"title"`
	StatementMD   string     `json:"statement_md"`
	TimeLimitMS   int        `json:"time_limit_ms"`
	MemoryLimitMB int        `json:"memory_limit_mb"`
	Languages     []Language `json:"languages"`
	Samples       []Sample   `json:"samples"`
	// OpenTestCount is how many open tests a Run checks after the
	// samples (D115); their inputs are never listed.
	OpenTestCount int              `json:"open_test_count"`
	Draft         *Draft           `json:"draft"`
	Submissions   []SubmissionView `json:"submissions"`
	ServerNow     int64            `json:"server_now"`
}

// Language is a choice in the editor's language select.
type Language struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// TimeFactor multiplies the problem's time limit for this language
	// (the judge's table), so the editor can show the limit a program
	// really gets (walk 2026-10-21, D323).
	TimeFactor float64 `json:"time_factor"`
}

// languages is the editor's list, in display order, with the judge's
// time factors.
var languages = func() []Language {
	out := []Language{{ID: "cpp17", Name: "C++17"}, {ID: "java21", Name: "Java 21"}, {ID: "python3", Name: "Python 3"}, {ID: "node22", Name: "JavaScript (Node 22)"}}
	for i := range out {
		out[i].TimeFactor = judge.Languages[out[i].ID].TimeFactor
	}
	return out
}()

// Workspace loads a problem for the editor. Organisers may try a problem
// before it is validated; students reach problems only through contests,
// which accept validated problems only (M4). In a contest it serves the
// pinned revision (sc.RevisionID), never the bank's working copy, and
// records the first time a student is served it.
func (s *Service) Workspace(ctx context.Context, problemID, userID int64, sc Scope) (*Workspace, error) {
	p, err := s.Problems.Get(ctx, problemID)
	if errors.Is(err, problems.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w := &Workspace{ProblemID: p.ID, Title: p.Title, StatementMD: p.Statement, TimeLimitMS: p.TimeLimitMS,
		MemoryLimitMB: p.MemoryLimitMB, Languages: []Language{}, Samples: []Sample{}, Submissions: []SubmissionView{},
		ServerNow: s.now().UnixMilli(), OpenTestCount: len(p.Open)}
	for _, l := range languages {
		if sc.allows(l.ID) {
			w.Languages = append(w.Languages, l)
		}
	}
	tests := make([]problems.RevisionTest, 0, len(p.Tests))
	for _, t := range p.Tests {
		tests = append(tests, problems.RevisionTest{Idx: t.Idx, Kind: p.TestKind(t.Idx), InputSHA: t.InputSHA, OutputSHA: t.OutputSHA})
	}
	if sc.RevisionID > 0 {
		rev, err := s.Problems.RevisionByID(ctx, sc.RevisionID)
		if err != nil {
			return nil, err
		}
		// A revision reconstructed at the upgrade has no statement; the
		// working copy is what v1.1.0 served for it.
		if !rev.Reconstructed && rev.ProblemID == p.ID {
			w.Title, w.StatementMD = rev.Title, rev.Statement
			if rev.TimeLimitMS != nil && rev.MemoryLimitMB != nil {
				w.TimeLimitMS, w.MemoryLimitMB = *rev.TimeLimitMS, *rev.MemoryLimitMB
			}
			if tests, err = s.Problems.RevisionTests(ctx, rev.ID); err != nil {
				return nil, err
			}
			w.OpenTestCount = 0
			for _, t := range tests {
				if t.Kind == problems.KindOpen {
					w.OpenTestCount++
				}
			}
		}
		if sc.LogView {
			if err := s.logView(ctx, userID, sc.ContestID, sc.RevisionID); err != nil {
				return nil, err
			}
		}
	}
	for _, t := range tests {
		if t.Kind != problems.KindSample || t.OutputSHA == "" {
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
	w.Submissions, err = s.listMine(ctx, userID, problemID, sc.ContestID, sc.View)
	return w, err
}

// logView records the first time a student is served a revision in a
// contest (B2.4). The read comes first, so a page load that was served
// before costs no write.
func (s *Service) logView(ctx context.Context, userID, contestID, revisionID int64) error {
	var one int
	err := s.DB.QueryRowContext(ctx, "SELECT 1 FROM revision_views WHERE user_id = ? AND contest_id = ? AND revision_id = ?",
		userID, contestID, revisionID).Scan(&one)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO revision_views (user_id, contest_id, revision_id, first_seen_at) VALUES (?, ?, ?, ?)`,
		userID, contestID, revisionID, s.now().UnixMilli())
	return err
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
	if err := sc.checkLanguage(language); err != nil {
		return err
	}
	if err := s.limits.allow(userID, "draft", draftGap, s.now(), "Drafts are saved"); err != nil {
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
	if err := r.Scope.checkLanguage(r.Language); err != nil {
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
	if err := checkPin(ctx, tx, p, r.Scope.RevisionID); err != nil {
		return nil, false, err
	}
	if r.ClientKey != "" {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM submissions WHERE user_id = ? AND problem_id = ? AND coalesce(contest_id, 0) = ?
			AND client_key = ? AND client_key <> ''`, r.UserID, r.ProblemID, r.Scope.ContestID, r.ClientKey).Scan(&id)
		if err == nil {
			tx.Rollback()
			v, err := s.GetWith(ctx, id, r.Scope.View)
			return v, true, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	}
	now := s.now()
	// After the repeat check, so a retried request still gets its
	// submission back.
	if err := checkCompileTimeout(r.UserID, r.Scope.ContestID, now); err != nil {
		return nil, false, err
	}
	var pending, judged, count int
	// A rejudge waiting in the queue is the organisers' work, not the
	// student's own pending submission.
	// A submission the judge could not finish is not counted, so it does
	// not use up one of the student's attempts either.
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(state IN ('queued', 'running') AND rejudged_at IS NULL), 0),
		coalesce(sum(state = 'done'), 0), coalesce(sum(state <> 'error'), 0)
		FROM submissions WHERE user_id = ? AND problem_id = ? AND coalesce(contest_id, 0) = ? AND kind = 'submit'`,
		r.UserID, r.ProblemID, r.Scope.ContestID).Scan(&pending, &judged, &count); err != nil {
		return nil, false, err
	}
	if r.Scope.MaxPerProblem > 0 && count >= r.Scope.MaxPerProblem {
		return nil, false, &InputError{fmt.Sprintf("You have used all %d submissions for this problem.", r.Scope.MaxPerProblem)}
	}
	// In exam mode no refusal may depend on judging: "still being judged"
	// would tell the student when their hidden submission finished, a
	// timing channel on hidden tests. A new submission queues behind the
	// pending one instead, still bounded by the cap and the gap.
	if pending > 0 && !r.Scope.View.Hide {
		return nil, false, &RateError{"Your last submission for this problem is still being judged. Wait for its result.", 5 * time.Second}
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
		max_score, client_key, typed_chars, pasted_chars, focus_losses, client_hash, practice, created_at, revision_id)
		VALUES (?, ?, ?, 'submit', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		r.ProblemID, contest, r.UserID, r.Language, r.Source, srcSHA, maxScore, r.ClientKey,
		clamp(r.Typed), clamp(r.Pasted), clamp(r.Blur), r.ClientHash, r.Scope.Practice, now.UnixMilli(), nullID(r.Scope.RevisionID)).Scan(&id)
	if err != nil {
		return nil, false, err
	}
	pl := submitPayload(p, id, judgeapi.Program{Language: r.Language, SourceSHA: srcSHA})
	pl.StopOnSampleFail = !r.Scope.NoStopOnSample
	pl.Reveal = r.Scope.Reveal
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
	if err := tagJob(ctx, tx, jobID, r.Scope); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	v, err = s.GetWith(ctx, id, r.Scope.View)
	return v, false, err
}

func clamp(n int) int { return max(0, min(n, 10_000_000)) }

// nullID stores a missing id as NULL.
func nullID(id int64) any {
	if id > 0 {
		return id
	}
	return nil
}

// tagJob records a job's contest (so a paused contest's jobs wait) and the
// revision it judges against (B2.10).
func tagJob(ctx context.Context, tx *sql.Tx, jobID int64, sc Scope) error {
	if sc.ContestID <= 0 && sc.RevisionID <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, "UPDATE jobs SET contest_id = ?, revision_id = ? WHERE id = ?", nullID(sc.ContestID), nullID(sc.RevisionID), jobID)
	return err
}

// NotReadyError says why nobody can submit to a problem yet.
type NotReadyError struct{ Problems []string }

// Error joins the reasons.
func (e *NotReadyError) Error() string { return strings.Join(e.Problems, " ") }

// checkPin refuses, inside the transaction, a problem whose loaded copy
// is not the revision the contest pins: a save between the Ready check
// and the load would otherwise judge new tests under the old revision's
// name. Outside a contest pin (revision 0) there is nothing to check.
func checkPin(ctx context.Context, tx *sql.Tx, p *problems.Problem, revisionID int64) error {
	if revisionID <= 0 {
		return nil
	}
	var hash string
	if err := tx.QueryRowContext(ctx, "SELECT content_hash FROM problem_revisions WHERE id = ?", revisionID).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrPinMoved
		}
		return err
	}
	if hash != p.ContentHash {
		return ErrPinMoved
	}
	return nil
}

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
// subtask order. Open tests are Run's, never Submit's (D115).
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

// RunRequest is one press of Run: on the samples and open tests, or on a
// custom input.
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
	if err := r.Scope.checkLanguage(r.Language); err != nil {
		return nil, err
	}
	if r.Input != nil && len(*r.Input) > MaxInput {
		return nil, &InputError{"The input is longer than 64 KB."}
	}
	if err := checkCompileTimeout(r.UserID, r.Scope.ContestID, s.now()); err != nil {
		return nil, err
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
	if err := checkPin(ctx, tx, p, r.Scope.RevisionID); err != nil {
		return nil, err
	}
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
		// The samples, then the open tests in order (D115): the judge runs
		// an open test only while everything before it passed.
		for _, t := range p.Tests {
			if p.IsSample(t.Idx) {
				pl.Inputs = append(pl.Inputs, judgeapi.RunInput{Idx: t.Idx, InputSHA: t.InputSHA, ExpectedSHA: t.OutputSHA})
			}
		}
		for _, t := range p.Tests {
			if p.IsOpen(t.Idx) {
				pl.Inputs = append(pl.Inputs, judgeapi.RunInput{Idx: t.Idx, InputSHA: t.InputSHA, ExpectedSHA: t.OutputSHA, Open: true})
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
	// A run row keeps only the hash of its code: the text is in the blob
	// store already, and up to 128 KB inline per run would make every
	// later scan of the table walk overflow pages. The custom input is
	// likewise kept only as a blob (in the job payload).
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, input, created_at, revision_id)
		VALUES (?, ?, ?, 'run', ?, '', ?, '', ?, ?) RETURNING id`, r.ProblemID, contest, r.UserID, r.Language, srcSHA,
		s.now().UnixMilli(), nullID(r.Scope.RevisionID)).Scan(&id); err != nil {
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
	if err := tagJob(ctx, tx, jobID, r.Scope); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRunWith(ctx, id, r.Scope.View)
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
	// Everything that can refuse the batch is checked before the write
	// lock is taken: kinds, the rate, and the cap (a plain read).
	for _, e := range evs {
		switch e.Kind {
		case "paste", "blur", "focus":
		default:
			return &InputError{"Unknown event kind."}
		}
	}
	if len(evs) == 0 {
		return nil
	}
	if err := s.limits.allow(userID, "events", eventsGap, s.now(), "Editor events are sent"); err != nil {
		return err
	}
	var have, exists int
	if err := s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM integrity_events WHERE user_id = ? AND problem_id = ? AND contest_id = ?),
		(SELECT count(*) FROM problems WHERE id = ?)`, userID, problemID, sc.ContestID, problemID).Scan(&have, &exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	if have+len(evs) > maxEventsPerScope {
		return nil // enough recorded; dropping more loses nothing a reviewer needs
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now().UnixMilli()
	for _, e := range evs {
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
	i, err := s.Info(ctx, id)
	return i.UserID, i.Kind, err
}

// Info is what access checks need to know about a submission or run.
type Info struct {
	UserID, ContestID, ProblemID int64
	Kind                         string
}

// Info returns who made a submission or run, and in which contest (0
// outside contests).
func (s *Service) Info(ctx context.Context, id int64) (Info, error) {
	var i Info
	err := s.DB.QueryRowContext(ctx, "SELECT user_id, coalesce(contest_id, 0), problem_id, kind FROM submissions WHERE id = ?", id).
		Scan(&i.UserID, &i.ContestID, &i.ProblemID, &i.Kind)
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, ErrNotFound
	}
	return i, err
}
