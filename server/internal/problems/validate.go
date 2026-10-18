package problems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// ValidatePriority puts validation behind every contest job (lower runs
// first; M3 uses 10 to 60 for runs, submissions and rejudges).
const ValidatePriority = 90

// NotReadyError lists what stops a problem from being validated.
type NotReadyError struct{ Problems []string }

func (e *NotReadyError) Error() string { return strings.Join(e.Problems, " ") }

// Validation states shown to setters.
const (
	ValidationQueued  = "queued"
	ValidationRunning = "running"
	ValidationPassed  = "passed"
	ValidationFailed  = "failed"
	ValidationError   = "error"
)

// Validation is a validation run as the editor shows it.
type Validation struct {
	ID            int64
	State         string
	ContentHash   string
	Current       bool
	QueuedAt      time.Time
	FinishedAt    time.Time
	QueuePosition int
	Compile       []judgeapi.CompileResult
	Tests         []judgeapi.TestResult
	Summary       *Summary
	Error         string
}

// Summary is the server's verdict on a finished validation.
type Summary struct {
	Passed          bool     `json:"passed"`
	ReferencePassed int      `json:"reference_passed"`
	ReferenceTotal  int      `json:"reference_total"`
	BruteAgreed     int      `json:"brute_agreed"`
	BruteTotal      int      `json:"brute_total"`
	ValidatorPassed int      `json:"validator_passed"`
	ValidatorTotal  int      `json:"validator_total"`
	MaxTimeMS       int      `json:"max_time_ms"`
	TimeLimitMS     int      `json:"time_limit_ms"`
	Problems        []string `json:"problems"`
	Warnings        []string `json:"warnings"`
}

