package problems

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// packOf reads the a-plus-b example with statement.md and the tags line of
// problem.yaml replaced; an empty argument keeps the file as it is.
func packOf(t *testing.T, statement, tagsLine string) *Pack {
	t.Helper()
	data := zipDir(t, filepath.Join(examples, "a-plus-b"))
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		switch {
		case f.Name == "statement.md" && statement != "":
			b = []byte(statement)
		case f.Name == "problem.yaml" && tagsLine != "":
			b = []byte(strings.Replace(string(b), "tags: [math, implementation]", tagsLine, 1))
		}
		w, _ := zw.Create(f.Name)
		w.Write(b)
	}
	zw.Close()
	pk, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func revisionCount(t *testing.T, s *Service, id int64) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow("SELECT count(*) FROM problem_revisions WHERE problem_id = ?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportAsReportsTheConflict(t *testing.T) {
	s := newBank(t)
	old := importExample(t, s, "a-plus-b")
	_, err := s.ImportAs(context.Background(), packOf(t, "", ""), 0, ImportOptions{})
	var c *SlugConflictError
	if !errors.As(err, &c) || c.ExistingID != old.ID || c.Slug != "a-plus-b" || c.Suggested != "a-plus-b-2" {
		t.Fatalf("conflict: %v %+v", err, c)
	}
	if !strings.Contains(c.Error(), "Choose what to do with this pack.") || strings.Contains(c.Error(), "archive and rename") {
		t.Errorf("message %q", c.Error())
	}
	if _, err := s.ImportAs(context.Background(), packOf(t, "", ""), 0, ImportOptions{OnConflict: "nonsense"}); err == nil {
		t.Error("an unknown on_conflict was accepted")
	}
}

func TestImportAsRenameMakesANewProblem(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	old := importExample(t, s, "a-plus-b")
	p, err := s.ImportAs(ctx, packOf(t, "", ""), 0, ImportOptions{OnConflict: ImportRename, Slug: "a-plus-b-2"})
	if err != nil || p.ID == old.ID || p.Slug != "a-plus-b-2" {
		t.Fatalf("rename: %v %+v", err, p)
	}
	var fe FieldErrors
	if _, err := s.ImportAs(ctx, packOf(t, "", ""), 0, ImportOptions{OnConflict: ImportRename, Slug: "Bad Slug"}); !errors.As(err, &fe) || fe["slug"] == "" {
		t.Errorf("bad slug: %v", err)
	}
	// A chosen slug that is taken is a conflict again, naming that problem.
	var c *SlugConflictError
	if _, err := s.ImportAs(ctx, packOf(t, "", ""), 0, ImportOptions{OnConflict: ImportRename, Slug: "a-plus-b-2"}); !errors.As(err, &c) || c.ExistingID != p.ID {
		t.Errorf("taken chosen slug: %v", err)
	}
}

func TestImportAsArchiveExistingTakesTheSlug(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	old := importExample(t, s, "a-plus-b")
	p, err := s.ImportAs(ctx, packOf(t, "", ""), 0, ImportOptions{OnConflict: ImportArchiveExisting})
	if err != nil || p.ID == old.ID || p.Slug != "a-plus-b" {
		t.Fatalf("archive and import: %v %+v", err, p)
	}
	slug, original, archived := slugOf(t, s, old.ID)
	if !archived || original != "a-plus-b" || slug != archivedForm("a-plus-b", old.ID) {
		t.Errorf("old problem: slug %q original %q archived %v", slug, original, archived)
	}
	// With nothing in the way it is a plain import.
	s2 := newBank(t)
	q, err := s2.ImportAs(ctx, packOf(t, "", ""), 0, ImportOptions{OnConflict: ImportArchiveExisting})
	if err != nil || q.Slug != "a-plus-b" {
		t.Errorf("no conflict: %v %+v", err, q)
	}
}

