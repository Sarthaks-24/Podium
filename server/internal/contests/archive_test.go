package contests

import (
	"context"
	"errors"
	"testing"
)

func TestArchiveFreesTheContestSlugOnce(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	if err := r.svc.Archive(ctx, c.ID, owner); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Archive(ctx, c.ID, owner); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.Get(ctx, c.ID)
	if !got.Archived || got.ArchivedSlug != "week-1" || got.Slug != archivedForm("week-1", c.ID) {
		t.Fatalf("archived: slug %q original %q", got.Slug, got.ArchivedSlug)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = ?", c.ID, AuditArchive); n != 1 {
		t.Errorf("%d archive audit rows, want 1 (the second archive changed nothing)", n)
	}
	// A new contest takes the address; the restore must pick another.
	if again := r.contest(t, nil); again.Slug != "week-1" {
		t.Fatalf("new contest slug %q", again.Slug)
	}
	var taken *SlugTakenError
	if _, err := r.svc.Restore(ctx, c.ID, "", owner); !errors.As(err, &taken) || taken.Suggested != "week-1-2" {
		t.Fatalf("restore after reuse: %v", err)
	}
	back, err := r.svc.Restore(ctx, c.ID, "week-one", owner)
	if err != nil || back.Slug != "week-one" || back.Archived || back.ArchivedSlug != "" {
		t.Fatalf("restore with a chosen slug: %v %+v", err, back)
	}
	if n := count(t, r, "SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = ?", c.ID, AuditRestore); n != 1 {
		t.Errorf("%d restore audit rows, want 1", n)
	}
}
