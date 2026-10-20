package integrity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/notifications"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// studentsRig is the integrity rig with a contest that has two scored
// problems (A and B, 100 points each), three students who started it,
// the contests service, a second admin (101) and an owner (102).
type studentsRig struct {
	*rig
	contests *contests.Service
	probB    int64
}

func newStudentsRig(t *testing.T) *studentsRig {
	t.Helper()
	r := newRig(t)
	ctx := context.Background()
	pk, err := problems.ReadPack(zipDir(t, "../../../problems/pair-with-sum"))
	if err != nil {
		t.Fatal(err)
	}
	pb, err := r.svc.Problems.Import(ctx, pk, 0)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, r.db, "UPDATE contest_problems SET points = '[100]' WHERE contest_id = ?", r.contest)
	mustExec(t, r.db, `INSERT INTO contest_problems (contest_id, problem_id, label, ord, points) VALUES (?, ?, 'B', 1, '[100]')`, r.contest, pb.ID)
	mustExec(t, r.db, "UPDATE contests SET title = 'Week 6', graded = 1 WHERE id = ?", r.contest)
	for u := 1; u <= 3; u++ {
		mustExec(t, r.db, "INSERT INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (?, ?, 0, 0)", r.contest, u)
	}
	mustExec(t, r.db, `INSERT INTO users (id, email, name, role, created_at) VALUES (101, 'second@college.edu', 'Second', 'admin', 0),
		(102, 'owner@college.edu', 'Owner', 'owner', 0)`)
	tick := func() time.Time { return *r.clock }
	cs := &contests.Service{DB: r.db, Problems: r.svc.Problems, Now: tick, BoardEvery: time.Nanosecond}
	r.svc.Contests = cs
	return &studentsRig{rig: r, contests: cs, probB: pb.ID}
}

