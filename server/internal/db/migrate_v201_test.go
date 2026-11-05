package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// 0010 moves the slug of every already-archived problem and contest to
// <base>-archived-<id> and keeps the original in archived_slug. A row whose
// new slug is taken is left as it is, a long slug is cut to 64 characters
// without a trailing hyphen, and live rows are untouched.
func TestArchivedSlugMigration(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0009_v401_contest_invites"); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 60) + "-bcd"
	problem := func(id int, slug string, archived int) {
		t.Helper()
		_, err := d.ExecContext(ctx, `INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker,
			reference_language, archived, created_at, updated_at, content_hash)
			VALUES (?, ?, 't', 1000, 256, 'tokens', 'cpp17', ?, 0, 0, 'h')`, id, slug, archived)
		if err != nil {
			t.Fatal(err)
		}
	}
	problem(1, "two-sum", 1)
	problem(2, "two-sum-archived-2", 0) // takes the form problem 3 would get
	problem(3, "kept", 1)               // collides: its form is not free once 4 exists
	problem(4, "kept-archived-3", 0)
	problem(5, long, 1)
	problem(6, "live", 0)
	contest := func(id int, slug string, archived int) {
		t.Helper()
		_, err := d.ExecContext(ctx, `INSERT INTO contests (id, slug, title, starts_at, ends_at, archived, created_at, updated_at)
			VALUES (?, ?, 'c', 0, 1, ?, 0, 0)`, id, slug, archived)
		if err != nil {
			t.Fatal(err)
		}
	}
	contest(1, "week-3", 1)
	contest(2, "week-4", 0)

	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatalf("migrate with a colliding row: %v", err)
	}
	if len(applied) == 0 || applied[0] != "0010_v201" {
		t.Fatalf("applied %v, want 0010_v201 first", applied)
	}

	slugs := func(table string, id int) (slug, original string) {
		t.Helper()
		if err := d.QueryRowContext(ctx, "SELECT slug, archived_slug FROM "+table+" WHERE id = ?", id).Scan(&slug, &original); err != nil {
			t.Fatal(err)
		}
		return
	}
	check := func(table string, id int, wantSlug, wantOriginal string) {
		t.Helper()
		if s, o := slugs(table, id); s != wantSlug || o != wantOriginal {
			t.Errorf("%s %d: slug %q archived_slug %q, want %q and %q", table, id, s, o, wantSlug, wantOriginal)
		}
	}
	check("problems", 1, "two-sum-archived-1", "two-sum")
	check("problems", 3, "kept", "") // collision: left as it is
	check("problems", 6, "live", "")
	check("contests", 1, "week-3-archived-1", "week-3")
	check("contests", 2, "week-4", "")

	s, o := slugs("problems", 5)
	if o != long || len(s) > 64 || strings.Contains(s, "--") || !strings.HasSuffix(s, "-archived-5") {
		t.Errorf("long slug became %q (original %q)", s, o)
	}
}
