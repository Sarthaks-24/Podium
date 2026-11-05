package problems

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func slugOf(t *testing.T, s *Service, id int64) (slug, original string, archived bool) {
	t.Helper()
	p, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Slug, p.ArchivedSlug, p.Archived
}

func TestArchiveFreesTheSlugAndKeepsTheOriginal(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	if err := s.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	slug, original, archived := slugOf(t, s, p.ID)
	if !archived || original != "a-plus-b" || slug != archivedForm("a-plus-b", p.ID) {
		t.Fatalf("after archive: slug %q original %q archived %v", slug, original, archived)
	}
	// Archiving again keeps the first original and the slug it took.
	if err := s.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if s2, o2, _ := slugOf(t, s, p.ID); s2 != slug || o2 != "a-plus-b" {
		t.Errorf("second archive changed slug %q original %q", s2, o2)
	}
	// The slug is free: a new problem can use it.
	again, err := s.Create(ctx, "A plus B", "a-plus-b", 0, "")
	if err != nil || again.Slug != "a-plus-b" {
		t.Fatalf("create with the freed slug: %v %+v", err, again)
	}
	if err := s.Archive(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("archive of a missing problem: %v", err)
	}
}

func TestArchivedSlugIsLockedUntilRestore(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	if err := s.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, p.ID)
	e := editOf(got)
	e.Title = "Renamed while archived"
	if _, err := s.Update(ctx, p.ID, e); err != nil {
		t.Fatalf("other fields stay editable: %v", err)
	}
	e.Slug = "something-else"
	var fe FieldErrors
	if _, err := s.Update(ctx, p.ID, e); !errors.As(err, &fe) || fe["slug"] != "Restore the problem to change its slug." {
		t.Errorf("slug change while archived: %v", err)
	}
}

func TestRestoreGivesTheSlugBack(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.Archive(ctx, p.ID)
	got, err := s.Restore(ctx, p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "a-plus-b" || got.Archived || got.ArchivedSlug != "" {
		t.Errorf("restored: slug %q archived %v original %q", got.Slug, got.Archived, got.ArchivedSlug)
	}
	// Restoring a problem that is not archived changes nothing.
	if again, err := s.Restore(ctx, p.ID, ""); err != nil || again.Slug != "a-plus-b" {
		t.Errorf("restore of a live problem: %v", err)
	}
	if _, err := s.Restore(ctx, 9999, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("restore of a missing problem: %v", err)
	}
}

func TestRestoreAfterReuseNeedsANewSlug(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.Archive(ctx, p.ID)
	if _, err := s.Create(ctx, "A plus B", "a-plus-b", 0, ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.Restore(ctx, p.ID, "")
	var taken *SlugTakenError
	if !errors.As(err, &taken) || taken.Slug != "a-plus-b" || taken.Suggested != "a-plus-b-2" ||
		taken.Error() != "Another problem now uses a-plus-b. Choose a new slug." {
		t.Fatalf("restore after reuse: %v", err)
	}
	if _, _, archived := slugOf(t, s, p.ID); !archived {
		t.Error("a refused restore unarchived the problem")
	}
	got, err := s.Restore(ctx, p.ID, taken.Suggested)
	if err != nil || got.Slug != "a-plus-b-2" || got.Archived {
		t.Fatalf("restore with a chosen slug: %v %+v", err, got)
	}
	// A chosen slug is checked.
	s.Archive(ctx, p.ID)
	var fe FieldErrors
	if _, err := s.Restore(ctx, p.ID, "Bad Slug"); !errors.As(err, &fe) || fe["slug"] == "" {
		t.Errorf("restore with an invalid slug: %v", err)
	}
	if _, err := s.Restore(ctx, p.ID, "a-plus-b"); !errors.As(err, &taken) {
		t.Errorf("restore with a taken chosen slug: %v", err)
	}
}

func TestArchivedExportNamesTheOriginalSlug(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	s.Archive(ctx, p.ID)
	got, _ := s.Get(ctx, p.ID)
	if got.PackSlug() != "a-plus-b" {
		t.Errorf("pack slug %q", got.PackSlug())
	}
	var buf bytes.Buffer
	if err := WritePack(&buf, got, func(sha string) ([]byte, error) { return s.Blobs.Read(sha, MaxPackFile+1) }); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name != "problem.yaml" {
			continue
		}
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		if !strings.Contains(string(body), "slug: a-plus-b\n") {
			t.Errorf("problem.yaml does not carry the original slug:\n%s", body)
		}
		return
	}
	t.Error("no problem.yaml in the pack")
}

func TestArchivedFormFitsTheSlugRule(t *testing.T) {
	long := strings.Repeat("a", 60) + "-bcd"
	for _, base := range []string{"x", long, strings.Repeat("a", 64), strings.Repeat("a-", 32)[:63]} {
		got := archivedForm(base, 123456)
		if len(got) > 64 || !slugPattern.MatchString(got) || !strings.HasSuffix(got, "-archived-123456") {
			t.Errorf("archivedForm(%q) = %q", base, got)
		}
	}
}

func TestOpenContestsListsUnendedPins(t *testing.T) {
	s := newBank(t)
	ctx := context.Background()
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	p := importExample(t, s, "a-plus-b")
	var rev int64
	if err := s.DB.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ?", p.ID).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	add := func(id int, slug string, startsIn, endsIn time.Duration, published int) {
		t.Helper()
		_, err := s.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0)`, id, slug, slug, now.Add(startsIn).UnixMilli(), now.Add(endsIn).UnixMilli(), published)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (?, ?, 'A', 0, ?)`, id, p.ID, rev); err != nil {
			t.Fatal(err)
		}
	}
	add(1, "future", time.Hour, 2*time.Hour, 1)
	add(2, "running", -time.Hour, time.Hour, 1)
	add(3, "over", -3*time.Hour, -time.Hour, 1)
	add(4, "draft", time.Hour, 2*time.Hour, 0)
	open, err := s.OpenContests(ctx, p.ID)
	if err != nil || len(open) != 2 || open[0].Slug != "running" || open[1].Slug != "future" {
		t.Fatalf("open contests %+v %v", open, err)
	}
}
