package problems

// Rewrite with AI, the Replace half (v2.1.0 stage W): one transaction that
// archives a copy of the old problem, puts the validated rewrite on the old
// problem as one new revision and archives the rewrite.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// rewriteRig is a bank with an old problem (a-plus-b) and a rewrite of it
// (pair-with-sum, a different pack) that is an AI draft and has passed
// Validate. base is the old problem's fingerprint when the rewrite began.
type rewriteRig struct {
	s     *Service
	old   *Problem
	draft *Problem
	base  string
	now   time.Time
}

func newRewriteRig(t *testing.T) *rewriteRig {
	t.Helper()
	s := newBank(t)
	ctx := context.Background()
	r := &rewriteRig{s: s, now: time.UnixMilli(1_800_000_000_000)}
	s.Now = func() time.Time { return r.now }
	if _, err := s.DB.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'asha@college.edu', 'student', 0)`); err != nil {
		t.Fatal(err)
	}
	r.old = importExample(t, s, "a-plus-b")
	r.draft = importExample(t, s, "pair-with-sum")
	for _, id := range []int64{r.old.ID, r.draft.ID} {
		if _, err := s.RequestValidation(ctx, id, 0); err != nil {
			t.Fatal(err)
		}
		fakeJudge(t, s, 50, nil)
	}
	r.old, _ = s.Get(ctx, r.old.ID)
	if _, err := s.DB.Exec("UPDATE problems SET origin = 'ai' WHERE id = ?", r.draft.ID); err != nil {
		t.Fatal(err)
	}
	if r.draft, _ = s.Get(ctx, r.draft.ID); !r.draft.Validated() {
		t.Fatal("setup: the rewrite is not validated")
	}
	snap, err := s.EditSnapshot(ctx, r.old.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.base = snap.Hash
	return r
}

func (r *rewriteRig) replace(t *testing.T) (*Problem, *Problem, error) {
	t.Helper()
	return r.s.ReplaceWithRewrite(context.Background(), r.old.ID, r.draft.ID, r.base, 0)
}

// export is a problem's pack files by name, test data included.
func (r *rewriteRig) export(t *testing.T, id int64) map[string][]byte {
	t.Helper()
	p, err := r.s.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := WritePack(&buf, p, func(sha string) ([]byte, error) { return r.s.Blobs.Read(sha, MaxPackFile+1) }); err != nil {
		t.Fatal(err)
	}
	files, err := readZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func problemCount(t *testing.T, s *Service) int { return count(t, s, "SELECT count(*) FROM problems") }

func TestReplaceMakesOneRevisionKeepsIdAndSlugAndArchivesTheRewrite(t *testing.T) {
	r := newRewriteRig(t)
	ctx := context.Background()
	out, cp, err := r.replace(t)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != r.old.ID || out.Slug != r.old.Slug {
		t.Errorf("replaced problem is %d %s, want %d %s", out.ID, out.Slug, r.old.ID, r.old.Slug)
	}
	if n := revisionCount(t, r.s, r.old.ID); n != 2 {
		t.Errorf("%d revisions, want 2", n)
	}
	rev, _ := r.s.CurrentRevision(ctx, r.old.ID)
	if rev.Reason != "Replaced with an AI rewrite" {
		t.Errorf("revision reason %q", rev.Reason)
	}
	if out.Statement != r.draft.Statement || out.Checker != r.draft.Checker || len(out.Tests) != len(r.draft.Tests) || out.Tests[0].InputSHA != r.draft.Tests[0].InputSHA {
		t.Error("the old problem does not hold the rewrite's content")
	}
	if out.Origin != OriginAI || !hasTag(out.Tags, AIDraftTag) {
		t.Errorf("origin %s tags %v, want an AI draft", out.Origin, out.Tags)
	}
	if out.ValidatedHash != "" {
		t.Error("the replaced problem counts as validated before Validate ran")
	}
	if got, _ := r.s.Get(ctx, r.draft.ID); !got.Archived {
		t.Error("the rewrite was not archived")
	}
	if cp == nil || !cp.Archived || cp.ID == r.old.ID || cp.ID == r.draft.ID {
		t.Fatalf("copy %+v", cp)
	}
	if n := problemCount(t, r.s); n != 3 {
		t.Errorf("%d problems, want old, rewrite and copy", n)
	}
	var solutionByPerson int
	r.s.DB.QueryRow("SELECT solution_by_person FROM problems WHERE id = ?", r.old.ID).Scan(&solutionByPerson)
	if solutionByPerson != 0 {
		t.Error("the AI's solution counts as written by a person")
	}
}

func TestArchivedCopyExportsLikeTheOldProblemAndSurvivesGC(t *testing.T) {
	r := newRewriteRig(t)
	ctx := context.Background()
	before := r.export(t, r.old.ID)
	oldTests := append([]Test{}, r.old.Tests...)
	_, cp, err := r.replace(t)
	if err != nil {
		t.Fatal(err)
	}
	wantTitle := r.old.Title + " (before rewrite, 15 Jan 2027)"
	if cp.Title != wantTitle {
		t.Errorf("copy title %q, want %q", cp.Title, wantTitle)
	}
	if cp.PackSlug() != r.old.Slug {
		t.Errorf("copy pack slug %q, want %q", cp.PackSlug(), r.old.Slug)
	}
	if !r.old.Validated() || !cp.Validated() {
		t.Errorf("old validated %v, copy validated %v, want both", r.old.Validated(), cp.Validated())
	}

	// Same files, byte for byte, except the title line: the copy is named.
	same := func(label string, got map[string][]byte) {
		t.Helper()
		if len(got) != len(before) {
			t.Fatalf("%s: %d files, want %d", label, len(got), len(before))
		}
		for name, want := range before {
			have := got[name]
			if name == "problem.yaml" {
				want = []byte(strings.Replace(string(want), "title: "+r.old.Title, "title: "+wantTitle, 1))
			}
			if !bytes.Equal(have, want) {
				t.Errorf("%s: %s differs:\n%s\nwant:\n%s", label, name, have, want)
			}
		}
	}
	same("after replace", r.export(t, cp.ID))

	// A garbage collection a week later keeps every test file of the copy.
	later := r.now.Add(8 * 24 * time.Hour)
	age(t, r.s, r.now.Add(-48*time.Hour))
	if _, err := r.s.Blobs.GC(ctx, r.s.DB, later); err != nil {
		t.Fatal(err)
	}
	for _, tc := range oldTests {
		if !onDisk(r.s, tc.InputSHA) || !onDisk(r.s, tc.OutputSHA) {
			t.Errorf("test %d of the old problem was collected", tc.Idx)
		}
	}
	same("after gc", r.export(t, cp.ID))

	// Control: with the copy's test rows gone the same run collects them,
	// so the survival above is the copy's doing.
	if _, err := r.s.DB.Exec("DELETE FROM testcases WHERE problem_id = ?", cp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Blobs.GC(ctx, r.s.DB, later); err != nil {
		t.Fatal(err)
	}
	if onDisk(r.s, oldTests[0].InputSHA) {
		t.Error("control: the old tests were not collected without the copy")
	}
}

func TestArchivedCopyRestoresAsItsOwnProblem(t *testing.T) {
	r := newRewriteRig(t)
	ctx := context.Background()
	_, cp, err := r.replace(t)
	if err != nil {
		t.Fatal(err)
	}
	// The old slug is in use, so Restore offers a free one, as for any archived problem.
	_, err = r.s.Restore(ctx, cp.ID, "")
	var taken *SlugTakenError
	if !errors.As(err, &taken) || taken.Slug != r.old.Slug {
		t.Fatalf("restore with the old slug in use: %v", err)
	}
	got, err := r.s.Restore(ctx, cp.ID, taken.Suggested)
	if err != nil || got.Archived || got.Statement != r.old.Statement {
		t.Fatalf("restore: %+v %v", got, err)
	}
}

func TestReplaceRefusals(t *testing.T) {
	title := "A plus B"
	type refusal struct{ code, message string }
	cases := []struct {
		name  string
		setup func(t *testing.T, r *rewriteRig)
		want  refusal
	}{
		{"not validated", func(t *testing.T, r *rewriteRig) {
			r.s.DB.Exec("UPDATE problems SET validated_hash = NULL WHERE id = ?", r.draft.ID)
		}, refusal{"not_validated", "Validate this rewrite first."}},
		{"changed", func(t *testing.T, r *rewriteRig) {
			r.s.DB.Exec("UPDATE problems SET statement_md = statement_md || 'x' WHERE id = ?", r.old.ID)
		}, refusal{"changed", title + " changed after this rewrite began. Rewrite it again, or keep this rewrite as its own problem."}},
		{"running contest", func(t *testing.T, r *rewriteRig) {
			rev, _ := r.s.CurrentRevision(context.Background(), r.old.ID)
			r.s.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
				VALUES (1, 'wk3', 'Week 3', ?, ?, 1, 0, 0, 0)`, r.now.Add(-time.Hour).UnixMilli(), r.now.Add(time.Hour).UnixMilli())
			r.s.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, r.old.ID, rev.ID)
		}, refusal{"contest_running", "A contest that is running uses " + title + ", so it cannot be replaced now. Try again after the contest ends, or keep this rewrite as its own problem."}},
		{"submitted", func(t *testing.T, r *rewriteRig) {
			r.s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, 1, 'submit', 'cpp17', 'x', 'x', 0)`, r.old.ID)
		}, refusal{"in_use", "Students have submitted or saved code for " + title + ", so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests."}},
		{"ran", func(t *testing.T, r *rewriteRig) {
			r.s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, 1, 'run', 'cpp17', 'x', 'x', 0)`, r.old.ID)
		}, refusal{"in_use", "Students have submitted or saved code for " + title + ", so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests."}},
		{"saved code", func(t *testing.T, r *rewriteRig) {
			r.s.DB.Exec(`INSERT INTO drafts (problem_id, user_id, language, source, updated_at) VALUES (?, 1, 'cpp17', 'x', 0)`, r.old.ID)
		}, refusal{"in_use", "Students have submitted or saved code for " + title + ", so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests."}},
		{"archived", func(t *testing.T, r *rewriteRig) {
			if err := r.s.Archive(context.Background(), r.old.ID); err != nil {
				t.Fatal(err)
			}
		}, refusal{"archived", title + " is archived, so it cannot be replaced. Restore it first, or keep this rewrite as its own problem."}},
		{"deleted", func(t *testing.T, r *rewriteRig) {
			r.s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", r.old.ID)
		}, refusal{"deleted", title + " was deleted, so it cannot be replaced. Keep this rewrite as its own problem."}},
		{"rewrite archived", func(t *testing.T, r *rewriteRig) {
			if err := r.s.Archive(context.Background(), r.draft.ID); err != nil {
				t.Fatal(err)
			}
		}, refusal{"rewrite_gone", "This rewrite is archived or deleted, so it cannot replace " + title + ". Rewrite it again."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRewriteRig(t)
			if r.old.Title != title {
				t.Fatalf("fixture title %q", r.old.Title)
			}
			c.setup(t, r)
			revs, problems := revisionCount(t, r.s, r.old.ID), problemCount(t, r.s)
			_, _, err := r.replace(t)
			var got *RewriteRefusal
			if !errors.As(err, &got) {
				t.Fatalf("error %v, want a refusal", err)
			}
			if got.Code != c.want.code || got.Message != c.want.message {
				t.Errorf("refusal %q %q\nwant   %q %q", got.Code, got.Message, c.want.code, c.want.message)
			}
			if n := revisionCount(t, r.s, r.old.ID); n != revs {
				t.Errorf("a refused replace made %d revisions", n-revs)
			}
			if n := problemCount(t, r.s); n != problems {
				t.Errorf("a refused replace added %d problems", n-problems)
			}
		})
	}
}

