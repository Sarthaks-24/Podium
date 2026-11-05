package httpapi

// Archive and restore over HTTP for problems and contests (v2.0.1 stage A):
// a freed slug can be used again, a restore after reuse answers 409 with a
// suggested slug, and the archive dialog's list of unended contests.

import (
	"fmt"
	"testing"
	"time"
)

func conflictSlug(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	s, _ := e["suggested_slug"].(string)
	return s
}

func TestProblemArchiveAndRestoreOverHTTP(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	id := s.importProblem(owner, "two-sum")
	path := fmt.Sprintf("/api/admin/problems/%d", id)

	if code, out := owner.do("DELETE", path, nil, o); code != 204 {
		t.Fatalf("archive: %d %v", code, out)
	}
	_, got := owner.do("GET", path, nil, o)
	if got["archived"] != true || got["archived_slug"] != "two-sum" || got["slug"] == "two-sum" {
		t.Fatalf("archived problem: slug %v archived_slug %v", got["slug"], got["archived_slug"])
	}
	// The slug can be used again.
	s.importProblem(owner, "two-sum")

	code, out := owner.do("POST", path+"/restore", map[string]any{}, o)
	if code != 409 || conflictSlug(out) != "two-sum-2" || fieldErr(out, "slug") != "Another problem now uses two-sum. Choose a new slug." {
		t.Fatalf("restore after reuse: %d %v", code, out)
	}
	code, out = owner.do("POST", path+"/restore", map[string]any{"slug": "two-sum-2"}, o)
	if code != 200 || out["slug"] != "two-sum-2" || out["archived"] != false || out["archived_slug"] != "" {
		t.Fatalf("restore with a chosen slug: %d %v", code, out)
	}
	if code, out := owner.do("POST", "/api/admin/problems/99999/restore", nil, o); code != 404 {
		t.Errorf("restore of a missing problem: %d %v", code, out)
	}
}

func TestContestArchiveAndRestoreOverHTTP(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	start := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	p := s.importProblem(owner, "cr-a")
	id := s.createContest(owner, "week-3", start, map[string]int64{"A": p}, nil)
	path := fmt.Sprintf("/api/admin/contests/%d", id)

	if code, out := owner.do("DELETE", path, nil, o); code != 204 {
		t.Fatalf("archive: %d %v", code, out)
	}
	_, got := owner.do("GET", path, nil, o)
	if got["archived"] != true || got["archived_slug"] != "week-3" || got["slug"] == "week-3" {
		t.Fatalf("archived contest: slug %v archived_slug %v", got["slug"], got["archived_slug"])
	}
	// The address is free for a new contest.
	again := s.createContest(owner, "week-3", start, map[string]int64{"A": p}, nil)

	code, out := owner.do("POST", path+"/restore", nil, o)
	if code != 409 || conflictSlug(out) != "week-3-2" {
		t.Fatalf("restore after reuse: %d %v", code, out)
	}
	code, out = owner.do("POST", path+"/restore", map[string]any{"slug": "Bad Slug"}, o)
	if code != 400 || fieldErr(out, "slug") == "" {
		t.Errorf("restore with an invalid slug: %d %v", code, out)
	}
	code, out = owner.do("POST", path+"/restore", map[string]any{"slug": "week-3-2"}, o)
	if code != 200 || out["slug"] != "week-3-2" || out["archived"] != false {
		t.Fatalf("restore with a chosen slug: %d %v", code, out)
	}
	_ = again
}

// The problem page lists the unended contests that pin it, so the archive
// dialog can warn that the problem cannot be updated there until restored.
func TestProblemPageListsOpenContests(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	p := s.importProblem(owner, "open-a")
	start := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	s.createContest(owner, "Week 3 practice", start, map[string]int64{"A": p}, nil)
	_, got := owner.do("GET", fmt.Sprintf("/api/admin/problems/%d", p), nil, o)
	open, _ := got["open_contests"].([]any)
	if len(open) != 1 || got["hidden_open_contests"] != float64(0) {
		t.Fatalf("open contests %v hidden %v", got["open_contests"], got["hidden_open_contests"])
	}
	if title := open[0].(map[string]any)["title"]; title != "Week 3 practice" {
		t.Errorf("title %v", title)
	}
}
