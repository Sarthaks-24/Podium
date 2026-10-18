package problems

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
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
}

// List returns the bank, newest change first.
func (s *Service) List(ctx context.Context, archived bool) ([]BankEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.slug, p.title, p.difficulty, p.tags, p.origin, p.archived,
		p.content_hash, coalesce(p.validated_hash, ''), coalesce(p.validated_at, 0), p.updated_at,
		(SELECT count(*) FROM testcases t WHERE t.problem_id = p.id),
		(SELECT coalesce(sum(points), 0) FROM subtasks st WHERE st.problem_id = p.id)
		FROM problems p WHERE p.archived = 0 OR ? ORDER BY p.updated_at DESC, p.id DESC`, archived)
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
			&hash, &vhash, &vat, &upd, &sm.TestCount, &sm.Points); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(tags), &sm.Tags)
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
	var tags, samples, bruteLang, bruteSrc, valLang, valSrc string
	var vhash sql.NullString
	var vat, author sql.NullInt64
	var created, updated int64
	err := q.QueryRowContext(ctx, `SELECT slug, title, statement_md, time_limit_ms, memory_limit_mb, checker, epsilon,
		checker_source, reference_language, reference_source, brute_language, brute_source, validator_language,
		validator_source, tags, difficulty, samples, origin, author_id, archived, created_at, updated_at,
		content_hash, validated_hash, validated_at FROM problems WHERE id = ?`, id).Scan(
		&p.Slug, &p.Title, &p.Statement, &p.TimeLimitMS, &p.MemoryLimitMB, &p.Checker, &p.Epsilon,
		&p.CheckerSource, &p.Reference.Language, &p.Reference.Source, &bruteLang, &bruteSrc, &valLang,
		&valSrc, &tags, &p.Difficulty, &samples, &p.Origin, &author, &p.Archived, &created, &updated,
		&p.ContentHash, &vhash, &vat)
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
	json.Unmarshal([]byte(tags), &p.Tags)
	json.Unmarshal([]byte(samples), &p.Samples)
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

	rows, err := q.QueryContext(ctx, "SELECT idx, points, title, tests FROM subtasks WHERE problem_id = ? ORDER BY idx", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var st Subtask
		if err := rows.Scan(&st.Idx, &st.Points, &st.Title, &st.Tests); err != nil {
			rows.Close()
			return nil, err
		}
		p.Subtasks = append(p.Subtasks, st)
	}
	rows.Close()
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
	if err := s.insert(ctx, tx, p); err != nil {
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
	if err := s.insert(ctx, tx, p); err != nil {
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

func (s *Service) insert(ctx context.Context, tx *sql.Tx, p *Problem) error {
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
		validator_language, validator_source, tags, difficulty, samples, content_hash,
		origin, author_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		append(cols, p.Origin, author, now.UnixMilli(), now.UnixMilli())...).Scan(&p.ID)
	if isUnique(err) {
		return FieldErrors{"slug": fmt.Sprintf("Another problem already uses the slug %s.", p.Slug)}
	}
	if err != nil {
		return err
	}
	return s.writeChildren(ctx, tx, p)
}

// rowValues are the editable columns in the order insert and update use.
func rowValues(p *Problem) []any {
	var bl, bs, vl, vs string
	if p.Brute != nil {
		bl, bs = p.Brute.Language, p.Brute.Source
	}
	if p.Validator != nil {
		vl, vs = p.Validator.Language, p.Validator.Source
	}
	tags, _ := json.Marshal(nonNil(p.Tags))
	samples, _ := json.Marshal(nonNilInts(p.Samples))
	return []any{p.Slug, p.Title, p.Statement, p.TimeLimitMS, p.MemoryLimitMB, p.Checker, p.Epsilon,
		p.CheckerSource, p.Reference.Language, p.Reference.Source, bl, bs, vl, vs,
		string(tags), p.Difficulty, string(samples), p.ContentHash}
}

func (s *Service) writeChildren(ctx context.Context, tx *sql.Tx, p *Problem) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM subtasks WHERE problem_id = ?", p.ID); err != nil {
		return err
	}
	for _, st := range p.Subtasks {
		if _, err := tx.ExecContext(ctx, "INSERT INTO subtasks (problem_id, idx, points, title, tests) VALUES (?, ?, ?, ?, ?)",
			p.ID, st.Idx, st.Points, st.Title, st.Tests); err != nil {
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
	p.Title, p.Slug, p.Statement = e.Title, e.Slug, e.Statement
	p.TimeLimitMS, p.MemoryLimitMB = e.TimeLimitMS, e.MemoryLimitMB
	p.Checker, p.Epsilon, p.CheckerSource = e.Checker, e.Epsilon, e.CheckerSource
	p.Reference, p.Brute, p.Validator = e.Reference, emptyToNil(e.Brute), emptyToNil(e.Validator)
	p.Tags, p.Difficulty = nonNil(e.Tags), e.Difficulty
	p.Samples, p.Subtasks = nonNilInts(e.Samples), e.Subtasks
	if errs := checkFields(p); errs != nil {
		return nil, errs
	}
	if err := s.save(ctx, tx, p); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// emptyToNil treats a brute or validator with no source as absent.
func emptyToNil(src *Source) *Source {
	if src == nil || strings.TrimSpace(src.Source) == "" {
		return nil
	}
	return src
}

func (s *Service) save(ctx context.Context, tx *sql.Tx, p *Problem) error {
	p.UpdatedAt = s.now()
	p.ContentHash = ContentHash(p)
	_, err := tx.ExecContext(ctx, `UPDATE problems SET slug = ?, title = ?, statement_md = ?, time_limit_ms = ?,
		memory_limit_mb = ?, checker = ?, epsilon = ?, checker_source = ?, reference_language = ?, reference_source = ?,
		brute_language = ?, brute_source = ?, validator_language = ?, validator_source = ?, tags = ?, difficulty = ?,
		samples = ?, content_hash = ?, updated_at = ? WHERE id = ?`,
		append(rowValues(p), p.UpdatedAt.UnixMilli(), p.ID)...)
	if isUnique(err) {
		return FieldErrors{"slug": fmt.Sprintf("Another problem already uses the slug %s.", p.Slug)}
	}
	if err != nil {
		return err
	}
	return s.writeChildren(ctx, tx, p)
}

var looseTestFile = regexp.MustCompile(`^(?:tests/)?0*([1-9][0-9]{0,2})\.(in|out)$`)

// ReplaceTests swaps every test for those in a zip of NN.in and NN.out
// files (at the root or under tests/). Samples and subtask ranges are kept
// as written; if they no longer fit, the problem lists what to fix.
func (s *Service) ReplaceTests(ctx context.Context, id int64, zipData []byte) (*Problem, error) {
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
	if err := s.save(ctx, tx, p); err != nil {
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
