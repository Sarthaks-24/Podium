package httpapi

// Pack import when the slug is taken (v2.0.1 stage B): the 409 describes the
// existing problem, and on_conflict picks rename, archive_existing or replace.

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func (s *site) importWith(owner *browser, slug string, query url.Values) (int, map[string]any) {
	s.t.Helper()
	path := "/api/admin/problems/import"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return owner.raw("POST", path, "application/zip", packZip(s.t, slug, nil, false), s.srv.URL)
}

func TestImportConflictDescribesTheExistingProblem(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	id := s.importProblem(owner, "two-sum")
	start := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	s.createContest(owner, "Week 3 practice", start, map[string]int64{"A": id}, nil)

	code, out := s.importWith(owner, "two-sum", nil)
	e, _ := out["error"].(map[string]any)
	if code != 409 || e["code"] != "conflict" || e["suggested_slug"] != "two-sum-2" ||
		e["message"] != "A problem with the slug two-sum already exists. Choose what to do with this pack." {
		t.Fatalf("conflict: %d %v", code, out)
	}
	ex, _ := e["existing"].(map[string]any)
	open, _ := ex["open_contests"].([]any)
	if ex["id"] != float64(id) || ex["slug"] != "two-sum" || len(open) != 1 || ex["hidden_open_contests"] != float64(0) {
		t.Fatalf("existing: %v", ex)
	}
	if live, _ := ex["live_contests"].([]any); len(live) != 0 {
		t.Errorf("live contests %v", live)
	}
}

func TestImportOnConflictOverHTTP(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	id := s.importProblem(owner, "two-sum")

	// Rename: a new problem under the chosen slug.
	code, out := s.importWith(owner, "two-sum", url.Values{"on_conflict": {"rename"}, "slug": {"two-sum-2"}})
	if code != 201 || out["slug"] != "two-sum-2" || out["id"] == float64(id) {
		t.Fatalf("rename: %d %v", code, out)
	}
	if code, out := s.importWith(owner, "two-sum", url.Values{"on_conflict": {"rename"}, "slug": {"two-sum-2"}}); code != 409 {
		t.Errorf("rename to a taken slug: %d %v", code, out)
	}
	if code, out := s.importWith(owner, "two-sum", url.Values{"on_conflict": {"rename"}, "slug": {"Bad Slug"}}); code != 400 || fieldErr(out, "slug") == "" {
		t.Errorf("rename to a bad slug: %d %v", code, out)
	}
	if code, _ := s.importWith(owner, "two-sum", url.Values{"on_conflict": {"nope"}}); code != 400 {
		t.Errorf("unknown on_conflict: %d", code)
	}

	// Replace: the same problem gets a second revision.
	changed := packZip(t, "two-sum", map[string]string{"statement.md": "A changed statement."}, false)
	code, out = owner.raw("POST", "/api/admin/problems/import?on_conflict=replace&file_name=two-sum.zip", "application/zip", changed, o)
	if code != 201 || out["id"] != float64(id) || out["slug"] != "two-sum" {
		t.Fatalf("replace: %d %v", code, out)
	}
	_, got := owner.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, o)
	if rev, _ := got["revision"].(map[string]any); rev["rev"] != float64(2) {
		t.Errorf("revision after replace: %v", got["revision"])
	}

	// Archive and import: the old problem is archived, the new one has the slug.
	code, out = s.importWith(owner, "two-sum", url.Values{"on_conflict": {"archive_existing"}})
	if code != 201 || out["slug"] != "two-sum" || out["id"] == float64(id) {
		t.Fatalf("archive and import: %d %v", code, out)
	}
	_, got = owner.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, o)
	if got["archived"] != true || got["archived_slug"] != "two-sum" {
		t.Errorf("old problem: archived %v original %v", got["archived"], got["archived_slug"])
	}
}

func TestImportReplaceRefusedWhileLiveOverHTTP(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	id := s.importProblem(owner, "two-sum")
	start := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	s.createContest(owner, "Week 3 practice", start, map[string]int64{"A": id}, nil)
	s.at(start, 10)

	code, out := s.importWith(owner, "two-sum", nil)
	e, _ := out["error"].(map[string]any)
	ex, _ := e["existing"].(map[string]any)
	if live, _ := ex["live_contests"].([]any); code != 409 || len(live) != 1 {
		t.Fatalf("conflict while live: %d %v", code, out)
	}
	code, out = s.importWith(owner, "two-sum", url.Values{"on_conflict": {"replace"}})
	e, _ = out["error"].(map[string]any)
	msg, _ := e["message"].(string)
	if code != 409 || e["code"] != "problem_live" ||
		!strings.Contains(msg, "which is running. Edit it from the problem page, where you can update students.") {
		t.Fatalf("replace while live: %d %v", code, out)
	}
	// Archive and import is allowed.
	if code, out := s.importWith(owner, "two-sum", url.Values{"on_conflict": {"archive_existing"}}); code != 201 {
		t.Errorf("archive and import while live: %d %v", code, out)
	}
}
