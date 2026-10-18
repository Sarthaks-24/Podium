package integrity

import (
	"context"
	"errors"
	"testing"
	"time"
)

// seenAll returns the latest explanation version of every student in a
// case, as a reviewer who just loaded the case page would send it.
func seenAll(t *testing.T, r *rig, caseID int64) map[int64]int64 {
	t.Helper()
	c, err := r.svc.GetCase(context.Background(), caseID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]int64{}
	for _, u := range c.Users {
		if n := len(u.Explanations); n > 0 {
			out[u.UserID] = u.Explanations[n-1].CreatedAt
		}
	}
	return out
}

// copyCase checks a contest where student 7 copied student 2 and opens a
// case on that signal.
func copyCase(t *testing.T, r *rig) *Case {
	t.Helper()
	ctx := context.Background()
	c := copies[7]
	r.submit(t, 7, c.sub.Language, c.sub.Source, 100, nil)
	r.submit(t, c.of, independent[c.of].Language, independent[c.of].Source, 100, nil)
	if err := r.svc.Check(ctx, insertRun(t, r), r.contest); err != nil {
		t.Fatal(err)
	}
	run, _ := r.svc.Latest(ctx, r.contest)
	var id int64
	for _, s := range run.Signals {
		if s.Kind == "similar" && s.UserA == 2 && s.UserB == 7 {
			id = s.ID
		}
	}
	if id == 0 {
		t.Fatalf("no signal for students 2 and 7: %+v", run.Signals)
	}
	kase, err := r.svc.OpenCase(ctx, r.contest, 100, "Problem A, similar code", "The two programs match almost line for line.", []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	return kase
}

// wantRefusal checks that err is an InputError with exactly this sentence.
func wantRefusal(t *testing.T, step string, err error, sentence string) {
	t.Helper()
	var in *InputError
	if !errors.As(err, &in) || in.Message != sentence {
		t.Errorf("%s: got %v, want %q", step, err, sentence)
	}
}

const (
	notYet     = "Every student needs to explain, or to have had 7 days since the case was last shared, before you take action."
	reason     = "A reason that is long enough for the log."
	shareFirst = "Share the case with the students before taking action. They explain first."
	decided    = "The case is decided. Reopen it before changing the decision."
)

// Each refused change says why, and action waits for every student's
// turn: an explanation after the latest share, or the response window.
func TestDecideFollowsTheStudentsTurn(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	c := copyCase(t, r)
	decide := func(state string) error {
		_, err := r.svc.Decide(ctx, c.ID, 100, state, reason, seenAll(t, r, c.ID))
		return err
	}
	_, err := r.svc.Decide(ctx, c.ID, 100, "closed", reason, nil)
	wantRefusal(t, "unknown state", err, "Unknown case state.")
	wantRefusal(t, "open to actioned", decide(Actioned), shareFirst)
	wantRefusal(t, "open to open", decide(Open), "The case is already open. Reload to see its latest state.")
	if err := decide(AwaitingStudent); err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, "share twice", decide(AwaitingStudent), "The case is already shared with the students. Reload to see its latest state.")
	// The moment it is shared, nobody has had a turn yet.
	wantRefusal(t, "action at once", decide(Actioned), notYet)
	if err := r.svc.Explain(ctx, c.ID, 7, "We revised together from the same notes."); err != nil {
		t.Fatal(err)
	}
	// Student 2 has neither explained nor had 7 days.
	*r.clock = r.clock.Add(ResponseWindow - time.Minute)
	wantRefusal(t, "action before student 2's window ends", decide(Actioned), notYet)
	*r.clock = r.clock.Add(time.Minute)
	if err := decide(Actioned); err != nil {
		t.Fatalf("action after the window: %v", err)
	}
	wantRefusal(t, "actioned to cleared", decide(Cleared), decided)
	wantRefusal(t, "actioned to shared", decide(AwaitingStudent), decided)

	// A reopen ends the turn: the case must be shared again, and each
	// student gets a new window. Their earlier explanations do not count.
	if err := decide(Open); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetCase(ctx, c.ID)
	for _, u := range got.Users {
		if u.AskedAt != nil || u.RespondBy != nil || u.SharedAt == nil {
			t.Errorf("after the reopen user %d: asked %v respond by %v shared %v", u.UserID, u.AskedAt, u.RespondBy, u.SharedAt)
		}
	}
	wantRefusal(t, "reopened to actioned", decide(Actioned), shareFirst)
	*r.clock = r.clock.Add(time.Hour)
	if err := decide(AwaitingStudent); err != nil {
		t.Fatal(err)
	}
	got, _ = r.svc.GetCase(ctx, c.ID)
	if u := got.Users[0]; u.AskedAt == nil || *u.RespondBy != *u.AskedAt+ResponseWindow.Milliseconds() || *u.AskedAt != r.clock.UnixMilli() {
		t.Errorf("new turn: %+v", u)
	}
	wantRefusal(t, "action after the new share with old explanations", decide(Actioned), notYet)
	for _, u := range []int64{2, 7} {
		if err := r.svc.Explain(ctx, c.ID, u, "My account of how I wrote it, again."); err != nil {
			t.Fatal(err)
		}
	}
	if err := decide(Actioned); err != nil {
		t.Errorf("action after both explained: %v", err)
	}
}

