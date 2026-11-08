package problems

// AI edit, the problems side (v2.0.2 stage E): the snapshot the model sees,
// the merge of its reply over the current pack, and Accept as one revision.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func editTestBank(t *testing.T, slug string) (*Service, *Problem) {
	t.Helper()
	s := newBank(t)
	return s, importExample(t, s, slug)
}

func TestEditSnapshotHasNoTestData(t *testing.T) {
	s, p := editTestBank(t, "a-plus-b")
	snap, err := s.EditSnapshot(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Hash == "" || len(snap.Files["statement.md"]) == 0 || len(snap.Files["problem.yaml"]) == 0 {
		t.Fatalf("snapshot %v hash %q", snap.Files, snap.Hash)
	}
	for name := range snap.Files {
		if strings.HasPrefix(name, "tests/") {
			t.Errorf("test data in the snapshot: %s", name)
		}
	}
}

func TestMergeEditKeepsFilesTheModelDidNotReturnAndSavesNothing(t *testing.T) {
	s, p := editTestBank(t, "a-plus-b")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	merged, err := s.MergeEdit(ctx, snap, map[string][]byte{"statement.md": []byte(string(snap.Files["statement.md"]) + "\nExtra line.\n")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged["statement.md"]), "Extra line.") {
		t.Error("the model's statement is not in the merge")
	}
	for name, body := range snap.Files {
		if name != "statement.md" && string(merged[name]) != string(body) {
			t.Errorf("%s changed though the model did not return it", name)
		}
	}
	got, _ := s.Get(ctx, p.ID)
	if got.Statement != p.Statement || revisionCount(t, s, p.ID) != 1 {
		t.Error("a merge saved something")
	}
}

func TestMergeEditRefusals(t *testing.T) {
	s, p := editTestBank(t, "a-plus-b")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	yaml := string(snap.Files["problem.yaml"])
	for _, c := range []struct {
		name  string
		reply map[string][]byte
		want  string
	}{
		{"test data", map[string][]byte{"tests/01.in": []byte("1 2\n")}, "cannot change test data"},
		{"unknown file", map[string][]byte{"notes.txt": []byte("x")}, "is not a pack file"},
		{"folder", map[string][]byte{"../solution.cpp": []byte("x")}, "is not a pack file"},
		{"slug", map[string][]byte{"problem.yaml": []byte(strings.Replace(yaml, "slug: a-plus-b", "slug: other-slug", 1))}, "slug"},
		{"bad yaml", map[string][]byte{"problem.yaml": []byte("title: [")}, "problem.yaml"},
		{"empty reply", map[string][]byte{}, "did not change"},
	} {
		_, err := s.MergeEdit(ctx, snap, c.reply)
		var pe *PackError
		if !errors.As(err, &pe) || !strings.Contains(strings.Join(pe.Problems, " "), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

func TestAcceptEditMakesOneRevisionMarksADraftAndNeedsValidate(t *testing.T) {
	s, p := editTestBank(t, "a-plus-b")
	ctx := context.Background()
	if _, err := s.RequestValidation(ctx, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	fakeJudge(t, s, 50, nil)
	if got, _ := s.Get(ctx, p.ID); got.ValidatedHash != got.ContentHash {
		t.Fatal("setup: the problem is not validated")
	}
	snap, _ := s.EditSnapshot(ctx, p.ID)
	merged, err := s.MergeEdit(ctx, snap, map[string][]byte{"statement.md": []byte(string(snap.Files["statement.md"]) + "\nExtra line.\n")})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.AcceptEdit(ctx, p.ID, snap.Hash, merged, 0, "make it longer")
	if err != nil {
		t.Fatal(err)
	}
	if n := revisionCount(t, s, p.ID); n != 2 {
		t.Errorf("%d revisions, want 2", n)
	}
	if out.Origin != OriginAI || !hasTag(out.Tags, AIDraftTag) || !strings.Contains(out.Statement, "Extra line.") || out.Slug != p.Slug {
		t.Errorf("accepted: origin %s tags %v slug %s", out.Origin, out.Tags, out.Slug)
	}
	if len(out.Tests) != len(p.Tests) || out.Tests[0].InputSHA != p.Tests[0].InputSHA {
		t.Errorf("the tests changed: %d vs %d", len(out.Tests), len(p.Tests))
	}
	rev, _ := s.CurrentRevision(ctx, p.ID)
	if !strings.HasPrefix(rev.Reason, "AI edit: make it longer") {
		t.Errorf("revision reason %q", rev.Reason)
	}
	// Not ready until Validate passes on the new content.
	if out.ValidatedHash == out.ContentHash {
		t.Error("the edited problem counts as validated before Validate ran")
	}
	if _, err := s.RequestValidation(ctx, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	fakeJudge(t, s, 50, nil)
	if got, _ := s.Get(ctx, p.ID); got.ValidatedHash != got.ContentHash {
		t.Error("Validate on the edited content did not pass")
	}
}

func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

func TestAcceptEditRefusals(t *testing.T) {
	s, p := editTestBank(t, "a-plus-b")
	ctx := context.Background()
	snap, _ := s.EditSnapshot(ctx, p.ID)
	merged, _ := s.MergeEdit(ctx, snap, map[string][]byte{"statement.md": []byte(string(snap.Files["statement.md"]) + "\nMore.\n")})

	// The problem changed after the draft.
	if _, err := s.AcceptEdit(ctx, p.ID, "an-older-hash", merged, 0, "x"); !errors.Is(err, ErrEditStale) {
		t.Errorf("stale: %v", err)
	}
	// A running contest holds it.
	now := time.UnixMilli(1_800_000_000_000)
	s.Now = func() time.Time { return now }
	rev, _ := s.CurrentRevision(ctx, p.ID)
	s.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
		VALUES (1, 'wk3', 'Week 3', ?, ?, 1, 0, 0, 0)`, now.Add(-time.Hour).UnixMilli(), now.Add(time.Hour).UnixMilli())
	s.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, p.ID, rev.ID)
	var live *ReplaceLiveError
	if _, err := s.AcceptEdit(ctx, p.ID, snap.Hash, merged, 0, "x"); !errors.As(err, &live) {
		t.Errorf("running contest: %v", err)
	}
	if n := revisionCount(t, s, p.ID); n != 1 {
		t.Errorf("a refused accept made %d revisions", n)
	}
	// Ended contest: allowed, and the pinned revision is untouched.
	s.Now = func() time.Time { return now.Add(3 * time.Hour) }
	if _, err := s.AcceptEdit(ctx, p.ID, snap.Hash, merged, 0, "x"); err != nil {
		t.Fatalf("after the contest: %v", err)
	}
	var pinned int64
	s.DB.QueryRow("SELECT revision_id FROM contest_problems WHERE contest_id = 1").Scan(&pinned)
	if pinned != rev.ID {
		t.Errorf("the pinned revision moved: %d to %d", rev.ID, pinned)
	}
	// Deleted.
	s.DB.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", p.ID)
	if _, err := s.EditSnapshot(ctx, p.ID); !errors.Is(err, ErrDeleted) {
		t.Errorf("snapshot of a deleted problem: %v", err)
	}
	if _, err := s.AcceptEdit(ctx, p.ID, snap.Hash, merged, 0, "x"); !errors.Is(err, ErrDeleted) {
		t.Errorf("accept on a deleted problem: %v", err)
	}
}

func TestEditCustomCheckerAndFunctionProblemsRoundTrip(t *testing.T) {
	for _, slug := range []string{"pair-with-sum", "array-sum"} {
		s, p := editTestBank(t, slug)
		ctx := context.Background()
		snap, err := s.EditSnapshot(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if slug == "pair-with-sum" && snap.Files["checker.cpp"] == nil {
			t.Error("the custom checker is not offered to the model")
		}
		if slug == "array-sum" && !strings.Contains(string(snap.Files["problem.yaml"]), "mode: function") {
			t.Error("the function problem lost its mode")
		}
		merged, err := s.MergeEdit(ctx, snap, map[string][]byte{"statement.md": []byte(string(snap.Files["statement.md"]) + "\nMore.\n")})
		if err != nil {
			t.Fatalf("%s merge: %v", slug, err)
		}
		out, err := s.AcceptEdit(ctx, p.ID, snap.Hash, merged, 0, "x")
		if err != nil {
			t.Fatalf("%s accept: %v", slug, err)
		}
		if out.Mode != p.Mode || out.Checker != p.Checker || out.CheckerSource != p.CheckerSource || len(out.LockedTop) != len(p.LockedTop) {
			t.Errorf("%s changed more than the statement", slug)
		}
	}
}
