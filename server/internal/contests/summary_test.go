package contests

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// exec runs statements with foreign keys off, as seedBoard does (the
// fixtures name problems and decisions that are not in the tables).
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

// Summaries are written only once the contest is final (every window
// closed, judging done), never during it, and again after a rejudge.
func TestSummariesOnlyAtFinalAndAfterARejudge(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	seedBoard(t, r, c, 5, 2)
	*r.now = at(60)
	if _, err := r.svc.BuildSummaries(ctx, c); !errors.Is(err, ErrNotFinal) {
		t.Fatalf("during the contest: %v", err)
	}
	if n, err := r.svc.SweepSummaries(ctx); err != nil || n != 0 {
		t.Fatalf("sweep during the contest: %d %v", n, err)
	}
	// After the close, one submission still waits for the judge.
	*r.now = at(121)
	execNoFK(t, r, "UPDATE submissions SET state = 'queued' WHERE id = (SELECT min(id) FROM submissions)")
	if _, err := r.svc.BuildSummaries(ctx, c); !errors.Is(err, ErrNotFinal) {
		t.Fatalf("with judging left: %v", err)
	}
	execNoFK(t, r, "UPDATE submissions SET state = 'done'")
	n, err := r.svc.BuildSummaries(ctx, c)
	if err != nil || n != 5 {
		t.Fatalf("at final: %d %v", n, err)
	}
	if got := count(t, r, "SELECT count(*) FROM student_contest_summary WHERE contest_id = ?", c.ID); got != 5 {
		t.Errorf("%d rows", got)
	}
	if got := count(t, r, "SELECT count(*) FROM notifications WHERE kind = 'results_final'"); got != 5 {
		t.Errorf("%d results notices", got)
	}
	sums, _ := r.svc.ContestSummaries(ctx, c.ID)
	if sm := sums[0]; sm.Score != 250 || sm.MaxScore != 500 || sm.Percent != 50 || sm.Rank != 1 || sm.RankedCount != 5 ||
		sm.Submissions != 10 || sm.Accepted != 0 || len(sm.Problems) != 5 || sm.Problems[0].Attempts != 2 || sm.Problems[0].BestVerdict != "WA" ||
		sm.Problems[0].Subtasks[1].Earned != 30 {
		t.Errorf("summary %+v", sm)
	}
	// The sweep finds nothing new until a rejudge is recorded.
	if n, _ := r.svc.SweepSummaries(ctx); n != 0 {
		t.Errorf("sweep rebuilt an unchanged contest: %d", n)
	}
	*r.now = at(130)
	if err := r.svc.Record(ctx, c.ID, Actor{ID: 1, Email: "owner@college.edu", Role: "owner"}, AuditRejudge, map[string]any{"problem_label": "A"}); err != nil {
		t.Fatal(err)
	}
	if n, err := r.svc.SweepSummaries(ctx); err != nil || n != 1 {
		t.Errorf("sweep after a rejudge: %d %v", n, err)
	}
	if got := count(t, r, "SELECT count(*) FROM notifications WHERE kind = 'results_final'"); got != 5 {
		t.Errorf("results notices after the rebuild: %d", got)
	}
}

// With a held reveal the summaries are written at final but students are
// told only after the reveal.
func TestResultsNoticeWaitsForTheReveal(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, func(e *Edit) { e.HoldReveal = true })
	seedBoard(t, r, c, 3, 1)
	*r.now = at(121)
	if n, err := r.svc.SweepSummaries(ctx); err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	if got := count(t, r, "SELECT count(*) FROM notifications"); got != 0 {
		t.Errorf("%d notices before the reveal", got)
	}
	execNoFK(t, r, "UPDATE contests SET revealed_at = 1 WHERE id = 1")
	if _, err := r.svc.SweepSummaries(ctx); err != nil {
		t.Fatal(err)
	}
	if got := count(t, r, "SELECT count(*) FROM notifications WHERE kind = 'results_final'"); got != 3 {
		t.Errorf("%d notices after the reveal", got)
	}
}

