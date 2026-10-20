package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// v110Schema is the newest migration of release v1.1.0: the migration
// tests build that schema, add rows as v1.1.0 wrote them, then migrate.
const v110Schema = "0009_toolchains"

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// v110Fixtures writes a small v1.1.0 database: two problems, two contests,
// one pin that matches its problem's working copy and one pinned before the
// problem changed, with a judged submission whose job lists the old tests.
func v110Fixtures(t *testing.T, ctx context.Context, d *sql.DB) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", strings.Fields(q)[0:3], err)
		}
	}
	exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@college.edu', 'owner', 1), (2, 'asha@college.edu', 'student', 1)`)
	for _, b := range []string{"in1", "out1", "in2", "out2", "in3", "out3", "oldin2", "oldout2"} {
		exec(`INSERT INTO blobs (sha, size, created_at) VALUES (?, ?, 1)`, sha(b), len(b))
	}
	exec(`INSERT INTO problems (id, slug, title, statement_md, time_limit_ms, memory_limit_mb, checker, reference_language,
		reference_source, brute_language, brute_source, samples, created_at, updated_at, content_hash, validated_hash)
		VALUES (1, 'a-plus-b', 'A plus B', 'Add two numbers.', 1000, 256, 'tokens', 'cpp17', 'int main(){}', 'python3', 'print(1)',
		'[1]', 100, 200, 'hash-a-now', 'hash-a-now'),
		(2, 'max', 'Maximum', 'Find the maximum.', 2000, 128, 'exact', 'python3', 'print(2)', '', '', '[1]', 100, 900, 'hash-b-now', 'hash-b-now')`)
	exec(`INSERT INTO subtasks (problem_id, idx, points, title, tests) VALUES (1, 1, 40, 'Small', '2'), (1, 2, 60, 'Large', '3'),
		(2, 1, 100, '', '2')`)
	exec(`INSERT INTO testcases (problem_id, idx, input_sha, output_sha, input_size, output_size) VALUES
		(1, 1, ?, ?, 3, 4), (1, 2, ?, ?, 3, 4), (1, 3, ?, ?, 3, 4), (2, 1, ?, ?, 3, 4), (2, 2, ?, ?, 3, 4)`,
		sha("in1"), sha("out1"), sha("in2"), sha("out2"), sha("in3"), sha("out3"), sha("in1"), sha("out1"), sha("in2"), sha("out2"))
	exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES
		(1, 'week-1', 'Week 1', 1000, 5000, 50, 60), (2, 'week-2', 'Week 2', 6000, 9000, 70, 80)`)
	exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, pinned_hash, points) VALUES
		(1, 1, 'A', 0, 'hash-a-now', '[40,60]'), (2, 2, 'A', 0, 'hash-b-old', '[100]')`)
	exec(`INSERT INTO contest_audit (contest_id, actor_id, actor_email, actor_role, action, details, created_at)
		VALUES (1, 1, 'owner@college.edu', 'owner', 'problems', '{}', 300)`)
	payload, _ := json.Marshal(map[string]any{
		"submission_id": 1, "program": map[string]any{"language": "cpp17", "source_sha": sha("src")},
		"time_limit_ms": 1500, "memory_limit_mb": 64, "checker": map[string]any{"type": "exact", "epsilon": 1e-6},
		"tests": []map[string]any{
			{"idx": 1, "sample": true, "subtask": 0, "input_sha": sha("in1"), "output_sha": sha("out1")},
			{"idx": 2, "sample": false, "subtask": 1, "input_sha": sha("oldin2"), "output_sha": sha("oldout2")},
		},
		"subtasks": []map[string]any{{"idx": 1, "points": 100}}, "stop_on_sample_fail": true,
	})
	exec(`INSERT INTO jobs (id, kind, priority, state, payload, created_at) VALUES (1, 'submit', 20, 'done', ?, 7000)`, string(payload))
	exec(`INSERT INTO submissions (id, job_id, problem_id, contest_id, user_id, kind, language, source, source_sha, state, created_at)
		VALUES (1, 1, 2, 2, 2, 'submit', 'cpp17', 'x', ?, 'done', 7000)`, sha("src"))
}

