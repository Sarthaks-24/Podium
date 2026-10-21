package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
)

// A student listed as idle who signs in before the rollover's write keeps
// their account: the write re-checks inside its transaction (1812046,
// C2-019).
func TestRolloverSkipsAnAccountSeenSincePreview(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	old := now.Add(-200 * 24 * time.Hour).UnixMilli()
	if _, err := d.Exec(`INSERT INTO users (id, email, name, role, created_at, last_login_at) VALUES
		(10, 'idle@college.edu', 'Idle', 'student', 0, ?), (11, 'back@college.edu', 'Back', 'student', 0, ?)`, old, old); err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: d, Now: func() time.Time { return now }}
	seenBefore := now.Add(-IdleStudentPeriod)
	list, err := s.IdleStudents(ctx, seenBefore)
	if err != nil || len(list) != 2 {
		t.Fatalf("idle list: %v %v", list, err)
	}
	// Between the list and the write, one of them signs in.
	if _, err := d.Exec("UPDATE users SET last_login_at = ? WHERE id = 11", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	rep, err := s.disableListed(ctx, list, seenBefore, "2026 odd", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Disabled) != 1 || rep.Disabled[0].ID != 10 {
		t.Errorf("disabled %+v, want only account 10", rep.Disabled)
	}
	var disabled int
	d.QueryRow("SELECT disabled FROM users WHERE id = 11").Scan(&disabled)
	if disabled != 0 {
		t.Error("the student who signed in after the list was disabled")
	}
}
