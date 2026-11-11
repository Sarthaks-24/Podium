package contests

import (
	"context"
	"sync"
	"testing"
)

// V203-64: a taken slug and a repeated request key are told apart from the
// driver's error code, with the real SQLite driver. Parallel copies of one
// request pass the lookup before any insert, so the insert is what refuses
// all but one of them.

func inParallel(n int, f func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			f(i)
		}(i)
	}
	close(start)
	wg.Wait()
}

func TestTakenSlugGetsTheNextNumber(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	first := r.contest(t, nil)
	second := r.contest(t, nil)
	if first.Slug != "week-1" || second.Slug != "week-1-2" {
		t.Errorf("slugs %q and %q, want week-1 and week-1-2", first.Slug, second.Slug)
	}
	clone, err := r.svc.Clone(ctx, first.ID, "", "Week 1", t0, at(60), owner)
	if err != nil || clone.Slug != "week-1-3" {
		t.Errorf("clone onto a taken slug: %+v, %v; want week-1-3", clone, err)
	}
}

func TestParallelCreatesWithOneKeyMakeOneContest(t *testing.T) {
	r := newSvc(t)
	ids := make([]int64, 12)
	inParallel(len(ids), func(i int) {
		c, err := r.svc.Create(context.Background(), Edit{Title: "Same", StartsAt: t0, EndsAt: at(60), MinGapS: 30,
			MaxPerProblem: 15, Languages: []string{"cpp17"}}, 1, "k1")
		if err != nil {
			t.Errorf("create %d: %v", i, err)
			return
		}
		ids[i] = c.ID
	})
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("ids %v: one request key must give one contest", ids)
		}
	}
	if n := count(t, r, "SELECT count(*) FROM contests"); n != 1 {
		t.Errorf("%d contests stored, want 1", n)
	}
}

func TestParallelCreatesWithOneTitleAndOtherKeysGetOtherSlugs(t *testing.T) {
	r := newSvc(t)
	inParallel(6, func(i int) {
		if _, err := r.svc.Create(context.Background(), Edit{Title: "Same", StartsAt: t0, EndsAt: at(60), MinGapS: 30,
			MaxPerProblem: 15, Languages: []string{"cpp17"}}, 1, string(rune('a'+i))); err != nil {
			t.Errorf("create %d: %v", i, err)
		}
	})
	if n := count(t, r, "SELECT count(DISTINCT slug) FROM contests"); n != 6 {
		t.Errorf("%d distinct slugs, want 6", n)
	}
}

func TestParallelAnnouncementsAndQuestionsWithOneKeyStoreOne(t *testing.T) {
	r := newSvc(t)
	c := r.contest(t, nil)
	inParallel(12, func(i int) {
		if _, err := r.svc.Announce(context.Background(), c, "Hello", 1, "ann"); err != nil {
			t.Errorf("announce %d: %v", i, err)
		}
		if _, err := r.svc.Ask(context.Background(), c, 2, nil, "Why?", "ask"); err != nil {
			t.Errorf("ask %d: %v", i, err)
		}
	})
	if n := count(t, r, "SELECT count(*) FROM announcements WHERE contest_id = ?", c.ID); n != 1 {
		t.Errorf("%d announcements, want 1", n)
	}
	if n := count(t, r, "SELECT count(*) FROM clarifications WHERE contest_id = ?", c.ID); n != 1 {
		t.Errorf("%d questions, want 1", n)
	}
}