func TestReplaceBadInput(t *testing.T) {
	r := newRewriteRig(t)
	ctx := context.Background()
	if _, _, err := r.s.ReplaceWithRewrite(ctx, 9999, r.draft.ID, r.base, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing old problem: %v", err)
	}
	if _, _, err := r.s.ReplaceWithRewrite(ctx, r.old.ID, 9999, r.base, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing rewrite: %v", err)
	}
	var fields FieldErrors
	if _, _, err := r.s.ReplaceWithRewrite(ctx, r.old.ID, r.old.ID, r.base, 0); !errors.As(err, &fields) || fields["draft_id"] == "" {
		t.Errorf("a problem as its own rewrite: %v", err)
	}
	r.s.DB.Exec("UPDATE problems SET origin = 'manual' WHERE id = ?", r.draft.ID)
	if _, _, err := r.s.ReplaceWithRewrite(ctx, r.old.ID, r.draft.ID, r.base, 0); !errors.As(err, &fields) || fields["draft_id"] == "" {
		t.Errorf("a rewrite that is not an AI draft: %v", err)
	}
	if n := revisionCount(t, r.s, r.old.ID); n != 1 || problemCount(t, r.s) != 2 {
		t.Error("bad input changed the bank")
	}
}

func TestReplaceTwiceMakesOneRevisionAndOneCopy(t *testing.T) {
	r := newRewriteRig(t)
	if _, _, err := r.replace(t); err != nil {
		t.Fatal(err)
	}
	var ref *RewriteRefusal
	if _, _, err := r.replace(t); !errors.As(err, &ref) {
		t.Fatalf("second replace: %v", err)
	}
	if n := revisionCount(t, r.s, r.old.ID); n != 2 || problemCount(t, r.s) != 3 {
		t.Errorf("%d revisions and %d problems after a second replace, want 2 and 3", n, problemCount(t, r.s))
	}
}

