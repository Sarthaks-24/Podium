package retention_test

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
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

func TestPeriodsAndFloors(t *testing.T) {
	if err := retention.CheckFloors(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"sessions": "12 hours after sign-in", "integrity_events": "180 days after the contest",
		"case_evidence": "2 years after the decision", "run_payloads": "30 days", "contact_messages": "180 days",
		"standing_entries": "2 years after the decision", "private_notes": "30 days", "notifications": "180 days",
		"record_views": "2 years", "rating_history": "4 years", "contest_summaries": "4 years after the contest",
		"draft_revisions": "90 days", "submissions": "4 years after the contest", "audit": "4 years", "backups": "14 days",
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
	return &retention.Job{DB: f.db, Now: func() time.Time { return f.now },
		ExportStandings: func(_ context.Context, id int64) ([]byte, error) {
			return []byte(`[{"rank":1,"user_id":2,"total":100}]`), nil
		}}
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

func TestContactMessages(t *testing.T) {
	f := newFixture(t)
	f.exec(`INSERT INTO contact_messages (user_id, topic, message, created_at) VALUES (2, 'other', 'old', ?), (2, 'other', 'new', ?)`,
		f.ago(181*day), f.ago(179*day))
	res, err := f.job().Run(f.ctx)
	if err != nil || res.ContactDeleted != 1 || f.count("SELECT count(*) FROM contact_messages WHERE message = 'new'") != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

// problemWithRevisions adds problem 1 with revisions 1 to n made at the
// given ages, each with one test.
func (f *fixture) problemWithRevisions(ages ...time.Duration) {
	f.exec(`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash)
		VALUES (1, 'p', 'P', 1000, 256, 'tokens', 'cpp17', 1, 1, 'h')`)
	for i, a := range ages {
		f.exec(`INSERT INTO problem_revisions (id, problem_id, rev, content_hash, title, created_at) VALUES (?, 1, ?, ?, 'P', ?)`,
			i+1, i+1, "h"+string(rune('0'+i)), f.ago(a))
		f.exec(`INSERT INTO revision_tests (revision_id, idx, kind, input_sha) VALUES (?, 1, 'hidden', 'in')`, i+1)
	}
}

func (f *fixture) contest(id int64, endedAgo time.Duration) {
	end := f.ago(endedAgo)
	f.exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (?, ?, 'C', ?, ?, 1, 1)`,
		id, "c"+string(rune('0'+id)), end-7200000, end)
}

func TestDraftRevisions(t *testing.T) {
	f := newFixture(t)
	// 1 unused and old: goes. 2 pinned, 3 judged against, 4 served: stay.
	// 5 unused but 80 days old: stays. 6 is the newest: always stays.
	f.problemWithRevisions(100*day, 100*day, 100*day, 100*day, 80*day, 100*day)
	f.contest(1, 10*day)
	f.exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, 1, 'A', 0, 2)`)
	f.exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, created_at, revision_id)
		VALUES (1, 1, 2, 'submit', 'cpp17', 'x', 's', ?, 3)`, f.ago(day))
	f.exec(`INSERT INTO revision_views (user_id, contest_id, revision_id, first_seen_at) VALUES (2, 1, 4, ?)`, f.ago(day))
	res, err := f.job().Run(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.RevisionsDeleted != 1 || f.count("SELECT count(*) FROM problem_revisions WHERE id = 1") != 0 ||
		f.count("SELECT count(*) FROM revision_tests WHERE revision_id = 1") != 0 || f.count("SELECT count(*) FROM problem_revisions") != 5 {
		t.Errorf("deleted %d; left %d revisions", res.RevisionsDeleted, f.count("SELECT count(*) FROM problem_revisions"))
	}
	// The cutoff row stays at the floor or older, so the trigger still
	// protects newer revisions.
	if f.count("SELECT before_ms <= ? FROM retention_cutoffs WHERE tbl = 'problem_revisions'", f.ago(90*day)) != 1 {
		t.Error("cutoff newer than 90 days")
	}
	if _, err := f.db.ExecContext(f.ctx, "DELETE FROM problem_revisions WHERE id = 5"); err == nil {
		t.Error("an 80-day-old revision could be deleted")
	}
}

func TestSubmissionsAfterFourYears(t *testing.T) {
	f := newFixture(t)
	f.problemWithRevisions(5*retention.Year, 5*retention.Year)
	f.contest(1, retention.Submissions+10*day) // past retention
	f.contest(2, retention.Submissions-10*day) // not yet
	for _, c := range []int64{1, 2} {
		f.exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (?, 1, 'A', 0, 1)`, c)
		f.exec(`INSERT INTO contest_pin_history (contest_id, label, problem_id, revision_id, from_at) VALUES (?, 'A', 1, 1, ?)`,
			c, f.ago(retention.Submissions+20*day))
		f.exec(`INSERT INTO revision_views (user_id, contest_id, revision_id, first_seen_at) VALUES (2, ?, 1, ?)`, c, f.ago(retention.Submissions+11*day))
		f.exec(`INSERT INTO jobs (id, kind, priority, state, payload, created_at, contest_id) VALUES (?, 'submit', 20, 'done', '{}', 1, ?)`, 10+c, c)
		f.exec(`INSERT INTO job_results (job_id, seq, kind, idx, role, verdict) VALUES (?, 1, 'test', 1, 'program', 'AC')`, 10+c)
		f.exec(`INSERT INTO submissions (job_id, problem_id, contest_id, user_id, kind, language, source, source_sha, state, created_at, revision_id)
			VALUES (?, 1, ?, 2, 'submit', 'cpp17', 'x', 's', 'done', 1, 1)`, 10+c, c)
	}
	f.exec("UPDATE contests SET archived = 1 WHERE id = 1")
	// A late pin change on the old contest, newer than the cutoff, waits.
	f.exec(`INSERT INTO contest_pin_history (contest_id, label, problem_id, revision_id, from_at) VALUES (1, 'A', 1, 2, ?)`, f.ago(day))
	// Outside contests: one old, one new.
	f.exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, state, created_at)
		VALUES (1, 2, 'submit', 'cpp17', 'x', 's', 'done', ?), (1, 2, 'submit', 'cpp17', 'x', 's', 'done', ?)`,
		f.ago(retention.Submissions+day), f.ago(day))

	j := f.job()
	res, err := j.Run(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.ContestsExpired != 1 || res.SubmissionsDeleted != 2 {
		t.Errorf("result %+v", res)
	}
	if f.count("SELECT count(*) FROM submissions WHERE contest_id = 1") != 0 || f.count("SELECT count(*) FROM submissions WHERE contest_id = 2") != 1 ||
		f.count("SELECT count(*) FROM submissions WHERE contest_id IS NULL") != 1 {
		t.Error("wrong submissions deleted")
	}
	if f.count("SELECT count(*) FROM jobs WHERE id = 11") != 0 || f.count("SELECT count(*) FROM job_results WHERE job_id = 11") != 0 ||
		f.count("SELECT count(*) FROM jobs WHERE id = 12") != 1 {
		t.Error("jobs of the old contest not deleted, or the new one's gone")
	}
	var standings string
	f.db.QueryRowContext(f.ctx, "SELECT standings FROM contest_results WHERE contest_id = 1").Scan(&standings)
	if !strings.Contains(standings, `"total":100`) || f.count("SELECT count(*) FROM contest_results") != 1 {
		t.Errorf("standings kept: %q", standings)
	}
	if f.count("SELECT count(*) FROM contest_pin_history WHERE contest_id = 1") != 1 || f.count("SELECT count(*) FROM revision_views WHERE contest_id = 1") != 0 ||
		f.count("SELECT count(*) FROM contest_pin_history WHERE contest_id = 2") != 1 || f.count("SELECT count(*) FROM revision_views WHERE contest_id = 2") != 1 {
		t.Error("pins and views")
	}
	if f.count("SELECT count(*) FROM contest_problems WHERE contest_id = 1 AND revision_id IS NULL") != 1 {
		t.Error("old contest still pins its revision")
	}
	// An old contest still open for practice keeps its pins and its fresh
	// practice; one with an open integrity case waits whole.
	f.contest(4, retention.Submissions+10*day)
	f.contest(5, retention.Submissions+10*day)
	f.exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (4, 1, 'A', 0, 1)`)
	f.exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, practice, created_at)
		VALUES (1, 4, 2, 'submit', 'cpp17', 'x', 's', 'done', 0, 1), (1, 4, 2, 'submit', 'cpp17', 'x', 's', 'done', 1, ?),
		(1, 5, 2, 'submit', 'cpp17', 'x', 's', 'done', 0, 1)`, f.ago(day))
	f.exec(`INSERT INTO integrity_cases (contest_id, state, title, created_at, updated_at) VALUES (5, 'open', 'Case', 1, 1)`)
	if _, err := j.Run(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.count("SELECT count(*) FROM submissions WHERE contest_id = 4") != 1 || f.count("SELECT count(*) FROM submissions WHERE contest_id = 4 AND practice = 1") != 1 ||
		f.count("SELECT count(*) FROM contest_problems WHERE contest_id = 4 AND revision_id = 1") != 1 {
		t.Error("practice-open contest: fresh practice or pin lost")
	}
	if f.count("SELECT count(*) FROM submissions WHERE contest_id = 5") != 1 || f.count("SELECT count(*) FROM contest_results WHERE contest_id = 5") != 0 {
		t.Error("contest with an open case was expired")
	}

	// A second pass changes nothing and writes no second export.
	exported := 0
	j.ExportStandings = func(context.Context, int64) ([]byte, error) { exported++; return []byte("[]"), nil }
	if res, err := j.Run(f.ctx); err != nil || res != (retention.Result{}) || exported != 0 {
		t.Errorf("second pass %+v %v, exported %d", res, err, exported)
	}
	// Without an export function nothing is deleted.
	f.contest(3, retention.Submissions+10*day)
	f.exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, created_at)
		VALUES (1, 3, 2, 'submit', 'cpp17', 'x', 's', 'done', 1)`)
	j.ExportStandings = nil
	if _, err := j.Run(f.ctx); err != nil || f.count("SELECT count(*) FROM submissions WHERE contest_id = 3") != 1 {
		t.Errorf("deleted without an export: %v", err)
	}
}

