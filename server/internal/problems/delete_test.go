package problems

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

var (
	ownerActor = auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner}
	adminActor = auth.Actor{ID: 2, Email: "admin@college.edu", Role: auth.Admin}
)

// deleteBank is a bank with the three people the rules name, a fixed clock
// and the clock's "now".
func deleteBank(t *testing.T) (*Service, time.Time) {
	t.Helper()
	s := newBank(t)
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	if _, err := s.DB.Exec(`INSERT INTO users (id, email, role, created_at) VALUES
		(1, 'owner@college.edu', 'owner', 0), (2, 'admin@college.edu', 'admin', 0), (3, 'organiser@college.edu', 'organiser', 0),
		(4, 'asha@college.edu', 'student', 0)`); err != nil {
		t.Fatal(err)
	}
	return s, now
}

// holdIn pins the problem in a new contest that starts and ends at the
// offsets from now (hours), published or not, with practice on.
func holdIn(t *testing.T, s *Service, now time.Time, problemID int64, id int64, slug string, startH, endH int, published int) {
	t.Helper()
	if _, err := s.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, 0, 0)`, id, slug, "Contest "+slug, now.Add(time.Duration(startH)*time.Hour).UnixMilli(),
		now.Add(time.Duration(endH)*time.Hour).UnixMilli(), published); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id)
		VALUES (?, ?, 'A', 0, (SELECT max(id) FROM problem_revisions WHERE problem_id = ?))`, id, problemID, problemID); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, s *Service, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func codeOf(err error) string {
	var na *NotAllowedError
	var un *UnendedContestsError
	switch {
	case errors.Is(err, ErrDeleted):
		return "deleted"
	case errors.As(err, &na):
		return "not_allowed:" + na.Message
	case errors.As(err, &un):
		return "in_unended_contest:" + un.Error()
	case err == nil:
		return ""
	}
	return err.Error()
}

// An unused problem: an admin may delete it. The row stays as a tombstone,
// its slug is freed and moved, the working data is gone, no revision is
// made, and the audit row names the person and the problem.
func TestAdminDeletesAnUnusedProblem(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	revs := count(t, s, "SELECT count(*) FROM problem_revisions WHERE problem_id = ?", p.ID)
	revTests := count(t, s, "SELECT count(*) FROM revision_tests")
	if err := s.Delete(ctx, p.ID, adminActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, p.ID)
	if err != nil || !got.Deleted || !got.Archived {
		t.Fatalf("tombstone: %+v %v", got, err)
	}
	if got.Slug != "a-plus-b-deleted-"+itoa(p.ID) || got.ArchivedSlug != "a-plus-b" {
		t.Errorf("slug %q, archived_slug %q", got.Slug, got.ArchivedSlug)
	}
	if len(got.Tests) != 0 || len(got.Subtasks) != 0 || got.Reference.Source != "" || got.Brute != nil || got.Validator != nil || got.Generator != nil || got.CheckerSource != "" {
		t.Errorf("working data left: tests %d subtasks %d", len(got.Tests), len(got.Subtasks))
	}
	if n := count(t, s, "SELECT count(*) FROM problem_revisions WHERE problem_id = ?", p.ID); n != revs {
		t.Errorf("revisions %d became %d: delete must not save", revs, n)
	}
	if n := count(t, s, "SELECT count(*) FROM revision_tests"); n != revTests {
		t.Errorf("revision_tests %d became %d", revTests, n)
	}
	var by int64
	var at int64
	if err := s.DB.QueryRow("SELECT deleted_by, deleted_at FROM problems WHERE id = ?", p.ID).Scan(&by, &at); err != nil || by != 2 || at == 0 {
		t.Errorf("deleted_by %d deleted_at %d %v", by, at, err)
	}
	var email, details string
	if err := s.DB.QueryRow("SELECT actor_email, details FROM site_audit WHERE action = 'problem_deleted'").Scan(&email, &details); err != nil {
		t.Fatal(err)
	}
	if email != "admin@college.edu" || !strings.Contains(details, `"slug":"a-plus-b"`) || strings.Contains(details, "statement") {
		t.Errorf("audit actor %q details %s", email, details)
	}
	list, _ := s.List(ctx, true)
	if len(list) != 0 {
		t.Errorf("bank still lists %d (archived included)", len(list))
	}
	deleted, err := s.ListDeleted(ctx)
	if err != nil || len(deleted) != 1 || deleted[0].Slug != "a-plus-b" || deleted[0].DeletedBy != "admin@college.edu" || deleted[0].Used {
		t.Errorf("deleted list %+v %v", deleted, err)
	}
	fkClean(t, s)
	// The slug is free for a new problem.
	if _, err := s.Create(ctx, "A plus B", "a-plus-b", 3, ""); err != nil {
		t.Errorf("slug not freed: %v", err)
	}
}

