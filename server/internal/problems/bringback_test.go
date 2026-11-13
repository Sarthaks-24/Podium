package problems

// Bring back an earlier version (v2.1.0 stage H): a revision keeps what a
// version needs, the collector keeps the last three revisions whole, and a
// version whose every file survives can be written back as a new revision.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"
)

// history is a problem with several versions that differ in every part a
// version restores. Revision 1 is the import; each version(n) is one more
// revision; newTests is one more that changes only the tests.
type history struct {
	s    *Service
	now  time.Time
	p    *Problem
	revs []*Revision // revs[i] is revision i+1
}

func newHistory(t *testing.T) *history {
	t.Helper()
	s, now := deleteBank(t)
	h := &history{s: s, now: now, p: importExample(t, s, "a-plus-b")}
	h.record(t)
	return h
}

func (h *history) record(t *testing.T) *Revision {
	t.Helper()
	rev, err := h.s.CurrentRevision(context.Background(), h.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.revs = append(h.revs, rev)
	return rev
}

// edit applies f to an edit of the working copy and records the revision it makes.
func (h *history) edit(t *testing.T, f func(e *Edit)) *Revision {
	t.Helper()
	ctx := context.Background()
	cur, err := h.s.Get(ctx, h.p.ID)
	if err != nil {
		t.Fatal(err)
	}
	e := editOf(cur)
	e.By, e.Reason = 3, "test"
	f(&e)
	if _, err := h.s.Update(ctx, h.p.ID, e); err != nil {
		t.Fatal(err)
	}
	return h.record(t)
}

// version makes version n: new texts for every program, the checker and the
// generator, new generator arguments, limits, statement and starting code.
func (h *history) version(t *testing.T, n int) *Revision {
	t.Helper()
	return h.edit(t, func(e *Edit) {
		e.Title = fmt.Sprintf("A plus B v%d", n)
		e.Statement = fmt.Sprintf("Statement %d.\n", n)
		e.TimeLimitMS = 1000 + 100*n
		e.Reference = Source{"cpp17", fmt.Sprintf("// reference %d\nint main() {}\n", n)}
		e.Brute = &Source{"python3", fmt.Sprintf("print('brute %d')\n", n)}
		e.Validator = &Source{"python3", fmt.Sprintf("print('validator %d')\n", n)}
		e.Checker, e.CheckerSource = CheckerCustom, fmt.Sprintf("// checker %d\nint main() {}\n", n)
		e.Generator = &GeneratorEdit{Source: &Source{"python3", fmt.Sprintf("print('generator %d')\n", n)}}
		for i := range e.Subtasks {
			e.Subtasks[i].GeneratorArgs = []string{fmt.Sprintf("n=%d", n*10+i)}
		}
		starters := map[string]string{"cpp17": fmt.Sprintf("// starting code %d\n", n)}
		e.Starters = &starters
	})
}

// newTests replaces every test, so the revision it makes lists new files.
func (h *history) newTests(t *testing.T, marker string) *Revision {
	t.Helper()
	var entries []entry
	for i := 1; i <= 6; i++ {
		entries = append(entries, entry{name: fmt.Sprintf("%d.in", i), body: fmt.Sprintf("%s %d\n", marker, i)},
			entry{name: fmt.Sprintf("%d.out", i), body: fmt.Sprintf("%d\n", i)})
	}
	if _, err := h.s.ReplaceTests(context.Background(), h.p.ID, zipEntries(t, entries...), RevisionNote{By: 3, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	return h.record(t)
}

// full is five revisions after the import: three versions, new tests, one more version.
func (h *history) full(t *testing.T) {
	t.Helper()
	h.version(t, 2)
	h.version(t, 3)
	h.newTests(t, "fresh")
	h.version(t, 4)
	h.version(t, 5)
}

// files are the blob hashes a revision refers to: programs and tests.
func (h *history) files(t *testing.T, r *Revision) []string {
	t.Helper()
	var out []string
	for _, sha := range []string{r.Reference.SHA, r.Brute.SHA, r.Validator.SHA, r.Generator.SHA, r.CheckerSHA} {
		if sha != "" {
			out = append(out, sha)
		}
	}
	tests, err := h.s.RevisionTests(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		out = append(out, tc.InputSHA)
		if tc.OutputSHA != "" {
			out = append(out, tc.OutputSHA)
		}
	}
	return out
}

func (h *history) collect(t *testing.T) {
	t.Helper()
	age(t, h.s, h.now.Add(-48*time.Hour))
	if _, err := h.s.Blobs.GC(context.Background(), h.s.DB, h.now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordingARevisionStoresProgramTextsAndGeneratorArguments(t *testing.T) {
	h := newHistory(t)
	ctx := context.Background()
	rev := h.version(t, 2)
	for name, c := range map[string]struct{ sha, text string }{
		"reference": {rev.Reference.SHA, "// reference 2\nint main() {}\n"},
		"brute":     {rev.Brute.SHA, "print('brute 2')\n"},
		"validator": {rev.Validator.SHA, "print('validator 2')\n"},
		"generator": {rev.Generator.SHA, "print('generator 2')\n"},
		"checker":   {rev.CheckerSHA, "// checker 2\nint main() {}\n"},
	} {
		got, err := h.s.Blobs.Read(c.sha, 1<<20)
		if err != nil || string(got) != c.text {
			t.Errorf("%s: stored %q (%v), want %q", name, got, err, c.text)
		}
	}
	if len(rev.Subtasks) != 2 || !slices.Equal(rev.Subtasks[0].GeneratorArgs, []string{"n=20"}) || !slices.Equal(rev.Subtasks[1].GeneratorArgs, []string{"n=21"}) {
		t.Errorf("recorded subtasks %+v", rev.Subtasks)
	}
	if first := h.revs[0]; first.Subtasks[0].GeneratorArgs == nil || len(first.Subtasks[0].GeneratorArgs) != 0 {
		t.Errorf("a subtask without arguments records %#v, want an empty list", first.Subtasks[0].GeneratorArgs)
	}

	// A change to generator arguments alone is a new revision; a save that
	// changes nothing is not.
	total := revisionCount(t, h.s, h.p.ID)
	h.edit(t, func(e *Edit) { e.Subtasks[0].GeneratorArgs = []string{"n=99"} })
	if n := revisionCount(t, h.s, h.p.ID); n != total+1 {
		t.Errorf("changing generator arguments made %d revisions, want 1", n-total)
	}
	cur, _ := h.s.Get(ctx, h.p.ID)
	if _, err := h.s.Update(ctx, h.p.ID, editOf(cur)); err != nil {
		t.Fatal(err)
	}
	if n := revisionCount(t, h.s, h.p.ID); n != total+1 {
		t.Errorf("a save that changed nothing made %d revisions", n-total-1)
	}
}

// A revision made before v2.1.0 has no generator arguments in its subtasks.
// It reads as such, is not offered, and does not make the problem look changed.
func TestARevisionFromBeforeRecordsNoGeneratorArguments(t *testing.T) {
	h := newHistory(t)
	ctx := context.Background()
	h.version(t, 2)
	latest := h.revs[1]
	var subtasks []map[string]any
	if err := json.Unmarshal(mustJSON(t, latest.Subtasks), &subtasks); err != nil {
		t.Fatal(err)
	}
	for _, st := range subtasks {
		delete(st, "generator_args")
	}
	rewriteRevision(t, h.s, "UPDATE problem_revisions SET subtasks = ? WHERE id = ?", string(mustJSON(t, subtasks)), latest.ID)
	old, err := h.s.RevisionByID(ctx, latest.ID)
	if err != nil || old.Subtasks[0].GeneratorArgs != nil {
		t.Fatalf("old revision reads %+v (%v), want no generator arguments", old.Subtasks, err)
	}
	if bad, err := CheckRevisions(ctx, h.s.DB); err != nil || len(bad) != 0 {
		t.Errorf("an old revision made the check report %v (%v)", bad, err)
	}
	list, _, err := h.s.History(ctx, h.p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].CanBringBack {
		t.Error("a revision without generator arguments is offered")
	}
}

// rewriteRevision runs an UPDATE of problem_revisions with the table's
// append-only trigger dropped, to make the old shapes a real database holds.
func rewriteRevision(t *testing.T, s *Service, query string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec("DROP TRIGGER problem_revisions_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollectorKeepsTheLastThreeRevisionsWhole(t *testing.T) {
	h := newHistory(t)
	h.full(t) // revisions 1 to 6
	if len(h.revs) != 6 {
		t.Fatalf("%d revisions", len(h.revs))
	}
	h.collect(t)
	// A file stays exactly when one of revisions 4 to 6 refers to it (revision
	// 4 changed only the tests, so it shares revision 3's programs).
	var kept []string
	for _, r := range h.revs[3:] {
		kept = append(kept, h.files(t, r)...)
	}
	for i, r := range h.revs {
		for _, sha := range h.files(t, r) {
			if want := slices.Contains(kept, sha); onDisk(h.s, sha) != want {
				t.Errorf("revision %d: %.12s on disk is %v, want %v", i+1, sha, !want, want)
			}
		}
	}
	// Guard against a vacuous loop: the old versions did lose files.
	oldTests, _ := h.s.RevisionTests(context.Background(), h.revs[0].ID)
	if onDisk(h.s, oldTests[0].InputSHA) || onDisk(h.s, h.revs[1].Reference.SHA) {
		t.Error("the original tests or revision 2's reference survived although no recent revision refers to them")
	}
}

func TestCollectorKeepsPinnedRevisionsWhole(t *testing.T) {
	h := newHistory(t)
	h.version(t, 2)
	holdIn(t, h.s, h.now, h.p.ID, 1, "pinned", -10, -5, 1) // an ended contest pins revision 2
	h.version(t, 3)
	h.newTests(t, "fresh")
	h.version(t, 4)
	h.version(t, 5)
	h.collect(t)
	for _, sha := range h.files(t, h.revs[1]) {
		if !onDisk(h.s, sha) {
			t.Errorf("a pinned revision lost %.12s", sha)
		}
	}
	if onDisk(h.s, h.revs[0].Reference.SHA) {
		t.Error("an unpinned old revision kept its reference")
	}
}

func TestBringBackGivesTheVersionsContent(t *testing.T) {
	h := newHistory(t)
	ctx := context.Background()
	h.full(t)
	// Bank details that stay as they are now.
	h.edit(t, func(e *Edit) {
		e.Tags, e.Difficulty = []string{"graphs"}, "hard"
		e.Generator = &GeneratorEdit{Source: &Source{"python3", "print('generator 5')\n"},
			Plan: []PlannedTest{{Test: 7, Subtask: 2, Seed: 1, Args: []string{"n=5"}}}}
	})
	before, _ := h.s.Get(ctx, h.p.ID)
	if _, err := h.s.DB.Exec("UPDATE problems SET solution_by_person = 1, validated_hash = content_hash WHERE id = ?", h.p.ID); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{1, 4, 0} { // versions 2 and 4, then the import
		want := h.revs[i]
		wantTests, _ := h.s.RevisionTests(ctx, want.ID)
		total := revisionCount(t, h.s, h.p.ID)
		got, rev, err := h.s.BringBack(ctx, h.p.ID, want.ID, 3)
		if err != nil {
			t.Fatalf("revision %d: %v", want.Rev, err)
		}
		if n := revisionCount(t, h.s, h.p.ID); n != total+1 {
			t.Errorf("revision %d: %d new revisions, want 1", want.Rev, n-total)
		}
		if rev.Rev != total+1 || rev.Reason != fmt.Sprintf("Brought back version %d", want.Rev) || rev.CreatedBy != 3 {
			t.Errorf("revision %d: new revision %+v", want.Rev, rev)
		}
		if got.ContentHash != want.ContentHash || got.Title != want.Title || got.Statement != want.Statement ||
			got.TimeLimitMS != *want.TimeLimitMS || got.MemoryLimitMB != *want.MemoryLimitMB ||
			got.Checker != *want.Checker || got.Epsilon != *want.Epsilon || !maps.Equal(got.Starters, want.Starters) || got.Mode != modeOrWhole(want.Mode) ||
			!maps.Equal(got.LockedTop, startersOrEmpty(want.LockedTop)) || !slices.Equal(got.Samples, want.Samples) {
			t.Errorf("revision %d: problem is %+v", want.Rev, got)
		}
		if refOf(&got.Reference) != want.Reference || refOf(got.Brute) != want.Brute || refOf(got.Validator) != want.Validator ||
			refOf(got.Generator) != want.Generator || (want.CheckerSHA != "" && SHA([]byte(got.CheckerSource)) != want.CheckerSHA) {
			t.Errorf("revision %d: programs differ", want.Rev)
		}
		for j, st := range got.Subtasks {
			w := want.Subtasks[j]
			if st.Idx != w.Idx || st.Points != w.Points || st.Title != w.Title || st.Tests != w.Tests || !slices.Equal(st.GeneratorArgs, w.GeneratorArgs) {
				t.Errorf("revision %d: subtask %d is %+v, want %+v", want.Rev, j, st, w)
			}
		}
		if len(got.Tests) != len(wantTests) {
			t.Fatalf("revision %d: %d tests, want %d", want.Rev, len(got.Tests), len(wantTests))
		}
		for j, tc := range got.Tests {
			w := wantTests[j]
			if tc.Idx != w.Idx || tc.InputSHA != w.InputSHA || tc.OutputSHA != w.OutputSHA || tc.InputSize != w.InputSize || tc.OutputSize != w.OutputSize {
				t.Errorf("revision %d: test %d is %+v, want %+v", want.Rev, j, tc, w)
			}
		}
		if !sameRevision(*rev, *want) {
			t.Errorf("revision %d: the new revision does not record the same problem", want.Rev)
		}
		if got.ID != h.p.ID || got.Slug != before.Slug || !slices.Equal(got.Tags, before.Tags) || got.Difficulty != before.Difficulty ||
			got.Origin != before.Origin || len(before.Plan) != 1 || !reflect.DeepEqual(got.Plan, before.Plan) {
			t.Errorf("revision %d: bank details changed: %+v", want.Rev, got)
		}
		if got.ValidatedHash != "" {
			t.Errorf("revision %d: the problem counts as validated before Validate ran", want.Rev)
		}
		var byPerson int
		h.s.DB.QueryRow("SELECT solution_by_person FROM problems WHERE id = ?", h.p.ID).Scan(&byPerson)
		if byPerson != 1 {
			t.Errorf("revision %d: who wrote the solution was reset", want.Rev)
		}
		h.s.DB.Exec("UPDATE problems SET validated_hash = content_hash WHERE id = ?", h.p.ID)
	}
	if bad, err := CheckRevisions(ctx, h.s.DB); err != nil || len(bad) != 0 {
		t.Errorf("check after bringing versions back: %v (%v)", bad, err)
	}
}

func TestBringBackTheCurrentVersionMakesNothing(t *testing.T) {
	h := newHistory(t)
	h.version(t, 2)
	total := revisionCount(t, h.s, h.p.ID)
	_, _, err := h.s.BringBack(context.Background(), h.p.ID, h.revs[1].ID, 3)
	assertRefusal(t, err, "current", "This is already the current version.")
	if n := revisionCount(t, h.s, h.p.ID); n != total {
		t.Errorf("made %d revisions", n-total)
	}
}

func TestAVersionWithAMissingFileIsNotOffered(t *testing.T) {
	h := newHistory(t)
	ctx := context.Background()
	h.full(t)
	h.collect(t) // revisions 1 and 2 lose their files
	list, total, err := h.s.History(ctx, h.p.ID, 10)
	if err != nil || total != 6 || len(list) != 6 {
		t.Fatalf("history %d of %d: %v", len(list), total, err)
	}
	// Newest first: revisions 6 to 4 were kept whole, 3 to 1 lost files.
	want := []bool{true, true, true, false, false, false}
	for i, e := range list {
		if e.Current != (i == 0) || e.Revision.Rev != 6-i {
			t.Errorf("entry %d: revision %d, current %v", i, e.Revision.Rev, e.Current)
		}
		if e.CanBringBack != want[i] {
			t.Errorf("revision %d: offered %v, want %v", e.Revision.Rev, e.CanBringBack, want[i])
		}
	}
	// Removing one file of a kept version withdraws the offer for it alone.
	if err := os.Remove(h.s.Blobs.path(h.revs[4].Generator.SHA)); err != nil {
		t.Fatal(err)
	}
	list, _, _ = h.s.History(ctx, h.p.ID, 10)
	if got := []bool{list[0].CanBringBack, list[1].CanBringBack, list[2].CanBringBack}; !slices.Equal(got, []bool{true, false, true}) {
		t.Errorf("after losing revision 5's generator, offers are %v", got)
	}
	// A lost test file withdraws it from every revision that lists it, and
	// asking for the version anyway changes nothing.
	tests, _ := h.s.RevisionTests(ctx, h.revs[4].ID)
	if err := os.Remove(h.s.Blobs.path(tests[2].OutputSHA)); err != nil {
		t.Fatal(err)
	}
	list, _, _ = h.s.History(ctx, h.p.ID, 10)
	if list[0].CanBringBack || list[2].CanBringBack {
		t.Error("revisions that list a lost test file are offered")
	}
	total2 := revisionCount(t, h.s, h.p.ID)
	before, _ := h.s.Get(ctx, h.p.ID)
	_, _, err = h.s.BringBack(ctx, h.p.ID, h.revs[4].ID, 3)
	assertRefusal(t, err, "not_kept", "This version's files were not kept, so it cannot be brought back.")
	after, _ := h.s.Get(ctx, h.p.ID)
	if revisionCount(t, h.s, h.p.ID) != total2 || after.ContentHash != before.ContentHash {
		t.Error("a refused bring back changed the problem")
	}
}

func TestAReconstructedRevisionIsNotOffered(t *testing.T) {
	h := newHistory(t)
	h.version(t, 2)
	rewriteRevision(t, h.s, "UPDATE problem_revisions SET reconstructed = 1 WHERE id = ?", h.revs[0].ID)
	list, _, _ := h.s.History(context.Background(), h.p.ID, 10)
	if list[1].CanBringBack {
		t.Error("a reconstructed revision is offered")
	}
}

func assertRefusal(t *testing.T, err error, code, message string) {
	t.Helper()
	var got *RewriteRefusal
	if !errors.As(err, &got) {
		t.Fatalf("error %v, want a refusal %s", err, code)
	}
	if got.Code != code || got.Message != message {
		t.Errorf("refusal %q %q\nwant   %q %q", got.Code, got.Message, code, message)
	}
}

func TestBringBackRefusals(t *testing.T) {
	const title = "A plus B v4"
	cases := []struct {
		name  string
		setup func(t *testing.T, h *history)
		check func(t *testing.T, err error)
	}{
		{"running contest", func(t *testing.T, h *history) {
			holdIn(t, h.s, h.now, h.p.ID, 1, "wk3", -1, 1, 1)
		}, func(t *testing.T, err error) {
			assertRefusal(t, err, "contest_running", "A contest that is running uses this problem. Try again after it ends.")
		}},
		{"student submitted", func(t *testing.T, h *history) {
			h.s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, 4, 'submit', 'cpp17', 'x', 'x', 0)`, h.p.ID)
		}, func(t *testing.T, err error) {
			assertRefusal(t, err, "in_use", "Students have code for this problem, so an older version cannot replace it. Copy it as a new problem instead.")
		}},
		{"student ran", func(t *testing.T, h *history) {
			h.s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, 4, 'run', 'cpp17', 'x', 'x', 0)`, h.p.ID)
		}, func(t *testing.T, err error) {
			assertRefusal(t, err, "in_use", "Students have code for this problem, so an older version cannot replace it. Copy it as a new problem instead.")
		}},
		{"student saved code", func(t *testing.T, h *history) {
			h.s.DB.Exec(`INSERT INTO drafts (problem_id, user_id, language, source, updated_at) VALUES (?, 4, 'cpp17', 'x', 0)`, h.p.ID)
		}, func(t *testing.T, err error) {
			assertRefusal(t, err, "in_use", "Students have code for this problem, so an older version cannot replace it. Copy it as a new problem instead.")
		}},
		{"archived", func(t *testing.T, h *history) {
			if err := h.s.Archive(context.Background(), h.p.ID); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, err error) {
			assertRefusal(t, err, "archived", title+" is archived, so a version cannot be brought back. Restore it first.")
		}},
		{"deleted", func(t *testing.T, h *history) {
			h.s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", h.p.ID)
		}, func(t *testing.T, err error) {
			if !errors.Is(err, ErrDeleted) {
				t.Errorf("error %v, want ErrDeleted", err)
			}
		}},
		{"language check open", func(t *testing.T, h *history) {
			payload := fmt.Sprintf(`{"problem_id": %d, "proof": {"round": "r1"}}`, h.p.ID)
			h.s.DB.Exec(`INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('validate', ?, 'queued', ?, 0)`, ProofPriority, payload)
		}, func(t *testing.T, err error) {
			if !errors.Is(err, ErrChecking) {
				t.Errorf("error %v, want ErrChecking", err)
			}
		}},
		{"another problem's revision", func(t *testing.T, h *history) {}, func(t *testing.T, err error) {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("error %v, want ErrNotFound", err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHistory(t)
			h.version(t, 2)
			h.version(t, 3)
			h.version(t, 4)
			ctx := context.Background()
			revID := h.revs[1].ID
			if c.name == "another problem's revision" {
				other := importExample(t, h.s, "array-sum")
				r, _ := h.s.CurrentRevision(ctx, other.ID)
				revID = r.ID
			}
			c.setup(t, h)
			total := revisionCount(t, h.s, h.p.ID)
			before, _ := h.s.Get(ctx, h.p.ID)
			_, _, err := h.s.BringBack(ctx, h.p.ID, revID, 3)
			c.check(t, err)
			if n := revisionCount(t, h.s, h.p.ID); n != total {
				t.Errorf("a refused bring back made %d revisions", n-total)
			}
			if after, _ := h.s.Get(ctx, h.p.ID); after.ContentHash != before.ContentHash || after.Title != before.Title {
				t.Error("a refused bring back changed the problem")
			}
		})
	}
}

// An organiser's own Try as a student is not use (V203-98); a student's is.
func TestBringBackIgnoresStaffCode(t *testing.T) {
	h := newHistory(t)
	h.version(t, 2)
	h.version(t, 3)
	for _, u := range []int{1, 2, 3} {
		h.s.DB.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at) VALUES (?, ?, 'run', 'cpp17', 'x', 'x', 0)`, h.p.ID, u)
		h.s.DB.Exec(`INSERT INTO drafts (problem_id, user_id, language, source, updated_at) VALUES (?, ?, 'cpp17', 'x', 0)`, h.p.ID, u)
	}
	if _, _, err := h.s.BringBack(context.Background(), h.p.ID, h.revs[1].ID, 3); err != nil {
		t.Fatalf("staff rows blocked it: %v", err)
	}
}

// A contest that holds the problem, scheduled or ended, keeps the version it pinned.
func TestBringBackLeavesPinnedContestsAlone(t *testing.T) {
	h := newHistory(t)
	ctx := context.Background()
	h.version(t, 2)
	h.version(t, 3)
	holdIn(t, h.s, h.now, h.p.ID, 1, "scheduled", 1, 3, 1)
	holdIn(t, h.s, h.now, h.p.ID, 2, "ended", -10, -5, 1)
	pins := func() string {
		var out string
		h.s.DB.QueryRow("SELECT group_concat(contest_id || ':' || revision_id) FROM (SELECT * FROM contest_problems ORDER BY contest_id)").Scan(&out)
		return out
	}
	before := pins()
	if _, _, err := h.s.BringBack(ctx, h.p.ID, h.revs[1].ID, 3); err != nil {
		t.Fatal(err)
	}
	if after := pins(); after != before {
		t.Errorf("pins moved: %s to %s", before, after)
	}
}

func TestBringBackTwiceMakesTwoRevisions(t *testing.T) {
	h := newHistory(t)
	ctx := context.Background()
	h.version(t, 2)
	h.version(t, 3)
	total := revisionCount(t, h.s, h.p.ID)
	for i := range 2 {
		if _, _, err := h.s.BringBack(ctx, h.p.ID, h.revs[1].ID, 3); err != nil {
			t.Fatalf("use %d: %v", i+1, err)
		}
		if i == 0 {
			if _, _, err := h.s.BringBack(ctx, h.p.ID, h.revs[2].ID, 3); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := revisionCount(t, h.s, h.p.ID); n != total+3 {
		t.Errorf("%d new revisions for three uses, want 3", n-total)
	}
}

// Bringing back a version whose solution an AI wrote must not leave the flag
// that says a person wrote the solution on a problem the AI drafted.
func TestBringBackClearsWhoWroteTheSolutionOnlyForAnAIProblemWhoseSolutionChanges(t *testing.T) {
	for _, c := range []struct {
		name     string
		origin   string
		revision int // index in h.revs
		wantFlag int
	}{
		{"ai problem, solution differs", OriginAI, 0, 0},
		{"manual problem, solution differs", OriginManual, 0, 1},
		{"ai problem, same solution", OriginAI, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHistory(t)
			h.version(t, 2)
			h.edit(t, func(e *Edit) { e.Title = "Renamed" })
			h.s.DB.Exec("UPDATE problems SET origin = ?, solution_by_person = 1 WHERE id = ?", c.origin, h.p.ID)
			if _, _, err := h.s.BringBack(context.Background(), h.p.ID, h.revs[c.revision].ID, 3); err != nil {
				t.Fatal(err)
			}
			var flag int
			h.s.DB.QueryRow("SELECT solution_by_person FROM problems WHERE id = ?", h.p.ID).Scan(&flag)
			if flag != c.wantFlag {
				t.Errorf("solution_by_person %d, want %d", flag, c.wantFlag)
			}
		})
	}
}
