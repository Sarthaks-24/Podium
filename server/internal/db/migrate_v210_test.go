package db

import (
	"context"
	"path/filepath"
	"testing"
)

// 0016 adds remember and last_seen_at to sessions. On a database shaped like
// v2.0.2 with sessions of two people it changes no row, makes every existing
// session ordinary (remember = 0, no last_seen_at, expires_at untouched),
// refuses a remember value other than 0 or 1, and a second Migrate has
// nothing to apply.
func TestSessionsMigrationLeavesEverySessionOrdinary(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := MigrateTo(ctx, d, "0015_v202_prompts"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@x.edu', 'owner', 1), (2, 'asha@x.edu', 'student', 2)`,
		`INSERT INTO sessions (id, user_id, created_at, expires_at, ip, user_agent) VALUES
			('aa', 1, 1000, 43201000, '1.2.3.4', 'Firefox'), ('bb', 2, 2000, 43202000, '', ''), ('cc', 2, 3000, 43203000, '', '')`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}

	applied, err := MigrateTo(ctx, d, "0016_v210_sessions")
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0] != "0016_v210_sessions" {
		t.Fatalf("applied %v, want only 0016_v210_sessions", applied)
	}
	var n, ordinary, unseen int
	var expiry int64
	if err := d.QueryRowContext(ctx, `SELECT count(*), sum(remember = 0), sum(last_seen_at IS NULL), sum(expires_at) FROM sessions`).
		Scan(&n, &ordinary, &unseen, &expiry); err != nil {
		t.Fatal(err)
	}
	if n != 3 || ordinary != 3 || unseen != 3 || expiry != 43201000+43202000+43203000 {
		t.Errorf("sessions %d, ordinary %d, never seen %d, expiry sum %d: want 3, 3, 3 and the old expiries", n, ordinary, unseen, expiry)
	}
	if _, err := d.ExecContext(ctx, "UPDATE sessions SET remember = 2 WHERE id = 'aa'"); err == nil {
		t.Error("remember = 2 was accepted")
	}
	if _, err := d.ExecContext(ctx, "UPDATE sessions SET remember = 1, last_seen_at = 5 WHERE id = 'aa'"); err != nil {
		t.Errorf("remember = 1 refused: %v", err)
	}
	var ok string
	if err := d.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
		t.Errorf("integrity_check: %q (%v)", ok, err)
	}
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&n); err != nil || n != 0 {
		t.Errorf("foreign_key_check: %d rows (%v)", n, err)
	}
	again, err := MigrateTo(ctx, d, "0016_v210_sessions")
	if err != nil || len(again) != 0 {
		t.Errorf("second Migrate applied %v (%v)", again, err)
	}
}