// fkClean fails when the database has a dangling reference or fails its integrity check.
func fkClean(t *testing.T, s *Service) {
	t.Helper()
	if n := count(t, s, "SELECT count(*) FROM pragma_foreign_key_check"); n != 0 {
		t.Errorf("foreign_key_check reports %d rows", n)
	}
	var ok string
	if err := s.DB.QueryRow("PRAGMA integrity_check").Scan(&ok); err != nil || ok != "ok" {
		t.Errorf("integrity_check: %q %v", ok, err)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// The roles and the used rule (3.2): an admin is refused a used problem, with
// the exact copy; the owner may delete it; a role below admin is refused;
// practice and runs count as use, validation does not.
func TestWhoMayDelete(t *testing.T) {
	s, now := deleteBank(t)
	ctx := context.Background()

	inContest := importExample(t, s, "a-plus-b")
	holdIn(t, s, now, inContest.ID, 1, "ended", -5, -3, 1)
	err := s.Delete(ctx, inContest.ID, adminActor)
	want := "not_allowed:Only the site owner can delete a problem that has been used. Archive it instead."
	if codeOf(err) != want {
		t.Errorf("admin on a used problem: %q, want %q", codeOf(err), want)
	}
	if got, _ := s.Get(ctx, inContest.ID); got.Deleted {
		t.Error("the refused delete changed the problem")
	}

	submitted := importExample(t, s, "pair-with-sum")
	put(t, s, "x")
	if _, err := s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at)
		VALUES (?, 4, 'run', 'cpp17', 'x', (SELECT sha FROM blobs LIMIT 1), 1)`, submitted.ID); err != nil {
		t.Fatal(err)
	}
	if got := codeOf(s.Delete(ctx, submitted.ID, adminActor)); got != want {
		t.Errorf("admin on a problem with only a run: %q", got)
	}

	// A validation job is not use.
	validated := importExample(t, s, "average-speed")
	if _, err := s.RequestValidation(ctx, validated.ID, 3); err != nil {
		t.Fatal(err)
	}
	for _, role := range []auth.Role{auth.Student, auth.Organiser} {
		if got := codeOf(s.Delete(ctx, validated.ID, auth.Actor{ID: 3, Email: "o@college.edu", Role: role})); got != "not_allowed:You do not have access to this." {
			t.Errorf("%s: %q", role, got)
		}
	}
	if err := s.Delete(ctx, validated.ID, adminActor); err != nil {
		t.Errorf("admin on a problem with only a validation job: %v", err)
	}

	if err := s.Delete(ctx, inContest.ID, ownerActor); err != nil {
		t.Errorf("owner on a used problem: %v", err)
	}
	if err := s.Delete(ctx, submitted.ID, ownerActor); err != nil {
		t.Errorf("owner on a submitted problem: %v", err)
	}
	fkClean(t, s)
}

// Nobody, owner included, deletes a problem while a contest holding it has
// not ended: draft, scheduled and running each refuse with 409 naming them.
func TestNobodyDeletesWhileAContestHasNotEnded(t *testing.T) {
	s, now := deleteBank(t)
	ctx := context.Background()
	for name, c := range map[string]struct {
		start, end, published int
	}{"draft": {1, 5, 0}, "scheduled": {2, 6, 1}, "running": {-1, 3, 1}} {
		p := importExample(t, s, "a-plus-b")
		holdIn(t, s, now, p.ID, p.ID, name, c.start, c.end, c.published)
		err := s.Delete(ctx, p.ID, ownerActor)
		var un *UnendedContestsError
		if !errors.As(err, &un) || len(un.Contests) != 1 || un.Contests[0].Slug != name {
			t.Errorf("%s as owner: %v", name, err)
		}
		// An admin is told about the role first: the contest makes it used.
		if got := codeOf(s.Delete(ctx, p.ID, adminActor)); !strings.HasPrefix(got, "not_allowed:Only the site owner") {
			t.Errorf("%s as admin: %q", name, got)
		}
		want := "This problem is in contests that have not ended: Contest " + name + ". Remove it from them or wait until they end."
		if err := s.Delete(ctx, p.ID, ownerActor); err == nil || err.Error() != want {
			t.Errorf("%s copy: %v", name, err)
		}
		if got, _ := s.Get(ctx, p.ID); got.Deleted {
			t.Errorf("%s: the refused delete changed the problem", name)
		}
		// The contest ends: now the owner may delete.
		if _, err := s.DB.Exec("UPDATE contests SET starts_at = ?, ends_at = ? WHERE id = ?", now.Add(-5*time.Hour).UnixMilli(), now.Add(-time.Hour).UnixMilli(), p.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Delete(ctx, p.ID, ownerActor); err != nil {
			t.Errorf("%s ended: %v", name, err)
		}
		// Make the next problem's slug free: the imports reuse a-plus-b.
	}
}

// Deleting an archived problem keeps the true original slug in
// archived_slug, and a second delete is refused.
func TestDeletingAnArchivedProblemKeepsItsOriginalSlug(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	if err := s.Archive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, p.ID, ownerActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, p.ID)
	if got.ArchivedSlug != "a-plus-b" || got.Slug != "a-plus-b-deleted-"+itoa(p.ID) {
		t.Errorf("slug %q archived_slug %q", got.Slug, got.ArchivedSlug)
	}
	if err := s.Delete(ctx, p.ID, ownerActor); !errors.Is(err, ErrDeleted) {
		t.Errorf("second delete: %v", err)
	}
	if err := s.Delete(ctx, 999, ownerActor); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing problem: %v", err)
	}
}

// Any failure after the audit insert rolls the whole delete back: the audit
// row, the tombstone, the slug and the tests.
func TestFailedDeleteChangesNothing(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	if _, err := s.DB.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON subtasks BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, p.ID, ownerActor); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("want the injected failure, got %v", err)
	}
	if n := count(t, s, "SELECT count(*) FROM site_audit WHERE action = 'problem_deleted'"); n != 0 {
		t.Errorf("audit rows left: %d", n)
	}
	got, _ := s.Get(ctx, p.ID)
	if got.Deleted || got.Archived || got.Slug != "a-plus-b" || len(got.Tests) != 6 || got.Reference.Source == "" {
		t.Errorf("problem changed: deleted %v slug %q tests %d", got.Deleted, got.Slug, len(got.Tests))
	}
}

// Blobs: an unused problem's tests are collected, a used problem's pinned
// revision keeps its tests.
func TestDeleteFreesUnusedBlobsAndKeepsPinnedOnes(t *testing.T) {
	s, now := deleteBank(t)
	ctx := context.Background()
	unused := importExample(t, s, "a-plus-b")
	used := importExample(t, s, "pair-with-sum")
	holdIn(t, s, now, used.ID, 1, "ended", -5, -3, 1)
	shas := func(id int64) []string {
		rows, err := s.DB.Query("SELECT input_sha FROM testcases WHERE problem_id = ?", id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var sha string
			rows.Scan(&sha)
			out = append(out, sha)
		}
		return out
	}
	unusedShas, usedShas := shas(unused.ID), shas(used.ID)
	if err := s.Delete(ctx, unused.ID, adminActor); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, used.ID, ownerActor); err != nil {
		t.Fatal(err)
	}
	age(t, s, now.Add(-72*time.Hour))
	if _, err := s.Blobs.GC(ctx, s.DB, now); err != nil {
		t.Fatal(err)
	}
	for _, sha := range unusedShas {
		if hasRow(t, s, sha) {
			t.Errorf("unused problem's blob %s survived gc", sha[:8])
		}
	}
	for _, sha := range usedShas {
		if !hasRow(t, s, sha) || !onDisk(s, sha) {
			t.Errorf("pinned revision's blob %s was collected", sha[:8])
		}
	}
}

// One test drives every write route against a tombstone: each answers
// ErrDeleted (409 deleted) and nothing is written.
func TestEveryWriteRouteRefusesATombstone(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	e := editOf(p)
	pk := packOf(t, "A new statement.", "")
	pk.Problem.Slug = "a-plus-b-deleted-" + itoa(p.ID)
	if err := s.Delete(ctx, p.ID, ownerActor); err != nil {
		t.Fatal(err)
	}
	revs := count(t, s, "SELECT count(*) FROM problem_revisions")
	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	for _, n := range []string{"01.in", "01.out"} {
		w, _ := zw.Create(n)
		w.Write([]byte("1 2\n"))
	}
	zw.Close()
	zipData := zb.Bytes()
	steps := map[string]func() error{
		"edit":        func() error { _, err := s.Update(ctx, p.ID, e); return err },
		"tests":       func() error { _, err := s.ReplaceTests(ctx, p.ID, zipData, RevisionNote{}); return err },
		"validate":    func() error { _, err := s.RequestValidation(ctx, p.ID, 3); return err },
		"generate":    func() error { _, err := s.RequestGeneration(ctx, p.ID, 3, false); return err },
		"generate+v":  func() error { _, err := s.RequestGenerationThenValidation(ctx, p.ID, 3); return err },
		"generator":   func() error { _, err := s.RequestInputGeneration(ctx, p.ID, 3); return err },
		"restore":     func() error { _, err := s.Restore(ctx, p.ID, ""); return err },
		"import over": func() error { _, err := s.ImportAs(ctx, pk, 3, ImportOptions{OnConflict: ImportReplace}); return err },
		"save": func() error {
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			d, err := load(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			return s.save(ctx, tx, d, RevisionNote{})
		},
	}
	for name, step := range steps {
		if err := step(); !errors.Is(err, ErrDeleted) {
			t.Errorf("%s: %v, want ErrDeleted", name, err)
		}
	}
	if err := ErrDeleted; err.Error() != "This problem was deleted and cannot be changed." {
		t.Errorf("copy: %q", err.Error())
	}
	if n := count(t, s, "SELECT count(*) FROM problem_revisions"); n != revs {
		t.Errorf("revisions %d became %d", revs, n)
	}
	if n := count(t, s, "SELECT count(*) FROM jobs"); n != 0 {
		t.Errorf("%d jobs were queued", n)
	}
}

// Deleting closes the problem's waiting setter jobs with the deleted
// reason, and a worker that held one gets the lease error on its report.
func TestDeleteClosesOpenSetterJobs(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	if _, err := s.RequestValidation(ctx, p.ID, 3); err != nil {
		t.Fatal(err)
	}
	claim, err := s.Jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindValidate}, Worker: "w"})
	if err != nil || claim == nil {
		t.Fatalf("claim %v %v", claim, err)
	}
	if err := s.Delete(ctx, p.ID, ownerActor); err != nil {
		t.Fatal(err)
	}
	job, err := s.Jobs.Get(ctx, claim.JobID)
	if err != nil || job.State != jobs.Failed || job.Error != "The problem was deleted." {
		t.Fatalf("job %+v %v", job, err)
	}
	if err := s.Jobs.Report(ctx, judgeapi.Report{JobID: claim.JobID, Lease: claim.Lease, Final: true}); !errors.Is(err, jobs.ErrLease) {
		t.Errorf("late report: %v", err)
	}
}

// A job that still finishes after the delete (its kind's Finish runs) closes
// failed with the deleted reason, writes nothing and is not retried.
func TestFinishAfterDeleteClosesCleanly(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p := importExample(t, s, "a-plus-b")
	if err := s.Delete(ctx, p.ID, ownerActor); err != nil {
		t.Fatal(err)
	}
	payloads := map[string]struct {
		kind    jobs.Kind
		payload any
	}{
		"validate":        {ValidateKind{}, judgeapi.ValidatePayload{ProblemID: p.ID, ContentHash: p.ContentHash}},
		"generate":        {GenerateKind{}, judgeapi.GeneratePayload{ProblemID: p.ID, ContentHash: p.ContentHash}},
		"generate_inputs": {GenerateInputsKind{}, judgeapi.GenerateInputsPayload{ProblemID: p.ID, ContentHash: p.ContentHash}},
	}
	for name, c := range payloads {
		raw, _ := json.Marshal(c.payload)
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		job := &jobs.Job{Kind: name, State: jobs.Done, Payload: raw}
		if err := c.kind.Finish(ctx, tx, job); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		tx.Rollback()
		if job.State != jobs.Failed || job.Error != "The problem was deleted." {
			t.Errorf("%s closed as %s %q", name, job.State, job.Error)
		}
		if !strings.Contains(string(job.Result.Summary), "The problem was deleted.") {
			t.Errorf("%s summary %s", name, job.Result.Summary)
		}
	}
	got, _ := s.Get(ctx, p.ID)
	if got.ValidatedHash != "" || len(got.Tests) != 0 {
		t.Error("a finished job wrote to the tombstone")
	}
}

// A repeated create with the key of a deleted problem is refused, not answered
// with the tombstone as if it had succeeded.
func TestRepeatedCreateOfADeletedProblemIsRefused(t *testing.T) {
	s, _ := deleteBank(t)
	ctx := context.Background()
	p, err := s.Create(ctx, "Keyed", "", 3, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, p.ID, ownerActor); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Create(ctx, "Keyed", "", 3, "key-1"); !errors.Is(err, ErrDeleted) || got != nil {
		t.Errorf("repeat create: %v %v", got, err)
	}
}