func TestImportAsReplaceSavesANewRevision(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	old := importExample(t, s, "a-plus-b")
	first, err := s.CurrentRevision(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A submission made on the first revision.
	if _, err := s.DB.Exec(`INSERT INTO users (id, email, name, role, created_at) VALUES (7, 's@college.edu', 'S', 'student', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, state, created_at, revision_id)
		VALUES (?, 7, 'submit', 'python', 'print(1)', 'x', 'done', 0, ?)`, old.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	pk := packOf(t, "A new statement.", "tags: [fresh]")
	p, err := s.ImportAs(ctx, pk, 7, ImportOptions{OnConflict: ImportReplace, FileName: "two-sum.zip"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != old.ID || p.Slug != "a-plus-b" || p.Statement != "A new statement." || len(p.Tests) != 6 || len(p.Tags) != 1 || p.Tags[0] != "fresh" {
		t.Fatalf("replaced problem: %+v", p)
	}
	if n := revisionCount(t, s, old.ID); n != 2 {
		t.Fatalf("revisions: %d", n)
	}
	latest, _ := s.CurrentRevision(ctx, old.ID)
	if latest.Rev != 2 || latest.Reason != "Replaced from pack two-sum.zip" {
		t.Errorf("revision %d reason %q", latest.Rev, latest.Reason)
	}
	// Old results keep the revision they were judged on, and it is unchanged.
	var rev int64
	s.DB.QueryRow("SELECT revision_id FROM submissions WHERE problem_id = ?", old.ID).Scan(&rev)
	if rev != first.ID {
		t.Errorf("submission points at revision %d, want %d", rev, first.ID)
	}
	kept, err := s.RevisionByID(ctx, first.ID)
	if err != nil || kept.Statement != old.Statement {
		t.Errorf("first revision changed: %v", err)
	}
}

func TestImportAsReplaceFollowsThePackForAIDraft(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	old := importExample(t, s, "a-plus-b")
	// The pack carries the draft marking: it stays, as a fresh Import would.
	p, err := s.ImportAs(ctx, packOf(t, "", "tags: [math, ai draft]"), 0, ImportOptions{OnConflict: ImportReplace})
	if err != nil || p.ID != old.ID {
		t.Fatal(err)
	}
	draft := false
	for _, tag := range p.Tags {
		draft = draft || isAIDraftTag(tag)
	}
	if !draft {
		t.Errorf("tags %v lost the ai draft marking", p.Tags)
	}
	// A pack without it takes the marking off, and origin is the pack's.
	p, err = s.ImportAs(ctx, packOf(t, "", "tags: [math]"), 0, ImportOptions{OnConflict: ImportReplace})
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range p.Tags {
		if isAIDraftTag(tag) {
			t.Errorf("tags %v still carry the draft marking", p.Tags)
		}
	}
	if p.Origin != OriginImport {
		t.Errorf("origin %q", p.Origin)
	}
}

func TestImportAsReplaceRefusedWhileLive(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	old := importExample(t, s, "a-plus-b")
	rev, _ := s.CurrentRevision(ctx, old.ID)
	if _, err := s.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
		VALUES (1, 'wk3', 'Week 3', ?, ?, 1, 0, 0, 0)`, now.Add(-time.Hour).UnixMilli(), now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, old.ID, rev.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.ImportAs(ctx, packOf(t, "Changed.", ""), 0, ImportOptions{OnConflict: ImportReplace})
	var live *ReplaceLiveError
	if !errors.As(err, &live) || live.ExistingID != old.ID || len(live.Live) != 1 {
		t.Fatalf("replace while live: %v", err)
	}
	if n := revisionCount(t, s, old.ID); n != 1 {
		t.Errorf("a refused replace made %d revisions", n)
	}
	got, _ := s.Get(ctx, old.ID)
	if got.Statement != old.Statement {
		t.Error("a refused replace changed the problem")
	}
	// Archive and import is not blocked: the old problem keeps serving.
	if _, err := s.ImportAs(ctx, packOf(t, "", ""), 0, ImportOptions{OnConflict: ImportArchiveExisting}); err != nil {
		t.Errorf("archive and import while live: %v", err)
	}
}

func TestReplaceReasonIsCleaned(t *testing.T) {
	if got := replaceReason("a\nb.zip"); got != "Replaced from pack ab.zip" {
		t.Errorf("%q", got)
	}
	if got := replaceReason(""); got != "Replaced from a pack" {
		t.Errorf("%q", got)
	}
	if got := replaceReason(strings.Repeat("x", 500)); len(got) > 100 {
		t.Errorf("long name kept: %d", len(got))
	}
}
