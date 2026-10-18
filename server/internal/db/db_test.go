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
	if len(applied) == 0 || applied[0] != "0001_identity" {
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

// TestWizardMigrationWidensJobKinds upgrades a database holding jobs and
// their results from 0006 to 0007 (D52): the jobs.kind CHECK is widened in
// place, existing rows and their results survive, every pooled connection
// sees the new constraint, and the file passes integrity and foreign key
// checks.
func TestWizardMigrationWidensJobKinds(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	names, _ := migrationNames()
	run := func(before bool) {
		t.Helper()
		for _, name := range names {
			if (path.Base(name) < "0007") != before {
				continue
			}
			body, _ := migrationFS.ReadFile(name)
			if _, err := apply(ctx, d, strings.TrimSuffix(path.Base(name), ".sql"), string(body)); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
	insertJob := func(kind string) error {
		_, err := d.ExecContext(ctx, "INSERT INTO jobs (kind, priority, payload, created_at) VALUES (?, 1, '{}', 1)", kind)
		return err
	}
	run(true)
	if err := insertJob("generate"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO job_results (job_id, seq, kind, role, verdict) VALUES (1, 1, 'test', 'program', 'AC')"); err != nil {
		t.Fatal(err)
	}
	if err := insertJob("generate_inputs"); err == nil {
		t.Fatal("0006 accepted generate_inputs")
	}
	run(false)
	// Connections opened before the upgrade must see the new constraint.
	for i := 0; i < 8; i++ {
		if err := insertJob("generate_inputs"); err != nil {
			t.Fatalf("insert %d after 0007: %v", i, err)
		}
	}
	if err := insertJob("bogus"); err == nil {
		t.Error("the widened CHECK accepts any kind")
	}
	var results int
	d.QueryRowContext(ctx, "SELECT count(*) FROM job_results WHERE job_id = 1").Scan(&results)
	if results != 1 {
		t.Errorf("job_results lost rows: %d", results)
	}
	var check string
	if err := d.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Errorf("integrity_check: %q %v", check, err)
	}
	rows, err := d.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Error("foreign_key_check found violations")
	}
	rows.Close()
}
