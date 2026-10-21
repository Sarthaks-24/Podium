package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/db"
)

func historyDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "podium.db")
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	return path, d
}

func TestRunsReport(t *testing.T) {
	path, d := historyDB(t)
	for _, q := range []string{
		`INSERT INTO users (id, email, role, created_at) VALUES (1, 'asha@college.edu', 'student', 0), (2, 'ravi@college.edu', 'student', 0), (3, 'ta@college.edu', 'admin', 0)`,
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash)
			VALUES (1, 'p', 'P', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h')`,
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'week-1', 'Week 1', 1792310400000, 1792317600000, 0, 0)`,
		`INSERT INTO jobs (id, kind, priority, payload, created_at) VALUES
			(1, 'run', 10, '{"inputs":[{"idx":0,"input_sha":"x"}]}', 0),
			(2, 'run', 10, '{"inputs":[{"idx":1,"input_sha":"x"},{"idx":2,"input_sha":"y"}]}', 0),
			(3, 'run', 10, '{"inputs":[{"idx":0,"input_sha":"x"}]}', 0),
			(4, 'run', 10, '{"inputs":[{"idx":0,"input_sha":"x"}]}', 0)`,
		`INSERT INTO submissions (job_id, problem_id, contest_id, user_id, kind, language, source, source_sha, created_at) VALUES
			(1, 1, 1, 1, 'run', 'cpp17', '', 's', 0), (2, 1, 1, 1, 'run', 'cpp17', '', 's', 0),
			(3, 1, 1, 2, 'run', 'cpp17', '', 's', 0), (4, 1, 1, 3, 'run', 'cpp17', '', 's', 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var out bytes.Buffer
	if code := runsReportCmd([]string{"-db", path}, &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	// Two students, three runs, two of them on a custom input; the
	// organiser's run is left out.
	if !strings.Contains(out.String(), "| week-1 | 2026-10-18 08:00 UTC | 2 | 3 | 2 | 67% | 2 |") {
		t.Errorf("report:\n%s", out.String())
	}
}

func TestSawNeedsItsFlags(t *testing.T) {
	path, _ := historyDB(t)
	var out bytes.Buffer
	if code := sawCmd([]string{"-db", path, "-contest", "x"}, &out); code != 2 {
		t.Errorf("missing flags: exit %d", code)
	}
	out.Reset()
	if code := sawCmd([]string{"-db", path, "-contest", "nope", "-email", "a@b.c", "-label", "A"}, &out); code != 2 || !strings.Contains(out.String(), "nope") {
		t.Errorf("unknown contest: exit %d %s", code, out.String())
	}
}

func TestMigrateACopy(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "copy.db")
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.MigrateTo(ctx, d, "0009_toolchains"); err != nil {
		t.Fatal(err)
	}
	d.Close()
	t.Setenv("PODIUM_TESTS_DIR", t.TempDir())
	var out bytes.Buffer
	if code := migrateCmd([]string{"-db", path}, &out); code != 0 || !strings.Contains(out.String(), "0011_site_audit") ||
		!strings.Contains(out.String(), "revisions: ok") || !strings.Contains(out.String(), "result: ok") {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	t.Setenv("PODIUM_DB_PATH", path)
	out.Reset()
	if code := migrateCmd([]string{"-db", path}, &out); code != 2 || !strings.Contains(out.String(), "live database") {
		t.Errorf("the live database: exit %d %s", code, out.String())
	}

	// A database another process has open (its -wal file exists) is
	// refused even without PODIUM_DB_PATH, as under sudo -u podium.
	t.Setenv("PODIUM_DB_PATH", "")
	held, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, err := held.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS probe (x)"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := migrateCmd([]string{"-db", path}, &out); code != 2 || !strings.Contains(out.String(), "-wal exists") {
		t.Errorf("a database in use: exit %d %s", code, out.String())
	}
}
