package contests

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
)

type svcRig struct {
	svc *Service
	now *time.Time
}

func newSvc(t testing.TB) *svcRig {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, name, role, created_at) VALUES
		(1, 'owner@college.edu', 'Owner', 'owner', 0), (2, 'asha@college.edu', 'Asha', 'student', 0), (3, 'bilal@college.edu', '', 'student', 0)`); err != nil {
		t.Fatal(err)
	}
	now := t0.Add(-time.Hour)
	r := &svcRig{now: &now}
	r.svc = &Service{DB: d, Now: func() time.Time { return *r.now }}
	return r
}

func (r *svcRig) contest(t testing.TB, mutate func(*Edit)) *Contest {
	t.Helper()
	e := Edit{Slug: "week-1", Title: "Week 1", StartsAt: t0, EndsAt: at(120), StartMode: FixedStart, Scoring: BestSubtask,
		Access: "domain", Feedback: "subtask", Graded: true, PracticeAfterEnd: true, StopOnSampleFail: true, MinGapS: 30,
		MaxPerProblem: 15, Languages: []string{"cpp17"}, FreezeMinutes: 30}
	if mutate != nil {
		mutate(&e)
	}
	c, err := r.svc.Create(context.Background(), e, 1)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestWindowModeWithExtensions(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, func(e *Edit) { e.StartMode, e.DurationMinutes = WindowStart, 60 })
	*r.now = at(100)
	if err := r.svc.Start(ctx, c, 2, true); err != nil {
		t.Fatal(err)
	}
	st, _ := r.svc.State(ctx, c, 2)
	// Started at 100 with 60 minutes, but the contest closes at 120.
	if !st.Open || !st.Window.Ends.Equal(at(120)) {
		t.Errorf("capped window %+v", st)
	}
	// A personal extension moves both the cap and the end.
	if err := r.svc.SetExtension(ctx, c.ID, 2, 15, "accommodation"); err != nil {
		t.Fatal(err)
	}
	st, _ = r.svc.State(ctx, c, 2)
	if !st.Window.Ends.Equal(at(135)) || st.Extension != 15*time.Minute {
		t.Errorf("extended window %+v", st)
	}
	*r.now = at(130)
	st, _ = r.svc.State(ctx, c, 2)
	if !st.Open || c.StateFor(st, *r.now) != StateLive {
		t.Errorf("inside the extension: %+v %s", st, c.StateFor(st, *r.now))
	}
	// Bilal never started: after the close he cannot start, and practice
	// is open to him.
	st3, _ := r.svc.State(ctx, c, 3)
	if !st3.Over || !c.PracticeOpen(st3, *r.now) {
		t.Errorf("non-starter after the close %+v", st3)
	}
	if err := r.svc.Start(ctx, c, 3, true); err == nil {
		t.Error("started after the close")
	}
	// Asha's window ended at 135: practice only once the contest closed
	// for her too, which it has.
	*r.now = at(136)
	st, _ = r.svc.State(ctx, c, 2)
	if !st.Over || c.StateFor(st, *r.now) != StatePractice {
		t.Errorf("after her window: %+v", st)
	}
}

func TestStudentBoardVariants(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, func(e *Edit) { e.HoldReveal = true })
	variant := func(when time.Time) string {
		*r.now = when
		c, _ = r.svc.Get(ctx, c.ID)
		st, _ := r.svc.State(ctx, c, 2)
		v, err := r.svc.StudentVariant(ctx, c, st)
		if err != nil {
			return err.Error()
		}
		return v
	}
	if v := variant(at(60)); v != VariantFull {
		t.Errorf("before the freeze: %s", v)
	}
	if v := variant(at(95)); v != VariantFrozen {
		t.Errorf("during the freeze: %s", v)
	}
	if v := variant(at(130)); v != VariantFrozen {
		t.Errorf("held after the end: %s", v)
	}
	if err := r.svc.Reveal(ctx, c); err != nil {
		t.Fatal(err)
	}
	if v := variant(at(131)); v != VariantFull {
		t.Errorf("after the reveal: %s", v)
	}
	// Exam mode: no board until the student's window is over.
	r.svc.DB.Exec("UPDATE contests SET feedback_mode = 'exam' WHERE id = ?", c.ID)
	if v := variant(at(60)); v != ErrBoardClosed.Error() {
		t.Errorf("exam mode during the contest: %s", v)
	}
}

func TestRevealWaitsForEveryWindow(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	r.svc.SetExtension(ctx, c.ID, 2, 10, "")
	*r.now = at(125)
	if err := r.svc.Reveal(ctx, c); err != ErrRevealEarly {
		t.Errorf("reveal while a student still has time: %v", err)
	}
	*r.now = at(131)
	if err := r.svc.Reveal(ctx, c); err != nil {
		t.Errorf("reveal after every window: %v", err)
	}
}

func TestRetentionClearsOldIntegrityData(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	old := r.contest(t, nil)
	recent := r.contest(t, func(e *Edit) { e.Slug = "week-2" })
	for id, days := range map[int64]int{old.ID: 200, recent.ID: 10} {
		end := t0.Add(-time.Duration(days) * 24 * time.Hour)
		if _, err := r.svc.DB.Exec("UPDATE contests SET starts_at = ?, ends_at = ? WHERE id = ?", end.Add(-time.Hour).UnixMilli(), end.UnixMilli(), id); err != nil {
			t.Fatal(err)
		}
	}
	for _, cid := range []int64{old.ID, recent.ID} {
		r.svc.DB.Exec(`INSERT INTO integrity_events (user_id, problem_id, contest_id, kind, size, created_at) VALUES (2, 1, ?, 'paste', 50, 0)`, cid)
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language,
		created_at, updated_at, content_hash) VALUES (1, 'p', 'P', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h')`); err != nil {
		t.Fatal(err)
	}
	for _, cid := range []int64{old.ID, recent.ID} {
		if _, err := r.svc.DB.Exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, typed_chars,
			pasted_chars, focus_losses, client_hash, created_at) VALUES (1, ?, 2, 'submit', 'cpp17', '', 's', 10, 20, 3, 'abc', 0)`, cid); err != nil {
			t.Fatal(err)
		}
	}
	*r.now = t0
	events, subs, err := r.svc.Retain(ctx)
	if err != nil || events != 1 || subs != 1 {
		t.Fatalf("retain: %d events, %d submissions, %v", events, subs, err)
	}
	var left int
	r.svc.DB.QueryRow(`SELECT count(*) FROM submissions WHERE client_hash = '' AND typed_chars = 0 AND contest_id = ?`, old.ID).Scan(&left)
	if left != 1 {
		t.Error("old submission still holds integrity counts")
	}
	if events, subs, _ := r.svc.Retain(ctx); events != 0 || subs != 0 {
		t.Errorf("second run changed %d and %d", events, subs)
	}
}

func TestSecondSessionRecordedOncePerQuietPeriod(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	*r.now = at(10)
	r.svc.Start(ctx, c, 2, true)
	for i := 0; i < 3; i++ {
		if err := r.svc.RecordSecondSession(ctx, 2); err != nil {
			t.Fatal(err)
		}
	}
	*r.now = at(25)
	r.svc.RecordSecondSession(ctx, 2)
	var n int
	r.svc.DB.QueryRow("SELECT count(*) FROM integrity_events WHERE kind = 'second_session'").Scan(&n)
	if n != 2 {
		t.Errorf("recorded %d, want 2", n)
	}
}

func TestCSVInjectionIsNeutralised(t *testing.T) {
	for in, want := range map[string]string{
		"=SUM(A1)": "'=SUM(A1)", "+91": "'+91", "-1": "'-1", "@cmd": "'@cmd", "\tx": "'\tx", "\rx": "'\rx",
		"Asha": "Asha", "": "",
	} {
		if got := CSVCell(in); got != want {
			t.Errorf("CSVCell(%q) = %q, want %q", in, got, want)
		}
	}
	c := &Contest{Problems: []ContestProblem{{Label: "A"}}}
	var buf bytes.Buffer
	WriteCSV(&buf, c, []ExportRow{{Row: Row{Rank: 1, Name: "=1+1", Total: 40, Cells: []Cell{{Label: "A", Score: 40}}}, Email: "a@b.c"}})
	if lines := strings.Split(strings.TrimSpace(buf.String()), "\n"); lines[1] != "1,'=1+1,a@b.c,,,,40,40" {
		t.Errorf("csv %q", buf.String())
	}
}

// The console's "Waiting or judging" and "Judge error" filters match on
// state, because neither kind of submission has a verdict.
func TestFeedFiltersPendingAndError(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	if _, err := r.svc.DB.Exec(`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language,
		created_at, updated_at, content_hash) VALUES (1, 'p', 'P', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h')`); err != nil {
		t.Fatal(err)
	}
	for _, s := range [][2]string{{"queued", ""}, {"running", ""}, {"done", "WA"}, {"error", ""}} {
		if _, err := r.svc.DB.Exec(`INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, verdict, created_at)
			VALUES (1, ?, 2, 'submit', 'cpp17', '', 's', ?, ?, 0)`, c.ID, s[0], s[1]); err != nil {
			t.Fatal(err)
		}
	}
	for filter, want := range map[string]string{"pending": "running queued", "error": "error", "wa": "done", "": "error done running queued"} {
		items, _, err := r.svc.Feed(ctx, c, FeedFilter{Verdict: filter})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, it := range items {
			got = append(got, it.State)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("filter %q: got %v, want %s", filter, got, want)
		}
	}
}

// execNoFK runs statements with foreign keys off, for fixtures that name
// rows the test does not need.
func execNoFK(t testing.TB, r *svcRig, qs ...string) {
	t.Helper()
	ctx := context.Background()
	conn, err := r.svc.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	for _, q := range qs {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func count(t testing.TB, r *svcRig, q string, args ...any) int {
	t.Helper()
	var n int
	if err := r.svc.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
