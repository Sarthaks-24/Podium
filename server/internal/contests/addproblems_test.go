package contests

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// bankProblem adds a validated problem with one revision pinning its
// current content, and returns its id.
func bankProblem(t *testing.T, r *svcRig, id int64) int64 {
	t.Helper()
	h := fmt.Sprintf("h%d", id)
	execNoFK(t, r,
		fmt.Sprintf(`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash, validated_hash)
			VALUES (%d, 'p%d', 'Problem %d', 1000, 256, 'tokens', 'cpp17', 0, 0, '%s', '%s')`, id, id, id, h, h),
		fmt.Sprintf(`INSERT INTO problem_revisions (id, problem_id, rev, content_hash, title, created_at, subtasks)
			VALUES (%d, %d, 1, '%s', 'Problem %d', 0, '[{"idx":1,"points":100}]')`, id, id, h, id),
		fmt.Sprintf(`INSERT INTO subtasks (problem_id, idx, points) VALUES (%d, 1, 100)`, id))
	return id
}

func labels(t *testing.T, r *svcRig, contestID int64) string {
	t.Helper()
	rows, err := r.svc.DB.Query("SELECT label FROM contest_problems WHERE contest_id = ? ORDER BY ord", contestID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := ""
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		out += l
	}
	return out
}