// A review decision's zero shows on the full board at once, and on the
// frozen board only once the contest is over for everyone.
func TestFrozenBoardWaitsForTheEnd(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	seedBoard(t, r, c, 2, 1)
	*r.now = at(100) // inside the freeze
	execNoFK(t, r, "INSERT INTO outcome_adjustments (contest_id, user_id, problem_id, decision_id, created_at) VALUES (1, 1000, 100, 1, 0)")
	scoreA := func(variant string) int {
		t.Helper()
		st, err := r.svc.computeBoard(ctx, c, variant, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range st.Rows {
			if row.UserID == 1000 {
				return row.Cells[0].Score
			}
		}
		t.Fatal("no row")
		return 0
	}
	if full, frozen := scoreA(VariantFull), scoreA(VariantFrozen); full != 0 || frozen != 50 {
		t.Errorf("during the freeze: full %d, frozen %d", full, frozen)
	}
	*r.now = at(121)
	if frozen := scoreA(VariantFrozen); frozen != 0 {
		t.Errorf("after the end: frozen %d", frozen)
	}
	// A decision made after the close stays on the frozen board even when
	// an extension keeps the contest frozen again (security review 7).
	execNoFK(t, r, "DELETE FROM outcome_adjustments",
		"INSERT INTO outcome_adjustments (contest_id, user_id, problem_id, decision_id, created_at) VALUES (1, 1000, 100, 2, "+
			itoaMs(at(121))+")")
	*r.now = at(100)
	if frozen := scoreA(VariantFrozen); frozen != 0 {
		t.Errorf("a decision after the close, frozen board again: %d", frozen)
	}
}

func itoaMs(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

// The 700-student summary build, timed for calibration.md.
func TestSummaryBuildFor700Students(t *testing.T) {
	if testing.Short() {
		t.Skip("timed build")
	}
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	seedBoard(t, r, c, 700, 4)
	*r.now = at(121)
	start := time.Now()
	n, err := r.svc.BuildSummaries(ctx, c)
	took := time.Since(start)
	if err != nil || n != 700 {
		t.Fatalf("build: %d %v", n, err)
	}
	t.Logf("summary build: 700 students, 5 problems, %d submissions: %v", 700*5*4, took)
	if took > 30*time.Second {
		t.Errorf("the build took %v", took)
	}
}

// auditAt writes one audit row at a fixed time without the service's
// clock, so a test clock can stand in for another request (C1-006).
func auditAt(t testing.TB, r *svcRig, contestID int64, action string, ms int64) {
	t.Helper()
	if _, err := r.svc.DB.Exec(`INSERT INTO contest_audit (contest_id, actor_id, actor_email, actor_role, action, details, created_at)
		VALUES (?, 1, 'owner@college.edu', 'owner', ?, '{}', ?)`, contestID, action, ms); err != nil {
		t.Fatal(err)
	}
}

// A rejudge recorded while a build runs is newer than the build's
// summarised_at, so the next sweep builds again (C1-006 a).
func TestSummariesRebuildAfterARejudgeDuringTheBuild(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	seedBoard(t, r, c, 3, 1)
	base := at(121)
	calls := 0
	// Every read of the clock moves it 10 ms; the second read (inside the
	// build, after it started) stands for a rejudge landing at +5 ms.
	r.svc.Now = func() time.Time {
		calls++
		now := base.Add(time.Duration(calls-1) * 10 * time.Millisecond)
		if calls == 2 {
			auditAt(t, r, c.ID, AuditRejudge, base.Add(5*time.Millisecond).UnixMilli())
		}
		return now
	}
	if _, err := r.svc.BuildSummaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	if got := count(t, r, "SELECT summarised_at FROM contests WHERE id = ?", c.ID); int64(got) != base.UnixMilli() {
		t.Errorf("summarised_at %d, want the time the build started (%d)", got, base.UnixMilli())
	}
	if n, err := r.svc.SweepSummaries(ctx); err != nil || n != 1 {
		t.Errorf("sweep after a rejudge during the build: %d %v", n, err)
	}
}

// A review decision that changed the board and a settings change after
// the contest is final each make the sweep build again (C1-006 b, c).
func TestSummariesRebuildAfterABoardAdjustmentOrSettings(t *testing.T) {
	for _, action := range []string{AuditBoardAdjusted, AuditSettings} {
		t.Run(action, func(t *testing.T) {
			r := newSvc(t)
			ctx := context.Background()
			c := r.contest(t, nil)
			seedBoard(t, r, c, 3, 1)
			*r.now = at(121)
			if n, err := r.svc.SweepSummaries(ctx); err != nil || n != 1 {
				t.Fatalf("first sweep: %d %v", n, err)
			}
			if n, _ := r.svc.SweepSummaries(ctx); n != 0 {
				t.Fatalf("sweep rebuilt an unchanged contest: %d", n)
			}
			*r.now = at(130)
			auditAt(t, r, c.ID, action, at(130).UnixMilli())
			if n, err := r.svc.SweepSummaries(ctx); err != nil || n != 1 {
				t.Errorf("sweep after %s: %d %v", action, n, err)
			}
		})
	}
}
