package problems

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// assertRevisions fails when any problem's working copy is not what its
// newest revision records (B2.2).
func assertRevisions(t *testing.T, s *Service) {
	t.Helper()
	bad, err := CheckRevisions(context.Background(), s.DB)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("writes skipped their revision:\n%s", strings.Join(bad, "\n"))
	}
}

func revCount(t *testing.T, s *Service, id int64) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow("SELECT count(*) FROM problem_revisions WHERE problem_id = ?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestEverySaveThatChangesAProblemWritesOneRevision(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	assertRevisions(t, s)
	if n := revCount(t, s, p.ID); n != 1 {
		t.Fatalf("import made %d revisions", n)
	}
	created, err := s.Create(ctx, "Blank", "blank", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertRevisions(t, s)
	if n := revCount(t, s, created.ID); n != 1 {
		t.Fatalf("create made %d revisions", n)
	}

	// A save that changes nothing makes none; tags and difficulty are bank
	// details and make none either.
	e := editOf(p)
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	e.Tags, e.Difficulty = []string{"sums"}, "hard"
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	if n := revCount(t, s, p.ID); n != 1 {
		t.Fatalf("saves that changed nothing a revision records made %d revisions", n)
	}

	// A statement change, a subtask title and a limit each make one.
	e.Statement = "Add them."
	got, err := s.Update(ctx, p.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	e.Subtasks[0].Title = "Tiny"
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	e.TimeLimitMS = 1500
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	if n := revCount(t, s, p.ID); n != 4 {
		t.Fatalf("three changing saves left %d revisions, want 4", n)
	}
	assertRevisions(t, s)
	r2, err := s.RevisionByNumber(ctx, p.ID, 2)
	if err != nil || r2.Statement != "Add them." || r2.ContentHash != got.ContentHash || r2.Title != p.Title {
		t.Fatalf("revision 2 %+v %v", r2, err)
	}
	r1, _ := s.RevisionByNumber(ctx, p.ID, 1)
	if !StatementChanged(r1, r2) || !StatementOnly(r1, r2) {
		t.Error("revision 2 changed the statement only")
	}
	r4, _ := s.RevisionByNumber(ctx, p.ID, 4)
	if ch := RevisionChanges(r2, r4); strings.Join(ch, ",") != "limits,subtasks" {
		t.Errorf("changes from 2 to 4: %v", ch)
	}

	// Replacing tests makes one, with the tests listed by kind.
	if _, err := s.ReplaceTests(ctx, p.ID, zipEntries(t, entry{name: "1.in", body: "1 2\n"}, entry{name: "1.out", body: "3\n"},
		entry{name: "2.in", body: "2 2\n"}, entry{name: "2.out", body: "4\n"}, entry{name: "3.in", body: "3 3\n"}, entry{name: "3.out", body: "6\n"}),
		RevisionNote{By: 0, Reason: "Fewer tests."}); err != nil {
		t.Fatal(err)
	}
	assertRevisions(t, s)
	latest, _ := s.CurrentRevision(ctx, p.ID)
	if latest.Rev != 5 || latest.Reason != "Fewer tests." {
		t.Fatalf("after replacing tests %+v", latest)
	}
	tests, _ := s.RevisionTests(ctx, latest.ID)
	if len(tests) != 3 || tests[0].Kind != KindSample || tests[1].Kind != KindSample || tests[2].Kind != KindHidden {
		t.Fatalf("revision tests %+v", tests)
	}
	// Archiving and validating change no revision.
	if err := s.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	assertRevisions(t, s)

	// Revisions are append only.
	if _, err := s.DB.ExecContext(ctx, "UPDATE problem_revisions SET title = 'x'"); err == nil {
		t.Error("a revision was updated")
	}
}

// A save that changes a problem a live contest pins needs a reason.
func TestReasonRequiredWhileALiveContestPinsTheProblem(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	p := importExample(t, s, "a-plus-b")
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO contests (id, slug, title, starts_at, ends_at, published, created_at, updated_at)
		VALUES (1, 'week-1', 'Week 1', ?, ?, 1, 0, 0)`, now.Add(-time.Hour).UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id)
		VALUES (1, ?, 'A', 0, (SELECT max(id) FROM problem_revisions WHERE problem_id = ?))`, p.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	live, err := s.LiveContests(ctx, p.ID)
	if err != nil || len(live) != 1 || live[0].Label != "A" {
		t.Fatalf("live %+v %v", live, err)
	}
	e := editOf(p)
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatalf("a save that changes nothing needs no reason: %v", err)
	}
	e.Statement = "Changed."
	var fields FieldErrors
	if _, err := s.Update(ctx, p.ID, e); !errors.As(err, &fields) || !strings.Contains(fields["reason"], "Week 1") {
		t.Fatalf("change without a reason: %v", err)
	}
	if _, err := s.ReplaceTests(ctx, p.ID, zipEntries(t, entry{name: "1.in", body: "1"}), RevisionNote{}); !errors.As(err, &fields) {
		t.Fatalf("test replacement without a reason: %v", err)
	}
	e.Reason = "Typo in the input section."
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	r, _ := s.CurrentRevision(ctx, p.ID)
	if r.Rev != 2 || r.Reason != "Typo in the input section." {
		t.Fatalf("revision %+v", r)
	}
	// After the contest no reason is needed.
	now = now.Add(2 * time.Hour)
	e.Statement, e.Reason = "Changed again.", ""
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTests(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	e := editOf(p)
	hash := p.ContentHash
	// Test 2 is a sample in the example; make test 3 open, out of its subtask.
	open := []int{3}
	e.Open = &open
	var fields FieldErrors
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, p.ID)
	if !got.IsOpen(3) || got.TestKind(3) != KindOpen || got.ContentHash == hash {
		t.Fatalf("open tests %v, hash changed %v", got.Open, got.ContentHash != hash)
	}
	// Test 3 is still in its subtask's range.
	if probs := strings.Join(got.StructureProblems(), " "); !strings.Contains(probs, "Test 3 is in subtask 1 and in the open tests") {
		t.Errorf("structure problems: %s", probs)
	}
	// Leaving open out keeps it.
	e.Open = nil
	got, _ = s.Update(ctx, p.ID, e)
	if !got.IsOpen(3) {
		t.Error("a save without open erased the open tests")
	}
	both := []int{1}
	e.Open = &both
	if _, err := s.Update(ctx, p.ID, e); !errors.As(err, &fields) || !strings.Contains(fields["open"], "sample and an open test") {
		t.Errorf("a sample that is also open: %v", err)
	}
	// The hash of a problem without open tests is what it was before D115.
	q := *p
	q.Open = []int{}
	if ContentHash(&q) != hash {
		t.Error("an empty open list changed the content hash")
	}
	assertRevisions(t, s)
}

func TestLineDiff(t *testing.T) {
	lines, coarse := LineDiff("a\nb\nc\nd\n", "a\nB\nc\nd\ne\n")
	var got []string
	for _, l := range lines {
		got = append(got, l.Op+":"+l.Text)
	}
	if coarse || strings.Join(got, " ") != "same:a del:b add:B same:c same:d add:e" {
		t.Fatalf("diff %v coarse %v", got, coarse)
	}
	if lines, _ := LineDiff("", ""); len(lines) != 0 {
		t.Errorf("empty diff %v", lines)
	}
	var big1, big2 strings.Builder
	for i := range 1100 {
		big1.WriteString("x" + strconv.Itoa(i) + "\n")
		big2.WriteString("y" + strconv.Itoa(i) + "\n")
	}
	if lines, coarse := LineDiff(big1.String(), big2.String()); !coarse || len(lines) != 2200 || lines[0].Op != "del" || lines[2199].Op != "add" {
		t.Errorf("a large rewrite: coarse %v, %d lines", coarse, len(lines))
	}
}

// writeSQL matches a statement that writes a table a revision records.
var writeSQL = regexp.MustCompile(`(?is)\b(INSERT\s+(OR\s+\w+\s+)?INTO|UPDATE|DELETE\s+FROM)\s+(problems|subtasks|testcases)\b(.*)`)

// revisionFreeWrites are the functions allowed to write those tables
// without making a revision, and what they may change: none of it is
// recorded in a revision (validation state, bank details).
var revisionFreeWrites = map[string]string{
	"insert":        "INSERT INTO problems, followed by writeRevision",
	"save":          "UPDATE problems, followed by writeRevision",
	"writeChildren": "subtasks and testcases, called by insert and save only",
	"Update":        "validated_hash and solution_by_person after save",
	"Archive":       "archived",
	"Finish":        "validated_hash after a validation",
}

// No code writes problems, subtasks or testcases except through save and
// insert, which record the revision, or the listed writes of fields a
// revision does not record (B2.2: a test fails if a write skips its
// revision). It reads every Go file of the server.
func TestNoWritePathSkipsTheRevision(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				m := writeSQL.FindStringSubmatch(lit.Value)
				if m == nil {
					return true
				}
				where := fset.Position(lit.Pos())
				if filepath.Base(filepath.Dir(path)) != "problems" {
					t.Errorf("%s: %s writes %s outside package problems", where, fn.Name.Name, m[3])
					return true
				}
				if _, ok := revisionFreeWrites[fn.Name.Name]; !ok {
					t.Errorf("%s: %s writes %s without going through save or insert", where, fn.Name.Name, m[3])
				}
				if strings.EqualFold(m[3], "problems") && strings.HasPrefix(strings.ToUpper(m[1]), "UPDATE") &&
					fn.Name.Name != "save" {
					set := strings.ToLower(m[4])
					if i := strings.Index(set, " where "); i >= 0 {
						set = set[:i]
					}
					for _, col := range []string{"title", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "epsilon",
						"samples", "open_tests", "reference_", "brute_", "validator_", "generator_", "content_hash"} {
						if strings.Contains(set, col) {
							t.Errorf("%s: %s updates %s, which a revision records, outside save", where, fn.Name.Name, col)
						}
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Pack format 2 carries open tests; format 1 packs still import, and an
// export writes format 1 unless the problem has open tests.
func TestPackFormatTwoOpenTests(t *testing.T) {
	withOpen := func(head string) []entry {
		entries := minimalPack(entry{name: "tests/03.in", body: "3"}, entry{name: "tests/03.out", body: "3"})
		for i := range entries {
			if entries[i].name == "problem.yaml" {
				entries[i].body = head + entries[i].body + "open: [3]\n"
			}
		}
		return entries
	}
	pk, err := ReadPack(zipEntries(t, withOpen("format: 2\n")...))
	if err != nil {
		t.Fatal(err)
	}
	if len(pk.Problem.Open) != 1 || pk.Problem.Open[0] != 3 || len(pk.Problem.StructureProblems()) != 0 {
		t.Fatalf("open %v problems %v", pk.Problem.Open, pk.Problem.StructureProblems())
	}
	if _, err := ReadPack(zipEntries(t, withOpen("")...)); err == nil || !strings.Contains(err.Error(), "need format: 2") {
		t.Errorf("open tests in a format 1 pack: %v", err)
	}
	for _, tc := range []struct {
		p    *Problem
		want string
	}{{&pk.Problem, "format: 2\n"}, {&Problem{Title: "x", Slug: "x", Reference: Source{"cpp17", "x"}}, "format: 1\n"}} {
		var out bytes.Buffer
		if err := WritePack(&out, tc.p, func(string) ([]byte, error) { return []byte("1"), nil }); err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if f.Name == "problem.yaml" {
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				if !strings.HasPrefix(string(b), tc.want) {
					t.Errorf("export of %s starts %q, want %q", tc.p.Slug, clip(string(b)), tc.want)
				}
			}
		}
	}
}
