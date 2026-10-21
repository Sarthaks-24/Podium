package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// v120Schema is the newest migration of release v1.2.0.
const v120Schema = "0011_site_audit"

// openV120 builds a v1.2.0 database holding the v1.1.0 fixtures migrated
// by v1.2.0, the state a real upgrade to v1.3.0 starts from.
func openV120(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	d, ctx := openV110(t)
	v110Fixtures(t, ctx, d)
	if _, err := MigrateTo(ctx, d, v120Schema); err != nil {
		t.Fatal(err)
	}
	return d, ctx
}

func TestMigrateFromV120(t *testing.T) {
	d, ctx := openV120(t)
	var before []string
	rows, err := d.QueryContext(ctx, "SELECT contest_id || ':' || label || ':' || pinned_hash || ':' || revision_id FROM contest_problems ORDER BY contest_id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		before = append(before, s)
	}
	rows.Close()
	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(applied, ",") != "0012_contact,0013_students,0014_results_notice" {
		t.Fatalf("applied %v", applied)
	}
	rep, err := Check(ctx, d)
	if err != nil || !rep.OK() || len(rep.Pending) != 0 {
		t.Fatalf("check after migration: %+v, %v", rep, err)
	}
	// The column is gone, and each pin's revision holds the hash it had.
	if _, err := d.ExecContext(ctx, "SELECT pinned_hash FROM contest_problems"); err == nil {
		t.Fatal("pinned_hash still exists")
	}
	var after []string
	rows, err = d.QueryContext(ctx, `SELECT cp.contest_id || ':' || cp.label || ':' || r.content_hash || ':' || cp.revision_id
		FROM contest_problems cp JOIN problem_revisions r ON r.id = cp.revision_id ORDER BY cp.contest_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		after = append(after, s)
	}
	rows.Close()
	if strings.Join(before, ",") != strings.Join(after, ",") || len(after) != 2 {
		t.Fatalf("pins before %v, after %v", before, after)
	}
	// The new tables take rows; a topic outside the list is refused.
	if _, err := d.ExecContext(ctx, `INSERT INTO contact_messages (user_id, topic, message, created_at) VALUES (2, 'contest', 'hi', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO contact_messages (user_id, topic, message, created_at) VALUES (2, 'spam', 'hi', 1)`); err == nil {
		t.Fatal("unknown topic accepted")
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO contest_results (contest_id, exported_at, standings) VALUES (1, 1, '[]')`); err != nil {
		t.Fatal(err)
	}
}

// A pin with no revision (impossible after 0010's backfill) stops the
// contract step instead of losing its hash.
func TestMigrate0012RefusesUnmappedPin(t *testing.T) {
	d, ctx := openV120(t)
	if _, err := d.ExecContext(ctx, "UPDATE contest_problems SET revision_id = NULL WHERE contest_id = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, d); err == nil || !strings.Contains(err.Error(), "pins without a revision") {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	d.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version = '0012_contact'").Scan(&n)
	if n != 0 {
		t.Fatal("0012 recorded as applied")
	}
}
