package db

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// 0014 adds function problems (v2.0.2 stage B): mode and the locked parts
// on problems and problem_revisions, top_lines on submissions. A v2.0.1
// database keeps every row, every old row reads as a whole-program problem
// with no locked code, and a second run applies nothing.
func TestFunctionMigrationKeepsEveryRow(t *testing.T) {
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
		`INSERT INTO users (id, email, name, role, created_at) VALUES (1, 'o@x.edu', 'O', 'owner', 0), (2, 's@x.edu', 'S', 'student', 0)`,
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash, starters)
			VALUES (1, 'a', 'A', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h1', '{"cpp17":"int main(){}"}'), (2, 'b', 'B', 1000, 256, 'tokens', 'java21', 0, 0, 'h2', '{}')`,
		`INSERT INTO problem_revisions (id, problem_id, rev, content_hash, title, created_at) VALUES (1, 1, 1, 'h1', 'A', 0), (2, 2, 1, 'h2', 'B', 0)`,
		`INSERT INTO submissions (id, problem_id, user_id, kind, language, source, source_sha, created_at, revision_id)
			VALUES (1, 1, 2, 'submit', 'cpp17', 'int main(){}', 'x', 0, 1), (2, 2, 2, 'run', 'java21', 'class Main{}', 'y', 0, NULL)`,
		`INSERT INTO drafts (user_id, problem_id, language, source, updated_at) VALUES (2, 1, 'cpp17', 'int main(){}', 0)`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := d.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tables := []string{"problems", "problem_revisions", "submissions", "drafts", "users"}
	before := map[string]int{}
	for _, tb := range tables {
		before[tb] = count("SELECT count(*) FROM " + tb)
	}

	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(applied, "0014_v202_function") {
		t.Fatalf("applied %v, want 0014_v202_function among them", applied)
	}
	for _, tb := range tables {
		if n := count("SELECT count(*) FROM " + tb); n != before[tb] {
			t.Errorf("%s: %d rows, had %d", tb, n, before[tb])
		}
	}
	if n := count(`SELECT count(*) FROM problems WHERE mode = 'whole' AND locked_top = '{}' AND locked_bottom = '{}'`); n != 2 {
		t.Errorf("problems read as whole with no locked code: %d, want 2", n)
	}
	if n := count(`SELECT count(*) FROM problem_revisions WHERE mode = 'whole' AND locked_top = '{}' AND locked_bottom = '{}'`); n != 2 {
		t.Errorf("revisions read as whole with no locked code: %d, want 2", n)
	}
	if n := count(`SELECT count(*) FROM submissions WHERE top_lines = 0`); n != 2 {
		t.Errorf("submissions with top_lines 0: %d, want 2", n)
	}
	if n := count(`SELECT count(*) FROM problems WHERE starters = '{"cpp17":"int main(){}"}'`); n != 1 {
		t.Error("starters changed")
	}
	var ok string
	if err := d.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
		t.Errorf("integrity_check: %q (%v)", ok, err)
	}
	if n := count("SELECT count(*) FROM pragma_foreign_key_check"); n != 0 {
		t.Errorf("foreign_key_check: %d rows", n)
	}
	for _, bad := range []string{
		`UPDATE problems SET mode = 'half' WHERE id = 1`,
		`UPDATE problems SET locked_top = 'not json' WHERE id = 1`,
		`UPDATE problems SET locked_bottom = 'not json' WHERE id = 1`,
	} {
		if _, err := d.ExecContext(ctx, bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
	again, err := Migrate(ctx, d)
	if err != nil || len(again) != 0 {
		t.Errorf("second run applied %v (%v)", again, err)
	}
}

// On an empty database every migration applies and the new columns exist.
func TestFunctionMigrationOnAnEmptyDatabase(t *testing.T) {
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
		"SELECT mode, locked_top, locked_bottom FROM problems",
		"SELECT mode, locked_top, locked_bottom FROM problem_revisions",
		"SELECT top_lines FROM submissions",
	} {
		rows, err := d.QueryContext(ctx, q)
		if err != nil {
			t.Errorf("%s: %v", q, err)
			continue
		}
		rows.Close()
	}
}
