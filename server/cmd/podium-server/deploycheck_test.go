package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
)

// guardDB is a migrated database with an owner and one published contest
// from 14:00 to 16:00 UTC on 2026-11-10, a 15-minute contest-wide
// extension and one student's 10-minute extension. The server is not
// running: deploy-check reads the file directly.
func guardDB(t *testing.T) (path string, raw *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path = filepath.Join(t.TempDir(), "podium.db")
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	d.Close()
	// Fixtures without foreign keys, so a submission needs no problem row.
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	starts := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC).UnixMilli()
	ends := time.Date(2026, 11, 10, 16, 0, 0, 0, time.UTC).UnixMilli()
	for _, q := range []string{
		`INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@college.edu', 'owner', 0), (2, 's@college.edu', 'student', 0)`,
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, extended_minutes, created_at, updated_at)
			VALUES (1, 'weekly-3', 'Weekly 3', ` + itoa(starts) + `, ` + itoa(ends) + `, 1, 15, 0, 0)`,
		// Drafts and archived contests never block a deploy.
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, created_at, updated_at)
			VALUES (2, 'draft', 'Draft', ` + itoa(starts) + `, ` + itoa(ends) + `, 0, 0, 0)`,
		`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, archived, created_at, updated_at)
			VALUES (3, 'old', 'Old', ` + itoa(starts) + `, ` + itoa(ends) + `, 1, 1, 0, 0)`,
		`INSERT INTO contest_extensions (contest_id, user_id, minutes) VALUES (1, 2, 10)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return path, raw
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func runGuard(t *testing.T, path, at string, extra ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := deployCheckCmd(append([]string{"-db", path, "-at", at}, extra...), &out)
	return code, out.String()
}

func TestDeployCheckWindows(t *testing.T) {
	path, raw := guardDB(t)
	for _, c := range []struct {
		at   string
		code int
		want []string
	}{
		// More than 48 hours before: allowed, and says until when.
		{"2026-11-08T13:59:00Z", 0, []string{"allowed\n", `next contest "weekly-3"`, "allowed until 2026-11-08 14:00 UTC"}},
		// Inside the 48 hours.
		{"2026-11-08T14:00:00Z", 1, []string{"refused\n", `"weekly-3"`, "in 48 h", "48 hours before a contest", "--force"}},
		{"2026-11-10T13:30:00Z", 1, []string{"refused\n", "in 30 min"}},
		// Running: the close is 16:00 + 15 min + the largest personal 10 min.
		{"2026-11-10T14:00:00Z", 1, []string{"refused\n", "running until 2026-11-10 16:25 UTC"}},
		{"2026-11-10T16:24:00Z", 1, []string{"refused\n", "running until"}},
		// After every window closed, with nothing waiting for the judge.
		{"2026-11-10T16:25:00Z", 0, []string{"allowed\n", "no published contest"}},
	} {
		code, out := runGuard(t, path, c.at)
		if code != c.code {
			t.Errorf("at %s: exit %d, want %d\n%s", c.at, code, c.code, out)
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("at %s: output lacks %q:\n%s", c.at, w, out)
			}
		}
	}

	// The queue still drains after the end: refused until it is empty.
	// Practice submissions after the end do not count.
	if _, err := raw.Exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, practice, created_at)
		VALUES (1, 1, 2, 'submit', 'cpp17', 'x', 'x', 'running', 0, 0), (1, 1, 2, 'submit', 'cpp17', 'x', 'x', 'queued', 1, 0)`); err != nil {
		t.Fatal(err)
	}
	if code, out := runGuard(t, path, "2026-11-11T09:00:00Z"); code != 1 || !strings.Contains(out, "1 of its submissions are still waiting") {
		t.Errorf("draining queue: exit %d\n%s", code, out)
	}
	raw.Exec(`UPDATE submissions SET state = 'done'`)
	if code, out := runGuard(t, path, "2026-11-11T09:00:00Z"); code != 0 {
		t.Errorf("drained queue: exit %d\n%s", code, out)
	}
}

func TestDeployCheckForceWritesTheAuditLog(t *testing.T) {
	path, raw := guardDB(t)
	at := "2026-11-10T15:00:00Z"
	if code, out := runGuard(t, path, at, "-force", "short"); code != 2 || !strings.Contains(out, "at least 10 characters") {
		t.Errorf("short reason: exit %d\n%s", code, out)
	}
	code, out := runGuard(t, path, at, "-force", "Fix the broken checker on B", "-release", "v1.0.1")
	if code != 0 || !strings.HasPrefix(out, "allowed (forced)\n") || !strings.Contains(out, "audit log of 1 contest") {
		t.Fatalf("forced: exit %d\n%s", code, out)
	}
	var action, details, email string
	var contest int64
	if err := raw.QueryRow(`SELECT contest_id, action, details, actor_email FROM contest_audit`).Scan(&contest, &action, &details, &email); err != nil {
		t.Fatal(err)
	}
	if contest != 1 || action != auditDeployForced || email != "owner@college.edu" ||
		!strings.Contains(details, "Fix the broken checker on B") || !strings.Contains(details, "v1.0.1") {
		t.Errorf("audit row: contest %d %s %s %s", contest, action, email, details)
	}
	// An allowed deploy records nothing, forced or not.
	if code, _ := runGuard(t, path, "2026-11-20T00:00:00Z", "-force", "Nothing to override here"); code != 0 {
		t.Errorf("force when allowed: exit %d", code)
	}
	var n int
	raw.QueryRow(`SELECT count(*) FROM contest_audit`).Scan(&n)
	if n != 1 {
		t.Errorf("%d audit rows, want 1", n)
	}
}

func TestDeployCheckCannotRun(t *testing.T) {
	var out bytes.Buffer
	if code := deployCheckCmd([]string{"-db", filepath.Join(t.TempDir(), "nope.db")}, &out); code != 2 {
		t.Errorf("missing database: exit %d\n%s", code, out.String())
	}
}