// Clearing never hurts a student, so it needs no share and no wait.
func TestClearAnyTimeAfterOpening(t *testing.T) {
	r := newRig(t)
	c := copyCase(t, r)
	got, err := r.svc.Decide(context.Background(), c.ID, 100, Cleared, reason, nil)
	if err != nil || got.State != Cleared {
		t.Fatalf("clear an open case: %v %+v", err, got)
	}
}

// Every explanation version is kept, and a decision against an older
// version than the latest is refused.
func TestDecideRefusesAChangedExplanation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	c := copyCase(t, r)
	if _, err := r.svc.Decide(ctx, c.ID, 100, AwaitingStudent, reason, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Explain(ctx, c.ID, 7, "First version."); err != nil {
		t.Fatal(err)
	}
	read := seenAll(t, r, c.ID)
	// Saved in the same millisecond: still a newer version.
	if err := r.svc.Explain(ctx, c.ID, 7, "Second version, written after the first."); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Decide(ctx, c.ID, 100, Cleared, reason, read); !errors.Is(err, ErrExplanationChanged) {
		t.Errorf("decided against an old version: %v", err)
	}
	if _, err := r.svc.Decide(ctx, c.ID, 100, Cleared, reason, nil); !errors.Is(err, ErrExplanationChanged) {
		t.Errorf("decided without saying what was read: %v", err)
	}
	got, _ := r.svc.GetCase(ctx, c.ID)
	var vs []Explanation
	for _, u := range got.Users {
		if u.UserID == 7 {
			vs = u.Explanations
			if u.Explanation != "Second version, written after the first." {
				t.Errorf("latest explanation %q", u.Explanation)
			}
		}
	}
	if len(vs) != 2 || vs[0].Text != "First version." || vs[1].CreatedAt <= vs[0].CreatedAt {
		t.Fatalf("versions %+v", vs)
	}
	got, err := r.svc.Decide(ctx, c.ID, 100, Cleared, reason, seenAll(t, r, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	last := got.Decisions[len(got.Decisions)-1]
	if last.ExplanationsSeen["7"] != vs[1].CreatedAt || last.ByName != "Organiser" || last.By != 100 {
		t.Errorf("decision %+v", last)
	}
}

// A student in the case never reviews it, even after becoming an admin.
func TestStudentInTheCaseCannotDecide(t *testing.T) {
	r := newRig(t)
	c := copyCase(t, r)
	mustExec(t, r.db, "UPDATE users SET role = 'admin' WHERE id = 2")
	if !c.Involves(2) || c.Involves(100) {
		t.Errorf("involves: %+v", c.Users)
	}
	if _, err := r.svc.Decide(context.Background(), c.ID, 2, Cleared, reason, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("student 2 decided their own case: %v", err)
	}
}

// The database refuses to change or remove a decision.
func TestDecisionLogIsAppendOnly(t *testing.T) {
	r := newRig(t)
	c := copyCase(t, r)
	if _, err := r.db.Exec("UPDATE integrity_decisions SET note = 'rewritten' WHERE case_id = ?", c.ID); err == nil {
		t.Error("a decision was updated")
	}
	if _, err := r.db.Exec("DELETE FROM integrity_decisions WHERE case_id = ?", c.ID); err == nil {
		t.Error("a decision was deleted")
	}
	if _, err := r.db.Exec("DELETE FROM integrity_cases WHERE id = ?", c.ID); err == nil {
		t.Error("a case with decisions was deleted")
	}
	got, _ := r.svc.GetCase(context.Background(), c.ID)
	if len(got.Decisions) != 1 || got.Decisions[0].Note != "The two programs match almost line for line." {
		t.Errorf("log %+v", got.Decisions)
	}
}

// A case's pair view comes from its own copy of the evidence: after a
// re-run reshuffles the live signals it still shows the same two
// students' programs, and no live signal reuses the case's signal id.
func TestCasePairSurvivesARerun(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	c := copyCase(t, r)
	sig, a, b, err := r.svc.CasePair(ctx, c, 0)
	if err != nil || a.UserID != 2 || b.UserID != 7 || len(a.Spans) == 0 || sig.UserA != 2 {
		t.Fatalf("case pair: %v %+v %+v", err, a, b)
	}
	before := a.Source + b.Source

	// Student 7's submission leaves the pool and the other copies arrive.
	mustExec(t, r.db, "UPDATE submissions SET state = 'error' WHERE user_id = 7")
	for id, cc := range copies {
		if id == 7 {
			continue
		}
		r.submit(t, id, cc.sub.Language, cc.sub.Source, 100, nil)
		r.submit(t, cc.of, independent[cc.of].Language, independent[cc.of].Source, 100, nil)
	}
	if err := r.svc.Check(ctx, insertRun(t, r), r.contest); err != nil {
		t.Fatal(err)
	}
	run, _ := r.svc.Latest(ctx, r.contest)
	if len(run.Signals) == 0 {
		t.Fatal("the re-run found nothing")
	}
	for _, s := range run.Signals {
		if s.ID <= sig.ID {
			t.Errorf("live signal %d reuses an id at or below the case's %d", s.ID, sig.ID)
		}
	}
	_, a2, b2, err := r.svc.CasePair(ctx, c, 0)
	if err != nil || a2.UserID != 2 || b2.UserID != 7 || a2.Source+b2.Source != before {
		t.Errorf("after the re-run: %v %+v %+v", err, a2, b2)
	}
	for _, i := range []int{-1, 1} {
		if _, _, _, err := r.svc.CasePair(ctx, c, i); !errors.Is(err, ErrNotFound) {
			t.Errorf("evidence %d: %v", i, err)
		}
	}
	// A submission moved to another contest is no longer shown.
	other := mustExec(t, r.db, `INSERT INTO contests (slug, title, starts_at, ends_at, created_at, updated_at) VALUES ('d', 'D', 0, 1, 0, 0)`)
	oid, _ := other.LastInsertId()
	mustExec(t, r.db, "UPDATE submissions SET contest_id = ? WHERE id = ?", oid, a.SubmissionID)
	if _, _, _, err := r.svc.CasePair(ctx, c, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("pair from another contest's submission: %v", err)
	}
}

// Checks and signals go 180 days after the contest; cases keep theirs.
func TestRetainSignalsDeletesOldChecks(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	c := copyCase(t, r) // contest r.contest ended in 1970
	day := int64(24 * time.Hour / time.Millisecond)
	now := r.clock.UnixMilli()
	recent := mustExec(t, r.db, `INSERT INTO contests (slug, title, starts_at, ends_at, created_at, updated_at) VALUES ('new', 'New', 0, ?, 0, 0)`,
		now-179*day)
	rid, _ := recent.LastInsertId()
	run := mustExec(t, r.db, `INSERT INTO integrity_runs (contest_id, state, started_at) VALUES (?, 'done', 0)`, rid)
	runID, _ := run.LastInsertId()
	mustExec(t, r.db, `INSERT INTO integrity_signals (run_id, contest_id, kind, strength, score, user_a) VALUES (?, ?, 'probing', 'moderate', 6, 1)`,
		runID, rid)
	// A check still running on the old contest is left for the next pass.
	mustExec(t, r.db, `INSERT INTO integrity_runs (contest_id, state, started_at) VALUES (?, 'running', 0)`, r.contest)

	signals, runs, err := r.svc.RetainSignals(ctx)
	if err != nil || signals == 0 || runs != 1 {
		t.Fatalf("retain: %d signals, %d runs, %v", signals, runs, err)
	}
	var left int
	r.db.QueryRow("SELECT count(*) FROM integrity_signals WHERE contest_id = ?", r.contest).Scan(&left)
	if left != 0 {
		t.Errorf("%d old signals left", left)
	}
	r.db.QueryRow("SELECT count(*) FROM integrity_signals WHERE contest_id = ?", rid).Scan(&left)
	if left != 1 {
		t.Errorf("the recent contest's signals: %d", left)
	}
	r.db.QueryRow("SELECT count(*) FROM integrity_runs WHERE contest_id = ?", r.contest).Scan(&left)
	if left != 1 {
		t.Errorf("old runs left: %d, want only the running one", left)
	}
	// The case still has its evidence and its pair view.
	if _, a, _, err := r.svc.CasePair(ctx, c, 0); err != nil || a.UserID != 2 {
		t.Errorf("case pair after retention: %v", err)
	}
	if s, n, err := r.svc.RetainSignals(ctx); err != nil || s != 0 || n != 0 {
		t.Errorf("second pass: %d %d %v", s, n, err)
	}
	// Retain runs this step too.
	*r.clock = r.clock.Add(48 * time.Hour)
	if _, err := r.svc.Retain(ctx); err != nil {
		t.Fatal(err)
	}
	r.db.QueryRow("SELECT count(*) FROM integrity_signals WHERE contest_id = ?", rid).Scan(&left)
	if left != 0 {
		t.Errorf("Retain left the now old contest's signals: %d", left)
	}
}
