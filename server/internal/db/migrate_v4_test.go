package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// A migration run with foreign keys off that leaves a broken reference is
// rolled back whole, and the next connection enforces them again.
func TestKeysOffMigrationRefusesBrokenReferences(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	body := foreignKeysOff + "\nINSERT INTO sessions (id, user_id, created_at, expires_at) VALUES ('orphan', 999, 0, 0);"
	if _, err := apply(ctx, d, "9998_broken", body); err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("want a refusal naming a foreign key, got %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM sessions) + (SELECT count(*) FROM schema_migrations WHERE version = '9998_broken')").Scan(&n); err != nil || n != 0 {
		t.Errorf("rows left behind: %d (%v)", n, err)
	}
	if err := d.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&n); err != nil || n != 1 {
		t.Errorf("foreign_keys = %d (%v), want 1", n, err)
	}
}

// 0002 rebuilds users with foreign keys off. With them on, dropping users
// would cascade into sessions, null announcements.created_by and be refused
// by site_audit; this checks every child row survives and points at the
// same person, and that the stored role "admin" became "organiser".
func TestRolesMigrationKeepsEveryRow(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0001_v3"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, name, role, google_sub, created_at, theme) VALUES
			(1, 'owner@x.edu', 'Priya', 'owner', 'g1', 1, 'dark'),
			(2, 'ta@x.edu', 'Arjun', 'admin', 'g2', 2, ''),
			(3, 'asha@x.edu', 'Asha', 'student', NULL, 3, '')`,
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES ('s2', 2, 0, 9), ('s3', 3, 0, 9)`,
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'c', 'C', 0, 1, 0, 0)`,
		`INSERT INTO announcements (contest_id, body_md, created_by, created_at) VALUES (1, 'hi', 2, 0)`,
		`INSERT INTO site_audit (at, actor_id, actor_role, action) VALUES (0, 2, 'admin', 'test')`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 || applied[0] != "0002_v4_roles" {
		t.Fatalf("applied %v, want 0002_v4_roles first", applied)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := d.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count("SELECT count(*) FROM users WHERE (id, role) IN (VALUES (1, 'owner'), (2, 'organiser'), (3, 'student'))"); n != 3 {
		t.Errorf("users with the expected roles: %d, want 3", n)
	}
	if n := count("SELECT count(*) FROM users WHERE id = 1 AND google_sub = 'g1' AND theme = 'dark' AND name = 'Priya'"); n != 1 {
		t.Error("the owner's other columns did not survive")
	}
	if n := count("SELECT count(*) FROM sessions"); n != 2 {
		t.Errorf("sessions: %d, want 2", n)
	}
	if n := count("SELECT count(*) FROM announcements WHERE created_by = 2"); n != 1 {
		t.Error("the announcement lost its author")
	}
	if n := count("SELECT count(*) FROM site_audit WHERE actor_id = 2"); n != 1 {
		t.Error("the audit row lost its actor")
	}
	if n := count("SELECT count(*) FROM pragma_foreign_key_check"); n != 0 {
		t.Errorf("foreign key check found %d problems", n)
	}
	// Every connection still enforces foreign keys and the new CHECK.
	for range MaxConns + 1 {
		if n := count("PRAGMA foreign_keys"); n != 1 {
			t.Fatalf("foreign_keys = %d after the migration, want 1", n)
		}
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES ('x', 999, 0, 0)"); err == nil {
		t.Error("foreign key not enforced after the migration")
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO users (email, role, created_at) VALUES ('new@x.edu', 'admin', 0)"); err != nil {
		t.Errorf("the new admin role is refused: %v", err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO users (email, role, created_at) VALUES ('bad@x.edu', 'root', 0)"); err == nil {
		t.Error("role CHECK not enforced after the rebuild")
	}
}

// 0003 drops the allowed domains and adds the refused sign-ins, from a
// database in the 0002 state that has a domain on its list.
func TestSignInMigration(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0002_v4_roles"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO allowed_domains (domain, created_at) VALUES ('college.edu', 0)"); err != nil {
		t.Fatal(err)
	}
	if applied, err := Migrate(ctx, d); err != nil || len(applied) == 0 || applied[0] != "0003_v4_signin" {
		t.Fatalf("applied %v, %v", applied, err)
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name = 'allowed_domains'").Scan(&n); err != nil || n != 0 {
		t.Errorf("allowed_domains still there: %d %v", n, err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO refused_signins (email, first_at, last_at) VALUES ('X@Y.org', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO refused_signins (email, first_at, last_at) VALUES ('x@y.org', 1, 1)"); err == nil {
		t.Error("refused_signins.email should ignore case")
	}
}

// 0004 gives problems and revisions that exist an empty set of starters,
// which means the web app's own, and the column holds only JSON.
func TestStartersMigration(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0003_v4_signin"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash)
			VALUES (1, 'p', 'P', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h')`,
		`INSERT INTO problem_revisions (problem_id, rev, content_hash, title, created_at) VALUES (1, 1, 'h', 'P', 0)`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if applied, err := Migrate(ctx, d); err != nil || len(applied) == 0 || applied[0] != "0004_v4_starters" {
		t.Fatalf("applied %v, %v", applied, err)
	}
	var a, b string
	if err := d.QueryRowContext(ctx, "SELECT (SELECT starters FROM problems), (SELECT starters FROM problem_revisions)").Scan(&a, &b); err != nil || a != "{}" || b != "{}" {
		t.Errorf("starters after the migration: %q %q %v, want {} and {}", a, b, err)
	}
	if _, err := d.ExecContext(ctx, "UPDATE problems SET starters = 'nope'"); err == nil {
		t.Error("starters accepted text that is not JSON")
	}
}
