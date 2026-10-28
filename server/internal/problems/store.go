package problems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judge"
)

// Errors the API maps to status codes.
var (
	ErrNotFound  = errors.New("problem not found")
	ErrSlugTaken = errors.New("slug taken")
)

// Service manages the problem bank.
type Service struct {
	DB    *sql.DB
	Blobs Blobs
	Jobs  *jobs.Service
	// Now is the clock; tests replace it.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// BankEntry is one row of the problem bank.
type BankEntry struct {
	ID          int64
	Slug        string
	Title       string
	Difficulty  string
	Tags        []string
	Origin      string
	Archived    bool
	TestCount   int
	Points      int
	Validated   bool
	ValidatedAt time.Time
	UpdatedAt   time.Time
	// ShownInPractice: a contest that pins the problem has shown students
	// its hidden tests, being open for practice after its end (D115). Its
	// hidden tests may be known.
	ShownInPractice bool
}

// shownInPractice is the SQL for BankEntry.ShownInPractice of problem p
// at the time given as an argument. Practice opens at the latest
// final time, so ends_at marks it a little early, never late. Only a
// published contest shows anything to students (C2-014). It reads the
// contests as they are now; contests.SetProblems keeps such a problem in
// its contest, so the mark does not disappear when it is taken out.
const shownInPractice = `EXISTS (SELECT 1 FROM contest_problems cp JOIN contests c ON c.id = cp.contest_id
		WHERE cp.problem_id = p.id AND c.published = 1
		AND c.practice_after_end = 1 AND c.ends_at <= ?)`

// List returns the bank, newest change first.
func (s *Service) List(ctx context.Context, archived bool) ([]BankEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.slug, p.title, p.difficulty, p.tags, p.origin, p.archived,
		p.content_hash, coalesce(p.validated_hash, ''), coalesce(p.validated_at, 0), p.updated_at,
		(SELECT count(*) FROM testcases t WHERE t.problem_id = p.id),
		(SELECT coalesce(sum(points), 0) FROM subtasks st WHERE st.problem_id = p.id), `+shownInPractice+`
		FROM problems p WHERE p.archived = 0 OR ? ORDER BY p.updated_at DESC, p.id DESC`, s.now().UnixMilli(), archived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BankEntry{}
	for rows.Next() {
		var sm BankEntry
		var tags, hash, vhash string
		var vat, upd int64
		if err := rows.Scan(&sm.ID, &sm.Slug, &sm.Title, &sm.Difficulty, &tags, &sm.Origin, &sm.Archived,
			&hash, &vhash, &vat, &upd, &sm.TestCount, &sm.Points, &sm.ShownInPractice); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tags), &sm.Tags); err != nil {
			return nil, fmt.Errorf("problem %d tags: %w", sm.ID, err)
		}
		sm.Validated = vhash != "" && vhash == hash
		if vat > 0 {
			sm.ValidatedAt = time.UnixMilli(vat)
		}
		sm.UpdatedAt = time.UnixMilli(upd)
		out = append(out, sm)
	}
	return out, rows.Err()
}

// Get loads a problem with its subtasks and tests.
func (s *Service) Get(ctx context.Context, id int64) (*Problem, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return load(ctx, tx, id)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func load(ctx context.Context, q queryer, id int64) (*Problem, error) {
	p := &Problem{ID: id}
	var tags, samples, bruteLang, bruteSrc, valLang, valSrc, genLang, genSrc, plan string
	var vhash sql.NullString
	var vat, author sql.NullInt64
	var created, updated int64
	err := q.QueryRowContext(ctx, `SELECT slug, title, statement_md, time_limit_ms, memory_limit_mb, checker, epsilon,
		checker_source, reference_language, reference_source, brute_language, brute_source, validator_language,
		validator_source, tags, difficulty, samples, origin, author_id, archived, created_at, updated_at,
		content_hash, validated_hash, validated_at, generator_language, generator_source, generator_plan
		FROM problems WHERE id = ?`, id).Scan(
		&p.Slug, &p.Title, &p.Statement, &p.TimeLimitMS, &p.MemoryLimitMB, &p.Checker, &p.Epsilon,
		&p.CheckerSource, &p.Reference.Language, &p.Reference.Source, &bruteLang, &bruteSrc, &valLang,
		&valSrc, &tags, &p.Difficulty, &samples, &p.Origin, &author, &p.Archived, &created, &updated,
		&p.ContentHash, &vhash, &vat, &genLang, &genSrc, &plan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if bruteSrc != "" {
		p.Brute = &Source{bruteLang, bruteSrc}
	}
	if valSrc != "" {
		p.Validator = &Source{valLang, valSrc}
	}
	if genSrc != "" {
		p.Generator = &Source{genLang, genSrc}
	}
	json.Unmarshal([]byte(plan), &p.Plan)
	// The AI-draft tag gates contests and practice: unreadable tags or
	// samples are an error, never an empty list.
	if err := json.Unmarshal([]byte(tags), &p.Tags); err != nil {
		return nil, fmt.Errorf("problem %d tags: %w", id, err)
	}
	if err := json.Unmarshal([]byte(samples), &p.Samples); err != nil {
		return nil, fmt.Errorf("problem %d samples: %w", id, err)
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.Samples == nil {
		p.Samples = []int{}
	}
	p.AuthorID = author.Int64
	p.ValidatedHash = vhash.String
	if vat.Valid {
		p.ValidatedAt = time.UnixMilli(vat.Int64)
	}
	p.CreatedAt, p.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)

	rows, err := q.QueryContext(ctx, "SELECT idx, points, title, tests, generator_args FROM subtasks WHERE problem_id = ? ORDER BY idx", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st Subtask
		var args string
		if err := rows.Scan(&st.Idx, &st.Points, &st.Title, &st.Tests, &args); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(args), &st.GeneratorArgs); err != nil {
			rows.Close()
			return nil, fmt.Errorf("subtask %d generator_args: %w", st.Idx, err)
		}
		p.Subtasks = append(p.Subtasks, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT idx, input_sha, output_sha, input_size, output_size FROM testcases
		WHERE problem_id = ? ORDER BY idx`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t Test
		var out sql.NullString
		if err := rows.Scan(&t.Idx, &t.InputSHA, &out, &t.InputSize, &t.OutputSize); err != nil {
			return nil, err
		}
		t.OutputSHA = out.String
		p.Tests = append(p.Tests, t)
	}
	return p, rows.Err()
}

