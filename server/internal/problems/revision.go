package problems

// Problem revisions (v2 plan B2.1 and B2.2, decision D61). The problems row
// is the editable working copy; every save that changes anything a
// revision records adds an immutable problem_revisions row with its tests
// in revision_tests. Contests pin a revision (package contests), so what a
// student was shown and what judged them can be read back later.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxReason bounds the reason a setter gives for a change.
const MaxReason = 500

// RevisionNote says who made a change and why. By is 0 for changes the
// server makes (generated outputs and inputs).
type RevisionNote struct {
	By     int64
	Reason string
}

// RevisionSubtask is a subtask as a revision records it.
type RevisionSubtask struct {
	Idx    int    `json:"idx"`
	Points int    `json:"points"`
	Title  string `json:"title"`
	Tests  string `json:"tests"`
}

// Revision is one immutable version of a problem.
type Revision struct {
	ID, ProblemID int64
	Rev           int
	ContentHash   string
	// StatementHash is the SHA-256 of Statement; "" when not recorded.
	StatementHash     string
	Title, Statement  string
	StatementRecorded bool
	// The limits and checker are nil only in a reconstructed revision no
	// judging job recorded them for.
	TimeLimitMS, MemoryLimitMB *int
	Checker                    *string
	Epsilon                    *float64
	CheckerSHA                 string
	Reference, Brute           SourceRef
	Validator, Generator       SourceRef
	Samples, Open              []int
	Subtasks                   []RevisionSubtask
	// Reconstructed marks a revision the upgrade to v1.2.0 made for a
	// contest pin older than the working copy: tests only.
	Reconstructed bool
	CreatedBy     int64
	CreatedAt     time.Time
	Reason        string
}

// SourceRef is a program as a revision records it: its language and the
// SHA-256 of its text ("" and "" when the problem has none).
type SourceRef struct {
	Language, SHA string
}

func refOf(s *Source) SourceRef {
	if s == nil {
		return SourceRef{}
	}
	return SourceRef{s.Language, SHA([]byte(s.Source))}
}

// RevisionTest is one test of a revision, by the hashes of its files.
type RevisionTest struct {
	Idx                   int
	Kind                  string
	InputSHA, OutputSHA   string
	InputSize, OutputSize int64
}

// revisionOf is the revision the working copy p is, without id, number,
// author or time.
func revisionOf(p *Problem) Revision {
	tl, mem, checker, eps := p.TimeLimitMS, p.MemoryLimitMB, p.Checker, p.Epsilon
	r := Revision{ProblemID: p.ID, ContentHash: p.ContentHash, StatementHash: SHA([]byte(p.Statement)),
		Title: p.Title, Statement: p.Statement, StatementRecorded: true,
		TimeLimitMS: &tl, MemoryLimitMB: &mem, Checker: &checker, Epsilon: &eps,
		Reference: refOf(&p.Reference), Brute: refOf(p.Brute), Validator: refOf(p.Validator), Generator: refOf(p.Generator),
		Samples: append([]int{}, p.Samples...), Open: append([]int{}, p.Open...), Subtasks: []RevisionSubtask{}}
	if p.Checker == CheckerCustom {
		r.CheckerSHA = SHA([]byte(p.CheckerSource))
	}
	for _, st := range p.Subtasks {
		r.Subtasks = append(r.Subtasks, RevisionSubtask{st.Idx, st.Points, st.Title, st.Tests})
	}
	return r
}

// sameRevision reports whether two revisions record the same problem:
// everything judging depends on (the content hash), the title and
// statement students read, the subtask titles they see, and the
// generator. Tags, slug, difficulty and the generator plan are bank
// details and make no revision.
func sameRevision(a, b Revision) bool {
	return a.ContentHash == b.ContentHash && a.StatementRecorded && b.StatementRecorded &&
		a.Title == b.Title && a.Statement == b.Statement && a.Generator == b.Generator &&
		slices.Equal(a.Subtasks, b.Subtasks)
}