func TestReplaceInParallelMakesOneRevisionAndOneCopy(t *testing.T) {
	r := newRewriteRig(t)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := r.s.ReplaceWithRewrite(context.Background(), r.old.ID, r.draft.ID, r.base, 0)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d replaces succeeded, want 1", won)
	}
	if n := revisionCount(t, r.s, r.old.ID); n != 2 || problemCount(t, r.s) != 3 {
		t.Errorf("%d revisions and %d problems, want 2 and 3", n, problemCount(t, r.s))
	}
}

// A contest that has not started holds the old revision until Update
// students now, and keeps holding it after a replace.
func TestReplaceLeavesAPinnedContestOnTheOldRevision(t *testing.T) {
	r := newRewriteRig(t)
	rev, _ := r.s.CurrentRevision(context.Background(), r.old.ID)
	r.s.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
		VALUES (1, 'wk4', 'Week 4', ?, ?, 1, 0, 0, 0)`, r.now.Add(time.Hour).UnixMilli(), r.now.Add(2*time.Hour).UnixMilli())
	r.s.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, r.old.ID, rev.ID)
	if _, _, err := r.replace(t); err != nil {
		t.Fatal(err)
	}
	var pinned, problem int64
	r.s.DB.QueryRow("SELECT revision_id, problem_id FROM contest_problems WHERE contest_id = 1").Scan(&pinned, &problem)
	if pinned != rev.ID || problem != r.old.ID {
		t.Errorf("the contest now holds revision %d of problem %d, want %d of %d", pinned, problem, rev.ID, r.old.ID)
	}
	if n := count(t, r.s, "SELECT count(*) FROM contest_problems"); n != 1 {
		t.Errorf("%d contest rows", n)
	}
}

func TestCopyTitleFitsTheTitleLimit(t *testing.T) {
	long := strings.Repeat("é", MaxTitle)
	got := copyTitle(long, time.UnixMilli(1_800_000_000_000))
	if n := len([]rune(got)); n > MaxTitle || !strings.HasSuffix(got, " (before rewrite, 15 Jan 2027)") {
		t.Errorf("%d runes: %q", n, got)
	}
	if got := copyTitle("Two sum", time.UnixMilli(1_800_000_000_000)); got != "Two sum (before rewrite, 15 Jan 2027)" {
		t.Errorf("short title: %q", got)
	}
}

// A language check that is open drops its result if the problem changes, so
// Replace waits for it, as an AI edit does.
func TestReplaceWaitsForAnOpenLanguageCheck(t *testing.T) {
	r := newRewriteRig(t)
	payload := fmt.Sprintf(`{"problem_id": %d, "proof": {"round": "r1"}}`, r.old.ID)
	if _, err := r.s.DB.Exec(`INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('validate', ?, 'queued', ?, 0)`, ProofPriority, payload); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.replace(t); !errors.Is(err, ErrChecking) {
		t.Fatalf("replace during a language check: %v", err)
	}
	if n := revisionCount(t, r.s, r.old.ID); n != 1 || problemCount(t, r.s) != 2 {
		t.Error("a refused replace changed the bank")
	}
}

// A rewrite already has the ai draft tag; the problem it replaces keeps
// exactly one, whatever the tag count was.
func TestReplaceKeepsOneAIDraftTagWithinTheTagLimit(t *testing.T) {
	r := newRewriteRig(t)
	tags := []string{"ai draft"}
	for i := 0; i < MaxTags; i++ {
		tags = append(tags, fmt.Sprintf("topic-%d", i))
	}
	list, _ := json.Marshal(tags)
	if _, err := r.s.DB.Exec("UPDATE problems SET tags = ? WHERE id = ?", string(list), r.draft.ID); err != nil {
		t.Fatal(err)
	}
	out, _, err := r.replace(t)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tag := range out.Tags {
		if isAIDraftTag(tag) {
			n++
		}
	}
	if len(out.Tags) > MaxTags || n != 1 {
		t.Errorf("tags %v: %d ai draft tags", out.Tags, n)
	}
}
