package db

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMigrationNames(t *testing.T) {
	names, err := migrationNames()
	if err != nil || len(names) == 0 {
		t.Fatalf("names %v, err %v", names, err)
	}
	pattern := regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)
	for i, n := range names {
		base := path.Base(n)
		if !pattern.MatchString(base) {
			t.Errorf("%s: migration files are named NNNN_snake_case.sql", base)
		}
		if want := fmt.Sprintf("%04d", i+1); !strings.HasPrefix(base, want+"_") {
			t.Errorf("%s: migrations must be numbered 0001, 0002, ... without gaps (want %s)", base, want)
		}
	}
}

func TestRefusesUnknownMigration(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES ('9999_future', 0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, d); err == nil || !strings.Contains(err.Error(), "9999_future") {
		t.Fatalf("want a refusal naming 9999_future, got %v", err)
	}
}

// A database from v2 meets the v3 binary: the refusal says what is true
// (walk bug B7), not "written by a newer podium-server", and changes nothing.
func TestOldDatabaseIsNamed(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "v2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.ExecContext(ctx, `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT;
		INSERT INTO schema_migrations VALUES ('0001_identity', 0), ('0002_problems', 0), ('0015_rating', 0)`); err != nil {
		t.Fatal(err)
	}
	_, err = Migrate(ctx, d)
	if err == nil || !strings.Contains(err.Error(), "predates v3 (it has migration 0001_identity)") ||
		!strings.Contains(err.Error(), "restore a v3 backup") || strings.Contains(err.Error(), "newer") {
		t.Fatalf("refusal for a v2 database: %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version = '0001_v3'").Scan(&n); err != nil || n != 0 {
		t.Errorf("the v3 schema ran on a v2 database: %d %v", n, err)
	}
}

func TestOpenAndMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "podium.db")
	d, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 || applied[0] != "0001_v3" {
		t.Fatalf("applied = %v", applied)
	}
	again, err := Migrate(ctx, d)
	if err != nil || len(again) != 0 {
		t.Fatalf("second migrate applied %v, err %v; want nothing", again, err)
	}

	var fk int
	if err := d.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d (%v), want 1", fk, err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO users (email, role, created_at) VALUES ('a@x.edu', 'root', 0)"); err == nil {
		t.Error("role CHECK constraint not enforced")
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES ('s', 999, 0, 0)"); err == nil {
		t.Error("foreign key not enforced")
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO users (email, created_at) VALUES ('Me@X.edu', 0), ('me@x.edu', 0)"); err == nil {
		t.Error("email uniqueness should ignore case")
	}
}

// The v3 schema holds exactly these tables (S1-07). A new table is a new
// numbered migration and a new row here.
func TestSchemaTables(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	rows, err := d.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"allowed_domains", "announcements", "blobs", "clarifications", "contest_audit", "contest_extensions",
		"contest_problems", "contest_starts", "contests", "drafts", "generations", "input_generations",
		"integrity_cases", "integrity_events", "integrity_runs", "integrity_signals", "job_results", "jobs",
		"problem_revisions", "problems", "revision_tests", "schema_migrations", "sessions", "settings", "site_audit",
		"submissions", "subtasks", "testcases", "users", "validations"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("tables:\n got %v\nwant %v", got, want)
	}
}

// contest_final is the end, plus the contest-wide extension, plus the
// longest personal extension (S1-08): the one SQL copy of Contest.Final.
func TestContestFinalView(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO users (id, email, created_at) VALUES (1, 'a@x.edu', 0), (2, 'b@x.edu', 0)",
		"INSERT INTO contests (id, slug, title, starts_at, ends_at, extended_minutes, created_at, updated_at) VALUES (1, 'c', 'C', 0, 600000, 5, 0, 0)",
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	final := func() int64 {
		t.Helper()
		var at int64
		if err := d.QueryRowContext(ctx, "SELECT final_at FROM contest_final WHERE contest_id = 1").Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	if got := final(); got != 600000+5*60000 {
		t.Errorf("without personal extensions: %d", got)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO contest_extensions (contest_id, user_id, minutes) VALUES (1, 1, 10), (1, 2, 25)"); err != nil {
		t.Fatal(err)
	}
	if got := final(); got != 600000+(5+25)*60000 {
		t.Errorf("with the longest personal extension of 25 min: %d", got)
	}
}