// RequestValidation queues a validation of the problem as it is now.
func (s *Service) RequestValidation(ctx context.Context, id, userID int64) (*Validation, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if probs := p.Problems(); len(probs) > 0 {
		return nil, &NotReadyError{probs}
	}
	// One open validation per problem: asking again for the same content
	// returns the run already waiting; a run for older content still
	// waiting is superseded.
	var openID, openJob int64
	var openHash string
	err = tx.QueryRowContext(ctx, `SELECT v.id, v.job_id, v.content_hash FROM validations v JOIN jobs j ON j.id = v.job_id
		WHERE v.problem_id = ? AND j.state IN ('queued', 'leased') ORDER BY v.id DESC LIMIT 1`, id).Scan(&openID, &openJob, &openHash)
	switch {
	case err == nil && openHash == p.ContentHash:
		tx.Rollback()
		return s.LatestValidation(ctx, id)
	case err == nil:
		if err := s.Jobs.Supersede(ctx, tx, openJob, "A newer version of the problem was sent for validation."); err != nil {
			return nil, err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	points := 0
	for _, st := range p.Subtasks {
		points += st.Points
	}
	payload := judgeapi.ValidatePayload{Points: points,
		ProblemID: p.ID, ContentHash: p.ContentHash,
		TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB,
		Checker: judgeapi.Checker{Type: p.Checker, Epsilon: p.Epsilon},
	}
	putSource := func(src Source) (judgeapi.Program, error) {
		sha, err := s.Blobs.Put(ctx, tx, []byte(src.Source))
		return judgeapi.Program{Language: src.Language, SourceSHA: sha}, err
	}
	if payload.Programs.Reference, err = putSource(p.Reference); err != nil {
		return nil, err
	}
	for _, opt := range []struct {
		src  *Source
		dest **judgeapi.Program
	}{{p.Brute, &payload.Programs.Brute}, {p.Validator, &payload.Programs.Validator}} {
		if opt.src == nil {
			continue
		}
		prog, err := putSource(*opt.src)
		if err != nil {
			return nil, err
		}
		*opt.dest = &prog
	}
	if p.Checker == CheckerCustom {
		if payload.Checker.SourceSHA, err = s.Blobs.Put(ctx, tx, []byte(p.CheckerSource)); err != nil {
			return nil, err
		}
	}
	for _, t := range p.Tests {
		payload.Tests = append(payload.Tests, judgeapi.Test{Idx: t.Idx, Sample: p.IsSample(t.Idx), InputSHA: t.InputSHA, OutputSHA: t.OutputSHA})
	}
	jobID, err := s.Jobs.Enqueue(ctx, tx, judgeapi.KindValidate, ValidatePriority, payload, userID)
	if err != nil {
		return nil, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO validations (problem_id, job_id, content_hash, requested_by, created_at)
		VALUES (?, ?, ?, ?, ?)`, p.ID, jobID, p.ContentHash, by, s.now().UnixMilli()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.LatestValidation(ctx, id)
}

// LatestValidation returns the problem's most recent validation, or
// ErrNotFound when it has never been validated.
func (s *Service) LatestValidation(ctx context.Context, id int64) (*Validation, error) {
	var v Validation
	var jobID, created int64
	var current string
	err := s.DB.QueryRowContext(ctx, `SELECT v.id, v.job_id, v.content_hash, v.created_at, p.content_hash
		FROM validations v JOIN problems p ON p.id = v.problem_id WHERE v.problem_id = ? ORDER BY v.id DESC LIMIT 1`, id).
		Scan(&v.ID, &jobID, &v.ContentHash, &created, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Current = v.ContentHash == current
	v.QueuedAt = time.UnixMilli(created)
	job, err := s.Jobs.Get(ctx, jobID)
	if err != nil {
		return nil, err
	}
	v.Compile, v.Tests, v.FinishedAt = job.Result.Compile, job.Result.Tests, job.FinishedAt
	switch job.State {
	case jobs.Queued:
		v.State = ValidationQueued
		if v.QueuePosition, err = s.Jobs.QueuePosition(ctx, jobID); err != nil {
			return nil, err
		}
	case jobs.Leased:
		v.State = ValidationRunning
	case jobs.Failed:
		v.State, v.Error = ValidationError, job.Error
	case jobs.Done:
		var sm Summary
		if err := json.Unmarshal(job.Result.Summary, &sm); err != nil {
			return nil, fmt.Errorf("validation %d summary: %w", v.ID, err)
		}
		v.Summary = &sm
		v.State = ValidationFailed
		if sm.Passed {
			v.State = ValidationPassed
		}
	}
	return &v, nil
}

// ValidateKind is the jobs.Kind for validation runs.
type ValidateKind struct {
	Now func() time.Time
}

var roleNames = map[string]string{
	judgeapi.RoleReference: "reference solution",
	judgeapi.RoleBrute:     "brute force",
	judgeapi.RoleValidator: "validator",
	judgeapi.RoleChecker:   "checker",
}

// CheckReport refuses results that do not fit the job: unknown tests or
// roles, a program the problem does not have, a duplicate within the
// report, or numbers no real run could produce. Results already stored
// are refused by the database's unique key.
func (ValidateKind) CheckReport(raw json.RawMessage, r judgeapi.Report) error {
	pl, err := decodePayload(raw)
	if err != nil {
		return err
	}
	present := map[string]bool{judgeapi.RoleReference: true,
		judgeapi.RoleBrute: pl.Programs.Brute != nil, judgeapi.RoleValidator: pl.Programs.Validator != nil,
		judgeapi.RoleChecker: pl.Checker.Type == CheckerCustom}
	compiled := map[string]bool{}
	for _, c := range r.Compile {
		switch {
		case !present[c.Role]:
			return jobs.Badf("compile result for %q, which this job does not have", jobs.Clip(c.Role))
		case compiled[c.Role]:
			return jobs.Badf("second compile result for %q", jobs.Clip(c.Role))
		case c.Verdict != string(judge.OK) && c.Verdict != string(judge.CompileError):
			return jobs.Badf("compile verdict %q", jobs.Clip(c.Verdict))
		case len(c.Output) > judgeapi.MaxCompileOutput || !utf8.ValidString(c.Output):
			return jobs.Badf("compile output too long or not UTF-8")
		}
		compiled[c.Role] = true
	}
	tests := map[int]bool{}
	for _, t := range pl.Tests {
		tests[t.Idx] = true
	}
	type key struct {
		idx  int
		role string
	}
	seen := map[key]bool{}
	for _, t := range r.Results {
		k := key{t.Idx, t.Role}
		switch {
		case !tests[t.Idx]:
			return jobs.Badf("result for test %d, which this job does not have", t.Idx)
		case !present[t.Role] || t.Role == judgeapi.RoleChecker:
			return jobs.Badf("result for role %q", jobs.Clip(t.Role))
		case seen[k]:
			return jobs.Badf("second result for test %d %s", t.Idx, t.Role)
		case !judgeapi.Verdicts[t.Verdict] || t.Verdict == string(judge.OK):
			return jobs.Badf("verdict %q", jobs.Clip(t.Verdict))
		case t.TimeMS < 0 || t.TimeMS > 600_000 || t.MemoryKB < 0 || t.MemoryKB > 8<<20:
			return jobs.Badf("impossible time or memory on test %d", t.Idx)
		case len(t.Message) > judgeapi.MaxMessage || !utf8.ValidString(t.Message):
			return jobs.Badf("message too long or not UTF-8")
		case t.Output != "" || t.OutputSHA != "":
			return jobs.Badf("output on test %d of a validation", t.Idx)
		}
		seen[k] = true
	}
	return nil
}

// Blobs lists the files a validation needs: sources, checker and tests.
func (ValidateKind) Blobs(raw json.RawMessage) ([]string, error) {
	pl, err := decodePayload(raw)
	if err != nil {
		return nil, err
	}
	out := []string{pl.Programs.Reference.SourceSHA}
	for _, p := range []*judgeapi.Program{pl.Programs.Brute, pl.Programs.Validator} {
		if p != nil {
			out = append(out, p.SourceSHA)
		}
	}
	if pl.Checker.SourceSHA != "" {
		out = append(out, pl.Checker.SourceSHA)
	}
	for _, t := range pl.Tests {
		out = append(out, t.InputSHA, t.OutputSHA)
	}
	return out, nil
}

// MaxDuration bounds a validation: four compiles, then every test at its
// worst (validator, reference, brute and checker all at their limits),
// plus slack, capped at two hours.
func (ValidateKind) MaxDuration(raw json.RawMessage) time.Duration {
	pl, err := decodePayload(raw)
	if err != nil {
		return 10 * time.Minute
	}
	perTest := 2*time.Duration(pl.TimeLimitMS)*time.Millisecond*3 + 3*time.Minute
	d := 4*time.Minute + time.Duration(len(pl.Tests))*perTest
	return min(d, 2*time.Hour)
}

func decodePayload(raw json.RawMessage) (judgeapi.ValidatePayload, error) {
	var pl judgeapi.ValidatePayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		return pl, fmt.Errorf("validate payload: %w", err)
	}
	return pl, nil
}

// Finish computes the summary from the recorded results (never from a
// verdict the judge sends about the whole problem) and, when it passed and
// the problem has not changed since, marks the problem validated.
func (k ValidateKind) Finish(ctx context.Context, tx *sql.Tx, job *jobs.Job) error {
	pl, err := decodePayload(job.Payload)
	if err != nil {
		return err
	}
	if job.State == jobs.Failed {
		return nil // the run did not happen; the last verdict stands
	}
	sm := summarise(pl, job.Result)
	b, err := json.Marshal(sm)
	if err != nil {
		return err
	}
	job.Result.Summary = b
	if !sm.Passed {
		// A failing run of the very content that was validated (a slower
		// judge, a fixed judge bug) withdraws the validation.
		_, err := tx.ExecContext(ctx, "UPDATE problems SET validated_hash = NULL, validated_at = NULL WHERE id = ? AND validated_hash = ?",
			pl.ProblemID, pl.ContentHash)
		return err
	}
	now := time.Now()
	if k.Now != nil {
		now = k.Now()
	}
	_, err = tx.ExecContext(ctx, `UPDATE problems SET validated_hash = ?, validated_at = ? WHERE id = ? AND content_hash = ?`,
		pl.ContentHash, now.UnixMilli(), pl.ProblemID, pl.ContentHash)
	return err
}

var verdictWords = map[string]string{
	"AC": "accepted", "WA": "wrong answer", "TLE": "time limit exceeded", "MLE": "memory limit exceeded",
	"RE": "runtime error", "OLE": "output limit exceeded", "CE": "compile error", "IE": "an internal error",
	"SK": "skipped",
}

func summarise(pl judgeapi.ValidatePayload, res jobs.Result) Summary {
	sm := Summary{Problems: []string{}, Warnings: []string{}}
	refLang := judge.Languages[pl.Programs.Reference.Language]
	sm.TimeLimitMS = int(float64(pl.TimeLimitMS) * refLang.TimeFactor)
	if sm.TimeLimitMS == 0 {
		sm.TimeLimitMS = pl.TimeLimitMS
	}

	compiled := map[string]bool{}
	for _, c := range res.Compile {
		if c.Verdict == string(judge.CompileError) {
			sm.Problems = append(sm.Problems, fmt.Sprintf("The %s does not compile.", roleNames[c.Role]))
			continue
		}
		compiled[c.Role] = true
	}
	need := []string{judgeapi.RoleReference}
	if pl.Programs.Brute != nil {
		need = append(need, judgeapi.RoleBrute)
	}
	if pl.Programs.Validator != nil {
		need = append(need, judgeapi.RoleValidator)
	}
	if pl.Checker.Type == CheckerCustom {
		need = append(need, judgeapi.RoleChecker)
	}

	// When the reference or the checker did not compile nothing ran, and
	// the compile error is the whole story.
	if hasCompileError(res, judgeapi.RoleReference) && !compiled[judgeapi.RoleReference] ||
		hasCompileError(res, judgeapi.RoleChecker) && !compiled[judgeapi.RoleChecker] {
		sm.ReferenceTotal = len(pl.Tests)
		sm.Passed = false
		return sm
	}
	by := map[string]map[int]judgeapi.TestResult{}
	for _, t := range res.Tests {
		if by[t.Role] == nil {
			by[t.Role] = map[int]judgeapi.TestResult{}
		}
		by[t.Role][t.Idx] = t
	}
	slowest := 0
	for _, t := range pl.Tests {
		ref, ok := by[judgeapi.RoleReference][t.Idx]
		sm.ReferenceTotal++
		switch {
		case !ok:
			if compiled[judgeapi.RoleReference] {
				sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the judge did not report the reference solution.", t.Idx))
			}
		case ref.Verdict == "AC":
			sm.ReferencePassed++
		default:
			sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: reference solution got %s.%s", t.Idx, verdictWords[ref.Verdict], detail(ref.Message)))
		}
		if ok && ref.TimeMS > sm.MaxTimeMS {
			sm.MaxTimeMS, slowest = ref.TimeMS, t.Idx
		}
		if pl.Programs.Validator != nil {
			sm.ValidatorTotal++
			v, ok := by[judgeapi.RoleValidator][t.Idx]
			switch {
			case !ok:
				if compiled[judgeapi.RoleValidator] {
					sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the judge did not report the validator.", t.Idx))
				}
			case v.Verdict == "AC":
				sm.ValidatorPassed++
			default:
				sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the validator rejected the input.%s", t.Idx, detail(v.Message)))
			}
		}
		if pl.Programs.Brute != nil {
			b, ok := by[judgeapi.RoleBrute][t.Idx]
			if !ok && compiled[judgeapi.RoleBrute] {
				sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the judge did not report the brute force.", t.Idx))
			}
			if ok {
				switch b.Verdict {
				case "AC":
					sm.BruteTotal++
					sm.BruteAgreed++
				case "WA":
					sm.BruteTotal++
					sm.Problems = append(sm.Problems, fmt.Sprintf("Test %d: the brute force disagrees with the expected output.%s", t.Idx, detail(b.Message)))
				case "SK", "TLE":
					// Too slow on a large test is what brute forces do.
				default:
					sm.BruteTotal++
					sm.Warnings = append(sm.Warnings, fmt.Sprintf("Test %d: the brute force got %s.", t.Idx, verdictWords[b.Verdict]))
				}
			}
		}
	}
	for _, role := range need {
		if !compiled[role] && !hasCompileError(res, role) {
			sm.Problems = append(sm.Problems, fmt.Sprintf("The judge did not report compiling the %s.", roleNames[role]))
		}
	}
	if sm.MaxTimeMS*2 > sm.TimeLimitMS {
		sm.Problems = append(sm.Problems, fmt.Sprintf("The reference solution took %d ms on test %d, more than half the %d ms limit. Raise the limit or speed up the reference.",
			sm.MaxTimeMS, slowest, sm.TimeLimitMS))
	} else if sm.MaxTimeMS*3 > sm.TimeLimitMS {
		sm.Warnings = append(sm.Warnings, fmt.Sprintf("The reference solution took %d ms on test %d, more than a third of the %d ms limit.",
			sm.MaxTimeMS, slowest, sm.TimeLimitMS))
	}
	if pl.Programs.Brute != nil && sm.BruteTotal == 0 && compiled[judgeapi.RoleBrute] {
		sm.Warnings = append(sm.Warnings, "The brute force finished no test in time, so it checked nothing.")
	}
	if pl.Programs.Brute == nil {
		sm.Warnings = append(sm.Warnings, "No brute force solution: the reference was not cross-checked.")
	}
	if pl.Programs.Validator == nil {
		sm.Warnings = append(sm.Warnings, "No input validator: the inputs were not checked against the constraints.")
	}
	if pl.Points != 0 && pl.Points != 100 {
		sm.Warnings = append(sm.Warnings, fmt.Sprintf("Subtask points add up to %d. Most contests expect 100.", pl.Points))
	}
	sm.Passed = len(sm.Problems) == 0
	return sm
}

func hasCompileError(res jobs.Result, role string) bool {
	for _, c := range res.Compile {
		if c.Role == role {
			return true
		}
	}
	return false
}

func detail(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	return " " + msg
}
