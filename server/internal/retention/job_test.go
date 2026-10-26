package retention_test

import (
	"bufio"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/retention"
)

// TestPeriodsMatchTheOperationsTable: docs/OPERATIONS.md "Retention"
// lists every period with its id; the table and periods.go must agree row
// for row, in order, so the privacy page, the docs and the jobs say one
// thing.
func TestPeriodsMatchTheOperationsTable(t *testing.T) {
	f, err := os.Open("../../../docs/OPERATIONS.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type row struct{ id, kept string }
	var doc []row
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "## Retention":
			in = true
		case in && strings.HasPrefix(line, "## "):
			in = false
		case in && strings.HasPrefix(line, "| `"):
			cells := strings.Split(strings.Trim(line, "|"), "|")
			doc = append(doc, row{strings.Trim(strings.TrimSpace(cells[0]), "`"), strings.TrimSpace(cells[2])})
		}
	}
	periods := retention.Periods()
	if len(doc) != len(periods) {
		t.Fatalf("OPERATIONS.md lists %d periods, periods.go %d: %v", len(doc), len(periods), doc)
	}
	for i, p := range periods {
		if doc[i].id != p.ID || doc[i].kept != p.Kept {
			t.Errorf("row %d: doc %v, code %s %q", i, doc[i], p.ID, p.Kept)
		}
	}
}

func TestPeriods(t *testing.T) {
	want := map[string]string{
		"sessions": "12 hours after sign-in", "integrity_events": "180 days after the contest",
		"run_payloads": "30 days", "backups": "14 days",
	}
	for _, p := range retention.Periods() {
		if want[p.ID] != p.Kept {
			t.Errorf("%s: %q, want %q", p.ID, p.Kept, want[p.ID])
		}
		delete(want, p.ID)
	}
	if len(want) != 0 {
		t.Errorf("missing rows %v", want)
	}
}

// fixture is a migrated database with the clock fixed at now.
type fixture struct {
	t   *testing.T
	ctx context.Context
	db  *sql.DB
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	// A fixed clock a little behind the database's own, so every cutoff is
	// one the triggers accept.
	f := &fixture{t: t, ctx: ctx, db: d, now: time.Now().Add(-time.Minute).Truncate(time.Second)}
	f.exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@college.edu', 'owner', 1), (2, 'asha@college.edu', 'student', 1)`)
	return f
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("%s: %v", strings.Join(strings.Fields(q)[:3], " "), err)
	}
}

func (f *fixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, q, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// ago is now minus d, in Unix ms.
func (f *fixture) ago(d time.Duration) int64 { return f.now.Add(-d).UnixMilli() }

func (f *fixture) job() *retention.Job {
	return &retention.Job{DB: f.db, Now: func() time.Time { return f.now }}
}

const day = 24 * time.Hour

func TestRunPayloads(t *testing.T) {
	f := newFixture(t)
	for i, c := range []struct {
		age   time.Duration
		state string
	}{{31 * day, "done"}, {29 * day, "done"}, {40 * day, "queued"}, {45 * day, "failed"}} {
		f.exec(`INSERT INTO jobs (id, kind, priority, state, payload, created_at) VALUES (?, 'run', 10, ?, '{"inputs":[{"idx":0,"input_sha":"abc"}]}', ?)`,
			i+1, c.state, f.ago(c.age))
		f.exec(`INSERT INTO job_results (job_id, seq, kind, idx, role, verdict, output) VALUES (?, 1, 'test', 0, 'program', 'AC', 'output text')`, i+1)
	}
	// A submit job of the same age is never touched by this rule.
	f.exec(`INSERT INTO jobs (id, kind, priority, state, payload, created_at) VALUES (9, 'submit', 20, 'done', '{"tests":[]}', ?)`, f.ago(60*day))
	f.exec(`INSERT INTO job_results (job_id, seq, kind, idx, role, verdict, output_sha) VALUES (9, 1, 'test', 1, 'program', 'AC', 'x')`)
	res, err := f.job().Run(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.RunsEmptied != 2 {
		t.Errorf("emptied %d, want 2", res.RunsEmptied)
	}
	for id, emptied := range map[int]bool{1: true, 2: false, 3: false, 4: true, 9: false} {
		payload := f.count("SELECT payload = '{}' FROM jobs WHERE id = ?", id) == 1
		output := f.count("SELECT output = '' FROM job_results WHERE job_id = ?", id) == 1
		if id == 9 {
			output = false
		}
		if payload != emptied || output != emptied {
			t.Errorf("job %d: payload emptied %v, output emptied %v, want %v", id, payload, output, emptied)
		}
	}
	// The verdict rows stay.
	if n := f.count("SELECT count(*) FROM job_results WHERE verdict = 'AC'"); n != 5 {
		t.Errorf("%d result rows", n)
	}
}

func TestBackupDefaultMatches(t *testing.T) {
	b, err := os.ReadFile("../../../deploy/backup/podium-backup")
	if err != nil {
		t.Fatal(err)
	}
	want := "keep=${keep:-" + strconv.Itoa(int(retention.Backups/retention.Day)) + "}"
	if !strings.Contains(string(b), want) {
		t.Errorf("podium-backup does not default to %s", want)
	}
}
