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
