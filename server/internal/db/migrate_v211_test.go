package db

import (
	"context"
	"path/filepath"
	"testing"
)

// 0017 adds contests.locked_code. On a database shaped like v2.1.0 with a
// contest it changes no row beyond the new column, which reads "shown",
// refuses any value but shown, folded and hidden, and a second Migrate has
// nothing to apply.
func TestLockedCodeMigrationDefaultsToShown(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0016_v210_sessions"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO contests (slug, title, starts_at, ends_at, created_at, updated_at)
		VALUES ('week-1', 'Week 1', 1000, 2000, 1, 1)`); err != nil {
		t.Fatal(err)
	}

	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != "0017_v211_locked_code" {
		t.Fatalf("applied %v, want only 0017_v211_locked_code", applied)
	}
	var mode, title string
	if err := d.QueryRowContext(ctx, `SELECT locked_code, title FROM contests WHERE slug = 'week-1'`).Scan(&mode, &title); err != nil {
		t.Fatal(err)
	}
	if mode != "shown" || title != "Week 1" {
		t.Errorf("locked_code %q, title %q: want shown and the old title", mode, title)
	}
	for _, v := range []string{"folded", "hidden", "shown"} {
		if _, err := d.ExecContext(ctx, "UPDATE contests SET locked_code = ? WHERE slug = 'week-1'", v); err != nil {
			t.Errorf("%s refused: %v", v, err)
		}
	}
	if _, err := d.ExecContext(ctx, "UPDATE contests SET locked_code = 'x' WHERE slug = 'week-1'"); err == nil {
		t.Error("locked_code = x was accepted")
	}
	again, err := Migrate(ctx, d)
	if err != nil || len(again) != 0 {
		t.Errorf("second Migrate applied %v (%v)", again, err)
	}
	var ok string
	if err := d.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
		t.Errorf("integrity_check: %q (%v)", ok, err)
	}
}