// StatementChanged reports whether the title or statement differ.
func StatementChanged(a, b *Revision) bool {
	return a.Title != b.Title || a.StatementHash != b.StatementHash || a.StatementRecorded != b.StatementRecorded
}

type txQueryer interface {
	queryer
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const revisionColumns = `id, problem_id, rev, content_hash, statement_hash, title, statement_md, statement_recorded,
	time_limit_ms, memory_limit_mb, checker, epsilon, checker_sha, reference_language, reference_sha, brute_language, brute_sha,
	validator_language, validator_sha, generator_language, generator_sha, samples, open_tests, subtasks, reconstructed,
	coalesce(created_by, 0), created_at, reason`

func scanRevision(sc interface{ Scan(...any) error }) (*Revision, error) {
	var r Revision
	var tl, mem sql.NullInt64
	var checker sql.NullString
	var eps sql.NullFloat64
	var samples, open, subtasks string
	var created int64
	err := sc.Scan(&r.ID, &r.ProblemID, &r.Rev, &r.ContentHash, &r.StatementHash, &r.Title, &r.Statement, &r.StatementRecorded,
		&tl, &mem, &checker, &eps, &r.CheckerSHA, &r.Reference.Language, &r.Reference.SHA, &r.Brute.Language, &r.Brute.SHA,
		&r.Validator.Language, &r.Validator.SHA, &r.Generator.Language, &r.Generator.SHA, &samples, &open, &subtasks,
		&r.Reconstructed, &r.CreatedBy, &created, &r.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if tl.Valid {
		v := int(tl.Int64)
		r.TimeLimitMS = &v
	}
	if mem.Valid {
		v := int(mem.Int64)
		r.MemoryLimitMB = &v
	}
	if checker.Valid {
		r.Checker = &checker.String
	}
	if eps.Valid {
		r.Epsilon = &eps.Float64
	}
	json.Unmarshal([]byte(samples), &r.Samples)
	json.Unmarshal([]byte(open), &r.Open)
	json.Unmarshal([]byte(subtasks), &r.Subtasks)
	r.Samples, r.Open = nonNilInts(r.Samples), nonNilInts(r.Open)
	if r.Subtasks == nil {
		r.Subtasks = []RevisionSubtask{}
	}
	r.CreatedAt = time.UnixMilli(created)
	return &r, nil
}

// latestRevision returns the problem's newest revision, or nil when it has
// none.
func latestRevision(ctx context.Context, q queryer, problemID int64) (*Revision, error) {
	r, err := scanRevision(q.QueryRowContext(ctx, "SELECT "+revisionColumns+
		" FROM problem_revisions WHERE problem_id = ? ORDER BY rev DESC LIMIT 1", problemID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return r, err
}

// writeRevision records p as a new revision unless its newest revision
// already records it. It returns the revision p now is and whether it was
// written.
func writeRevision(ctx context.Context, tx txQueryer, p *Problem, note RevisionNote, now time.Time) (*Revision, bool, error) {
	latest, err := latestRevision(ctx, tx, p.ID)
	if err != nil {
		return nil, false, err
	}
	r := revisionOf(p)
	if latest != nil && sameRevision(*latest, r) {
		return latest, false, nil
	}
	r.Rev = 1
	if latest != nil {
		r.Rev = latest.Rev + 1
	}
	r.CreatedBy, r.CreatedAt, r.Reason = note.By, now, limitReason(note.Reason)
	samples, _ := json.Marshal(nonNilInts(r.Samples))
	open, _ := json.Marshal(nonNilInts(r.Open))
	subtasks, _ := json.Marshal(r.Subtasks)
	var by any
	if note.By > 0 {
		by = note.By
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO problem_revisions (problem_id, rev, content_hash, statement_hash, title, statement_md,
		statement_recorded, time_limit_ms, memory_limit_mb, checker, epsilon, checker_sha, reference_language, reference_sha,
		brute_language, brute_sha, validator_language, validator_sha, generator_language, generator_sha, samples, open_tests,
		subtasks, reconstructed, created_by, created_at, reason)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?) RETURNING id`,
		p.ID, r.Rev, r.ContentHash, r.StatementHash, r.Title, r.Statement, *r.TimeLimitMS, *r.MemoryLimitMB, *r.Checker, *r.Epsilon,
		r.CheckerSHA, r.Reference.Language, r.Reference.SHA, r.Brute.Language, r.Brute.SHA, r.Validator.Language, r.Validator.SHA,
		r.Generator.Language, r.Generator.SHA, string(samples), string(open), string(subtasks), by, now.UnixMilli(), r.Reason).Scan(&r.ID)
	if err != nil {
		return nil, false, fmt.Errorf("record revision %d of problem %d: %w", r.Rev, p.ID, err)
	}
	for _, t := range p.Tests {
		if _, err := tx.ExecContext(ctx, `INSERT INTO revision_tests (revision_id, idx, kind, input_sha, output_sha, input_size, output_size)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, r.ID, t.Idx, p.TestKind(t.Idx), t.InputSHA, nullIfEmpty(t.OutputSHA), t.InputSize, t.OutputSize); err != nil {
			return nil, false, fmt.Errorf("record revision %d tests: %w", r.Rev, err)
		}
	}
	return &r, true, nil
}

func limitReason(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= MaxReason {
		return s
	}
	return strings.ToValidUTF8(s[:MaxReason], "")
}

// LiveContest is a running contest that pins a problem.
type LiveContest struct {
	ContestID int64  `json:"contest_id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Label     string `json:"label"`
}

// liveContestsOf lists the published, unarchived contests pinning the
// problem that are running at now: from their start to their close plus
// the longest personal extension.
func liveContestsOf(ctx context.Context, q queryer, problemID int64, now time.Time) ([]LiveContest, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.id, c.slug, c.title, cp.label FROM contest_problems cp JOIN contests c ON c.id = cp.contest_id
		WHERE cp.problem_id = ? AND c.published = 1 AND c.archived = 0 AND c.starts_at <= ?
		AND ? < c.ends_at + (c.extended_minutes + (SELECT coalesce(max(e.minutes), 0) FROM contest_extensions e WHERE e.contest_id = c.id)) * 60000
		ORDER BY c.starts_at, c.id`, problemID, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveContest{}
	for rows.Next() {
		var c LiveContest
		if err := rows.Scan(&c.ContestID, &c.Slug, &c.Title, &c.Label); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LiveContests lists the running contests that pin the problem.
func (s *Service) LiveContests(ctx context.Context, problemID int64) ([]LiveContest, error) {
	return liveContestsOf(ctx, s.DB, problemID, s.now())
}

// requireReason refuses a save that would make a new revision while a live
// contest pins the problem, unless the setter says why (B2.2).
func (s *Service) requireReason(ctx context.Context, tx *sql.Tx, p *Problem, note RevisionNote) error {
	if utf8.RuneCountInString(strings.TrimSpace(note.Reason)) > MaxReason {
		return FieldErrors{"reason": fmt.Sprintf("Keep the reason under %d characters.", MaxReason)}
	}
	if strings.TrimSpace(note.Reason) != "" {
		return nil
	}
	latest, err := latestRevision(ctx, tx, p.ID)
	if err != nil {
		return err
	}
	q := *p
	q.ContentHash = ContentHash(&q)
	if latest != nil && sameRevision(*latest, revisionOf(&q)) {
		return nil
	}
	live, err := liveContestsOf(ctx, tx, p.ID, s.now())
	if err != nil || len(live) == 0 {
		return err
	}
	return FieldErrors{"reason": fmt.Sprintf("This problem is %s in the live contest %s. Say why you are changing it; the reason is kept with the new revision.",
		live[0].Label, live[0].Title)}
}

// RevisionByID returns a revision by its id.
func (s *Service) RevisionByID(ctx context.Context, id int64) (*Revision, error) {
	return scanRevision(s.DB.QueryRowContext(ctx, "SELECT "+revisionColumns+" FROM problem_revisions WHERE id = ?", id))
}

// Revisions returns a problem's newest revisions, at most limit, and how
// many it has. Each is returned with the one before it, so callers can say
// what changed: prev[i] is the revision before list[i], or nil.
func (s *Service) Revisions(ctx context.Context, problemID int64, limit int) (list []*Revision, prev []*Revision, total int, err error) {
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM problem_revisions WHERE problem_id = ?", problemID).Scan(&total); err != nil {
		return nil, nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+revisionColumns+" FROM problem_revisions WHERE problem_id = ? ORDER BY rev DESC LIMIT ?", problemID, limit+1)
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	var all []*Revision
	for rows.Next() {
		r, err := scanRevision(rows)
		if err != nil {
			return nil, nil, 0, err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	for i := range min(len(all), limit) {
		list = append(list, all[i])
		if i+1 < len(all) {
			prev = append(prev, all[i+1])
		} else {
			prev = append(prev, nil)
		}
	}
	return list, prev, total, nil
}

// RevisionTests returns a revision's tests in order.
func (s *Service) RevisionTests(ctx context.Context, revisionID int64) ([]RevisionTest, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT idx, kind, input_sha, coalesce(output_sha, ''), input_size, output_size
		FROM revision_tests WHERE revision_id = ? ORDER BY idx`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RevisionTest{}
	for rows.Next() {
		var t RevisionTest
		if err := rows.Scan(&t.Idx, &t.Kind, &t.InputSHA, &t.OutputSHA, &t.InputSize, &t.OutputSize); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CheckRevisions lists every problem whose working copy is not recorded by
// its newest revision, and every contest pin without a revision. An empty
// list means no write skipped its revision (B2.2); podium-server check
// reports it.
func CheckRevisions(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT id FROM problems ORDER BY id")
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []string
	for _, id := range ids {
		p, err := load(ctx, db, id)
		if err != nil {
			return nil, err
		}
		latest, err := latestRevision(ctx, db, id)
		if err != nil {
			return nil, err
		}
		switch {
		case latest == nil:
			out = append(out, fmt.Sprintf("problem %d (%s) has no revision", id, p.Slug))
		case !sameRevision(*latest, revisionOf(p)):
			out = append(out, fmt.Sprintf("problem %d (%s) differs from its newest revision %d", id, p.Slug, latest.Rev))
		default:
			tests, err := (&Service{DB: db}).RevisionTests(ctx, latest.ID)
			if err != nil {
				return nil, err
			}
			if !sameTests(p, tests) {
				out = append(out, fmt.Sprintf("problem %d (%s) has tests its newest revision %d does not list", id, p.Slug, latest.Rev))
			}
		}
	}
	// The retention job releases the pins of a contest past retention once
	// it has kept its standings in contest_results; only other unpinned
	// labels are a fault (otherwise every backup after the first expiry
	// would fail its check).
	var pins int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM contest_problems cp WHERE cp.revision_id IS NULL
		AND NOT EXISTS (SELECT 1 FROM contest_results r WHERE r.contest_id = cp.contest_id)`).Scan(&pins); err != nil {
		return nil, err
	}
	if pins > 0 {
		out = append(out, fmt.Sprintf("%d contest pins have no revision", pins))
	}
	return out, nil
}

func sameTests(p *Problem, tests []RevisionTest) bool {
	if len(tests) != len(p.Tests) {
		return false
	}
	for i, t := range p.Tests {
		r := tests[i]
		if r.Idx != t.Idx || r.InputSHA != t.InputSHA || r.OutputSHA != t.OutputSHA || r.Kind != p.TestKind(t.Idx) {
			return false
		}
	}
	return true
}