func TestAddProblemsGivesNextFreeLettersAndKeepsPins(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	bankProblem(t, r, 1)
	bankProblem(t, r, 2)
	bankProblem(t, r, 3)
	if _, err := r.svc.SetProblems(ctx, c.ID, []ProblemChoice{{ProblemID: 1, Label: "A1"}, {ProblemID: 2, Label: "B"}}, owner, nil); err != nil {
		t.Fatal(err)
	}
	execNoFK(t, r, "INSERT INTO problem_revisions (id, problem_id, rev, content_hash, title, created_at, subtasks) VALUES (50, 1, 2, 'h1', 'x', 0, '[]')")
	res, err := r.svc.AddProblems(ctx, c.ID, []int64{3}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || res.Added[0].Label != "A" || len(res.Refused) != 0 {
		t.Fatalf("added %+v refused %+v, want label A (A1 blocks only A1)", res.Added, res.Refused)
	}
	if got := labels(t, r, c.ID); got != "A1BA" {
		t.Errorf("labels %q, want A1BA in order", got)
	}
	if rev := count(t, r, "SELECT revision_id FROM contest_problems WHERE contest_id = ? AND problem_id = 1", c.ID); rev != 1 {
		t.Errorf("existing row revision %d, want 1 kept", rev)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = 'problems'", c.ID); n != 2 {
		t.Errorf("%d problems audit entries, want 2 (set and add)", n)
	}
}

func TestAddProblemsManyAndNothingAdded(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	for i := int64(1); i <= 3; i++ {
		bankProblem(t, r, i)
	}
	res, err := r.svc.AddProblems(ctx, c.ID, []int64{1, 2, 3}, owner)
	if err != nil || len(res.Added) != 3 || res.Added[2].Label != "C" {
		t.Fatalf("%+v %v", res, err)
	}
	audits := count(t, r, "SELECT count(*) FROM contest_audit WHERE action = 'problems'")
	res, err = r.svc.AddProblems(ctx, c.ID, []int64{1}, owner)
	if err != nil || len(res.Added) != 0 || res.Refused[0].Reason != RefuseAlreadyAdded {
		t.Fatalf("%+v %v", res, err)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_audit WHERE action = 'problems'"); n != audits {
		t.Errorf("an add that added nothing wrote an audit entry")
	}
	if res.Added == nil || res.Refused == nil {
		t.Error("lists must not be nil")
	}
}

func TestAddProblemsRefusalCodes(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	for i := int64(1); i <= 8; i++ {
		bankProblem(t, r, i)
	}
	execNoFK(t, r,
		"UPDATE problems SET validated_hash = 'old' WHERE id = 2",
		"UPDATE problems SET deleted = 1 WHERE id = 3",
		"UPDATE problems SET archived = 1 WHERE id = 4",
		`UPDATE problems SET tags = '["ai draft"]' WHERE id = 5`,
		"UPDATE problems SET origin = 'ai', solution_by_person = 0 WHERE id = 6",
		`INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('validate', 88, 'queued', '{"problem_id":7,"proof":{"round":"r"}}', 0)`)
	res, err := r.svc.AddProblems(ctx, c.ID, []int64{1, 1, 2, 3, 4, 5, 6, 7, 99}, owner)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{1: RefuseAlreadyAdded, 2: RefuseNotValidated, 3: RefuseDeleted, 4: RefuseNotFound,
		5: RefuseAIDraft, 6: RefuseNeedsAI, 7: RefuseBeingChecked, 99: RefuseNotFound}
	if len(res.Added) != 1 || res.Added[0].ProblemID != 1 {
		t.Errorf("added %+v", res.Added)
	}
	for _, rf := range res.Refused {
		if want[rf.ProblemID] != rf.Reason {
			t.Errorf("problem %d: %q, want %q", rf.ProblemID, rf.Reason, want[rf.ProblemID])
		}
		delete(want, rf.ProblemID)
	}
	if len(want) != 0 {
		t.Errorf("no refusal for %v", want)
	}
}

func TestAddProblemsLimit(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	for i := int64(1); i <= 27; i++ {
		bankProblem(t, r, i)
	}
	ids := []int64{}
	for i := int64(1); i <= 26; i++ {
		ids = append(ids, i)
	}
	if res, err := r.svc.AddProblems(ctx, c.ID, ids, owner); err != nil || len(res.Added) != 26 || res.Added[25].Label != "Z" {
		t.Fatalf("%+v %v", res, err)
	}
	res, err := r.svc.AddProblems(ctx, c.ID, []int64{27}, owner)
	if err != nil || len(res.Refused) != 1 || res.Refused[0].Reason != RefuseLimit {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestAddProblemsUnknownContest(t *testing.T) {
	r := newSvc(t)
	if _, err := r.svc.AddProblems(context.Background(), 999, []int64{1}, owner); !errors.Is(err, ErrNotFound) {
		t.Errorf("err %v, want ErrNotFound", err)
	}
}

func TestAddProblemsPublishesToALiveContest(t *testing.T) {
	r := newSvc(t)
	var changed []int64
	r.svc.Changed = func(id int64) { changed = append(changed, id) }
	c := r.contest(t, nil)
	bankProblem(t, r, 1)
	if _, err := r.svc.AddProblems(context.Background(), c.ID, []int64{1}, owner); err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0] != c.ID {
		t.Errorf("Changed calls %v, want one for the contest", changed)
	}
}

func TestSetProblemsGuard(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	for i := int64(1); i <= 3; i++ {
		bankProblem(t, r, i)
	}
	two := []ProblemChoice{{ProblemID: 1, Label: "A"}}
	loaded := []int64{1}
	if _, err := r.svc.SetProblems(ctx, c.ID, two, owner, &[]int64{}); err != nil {
		t.Fatalf("an empty contest with [] must save: %v", err)
	}
	// An add, then a stale save: 409 and the added row stays.
	if _, err := r.svc.AddProblems(ctx, c.ID, []int64{2}, owner); err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.SetProblems(ctx, c.ID, two, owner, &loaded)
	if !errors.Is(err, ErrProblemsChanged) {
		t.Fatalf("stale save: %v, want ErrProblemsChanged", err)
	}
	if got := labels(t, r, c.ID); got != "AB" {
		t.Errorf("labels %q after the refusal, want AB", got)
	}
	// After Reload (the new list) the save works.
	both := []ProblemChoice{{ProblemID: 1, Label: "A"}, {ProblemID: 2, Label: "B"}}
	if _, err := r.svc.SetProblems(ctx, c.ID, both, owner, &[]int64{1, 2}); err != nil {
		t.Fatalf("save after reload: %v", err)
	}
	// A pin update does not change the ids, so it does not trip the guard.
	execNoFK(t, r, "UPDATE contest_problems SET revision_id = 1 WHERE problem_id = 1")
	if _, err := r.svc.SetProblems(ctx, c.ID, both, owner, &[]int64{1, 2}); err != nil {
		t.Fatalf("save after a pin update: %v", err)
	}
	// A bare array (nil guard) behaves as before: it replaces everything.
	if _, err := r.svc.SetProblems(ctx, c.ID, two, owner, nil); err != nil {
		t.Fatal(err)
	}
	if got := labels(t, r, c.ID); got != "A" {
		t.Errorf("labels %q after an unguarded save, want A", got)
	}
}

func TestSetProblemsGuardOnAnEmptyContest(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	bankProblem(t, r, 1)
	if _, err := r.svc.AddProblems(ctx, c.ID, []int64{1}, owner); err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.SetProblems(ctx, c.ID, []ProblemChoice{}, owner, &[]int64{})
	if !errors.Is(err, ErrProblemsChanged) {
		t.Fatalf("stale save with []: %v, want ErrProblemsChanged", err)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_problems WHERE contest_id = ?", c.ID); n != 1 {
		t.Errorf("%d rows, want the added one kept", n)
	}
}

// An AddProblems that commits just before the save: the save was started
// from the older list and is refused. The immediate transaction lock makes
// the two run one after the other, so the save sees the added row.
func TestSetProblemsGuardRaceWithAdd(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	bankProblem(t, r, 1)
	bankProblem(t, r, 2)
	if _, err := r.svc.SetProblems(ctx, c.ID, []ProblemChoice{{ProblemID: 1, Label: "A"}}, owner, &[]int64{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.svc.AddProblems(ctx, c.ID, []int64{2}, owner)
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_, err := r.svc.SetProblems(ctx, c.ID, []ProblemChoice{{ProblemID: 1, Label: "A"}}, owner, &[]int64{1})
	if !errors.Is(err, ErrProblemsChanged) {
		t.Fatalf("save after a committed add: %v", err)
	}
	if !isBusy(errors.New("database is locked (5) (SQLITE_BUSY)")) || isBusy(errors.New("other")) {
		t.Error("isBusy misreads errors")
	}
}