func TestAuditAfterFourYears(t *testing.T) {
	f := newFixture(t)
	f.contest(1, day)
	f.exec(`INSERT INTO site_audit (at, action) VALUES (?, 'role_granted'), (?, 'role_granted')`, f.ago(retention.Audit+day), f.ago(retention.Audit-day))
	f.exec(`INSERT INTO contest_audit (contest_id, actor_id, actor_email, actor_role, action, created_at) VALUES
		(1, 1, 'o', 'owner', 'pause', ?), (1, 1, 'o', 'owner', 'pause', ?)`, f.ago(retention.Audit+day), f.ago(retention.Audit-day))
	res, err := f.job().Run(f.ctx)
	if err != nil || res.AuditDeleted != 2 || f.count("SELECT count(*) FROM site_audit") != 1 || f.count("SELECT count(*) FROM contest_audit") != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	// The newer rows are still append only.
	if _, err := f.db.ExecContext(f.ctx, "DELETE FROM site_audit"); err == nil {
		t.Error("a site audit row inside its period could be deleted")
	}
}

// TestBackupDefaultMatches: the nightly backup's default keep matches the
// Backups period the privacy page states.
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

// TestOneFailingContestDoesNotStopThePass: an export that fails skips its
// contest; the other contests and the audit rule still run, and the error
// comes back (security review 2026-10-20, finding 8).
func TestOneFailingContestDoesNotStopThePass(t *testing.T) {
	f := newFixture(t)
	f.problemWithRevisions(5 * retention.Year)
	for _, c := range []int64{1, 2} {
		f.contest(c, retention.Submissions+10*day)
		f.exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, created_at)
			VALUES (1, ?, 2, 'submit', 'cpp17', 'x', 's', 'done', 1)`, c)
	}
	f.exec(`INSERT INTO site_audit (at, action) VALUES (?, 'role_granted')`, f.ago(retention.Audit+day))
	j := f.job()
	j.ExportStandings = func(_ context.Context, id int64) ([]byte, error) {
		if id == 1 {
			return nil, errors.New("board unreadable")
		}
		return []byte("[]"), nil
	}
	res, err := j.Run(f.ctx)
	if err == nil || !strings.Contains(err.Error(), "contest 1") {
		t.Errorf("error %v", err)
	}
	if res.ContestsExpired != 1 || res.AuditDeleted != 1 || f.count("SELECT count(*) FROM submissions WHERE contest_id = 1") != 1 {
		t.Errorf("result %+v", res)
	}
}
