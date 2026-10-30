package httpapi

// Idempotent creates over HTTP (v4 U13): the same client_key twice makes
// one row and gives the first one back, on each of the four create routes.
// No key, or another person's request with the same key, is never a repeat.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreatesWithTheSameKeyMakeOneRow(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	c := s.createContest(owner, "c1", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "")
	bilal := s.signedIn("bilal@college.edu", "Bilal")

	count := func(q string) int {
		t.Helper()
		var n int
		if err := s.db.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	id := func(out map[string]any) any { return out["id"] }
	clarID := func(out map[string]any) any { return out["clarification"].(map[string]any)["id"] }
	// post sends body n times and returns the ids it got back.
	post := func(b *browser, path string, body map[string]any, idOf func(map[string]any) any, n int) []any {
		t.Helper()
		var ids []any
		for range n {
			code, out := b.do("POST", path, body, s.srv.URL)
			if code != 201 {
				t.Fatalf("%s: %d %v", path, code, out)
			}
			ids = append(ids, idOf(out))
		}
		return ids
	}
	same := func(what string, ids []any) {
		t.Helper()
		if ids[0] != ids[1] {
			t.Errorf("%s with one key twice: ids %v, want the first one back", what, ids)
		}
	}

	same("a contest", post(owner, "/api/admin/contests", map[string]any{"title": "Keyed", "starts_at": start.UnixMilli(),
		"ends_at": start.Add(time.Hour).UnixMilli(), "client_key": "k-contest"}, id, 2))
	if n := count("SELECT count(*) FROM contests WHERE title = 'Keyed'"); n != 1 {
		t.Errorf("contests made: %d, want 1", n)
	}

	same("a problem", post(owner, "/api/admin/problems", map[string]any{"title": "Keyed problem", "client_key": "k-problem"}, id, 2))
	if n := count("SELECT count(*) FROM problems WHERE title = 'Keyed problem'"); n != 1 {
		t.Errorf("problems made: %d, want 1", n)
	}

	news := fmt.Sprintf("/api/admin/contests/%d/announcements", c)
	same("an announcement", post(owner, news, map[string]any{"body_md": "Keyed news", "client_key": "k-news"}, id, 2))
	if n := count("SELECT count(*) FROM announcements WHERE body_md = 'Keyed news'"); n != 1 {
		t.Errorf("announcements made: %d, want 1", n)
	}

	s.at(start, 5)
	ask := "/api/contests/c1/clarifications"
	same("a question", post(asha, ask, map[string]any{"question": "Is n at least 1?", "client_key": "k-q"}, clarID, 2))
	// Bilal's request with the same key is his own question.
	post(bilal, ask, map[string]any{"question": "Is n at least 1?", "client_key": "k-q"}, clarID, 1)
	if n := count("SELECT count(*) FROM clarifications WHERE question = 'Is n at least 1?'"); n != 2 {
		t.Errorf("questions made: %d, want Asha's and Bilal's", n)
	}

	// Eight copies at once: the write gate takes one statement at a time, so
	// some pass the lookup together and the unique index decides.
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := owner.do("POST", news, map[string]any{"body_md": "Burst", "client_key": "k-burst"}, s.srv.URL)
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 201 {
			t.Errorf("a copy in the burst: %d, want 201 with the first announcement", code)
		}
	}
	if n := count("SELECT count(*) FROM announcements WHERE body_md = 'Burst'"); n != 1 {
		t.Errorf("announcements from the burst: %d, want 1", n)
	}

	// Without a key nothing is a repeat.
	post(owner, news, map[string]any{"body_md": "Plain news"}, id, 2)
	if n := count("SELECT count(*) FROM announcements WHERE body_md = 'Plain news'"); n != 2 {
		t.Errorf("announcements without a key: %d, want 2", n)
	}

	// A key longer than Submit's 64 characters is refused.
	if code, out := owner.do("POST", "/api/admin/problems", map[string]any{"title": "Long key", "client_key": strings.Repeat("k", 65)}, s.srv.URL); code != 400 {
		t.Errorf("a 65-character key: %d %v, want 400", code, out)
	}
}