const starterSolution = `#include <bits/stdc++.h>
using namespace std;

int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);

    return 0;
}
`

const starterStatement = `Describe the task.

## Input

## Output

## Constraints
`

// Create starts a new problem with a starter statement and C++ solution.
func (s *Service) Create(ctx context.Context, title, slug string, authorID int64) (*Problem, error) {
	p := &Problem{
		Title: title, Slug: slug, Statement: starterStatement,
		TimeLimitMS: defaultTimeLimit, MemoryLimitMB: defaultMemoryMB,
		Checker: CheckerTokens, Epsilon: judge.DefaultEpsilon,
		Reference: Source{"cpp17", starterSolution},
		Tags:      []string{}, Samples: []int{}, Origin: OriginManual, AuthorID: authorID,
	}
	if errs := checkFields(p); errs != nil {
		return nil, onlyFields(errs, "title", "slug")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.insert(ctx, tx, p, RevisionNote{By: authorID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// onlyFields keeps the errors for the given fields; the rest come from
// defaults and cannot be wrong.
func onlyFields(errs FieldErrors, keep ...string) error {
	out := FieldErrors{}
	for _, k := range keep {
		if msg, ok := errs[k]; ok {
			out[k] = msg
		}
	}
	if len(out) == 0 {
		return errs
	}
	return out
}

// Import adds a parsed pack to the bank as a new problem.
func (s *Service) Import(ctx context.Context, pk *Pack, authorID int64) (*Problem, error) {
	p := &pk.Problem
	p.AuthorID = authorID
	// Refuse a taken slug before writing megabytes of tests to disk.
	var taken int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM problems WHERE slug = ?", p.Slug).Scan(&taken); err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, FieldErrors{"slug": fmt.Sprintf("Another problem already uses the slug %s.", p.Slug)}
	}
	if err := s.writeTests(p.Tests, pk.Inputs, pk.Outputs); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.recordTests(ctx, tx, p.Tests); err != nil {
		return nil, err
	}
	if err := s.insert(ctx, tx, p, RevisionNote{By: authorID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// writeTests stores the test files on disk and fills in their hashes. It
// runs before any transaction opens.
func (s *Service) writeTests(tests []Test, ins, outs map[int][]byte) error {
	for i := range tests {
		t := &tests[i]
		in, out := ins[t.Idx], outs[t.Idx]
		var err error
		if t.InputSHA, err = s.Blobs.Write(in); err != nil {
			return err
		}
		t.InputSize = int64(len(in))
		if out == nil {
			continue // missing output, generated later
		}
		if t.OutputSHA, err = s.Blobs.Write(out); err != nil {
			return err
		}
		t.OutputSize = int64(len(out))
	}
	return nil
}

func (s *Service) recordTests(ctx context.Context, tx *sql.Tx, tests []Test) error {
	for _, t := range tests {
		if err := s.Blobs.Record(ctx, tx, t.InputSHA, t.InputSize); err != nil {
			return err
		}
		if t.OutputSHA == "" {
			continue
		}
		if err := s.Blobs.Record(ctx, tx, t.OutputSHA, t.OutputSize); err != nil {
			return err
		}
	}
	return nil
}

// insert adds p as a new problem with its first revision.
func (s *Service) insert(ctx context.Context, tx *sql.Tx, p *Problem, note RevisionNote) error {
	now := s.now()
	p.CreatedAt, p.UpdatedAt = now, now
	p.ContentHash = ContentHash(p)
	var author any
	if p.AuthorID > 0 {
		author = p.AuthorID
	}
	cols := rowValues(p)
	err := tx.QueryRowContext(ctx, `INSERT INTO problems (slug, title, statement_md, time_limit_ms, memory_limit_mb,
		checker, epsilon, checker_source, reference_language, reference_source, brute_language, brute_source,
		validator_language, validator_source, tags, difficulty, samples, generator_language, generator_source,
		generator_plan, content_hash, origin, author_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		append(cols, p.Origin, author, now.UnixMilli(), now.UnixMilli())...).Scan(&p.ID)
	if isUnique(err) {
		return FieldErrors{"slug": fmt.Sprintf("Another problem already uses the slug %s.", p.Slug)}
	}
	if err != nil {
		return err
	}
	if err := s.writeChildren(ctx, tx, p); err != nil {
		return err
	}
	_, _, err = writeRevision(ctx, tx, p, note, now)
	return err
}

// rowValues are the editable columns in the order insert and update use.
func rowValues(p *Problem) []any {
	var bl, bs, vl, vs, gl, gs string
	if p.Brute != nil {
		bl, bs = p.Brute.Language, p.Brute.Source
	}
	if p.Validator != nil {
		vl, vs = p.Validator.Language, p.Validator.Source
	}
	if p.Generator != nil {
		gl, gs = p.Generator.Language, p.Generator.Source
	}
	tags, _ := json.Marshal(nonNil(p.Tags))
	samples, _ := json.Marshal(nonNilInts(p.Samples))
	plan, _ := json.Marshal(nonNilPlan(p.Plan))
	return []any{p.Slug, p.Title, p.Statement, p.TimeLimitMS, p.MemoryLimitMB, p.Checker, p.Epsilon,
		p.CheckerSource, p.Reference.Language, p.Reference.Source, bl, bs, vl, vs,
		string(tags), p.Difficulty, string(samples), gl, gs, string(plan), p.ContentHash}
}

func (s *Service) writeChildren(ctx context.Context, tx *sql.Tx, p *Problem) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM subtasks WHERE problem_id = ?", p.ID); err != nil {
		return err
	}
	for _, st := range p.Subtasks {
		args, _ := json.Marshal(nonNil(st.GeneratorArgs))
		if _, err := tx.ExecContext(ctx, "INSERT INTO subtasks (problem_id, idx, points, title, tests, generator_args) VALUES (?, ?, ?, ?, ?, ?)",
			p.ID, st.Idx, st.Points, st.Title, st.Tests, string(args)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM testcases WHERE problem_id = ?", p.ID); err != nil {
		return err
	}
	for _, t := range p.Tests {
		if _, err := tx.ExecContext(ctx, `INSERT INTO testcases (problem_id, idx, input_sha, output_sha, input_size, output_size)
			VALUES (?, ?, ?, ?, ?, ?)`, p.ID, t.Idx, t.InputSHA, nullIfEmpty(t.OutputSHA), t.InputSize, t.OutputSize); err != nil {
			return err
		}
	}
	return nil
}

// Edit is everything the editor can change in one save.
type Edit struct {
	Title, Slug, Statement     string
	TimeLimitMS, MemoryLimitMB int
	Checker                    string
	Epsilon                    float64
	CheckerSource              string
	Reference                  Source
	Brute, Validator           *Source
	Tags                       []string
	Difficulty                 string
	Samples                    []int
	Subtasks                   []Subtask
	// By is the person saving; Reason is why, required while a live
	// contest pins the problem and the save changes it (B2.2).
	By     int64
	Reason string
	// Generator, when set, replaces the generator, the plan and each
	// subtask's generator arguments (taken from Subtasks). When nil they
	// are kept, matched to subtasks by number, so an editor that does not
	// know about generators cannot erase one.
	Generator *GeneratorEdit
}

// GeneratorEdit is the generator part of an editor save. A nil or empty
// Source removes the generator; the plan must then be empty.
type GeneratorEdit struct {
	Source *Source
	Plan   []PlannedTest
}

// Update applies an edit. Field errors come back as FieldErrors.
func (s *Service) Update(ctx context.Context, id int64, e Edit) (*Problem, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	// The AI draft marking ends only when a person changes the statement or
	// a solution, and then the validation is withdrawn, so the problem must
	// pass again as the person left it before a contest can take it. A save
	// that changes neither keeps the marking (security council S10).
	draft := slices.ContainsFunc(p.Tags, isAIDraftTag)
	solutionEdited := e.Reference != p.Reference || !sameSource(emptyToNil(e.Brute), p.Brute)
	personEdit := solutionEdited || e.Statement != p.Statement
	p.Title, p.Slug, p.Statement = e.Title, e.Slug, e.Statement
	p.TimeLimitMS, p.MemoryLimitMB = e.TimeLimitMS, e.MemoryLimitMB
	p.Checker, p.Epsilon, p.CheckerSource = e.Checker, e.Epsilon, e.CheckerSource
	p.Reference, p.Brute, p.Validator = e.Reference, emptyToNil(e.Brute), emptyToNil(e.Validator)
	p.Tags, p.Difficulty = withoutAIDraftTag(nonNil(e.Tags)), e.Difficulty
	if draft && !personEdit {
		p.Tags = append(p.Tags, AIDraftTag)
	}
	oldArgs := map[int][]string{}
	for _, st := range p.Subtasks {
		oldArgs[st.Idx] = st.GeneratorArgs
	}
	p.Samples, p.Subtasks = nonNilInts(e.Samples), e.Subtasks
	if e.Generator != nil {
		p.Generator, p.Plan = emptyToNil(e.Generator.Source), e.Generator.Plan
	} else {
		for i := range p.Subtasks {
			if p.Subtasks[i].GeneratorArgs == nil {
				p.Subtasks[i].GeneratorArgs = oldArgs[i+1]
			}
		}
	}
	if errs := checkFields(p); errs != nil {
		return nil, errs
	}
	note := RevisionNote{By: e.By, Reason: e.Reason}
	if err := s.requireReason(ctx, tx, p, note); err != nil {
		return nil, err
	}
	if err := s.save(ctx, tx, p, note); err != nil {
		return nil, err
	}
	if draft && personEdit {
		if _, err := tx.ExecContext(ctx, "UPDATE problems SET validated_hash = NULL, validated_at = NULL WHERE id = ?", p.ID); err != nil {
			return nil, err
		}
		p.ValidatedHash, p.ValidatedAt = "", time.Time{}
	}
	// A reference or brute force a person wrote is what lets a
	// contest take a problem the AI wizard made (contests.SetProblems).
	if solutionEdited {
		if _, err := tx.ExecContext(ctx, "UPDATE problems SET solution_by_person = 1 WHERE id = ?", p.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// isAIDraftTag reports whether a tag is the AI draft marking.
func isAIDraftTag(t string) bool { return strings.ToLower(strings.TrimSpace(t)) == AIDraftTag }

// sameSource compares two optional programs.
func sameSource(a, b *Source) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// emptyToNil treats a brute or validator with no source as absent.
func emptyToNil(src *Source) *Source {
	if src == nil || strings.TrimSpace(src.Source) == "" {
		return nil
	}
	return src
}

// save writes the working copy and, when anything a revision records
// changed, a new revision. Every write of a problem, its subtasks or its
// tests goes through save or insert (B2.2).
func (s *Service) save(ctx context.Context, tx *sql.Tx, p *Problem, note RevisionNote) error {
	p.UpdatedAt = s.now()
	p.ContentHash = ContentHash(p)
	_, err := tx.ExecContext(ctx, `UPDATE problems SET slug = ?, title = ?, statement_md = ?, time_limit_ms = ?,
		memory_limit_mb = ?, checker = ?, epsilon = ?, checker_source = ?, reference_language = ?, reference_source = ?,
		brute_language = ?, brute_source = ?, validator_language = ?, validator_source = ?, tags = ?, difficulty = ?,
		samples = ?, generator_language = ?, generator_source = ?, generator_plan = ?, content_hash = ?, updated_at = ? WHERE id = ?`,
		append(rowValues(p), p.UpdatedAt.UnixMilli(), p.ID)...)
	if isUnique(err) {
		return FieldErrors{"slug": fmt.Sprintf("Another problem already uses the slug %s.", p.Slug)}
	}
	if err != nil {
		return err
	}
	if err := s.writeChildren(ctx, tx, p); err != nil {
		return err
	}
	_, _, err = writeRevision(ctx, tx, p, note, p.UpdatedAt)
	return err
}

var looseTestFile = regexp.MustCompile(`^(?:tests/)?0*([1-9][0-9]{0,2})\.(in|out)$`)

// ReplaceTests swaps every test for those in a zip of NN.in and NN.out
// files (at the root or under tests/). Samples, open tests and subtask
// ranges are kept as written; if they no longer fit, the problem lists
// what to fix. note names who replaced them and why (the reason is
// required while a live contest pins the problem).
func (s *Service) ReplaceTests(ctx context.Context, id int64, zipData []byte, note RevisionNote) (*Problem, error) {
	files, err := readZip(zipData)
	if err != nil {
		return nil, err
	}
	ins, outs := map[int][]byte{}, map[int][]byte{}
	var problems []string
	for name, body := range files {
		m := looseTestFile.FindStringSubmatch(name)
		if m == nil {
			if path.Base(name) != "problem.yaml" && path.Base(name) != "statement.md" {
				problems = append(problems, fmt.Sprintf("%q is not a test file. Name tests like 01.in and 01.out.", name))
			}
			continue
		}
		n, _ := strconv.Atoi(m[1])
		target := ins
		if m[2] == "out" {
			target = outs
		}
		if _, dup := target[n]; dup {
			problems = append(problems, fmt.Sprintf("Test %d has two %s files.", n, m[2]))
		}
		target[n] = body
	}
	for n := range outs {
		if _, ok := ins[n]; !ok {
			problems = append(problems, fmt.Sprintf("Test %d has an output but no input.", n))
		}
	}
	if len(ins) == 0 {
		problems = append(problems, "The zip has no tests. Name them like 01.in and 01.out.")
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, &PackError{problems}
	}

	tests := make([]Test, 0, len(ins))
	for n := range ins {
		tests = append(tests, Test{Idx: n})
	}
	sort.Slice(tests, func(i, j int) bool { return tests[i].Idx < tests[j].Idx })
	if err := s.writeTests(tests, ins, outs); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := load(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := s.recordTests(ctx, tx, tests); err != nil {
		return nil, err
	}
	p.Tests = tests
	if err := s.requireReason(ctx, tx, p, note); err != nil {
		return nil, err
	}
	if err := s.save(ctx, tx, p, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// Archive hides a problem from the bank. Its tests and history stay.
func (s *Service) Archive(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE problems SET archived = 1, updated_at = ? WHERE id = ?", s.now().UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TestBlob returns the hash of one test's input or output.
func (s *Service) TestBlob(ctx context.Context, id int64, idx int, output bool) (string, error) {
	col := "input_sha"
	if output {
		col = "output_sha"
	}
	var sha sql.NullString
	err := s.DB.QueryRowContext(ctx, "SELECT "+col+" FROM testcases WHERE problem_id = ? AND idx = ?", id, idx).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !sha.Valid) {
		return "", ErrNotFound
	}
	return sha.String, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: problems.slug")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilInts(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}
