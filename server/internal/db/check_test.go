package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCheckOnAMigratedDatabase(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r, err := Check(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := migrationNames()
	if !r.OK() || len(r.Applied) != 0 || len(r.Pending) != len(names) {
		t.Errorf("empty database: %+v", r)
	}
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("INSERT INTO users (email, created_at) VALUES ('a@college.edu', 1)"); err != nil {
		t.Fatal(err)
	}
	r, err = Check(ctx, d)
	if err != nil || !r.OK() || len(r.Pending) != 0 || len(r.Applied) != len(names) || r.Counts["users"] != 1 || r.Counts["submissions"] != 0 {
		t.Errorf("migrated database: %+v %v", r, err)
	}

	// A migration from a newer server, and a row pointing nowhere.
	if _, err := d.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES ('9999_future', 0)"); err != nil {
		t.Fatal(err)
	}
	conn, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES ('x', 42, 1, 2)"); err != nil {
		t.Fatal(err)
	}
	r, err = Check(ctx, d)
	if err != nil || r.OK() || len(r.Unknown) != 1 || len(r.ForeignKeys) != 1 {
		t.Errorf("damaged database: %+v %v", r, err)
	}
}