// sub stores a judged, counting submission of label scoring score, made
// minutes after the start.
func (r *studentsRig) sub(t *testing.T, user int64, label string, score, minutes int) int64 {
	t.Helper()
	pid := r.problem.ID
	if label == "B" {
		pid = r.probB
	}
	verdict := "WA"
	if score == 100 {
		verdict = "AC"
	}
	res := mustExec(t, r.db, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, verdict,
		score, max_score, earned, created_at) VALUES (?, ?, ?, 'submit', 'cpp17', 'int main(){}', 'sha', 'done', ?, ?, 100, ?, ?)`,
		pid, r.contest, user, verdict, score, "["+itoa(score)+"]", int64(minutes)*60000)
	id, _ := res.LastInsertId()
	return id
}

// board returns each student's total, rank and A score on the full board.
func (r *studentsRig) board(t *testing.T) map[int64][3]int {
	t.Helper()
	ctx := context.Background()
	c, err := r.contests.Get(ctx, r.contest)
	if err != nil {
		t.Fatal(err)
	}
	st, err := r.contests.BoardNow(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64][3]int{}
	for _, row := range st.Rows {
		out[row.UserID] = [3]int{row.Total, row.Rank, row.Cells[0].Score}
	}
	return out
}

// outcome takes a manual case on submission sub of user through share,
// reply, a proposal by 100 and a confirmation by 101, and returns it.
func (r *studentsRig) outcome(t *testing.T, user, sub int64, kind, label string) *Case {
	t.Helper()
	ctx := context.Background()
	ref, err := r.svc.Submission(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.svc.OpenManualCase(ctx, ref, 100, "Problem A, a question", reason)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Decide(ctx, c.ID, 100, AwaitingStudent, reason, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Explain(ctx, c.ID, user, "I wrote it myself from my notes."); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Propose(ctx, c.ID, 100, ProposeInput{Outcome: kind, ProblemLabel: label, Note: reason, Reason: forStudent,
		Seen: seenAll(t, r.rig, c.ID)}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetCase(ctx, c.ID)
	done, err := r.svc.Confirm(ctx, c.ID, got.Proposals[len(got.Proposals)-1].ID, 101, reason, "", seenAll(t, r.rig, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	return done
}

func (r *studentsRig) rating(t *testing.T, user int64) int {
	t.Helper()
	v, err := r.svc.Rating(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The two-person rule, the board recompute on a zero for a problem, and
// its reversal by an appeal that a reviewer who took no part upholds.
func TestZeroForProblemTwoPersonRuleAndAppeal(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	s1a := r.sub(t, 1, "A", 100, 10)
	r.sub(t, 1, "B", 50, 20)
	r.sub(t, 2, "A", 100, 30)
	r.sub(t, 2, "B", 50, 40)
	r.sub(t, 3, "A", 40, 5)
	before := r.board(t)
	if before[1] != [3]int{150, 1, 100} || before[2] != [3]int{150, 2, 100} || before[3] != [3]int{40, 3, 40} {
		t.Fatalf("board before: %v", before)
	}
	c, _ := r.contests.Get(ctx, r.contest)
	if _, err := r.contests.BuildSummaries(ctx, c); err != nil {
		t.Fatal(err)
	}

	ref, _ := r.svc.Submission(ctx, s1a)
	kase, err := r.svc.OpenManualCase(ctx, ref, 100, "Problem A, a question", reason)
	if err != nil || kase.Source != SourceManual || len(kase.Users) != 1 || kase.Users[0].UserID != 1 {
		t.Fatalf("manual case: %+v %v", kase, err)
	}
	if _, err := r.svc.Decide(ctx, kase.ID, 100, AwaitingStudent, reason, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Explain(ctx, kase.ID, 1, "I wrote it myself from my notes."); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Propose(ctx, kase.ID, 100, ProposeInput{Outcome: OutcomeZero, ProblemLabel: "A", Note: reason, Reason: forStudent,
		Seen: seenAll(t, r.rig, kase.ID)}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetCase(ctx, kase.ID)
	p := got.Proposals[0]
	if p.State != ProposalPending || p.ProblemLabel != "A" {
		t.Fatalf("proposal %+v", p)
	}
	// The proposer (also the opener) may not confirm; the owner is not
	// the sole organiser while two admins exist.
	if _, err := r.svc.Confirm(ctx, kase.ID, p.ID, 100, reason, "", seenAll(t, r.rig, kase.ID)); !errors.Is(err, ErrTwoPersonRule) {
		t.Errorf("the proposer confirmed: %v", err)
	}
	if err := r.svc.ForViewer(ctx, got, 100); err != nil || got.MayConfirm || got.SoleOrganiser {
		t.Errorf("viewer 100: may confirm %v, sole %v", got.MayConfirm, got.SoleOrganiser)
	}
	if err := r.svc.ForViewer(ctx, got, 101); err != nil || !got.MayConfirm {
		t.Errorf("viewer 101 may not confirm")
	}
	if _, err := r.svc.Confirm(ctx, kase.ID, p.ID, 101, reason, "", seenAll(t, r.rig, kase.ID)); err != nil {
		t.Fatal(err)
	}
	// Exactly one cell is zero; the rank order and tie-break follow.
	after := r.board(t)
	if after[1] != [3]int{50, 2, 0} || after[2] != [3]int{150, 1, 100} || after[3] != [3]int{40, 3, 40} {
		t.Fatalf("board after the decision: %v", after)
	}
	got, _ = r.svc.GetCase(ctx, kase.ID)
	d := got.Decisions[len(got.Decisions)-1]
	if got.State != Actioned || d.Outcome != OutcomeZero || d.OutcomeProblemLabel != "A" || d.ProposalID == nil || *d.ProposalID != p.ID {
		t.Fatalf("decision %+v", d)
	}
	var bb string
	r.db.QueryRow("SELECT board_before FROM integrity_decisions WHERE id = ?", d.ID).Scan(&bb)
	if bb != `{"1":{"rank":1,"total":150,"score":100}}` {
		t.Errorf("board before on the decision: %s", bb)
	}
	var audit string
	r.db.QueryRow("SELECT details FROM contest_audit WHERE action = 'board_adjusted'").Scan(&audit)
	if !strings.Contains(audit, `"rank_before":1`) || !strings.Contains(audit, `"total_before":150`) || !strings.Contains(audit, `"reversed":false`) {
		t.Errorf("audit %s", audit)
	}
	if v := r.rating(t, 1); v != 85 {
		t.Errorf("rating after zero for problem: %d", v)
	}
	// The public standings carry no mark; the export row does.
	c, _ = r.contests.Get(ctx, r.contest)
	r.contests.Invalidate(c.ID)
	st, err := r.contests.Board(ctx, c, contests.VariantFull)
	if err != nil {
		t.Fatal(err)
	}
	js, _ := json.Marshal(st)
	if strings.Contains(string(js), "zero") || strings.Contains(string(js), "adjust") || strings.Contains(string(js), "decision") {
		t.Errorf("public standings carry the decision: %s", js)
	}
	rows, _, err := r.contests.Export(ctx, c, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		want := row.UserID == 1
		if got := row.ZeroByDecision != nil && len(*row.ZeroByDecision) == 1 && (*row.ZeroByDecision)[0] == "A"; got != want {
			t.Errorf("export row %d zero %v", row.UserID, row.ZeroByDecision)
		}
	}
	if !c.BoardAdjustedAt.Equal(*r.clock) {
		t.Errorf("board adjusted at %v", c.BoardAdjustedAt)
	}
	// The summaries were rewritten with the board.
	sums, _ := r.contests.ContestSummaries(ctx, r.contest)
	for _, sm := range sums {
		if sm.UserID == 1 && (sm.Score != 50 || sm.Rank != 2 || !sm.Problems[0].Zeroed || sm.Problems[0].Score != 0) {
			t.Errorf("summary of student 1: %+v", sm)
		}
		if sm.UserID == 2 && sm.Rank != 1 {
			t.Errorf("summary of student 2: %+v", sm)
		}
	}
	// A rejudge rewrites verdicts, never the decision.
	mustExec(t, r.db, "UPDATE submissions SET score = 100, earned = '[100]', created_at = 60000 WHERE id = ?", s1a)
	if b := r.board(t); b[1][2] != 0 {
		t.Errorf("a rejudge brought the zero back: %v", b)
	}
	// The student's standing shows the outcome.
	sd, err := r.svc.StandingOf(ctx, 1)
	if err != nil || sd.Band != BandOnRecord || len(sd.Lines) != 1 || sd.Lines[0].Text != "Week 6: zero for problem A." ||
		sd.Lines[0].Reason != forStudent || !sd.Lines[0].CanAppeal {
		t.Fatalf("standing %+v %v", sd, err)
	}
	// The appeal: the opener, the proposer and the confirmer may not rule.
	a, err := r.svc.FileAppeal(ctx, 1, AppealInput{CaseID: kase.ID, Text: "I can show my notes from the lab session."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.FileAppeal(ctx, 1, AppealInput{CaseID: kase.ID, Text: "A second appeal of the same thing."}); err == nil {
		t.Error("a second appeal was accepted")
	}
	for _, by := range []int64{100, 101, 1} {
		if _, err := r.svc.Rule(ctx, a.ID, by, true, forStudent, ""); !errors.Is(err, ErrUninvolved) {
			t.Errorf("reviewer %d ruled: %v", by, err)
		}
	}
	got, _ = r.svc.GetCase(ctx, kase.ID)
	if err := r.svc.ForViewer(ctx, got, 102); err != nil || !got.MayRule {
		t.Error("the owner who took no part may not rule")
	}
	ruled, err := r.svc.Rule(ctx, a.ID, 102, true, forStudent, "")
	if err != nil || ruled.State != AppealUpheld {
		t.Fatalf("ruling %+v %v", ruled, err)
	}
	if b := r.board(t); b[1] != [3]int{150, 1, 100} {
		t.Errorf("board after the upheld appeal: %v", b)
	}
	if v := r.rating(t, 1); v != 100 {
		t.Errorf("rating after the upheld appeal: %d", v)
	}
	got, _ = r.svc.GetCase(ctx, kase.ID)
	if got.State != Cleared {
		t.Errorf("case after the upheld appeal: %s", got.State)
	}
	sd, _ = r.svc.StandingOf(ctx, 1)
	if sd.Band != BandGood || len(sd.Lines) != 0 {
		t.Errorf("standing after the upheld appeal: %+v", sd)
	}
	sums, _ = r.contests.ContestSummaries(ctx, r.contest)
	for _, sm := range sums {
		if sm.UserID == 1 && (sm.Score != 150 || sm.Rank != 1 || sm.Problems[0].Zeroed) {
			t.Errorf("summary after the appeal: %+v", sm)
		}
	}
	var reversed int
	r.db.QueryRow("SELECT count(*) FROM contest_audit WHERE action = 'board_adjusted' AND json_extract(details, '$.reversed') = 1").Scan(&reversed)
	if reversed != 1 {
		t.Errorf("reversal audit rows: %d", reversed)
	}
	// Notices: shared, decided, appeal decided; never a name or code.
	var kinds []string
	rs, _ := r.db.Query("SELECT kind FROM notifications WHERE user_id = 1 ORDER BY id")
	for rs.Next() {
		var k string
		rs.Scan(&k)
		kinds = append(kinds, k)
	}
	rs.Close()
	if strings.Join(kinds, ",") != "results_final,case_shared,case_decided,appeal_decided" {
		t.Errorf("notices %v", kinds)
	}
}

// The sole organiser may confirm their own proposal, with a reason.
func TestSoleOrganiserConfirmsAlone(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	mustExec(t, r.db, "UPDATE users SET role = 'student' WHERE id IN (100, 101)")
	sub := r.sub(t, 1, "A", 100, 10)
	ref, _ := r.svc.Submission(ctx, sub)
	c, err := r.svc.OpenManualCase(ctx, ref, 102, "Problem A, a question", reason)
	if err != nil {
		t.Fatal(err)
	}
	r.svc.Decide(ctx, c.ID, 102, AwaitingStudent, reason, nil)
	r.svc.Explain(ctx, c.ID, 1, "I wrote it myself from my notes.")
	if _, err := r.svc.Propose(ctx, c.ID, 102, ProposeInput{Outcome: OutcomeWarning, Note: reason, Reason: forStudent,
		Seen: seenAll(t, r.rig, c.ID)}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetCase(ctx, c.ID)
	if _, err := r.svc.Confirm(ctx, c.ID, got.Proposals[0].ID, 102, reason, "short", seenAll(t, r.rig, c.ID)); err == nil {
		t.Error("confirmed alone without a reason")
	}
	done, err := r.svc.Confirm(ctx, c.ID, got.Proposals[0].ID, 102, reason, "No other organiser has an account this term.", seenAll(t, r.rig, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	if d := done.Decisions[len(done.Decisions)-1]; d.SoleOrganiser == "" || d.Outcome != OutcomeWarning {
		t.Errorf("decision %+v", d)
	}
}

// Each outcome's step, the clamp on every path, the manual edit with its
// two-person rule, and appeals of outcomes and edits.
func TestRatingSteps(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	// Each outcome takes its step.
	w := r.outcome(t, 1, r.sub(t, 1, "A", 100, 10), OutcomeWarning, "")
	z := r.outcome(t, 2, r.sub(t, 2, "A", 100, 10), OutcomeZero, "A")
	r.outcome(t, 3, r.sub(t, 3, "A", 100, 10), OutcomeReferred, "")
	if a, b, c := r.rating(t, 1), r.rating(t, 2), r.rating(t, 3); a != 95 || b != 85 || c != 75 {
		t.Fatalf("after one outcome each: %d %d %d", a, b, c)
	}
	// Clamp at 0: a referral at 1.0 takes 1.0 and records -10.
	mustExec(t, r.db, "UPDATE student_rating SET value_tenths = 10 WHERE user_id = 3")
	r.outcome(t, 3, r.sub(t, 3, "B", 100, 10), OutcomeReferred, "")
	var delta int
	r.db.QueryRow("SELECT delta_tenths FROM rating_history WHERE user_id = 3 ORDER BY id DESC LIMIT 1").Scan(&delta)
	if v := r.rating(t, 3); v != 0 || delta != -10 {
		t.Errorf("clamped at 0: %d, delta %d", v, delta)
	}
	// Edit to 10.0 after the warning, then the warning is overturned: the
	// rating stays at 10.0.
	if pending, err := r.svc.EditRating(ctx, 1, 100, 100, "Restored after a conversation with the student."); err != nil || pending {
		t.Fatalf("edit up: %v %v", pending, err)
	}
	a, err := r.svc.FileAppeal(ctx, 1, AppealInput{CaseID: w.ID, Text: "I can show my notes from the lab session."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Rule(ctx, a.ID, 102, true, forStudent, ""); err != nil {
		t.Fatal(err)
	}
	if v := r.rating(t, 1); v != 100 {
		t.Errorf("edit to 100 then an upheld appeal: %d", v)
	}
	// Outcome (8.5), edit to 7.0, upheld appeal: 7.0 plus the 1.5 back.
	if pending, err := r.svc.EditRating(ctx, 2, 100, 70, "Lowered after a second conversation."); err != nil || pending {
		t.Fatalf("edit within the band: %v %v", pending, err)
	}
	a, _ = r.svc.FileAppeal(ctx, 2, AppealInput{CaseID: z.ID, Text: "I can show my notes from the lab session."})
	if _, err := r.svc.Rule(ctx, a.ID, 102, true, forStudent, ""); err != nil {
		t.Fatal(err)
	}
	if v := r.rating(t, 2); v != 85 {
		t.Errorf("outcome, edit, upheld appeal: %d, want 85", v)
	}
	// A band-lowering edit waits for a second organiser.
	*r.clock = r.clock.Add(time.Minute)
	pending, err := r.svc.EditRating(ctx, 2, 100, 50, "Repeated problems this term, discussed with the student.")
	if err != nil || !pending || r.rating(t, 2) != 85 {
		t.Fatalf("band-lowering edit: pending %v err %v rating %d", pending, err, r.rating(t, 2))
	}
	p, _ := r.svc.Pending(ctx, 2, 100)
	if p == nil || p.MayConfirm || p.ToTenths != 50 {
		t.Fatalf("pending %+v", p)
	}
	if err := r.svc.ConfirmRating(ctx, 2, p.ID, 100, ""); !errors.Is(err, ErrTwoPersonRule) {
		t.Errorf("the proposer confirmed: %v", err)
	}
	if err := r.svc.ConfirmRating(ctx, 2, p.ID, 101, ""); err != nil {
		t.Fatal(err)
	}
	if v := r.rating(t, 2); v != 50 {
		t.Errorf("after the confirmation: %d", v)
	}
	sd, _ := r.svc.StandingOf(ctx, 2)
	if sd.Band != BandRepeated || sd.Lines[0].Kind != "adjustment" || sd.Lines[0].Text != "Standing adjusted by an organiser." {
		t.Errorf("standing %+v", sd)
	}
	// The adjustment can be appealed: upheld, its change is reversed.
	a, err = r.svc.FileAppeal(ctx, 2, AppealInput{AdjustmentID: *sd.Lines[0].AdjustmentID, Text: "The conversation did not happen like that."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Rule(ctx, a.ID, 101, true, forStudent, ""); !errors.Is(err, ErrUninvolved) {
		t.Errorf("the confirmer ruled on the adjustment: %v", err)
	}
	if _, err := r.svc.Rule(ctx, a.ID, 102, true, forStudent, ""); err != nil {
		t.Fatal(err)
	}
	if v := r.rating(t, 2); v != 85 {
		t.Errorf("after the upheld adjustment appeal: %d", v)
	}
	// A proposal confirmed after the rating moved is refused.
	r.svc.EditRating(ctx, 1, 100, 40, "Lowered for repeated problems, discussed.")
	p, _ = r.svc.Pending(ctx, 1, 101)
	r.outcome(t, 1, r.sub(t, 1, "B", 100, 10), OutcomeWarning, "")
	if err := r.svc.ConfirmRating(ctx, 1, p.ID, 101, ""); !errors.Is(err, ErrRatingChanged) {
		t.Errorf("confirmed after the rating moved: %v", err)
	}
	if err := r.svc.DeclineRating(ctx, 1, p.ID, 101); err != nil {
		t.Errorf("decline: %v", err)
	}
	// Co-organisers never reach EditRating (the route checks the role);
	// nobody edits their own.
	if _, err := r.svc.EditRating(ctx, 100, 100, 50, "Nobody edits their own rating, ever."); err == nil {
		t.Error("edited own rating")
	}
}

// Concurrent writes to one rating go one at a time through the write
// gate: every change is applied once and recorded with the value it made.
func TestRatingWritesSerialise(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := r.db.BeginTx(ctx, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer tx.Rollback()
			if _, _, err := applyRatingTx(ctx, tx, 1, -1, ratingCause{cause: CauseManual, at: 1, actorID: 100, reason: "step"}); err != nil {
				t.Error(err)
				return
			}
			if err := tx.Commit(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if v := r.rating(t, 1); v != 80 {
		t.Errorf("after 20 concurrent steps: %d", v)
	}
	var distinct int
	r.db.QueryRow("SELECT count(DISTINCT new_value_tenths) FROM rating_history WHERE user_id = 1").Scan(&distinct)
	if distinct != 20 {
		t.Errorf("history values: %d distinct", distinct)
	}
}

// The semester rollover adds 0.5 once per label, skips a student with an
// outcome in the period, and is applied to them once the outcome is
// overturned.
func TestRolloverOncePerLabel(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	mustExec(t, r.db, "INSERT INTO student_rating (user_id, value_tenths, updated_at) VALUES (2, 80, 0), (3, 98, 0)")
	k := r.outcome(t, 1, r.sub(t, 1, "A", 100, 10), OutcomeWarning, "")
	*r.clock = r.clock.Add(time.Hour)
	dry, err := r.svc.Rollover(ctx, "2026-odd", false)
	if err != nil || dry.Applied || dry.Blocked != 1 {
		t.Fatalf("dry run %+v %v", dry, err)
	}
	if r.rating(t, 2) != 80 {
		t.Error("the dry run wrote")
	}
	rep, err := r.svc.Rollover(ctx, "2026-odd", true)
	if err != nil || !rep.Applied || rep.Blocked != 1 {
		t.Fatalf("rollover %+v %v", rep, err)
	}
	if a, b, c := r.rating(t, 1), r.rating(t, 2), r.rating(t, 3); a != 95 || b != 85 || c != 100 {
		t.Errorf("after the rollover: %d %d %d", a, b, c)
	}
	again, err := r.svc.Rollover(ctx, "2026-odd", true)
	if err != nil || !again.AlreadyDone || r.rating(t, 2) != 85 {
		t.Errorf("second run %+v %v, rating %d", again, err, r.rating(t, 2))
	}
	// The warning is overturned after the rollover: 0.5 back, plus the
	// rollover it blocked.
	a, _ := r.svc.FileAppeal(ctx, 1, AppealInput{CaseID: k.ID, Text: "I can show my notes from the lab session."})
	if _, err := r.svc.Rule(ctx, a.ID, 102, true, forStudent, ""); err != nil {
		t.Fatal(err)
	}
	var n int
	r.db.QueryRow("SELECT count(*) FROM rating_history WHERE user_id = 1 AND cause = 'rollover' AND semester_label = '2026-odd'").Scan(&n)
	if v := r.rating(t, 1); v != 100 || n != 1 {
		t.Errorf("after the upheld appeal: %d, rollover rows %d", v, n)
	}
}

// Every limit with a fixed clock: the 21-day cap, the reminder on day 5,
// 7 days from first opening, 14 days to decide, 14 to appeal, 14 to rule,
// and a notice held while the student is in a live contest.
func TestDeadlinesWithAFixedClock(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	day := 24 * time.Hour
	ref, _ := r.svc.Submission(ctx, r.sub(t, 1, "A", 100, 10))
	c, _ := r.svc.OpenManualCase(ctx, ref, 100, "Problem A, a question", reason)
	r.svc.Decide(ctx, c.ID, 100, AwaitingStudent, reason, nil)
	shared := r.clock.UnixMilli()
	got, _ := r.svc.GetCase(ctx, c.ID)
	if u := got.Users[0]; *u.NoticeAvailableAt != shared || *u.RespondBy != shared+ReplyCap.Milliseconds() {
		t.Fatalf("not opened: %+v", u)
	}
	*r.clock = r.clock.Add(ReminderAfter - time.Minute)
	if n, _ := r.svc.SweepReminders(ctx); n != 0 {
		t.Errorf("reminder before day 5: %d", n)
	}
	*r.clock = r.clock.Add(time.Minute)
	if n, _ := r.svc.SweepReminders(ctx); n != 1 {
		t.Errorf("reminder on day 5: %d", n)
	}
	if n, _ := r.svc.SweepReminders(ctx); n != 0 {
		t.Errorf("a second reminder: %d", n)
	}
	// Opened on day 6: 7 days from then, inside the cap.
	*r.clock = r.clock.Add(day)
	r.svc.Opened(ctx, c.ID, 1)
	got, _ = r.svc.GetCase(ctx, c.ID)
	if u := got.Users[0]; *u.RespondBy != r.clock.UnixMilli()+ResponseWindow.Milliseconds() {
		t.Errorf("opened on day 6: respond by %d", *u.RespondBy)
	}
	// Opened on day 20 of another share: the cap wins.
	*r.clock = r.clock.Add(ResponseWindow + time.Millisecond)
	got, _ = r.svc.GetCase(ctx, c.ID)
	if got.ReadyAt == nil || *got.DecideBy != *got.ReadyAt+DecideWindow.Milliseconds() || got.Overdue {
		t.Fatalf("ready: %v %v %v", got.ReadyAt, got.DecideBy, got.Overdue)
	}
	*r.clock = time.UnixMilli(*got.DecideBy + 1)
	got, _ = r.svc.GetCase(ctx, c.ID)
	if !got.Overdue {
		t.Error("not overdue after 14 days")
	}
	// The cap: a student who opens on day 20 has one day left.
	ref2, _ := r.svc.Submission(ctx, r.sub(t, 2, "A", 100, 10))
	c2, _ := r.svc.OpenManualCase(ctx, ref2, 100, "Problem A, a question", reason)
	r.svc.Decide(ctx, c2.ID, 100, AwaitingStudent, reason, nil)
	avail := r.clock.UnixMilli()
	*r.clock = r.clock.Add(20 * day)
	r.svc.Opened(ctx, c2.ID, 2)
	got, _ = r.svc.GetCase(ctx, c2.ID)
	if *got.Users[0].RespondBy != avail+ReplyCap.Milliseconds() {
		t.Errorf("cap: %d, want %d", *got.Users[0].RespondBy, avail+ReplyCap.Milliseconds())
	}
	// Appeals: 14 days from the decision, then 14 to rule.
	k := r.outcome(t, 3, r.sub(t, 3, "A", 100, 10), OutcomeWarning, "")
	decided := k.Decisions[len(k.Decisions)-1].CreatedAt
	*r.clock = time.UnixMilli(decided + AppealWindow.Milliseconds())
	if _, err := r.svc.FileAppeal(ctx, 3, AppealInput{CaseID: k.ID, Text: "I can show my notes from the lab session."}); err == nil {
		t.Error("appealed after 14 days")
	}
	*r.clock = time.UnixMilli(decided + AppealWindow.Milliseconds() - 1)
	a, err := r.svc.FileAppeal(ctx, 3, AppealInput{CaseID: k.ID, Text: "I can show my notes from the lab session."})
	if err != nil || a.DecideBy != a.CreatedAt+RulingWindow.Milliseconds() || a.Overdue {
		t.Fatalf("appeal %+v %v", a, err)
	}
	*r.clock = time.UnixMilli(a.DecideBy + 1)
	list, _ := r.svc.Appeals(ctx, true)
	if len(list) != 1 || !list[0].Overdue {
		t.Errorf("open appeals %+v", list)
	}
}

// A case shared while the student is in a live contest: the notice and
// the case wait until their window closes; nothing is delivered before.
func TestNoticesHeldDuringALiveContest(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	now := r.clock.UnixMilli()
	res := mustExec(t, r.db, `INSERT INTO contests (slug, title, starts_at, ends_at, published, created_at, updated_at)
		VALUES ('live', 'Live one', ?, ?, 1, 0, 0)`, now-3600000, now+3600000)
	live, _ := res.LastInsertId()
	mustExec(t, r.db, "INSERT INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (?, 1, ?, ?)", live, now-60000, now-60000)
	inbox := &notifications.Service{DB: r.db, Now: func() time.Time { return *r.clock }, Live: r.contests.InLiveContest, Due: r.svc.NoticeDue}
	ref, _ := r.svc.Submission(ctx, r.sub(t, 1, "A", 100, 10))
	c, _ := r.svc.OpenManualCase(ctx, ref, 100, "Problem A, a question", reason)
	if _, err := r.svc.Decide(ctx, c.ID, 100, AwaitingStudent, reason, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetCase(ctx, c.ID)
	if *got.Users[0].NoticeAvailableAt != now+3600000 {
		t.Errorf("available at %d, want the contest's end", *got.Users[0].NoticeAvailableAt)
	}
	page, err := inbox.List(ctx, 1, 0)
	if err != nil || !page.Held || len(page.Notices) != 0 || page.Unread != 0 {
		t.Fatalf("during the contest: %+v %v", page, err)
	}
	if n, _ := inbox.Unread(ctx, 1); n != 0 {
		t.Errorf("unread during the contest: %d", n)
	}
	if mine, _ := r.svc.MyCases(ctx, 1); len(mine) != 0 {
		t.Errorf("case visible during the contest: %+v", mine)
	}
	*r.clock = r.clock.Add(time.Hour + time.Minute)
	page, err = inbox.List(ctx, 1, 0)
	if err != nil || page.Held || len(page.Notices) != 1 || page.Unread != 1 {
		t.Fatalf("after the contest: %+v %v", page, err)
	}
	n := page.Notices[0]
	if n.Kind != notifications.CaseShared || n.DueAt == nil || *n.DueAt != now+3600000+ReplyCap.Milliseconds() ||
		n.Happened != "An organiser asked you to explain your submission to problem A in Week 6." || n.Reassurance == "" {
		t.Errorf("notice %+v", n)
	}
	if mine, _ := r.svc.MyCases(ctx, 1); len(mine) != 1 || mine[0].Evidence[0].Kind != KindManual || mine[0].Evidence[0].Source == "" {
		t.Errorf("case after the contest: %+v", mine)
	}
	js, _ := json.Marshal(page)
	for _, bad := range []string{"Student 1", "s1@", "int main", "Organiser", reason} {
		if strings.Contains(string(js), bad) {
			t.Errorf("notice carries %q: %s", bad, js)
		}
	}
	if err := inbox.MarkRead(ctx, 1, []int64{n.ID}, false); err != nil {
		t.Fatal(err)
	}
	if u, _ := inbox.Unread(ctx, 1); u != 0 {
		t.Errorf("unread after reading: %d", u)
	}
}

// Notes: reviewers only, never on one's own submission, kept with a case.
func TestNotesAndRecordViews(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	ref, _ := r.svc.Submission(ctx, r.sub(t, 1, "A", 100, 10))
	if _, err := r.svc.AddNote(ctx, ref, 1, "My own note on my own code.", nil); err == nil {
		t.Error("a note on one's own submission")
	}
	if _, err := r.svc.AddNote(ctx, ref, 100, "Lines 3 to 5 look like the editorial.", []LineRange{{3, 2}}); err == nil {
		t.Error("a backwards range was accepted")
	}
	n, err := r.svc.AddNote(ctx, ref, 100, "Lines 3 to 5 look like the editorial.", []LineRange{{3, 5}})
	if err != nil || n.ExpiresAt == nil || *n.ExpiresAt != n.CreatedAt+NoteRetention.Milliseconds() {
		t.Fatalf("note %+v %v", n, err)
	}
	if _, err := r.svc.OpenManualCase(ctx, ref, 100, "Problem A, a question", reason); err != nil {
		t.Fatal(err)
	}
	notes, _ := r.svc.Notes(ctx, ref.ID)
	if len(notes) != 1 || notes[0].ExpiresAt != nil {
		t.Errorf("note after the case: %+v", notes)
	}
	r.svc.LogView(ctx, 100, 1, ViewNotes, ref.ID)
	r.svc.LogView(ctx, 1, 1, ViewRecord, 0)
	views, _, _ := r.svc.RecordViews(ctx, 1, 0, 0)
	if len(views) != 1 || views[0].Viewer.ID != 100 || views[0].Kind != ViewNotes {
		t.Errorf("views %+v", views)
	}
}

// In a pair case the outcome is for one student: the other reads the
// decision as cleared, on their notice and on their standing.
func TestPairCaseOutcomeForOneStudent(t *testing.T) {
	r := newStudentsRig(t)
	ctx := context.Background()
	c := copyCase(t, r.rig)
	r.svc.Decide(ctx, c.ID, 100, AwaitingStudent, reason, nil)
	for _, u := range []int64{2, 7} {
		r.svc.Explain(ctx, c.ID, u, "My account of how I wrote it.")
	}
	if _, err := r.svc.Propose(ctx, c.ID, 100, ProposeInput{Outcome: OutcomeWarning, UserIDs: []int64{7}, Note: reason, Reason: forStudent,
		Seen: seenAll(t, r.rig, c.ID)}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetCase(ctx, c.ID)
	if _, err := r.svc.Confirm(ctx, c.ID, got.Proposals[0].ID, 101, reason, "", seenAll(t, r.rig, c.ID)); err != nil {
		t.Fatal(err)
	}
	inbox := &notifications.Service{DB: r.db, Now: func() time.Time { return *r.clock }}
	for u, want := range map[int64]string{7: "is decided: a warning.", 2: "is closed with no action."} {
		page, _ := inbox.List(ctx, u, 0)
		if len(page.Notices) == 0 || !strings.HasSuffix(page.Notices[0].Happened, want) {
			t.Errorf("student %d: %+v", u, page.Notices)
		}
	}
	if a, b := r.rating(t, 7), r.rating(t, 2); a != 95 || b != 100 {
		t.Errorf("ratings 7=%d 2=%d", a, b)
	}
	if sd, _ := r.svc.StandingOf(ctx, 2); len(sd.Lines) != 0 {
		t.Errorf("the cleared student has lines: %+v", sd.Lines)
	}
}
