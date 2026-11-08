package db

import (
	"context"
	"path/filepath"
	"testing"
)

// 0013 adds the tombstone columns to problems. On a database shaped like
// v2.0.1 (a used problem with a pinned revision, a submission and an
// archived one) it changes no row count, leaves every problem undeleted,
// passes the integrity and foreign key checks, and a second Migrate has
// nothing to apply.
func TestDeleteMigrationKeepsEveryRow(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0012_v201_theme"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@x.edu', 'owner', 1), (2, 'asha@x.edu', 'student', 2)`,
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash, archived, archived_slug)
			VALUES (1, 'two-sum', 'Two sum', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h1', 0, ''),
			       (2, 'old-archived-2', 'Old', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h2', 1, 'old')`,
		`INSERT INTO problem_revisions (id, problem_id, rev, content_hash, title, created_at) VALUES (1, 1, 1, 'h1', 'Two sum', 0)`,
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'week-3', 'Week 3', 0, 1, 0, 0)`,
		`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, 1, 'A', 1, 1)`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	counts := func() (n [4]int) {
		t.Helper()
		if err := d.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM problems), (SELECT count(*) FROM problem_revisions),
			(SELECT count(*) FROM contest_problems), (SELECT count(*) FROM users)`).Scan(&n[0], &n[1], &n[2], &n[3]); err != nil {
			t.Fatal(err)
		}
		return
	}
	before := counts()

	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 || applied[0] != "0013_v202_delete" {
		t.Fatalf("applied %v, want 0013_v202_delete first", applied)
	}
	if after := counts(); after != before {
		t.Errorf("row counts %v became %v", before, after)
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM problems WHERE deleted = 0 AND deleted_at IS NULL AND deleted_by IS NULL").Scan(&n); err != nil || n != 2 {
		t.Errorf("undeleted problems: %d (%v), want 2", n, err)
	}
	if _, err := d.ExecContext(ctx, "UPDATE problems SET deleted = 2 WHERE id = 1"); err == nil {
		t.Error("deleted = 2 was accepted")
	}
	var ok string
	if err := d.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
		t.Errorf("integrity_check: %q (%v)", ok, err)
	}
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&n); err != nil || n != 0 {
		t.Errorf("foreign_key_check: %d rows (%v)", n, err)
	}
	again, err := Migrate(ctx, d)
	if err != nil || len(again) != 0 {
		t.Errorf("second Migrate applied %v (%v)", again, err)
	}
}

// 0015 adds the prompt versions table. A row can be inserted; an update or
// a delete aborts; an unknown kind is refused; a second Migrate has nothing
// to apply.
func TestPromptVersionsMigrationIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@x.edu', 'owner', 1)`); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"wizard_rules", "pack_rules", "preferences"} {
		if _, err := d.ExecContext(ctx, `INSERT INTO ai_prompt_versions (kind, body, created_by, created_at) VALUES (?, 'x', 1, 5)`, kind); err != nil {
			t.Errorf("insert %s: %v", kind, err)
		}
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO ai_prompt_versions (kind, body, created_at) VALUES ('other', 'x', 5)`); err == nil {
		t.Error("an unknown kind was accepted")
	}
	if _, err := d.ExecContext(ctx, `UPDATE ai_prompt_versions SET body = 'y'`); err == nil {
		t.Error("an update was accepted")
	}
	if _, err := d.ExecContext(ctx, `DELETE FROM ai_prompt_versions`); err == nil {
		t.Error("a delete was accepted")
	}
	again, err := Migrate(ctx, d)
	if err != nil || len(again) != 0 {
		t.Errorf("second Migrate applied %v (%v)", again, err)
	}
}

// Deleting a user who saved a prompt version works (the foreign key sets
// created_by to NULL) and the row keeps its text; editing the text is still
// refused.
func TestDeletingAUserKeepsTheirPromptVersions(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, role, created_at) VALUES (9, 'o@x.edu', 'owner', 1)`,
		`INSERT INTO ai_prompt_versions (kind, body, created_by, created_at) VALUES ('wizard_rules', 'keep me', 9, 5)`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.ExecContext(ctx, `DELETE FROM users WHERE id = 9`); err != nil {
		t.Fatalf("deleting the user: %v", err)
	}
	var body string
	var by *int64
	if err := d.QueryRowContext(ctx, `SELECT body, created_by FROM ai_prompt_versions`).Scan(&body, &by); err != nil || body != "keep me" || by != nil {
		t.Errorf("row: %q %v %v", body, by, err)
	}
	if _, err := d.ExecContext(ctx, `UPDATE ai_prompt_versions SET body = 'x'`); err == nil {
		t.Error("editing the body was accepted")
	}
}
