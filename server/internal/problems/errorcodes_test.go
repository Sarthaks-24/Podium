package problems

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// V203-64: a taken slug and a repeated request key are told apart from the
// driver's error code, with the real SQLite driver.

func TestParallelCreatesWithOneKeyMakeOneProblem(t *testing.T) {
	s := newGenBank(t)
	ids := make([]int64, 12)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			p, err := s.Create(context.Background(), "Pair sums", "", 1, "k1")
			if err != nil {
				t.Errorf("create %d: %v", i, err)
				return
			}
			ids[i] = p.ID
		}(i)
	}
	close(start)
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("ids %v: one request key must give one problem", ids)
		}
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM problems").Scan(&n)
	if n != 1 {
		t.Errorf("%d problems stored, want 1", n)
	}
}

func TestInsertTellsAKeyRepeatFromATakenSlug(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, "Pair sums", "pair-sums", 1, "k1"); err != nil {
		t.Fatal(err)
	}
	try := func(slug, key string) error {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		p := &Problem{Title: "Other", Slug: slug, Statement: "s", TimeLimitMS: 1000, MemoryLimitMB: 256,
			Checker: CheckerTokens, Reference: Source{"cpp17", "x"}, Tags: []string{}, Samples: []int{}, Origin: OriginManual, AuthorID: 1}
		return s.insert(ctx, tx, p, RevisionNote{By: 1}, key)
	}
	if err := try("another", "k1"); !errors.Is(err, errRepeat) {
		t.Errorf("same key, new slug: %v, want errRepeat", err)
	}
	var fields FieldErrors
	if err := try("pair-sums", "k2"); !errors.As(err, &fields) || fields["slug"] == "" {
		t.Errorf("new key, taken slug: %v, want a slug field error", err)
	}
	if err := try("pair-sums", ""); !errors.As(err, &fields) || fields["slug"] == "" {
		t.Errorf("no key, taken slug: %v, want a slug field error", err)
	}
	if err := try("fresh", "k3"); err != nil {
		t.Errorf("a free slug and key: %v", err)
	}
}

func TestUpdateToATakenSlugIsRefused(t *testing.T) {
	s := newGenBank(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, "First", "first", 1, ""); err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(ctx, "Second", "second", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	e := editOf(b)
	e.Slug = "first"
	_, err = s.Update(ctx, b.ID, e)
	var fields FieldErrors
	if !errors.As(err, &fields) || fields["slug"] != "Another problem already uses the slug first." {
		t.Errorf("update to a taken slug: %v", err)
	}
}