func openV110(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "podium.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	applied, err := MigrateTo(ctx, d, v110Schema)
	if err != nil {
		t.Fatal(err)
	}
	if applied[len(applied)-1] != v110Schema {
		t.Fatalf("built up to %v, want %s", applied, v110Schema)
	}
	return d, ctx
}

func TestMigrateFromV110(t *testing.T) {
	d, ctx := openV110(t)
	v110Fixtures(t, ctx, d)
	applied, err := Migrate(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(applied, ",") != "0010_revisions,0011_site_audit,0012_contact,0013_students" {
		t.Fatalf("applied %v", applied)
	}
	rep, err := Check(ctx, d)
	if err != nil || !rep.OK() || len(rep.Pending) != 0 {
		t.Fatalf("check after migration: %+v, %v", rep, err)
	}

	type rev struct {
		id, rev                  int64
		hash, title, statement   string
		stmtHash, refSHA, bruteL string
		recorded, reconstructed  bool
		limit                    sql.NullInt64
		checker                  sql.NullString
		samples, subtasks        string
	}
	load := func(problem int64) []rev {
		rows, err := d.QueryContext(ctx, `SELECT id, rev, content_hash, title, statement_md, statement_hash, reference_sha, brute_language,
			statement_recorded, reconstructed, time_limit_ms, checker, samples, subtasks FROM problem_revisions WHERE problem_id = ? ORDER BY rev`, problem)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []rev
		for rows.Next() {
			var r rev
			if err := rows.Scan(&r.id, &r.rev, &r.hash, &r.title, &r.statement, &r.stmtHash, &r.refSHA, &r.bruteL, &r.recorded,
				&r.reconstructed, &r.limit, &r.checker, &r.samples, &r.subtasks); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}

	a := load(1)
	if len(a) != 1 || a[0].rev != 1 || a[0].hash != "hash-a-now" || a[0].reconstructed || !a[0].recorded {
		t.Fatalf("problem 1 revisions: %+v", a)
	}
	if a[0].statement != "Add two numbers." || a[0].stmtHash != sha("Add two numbers.") || a[0].refSHA != sha("int main(){}") || a[0].bruteL != "python3" {
		t.Fatalf("problem 1 revision 1 fields: %+v", a[0])
	}
	var subs []struct {
		Idx, Points  int
		Title, Tests string
	}
	if err := json.Unmarshal([]byte(a[0].subtasks), &subs); err != nil || len(subs) != 2 || subs[1].Tests != "3" || subs[0].Title != "Small" {
		t.Fatalf("subtasks %s: %v", a[0].subtasks, err)
	}

	b := load(2)
	if len(b) != 2 {
		t.Fatalf("problem 2 revisions: %+v", b)
	}
	old, now := b[0], b[1]
	if old.rev != 1 || !old.reconstructed || old.recorded || old.hash != "hash-b-old" || old.statement != "" {
		t.Fatalf("reconstructed revision: %+v", old)
	}
	if !old.limit.Valid || old.limit.Int64 != 1500 || old.checker.String != "exact" || old.samples != "[1]" {
		t.Fatalf("reconstructed limits from the job: %+v", old)
	}
	if !strings.Contains(old.subtasks, `"tests":"2"`) {
		t.Fatalf("reconstructed subtasks %s", old.subtasks)
	}
	if now.rev != 2 || now.reconstructed || now.hash != "hash-b-now" {
		t.Fatalf("working copy revision: %+v", now)
	}

	// The reconstructed revision's tests come from the job; the working
	// copy's from testcases, samples marked.
	tests := func(revID int64) map[int]string {
		rows, err := d.QueryContext(ctx, "SELECT idx, kind, input_sha FROM revision_tests WHERE revision_id = ?", revID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[int]string{}
		for rows.Next() {
			var idx int
			var kind, in string
			rows.Scan(&idx, &kind, &in)
			out[idx] = kind + ":" + in
		}
		return out
	}
	if got := tests(old.id); len(got) != 2 || got[2] != "hidden:"+sha("oldin2") || got[1] != "sample:"+sha("in1") {
		t.Fatalf("reconstructed tests %v", got)
	}
	if got := tests(a[0].id); len(got) != 3 || got[1] != "sample:"+sha("in1") || got[3] != "hidden:"+sha("in3") {
		t.Fatalf("working copy tests %v", got)
	}

	// Pins map to revisions; the history starts at the last problem change
	// or the contest's creation.
	var pin1, pin2 int64
	d.QueryRowContext(ctx, "SELECT revision_id FROM contest_problems WHERE contest_id = 1").Scan(&pin1)
	d.QueryRowContext(ctx, "SELECT revision_id FROM contest_problems WHERE contest_id = 2").Scan(&pin2)
	if pin1 != a[0].id || pin2 != old.id {
		t.Fatalf("pins %d %d, want %d %d", pin1, pin2, a[0].id, old.id)
	}
	var from1, from2 int64
	var actor sql.NullInt64
	d.QueryRowContext(ctx, "SELECT from_at, actor_id FROM contest_pin_history WHERE contest_id = 1").Scan(&from1, &actor)
	d.QueryRowContext(ctx, "SELECT from_at FROM contest_pin_history WHERE contest_id = 2").Scan(&from2)
	if from1 != 300 || from2 != 70 || actor.Valid {
		t.Fatalf("pin history from %d %d actor %v", from1, from2, actor)
	}
	// pinned_hash went in 0012 (the contract step); the pin's revision
	// carries the hash it held.
	var hash string
	d.QueryRowContext(ctx, `SELECT r.content_hash FROM contest_problems cp JOIN problem_revisions r ON r.id = cp.revision_id
		WHERE cp.contest_id = 2`).Scan(&hash)
	if hash != "hash-b-old" {
		t.Fatalf("pinned revision hash %q", hash)
	}
}

func TestAppendOnlyTablesAndCutoff(t *testing.T) {
	d, ctx := openV110(t)
	v110Fixtures(t, ctx, d)
	if _, err := Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO site_audit (at, action, details) VALUES (10, 'role_granted', '{}'), (99999999, 'role_granted', '{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO revision_views (user_id, contest_id, revision_id, first_seen_at) VALUES (2, 1, 1, 10)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO slug_history (entity, old_slug, entity_id, changed_at) VALUES ('contest', 'wk1', 1, 10)`); err != nil {
		t.Fatal(err)
	}
	refused := []string{
		"UPDATE problem_revisions SET title = 'x'",
		"DELETE FROM problem_revisions",
		"UPDATE revision_tests SET kind = 'open'",
		"DELETE FROM revision_tests",
		"UPDATE contest_pin_history SET label = 'B'",
		"DELETE FROM contest_pin_history",
		"UPDATE revision_views SET first_seen_at = 0",
		"DELETE FROM revision_views",
		"UPDATE site_audit SET action = 'x'",
		"DELETE FROM site_audit",
		"UPDATE slug_history SET old_slug = 'x'",
		"DELETE FROM slug_history",
		"UPDATE contest_audit SET action = 'x'",
		"DELETE FROM contest_audit",
	}
	for _, q := range refused {
		if _, err := d.ExecContext(ctx, q); err == nil || !strings.Contains(err.Error(), "append only") {
			t.Errorf("%s: want append only, got %v", q, err)
		}
	}
	// A cutoff of now, which would open the tables, is refused.
	for _, q := range []string{
		"INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('site_audit', CAST(unixepoch() AS INTEGER) * 1000)",
		"INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('problem_revisions', (CAST(unixepoch() AS INTEGER) - 30 * 86400) * 1000)",
	} {
		if _, err := d.ExecContext(ctx, q); err == nil || !strings.Contains(err.Error(), "may not be that recent") {
			t.Errorf("%s: %v", q, err)
		}
	}
	// The retention cutoff lets the old rows go and keeps the new ones.
	if _, err := d.ExecContext(ctx, `INSERT INTO retention_cutoffs (tbl, before_ms) VALUES ('site_audit', 1000), ('revision_views', 1000), ('contest_audit', 1000)`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DELETE FROM site_audit WHERE at < 1000", "DELETE FROM revision_views", "DELETE FROM contest_audit"} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Errorf("%s past the cutoff: %v", q, err)
		}
	}
	if _, err := d.ExecContext(ctx, "DELETE FROM site_audit"); err == nil {
		t.Error("a site audit row newer than the cutoff was deleted")
	}
	var n int
	d.QueryRowContext(ctx, "SELECT count(*) FROM site_audit").Scan(&n)
	if n != 1 {
		t.Fatalf("%d site audit rows left, want 1", n)
	}
}
