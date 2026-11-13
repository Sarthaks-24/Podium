package httpapi

// Bring back an earlier version over HTTP (v2.1.0 stage H): the history
// list, the bring back route, its refusals with their exact copy, and who may
// call either.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type bringRig struct {
	s     *site
	org   *browser
	id    int64
	first int64 // the revision the import made
	query string
}

// newBringRig imports a problem and edits its statement, so it has two
// revisions that differ in the statement and nothing else.
func newBringRig(t *testing.T) *bringRig {
	t.Helper()
	s := newSite(t)
	org := s.signedIn("owner@college.edu", "Owner")
	code, out := org.raw("POST", "/api/admin/problems/import", "application/zip", packZip(t, "bb-source", nil, false), s.srv.URL)
	if code != 201 {
		t.Fatalf("import: %d %v", code, out)
	}
	r := &bringRig{s: s, org: org, id: int64(out["id"].(float64))}
	r.query = fmt.Sprintf("/api/admin/problems/%d", r.id)
	edit := map[string]any{}
	for _, k := range []string{"title", "slug", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "epsilon",
		"checker_source", "reference", "brute", "validator", "tags", "difficulty", "samples"} {
		edit[k] = out[k]
	}
	var subtasks []map[string]any
	for _, st := range out["subtasks"].([]any) {
		m := st.(map[string]any)
		subtasks = append(subtasks, map[string]any{"points": m["points"], "title": m["title"], "tests": m["tests"]})
	}
	edit["subtasks"] = subtasks
	edit["statement_md"] = "A different statement.\n"
	if code, got := org.do("PUT", r.query, edit, s.srv.URL); code != 200 {
		t.Fatalf("edit: %d %v", code, got)
	}
	revs := r.revisions(t)
	if len(revs) != 2 {
		t.Fatalf("%d revisions, want 2", len(revs))
	}
	r.first = int64(revs[1].(map[string]any)["id"].(float64))
	return r
}

func (r *bringRig) revisions(t *testing.T) []any {
	t.Helper()
	code, out := r.org.do("GET", r.query+"/revisions", nil, r.s.srv.URL)
	if code != 200 {
		t.Fatalf("history: %d %v", code, out)
	}
	return out["revisions"].([]any)
}

func (r *bringRig) bring(rev int64) (int, map[string]any) {
	return r.org.do("POST", fmt.Sprintf("%s/revisions/%d/bring-back", r.query, rev), nil, r.s.srv.URL)
}

func TestBringBackOverHTTP(t *testing.T) {
	r := newBringRig(t)
	code, out := r.org.do("GET", r.query+"/revisions", nil, r.s.srv.URL)
	revs := out["revisions"].([]any)
	if code != 200 || len(revs) != 2 || out["total"] != float64(2) {
		t.Fatalf("history %d %v", code, out)
	}
	newest, oldest := revs[0].(map[string]any), revs[1].(map[string]any)
	if newest["rev"] != float64(2) || newest["current"] != true || newest["can_bring_back"] != true ||
		oldest["rev"] != float64(1) || oldest["current"] != false || oldest["can_bring_back"] != true {
		t.Errorf("entries %v", revs)
	}
	if _, ok := newest["created_at"].(float64); !ok || newest["created_by"] == nil || newest["reason"] == nil {
		t.Errorf("an entry misses a field: %v", newest)
	}

	r.s.validate(r.id)
	code, out = r.bring(r.first)
	if code != 200 || out["problem_id"] != float64(r.id) || out["rev"] != float64(3) || out["validation_queued"] != true {
		t.Fatalf("bring back: %d %v", code, out)
	}
	if list, ok := out["not_ready"].([]any); !ok || len(list) != 0 {
		t.Errorf("not_ready %v", out["not_ready"])
	}
	_, got := r.org.do("GET", r.query, nil, r.s.srv.URL)
	if got["statement_md"] == "A different statement.\n" || got["validated"] != false {
		t.Errorf("after bringing back: statement %q validated %v", got["statement_md"], got["validated"])
	}
	if n := len(r.revisions(t)); n != 3 {
		t.Errorf("%d revisions, want 3", n)
	}
}

func TestBringBackRefusalsOverHTTP(t *testing.T) {
	const (
		inUse   = "Students have code for this problem, so an older version cannot replace it. Copy it as a new problem instead."
		running = "A contest that is running uses this problem. Try again after it ends."
		notKept = "This version's files were not kept, so it cannot be brought back."
		current = "This is already the current version."
	)
	cases := []struct {
		name   string
		setup  func(t *testing.T, r *bringRig)
		status int
		code   string
		msg    string
	}{
		{"student code", func(t *testing.T, r *bringRig) {
			r.s.signedIn("asha@college.edu", "Asha")
			if _, err := r.s.db.Exec(`INSERT INTO submissions (problem_id, user_id, kind, language, source, source_sha, created_at)
				VALUES (?, (SELECT id FROM users WHERE email = 'asha@college.edu'), 'submit', 'cpp17', 'x', 'x', 0)`, r.id); err != nil {
				t.Fatal(err)
			}
		}, 409, "in_use", inUse},
		{"running contest", func(t *testing.T, r *bringRig) {
			now := time.Now()
			r.s.db.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, published, practice_after_end, created_at, updated_at)
				VALUES (1, 'wk3', 'Week 3', ?, ?, 1, 0, 0, 0)`, now.Add(-time.Hour).UnixMilli(), now.Add(time.Hour).UnixMilli())
			r.s.db.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id)
				VALUES (1, ?, 'A', 0, (SELECT max(id) FROM problem_revisions WHERE problem_id = ?))`, r.id, r.id)
		}, 409, "contest_running", running},
		{"archived", func(t *testing.T, r *bringRig) {
			if code, out := r.org.do("DELETE", r.query, nil, r.s.srv.URL); code >= 300 {
				t.Fatalf("archive: %d %v", code, out)
			}
		}, 409, "archived", "A plus B (bb-source) is archived, so a version cannot be brought back. Restore it first."},
		{"files not kept", func(t *testing.T, r *bringRig) {
			var sha string
			r.s.db.QueryRow("SELECT reference_sha FROM problem_revisions WHERE id = ?", r.first).Scan(&sha)
			if err := os.Remove(filepath.Join(r.s.bank.Blobs.Dir, sha[:2], sha)); err != nil {
				t.Fatal(err)
			}
		}, 409, "not_kept", notKept},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newBringRig(t)
			c.setup(t, r)
			code, out := r.bring(r.first)
			errIs(t, c.name, code, out, c.status, c.code, c.msg)
			if n := revisions(t, r.s, r.id); n != 2 {
				t.Errorf("a refused bring back left %d revisions", n)
			}
		})
	}
	t.Run("current version", func(t *testing.T) {
		r := newBringRig(t)
		revs := r.revisions(t)
		code, out := r.bring(int64(revs[0].(map[string]any)["id"].(float64)))
		errIs(t, "current", code, out, 409, "current", current)
	})
	t.Run("files not kept shows in the history", func(t *testing.T) {
		r := newBringRig(t)
		var sha string
		r.s.db.QueryRow("SELECT reference_sha FROM problem_revisions WHERE id = ?", r.first).Scan(&sha)
		os.Remove(filepath.Join(r.s.bank.Blobs.Dir, sha[:2], sha))
		if r.revisions(t)[1].(map[string]any)["can_bring_back"] != false {
			t.Error("a version with a missing file is offered")
		}
	})
}

func TestBringBackRoutesRolesAndBadInput(t *testing.T) {
	r := newBringRig(t)
	post := fmt.Sprintf("%s/revisions/%d/bring-back", r.query, r.first)
	get := r.query + "/revisions"
	student := r.s.signedIn("asha@college.edu", "Asha")
	anon := r.s.browser()
	for _, c := range []struct {
		who    string
		b      *browser
		status int
	}{{"student", student, 403}, {"anonymous", anon, 401}} {
		if code, _ := c.b.do("GET", get, nil, r.s.srv.URL); code != c.status {
			t.Errorf("%s history: %d, want %d", c.who, code, c.status)
		}
		if code, _ := c.b.do("POST", post, nil, r.s.srv.URL); code != c.status {
			t.Errorf("%s bring back: %d, want %d", c.who, code, c.status)
		}
	}
	if n := revisions(t, r.s, r.id); n != 2 {
		t.Errorf("refused callers made %d revisions", n)
	}

	for _, c := range []struct {
		name   string
		method string
		path   string
		status int
		code   string
	}{
		{"history of an unknown problem", "GET", "/api/admin/problems/99999/revisions", 404, "not_found"},
		{"history with a bad id", "GET", "/api/admin/problems/x/revisions", 404, "not_found"},
		{"unknown problem", "POST", "/api/admin/problems/99999/revisions/1/bring-back", 404, "not_found"},
		{"bad problem id", "POST", "/api/admin/problems/x/revisions/1/bring-back", 404, "not_found"},
		{"bad revision id", "POST", r.query + "/revisions/x/bring-back", 404, "not_found"},
		{"zero revision id", "POST", r.query + "/revisions/0/bring-back", 404, "not_found"},
		{"unknown revision", "POST", r.query + "/revisions/99999/bring-back", 404, "not_found"},
	} {
		code, out := r.org.do(c.method, c.path, nil, r.s.srv.URL)
		errIs(t, c.name, code, out, c.status, c.code, "")
	}

	// A revision of another problem is not found, and nothing changes.
	code, out := r.org.raw("POST", "/api/admin/problems/import", "application/zip", packZip(t, "bb-other", nil, false), r.s.srv.URL)
	if code != 201 {
		t.Fatalf("import: %d %v", code, out)
	}
	other := int64(out["id"].(float64))
	var otherRev int64
	r.s.db.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ?", other).Scan(&otherRev)
	code, out = r.bring(otherRev)
	errIs(t, "another problem's revision", code, out, 404, "not_found", "")

	// An organiser may call both.
	if code, out := r.org.do("POST", "/api/admin/people", map[string]any{"email": "setter@college.edu", "role": "organiser"}, r.s.srv.URL); code >= 300 {
		t.Fatalf("make organiser: %d %v", code, out)
	}
	setter := r.s.signedIn("setter@college.edu", "Setter")
	if code, _ := setter.do("GET", get, nil, r.s.srv.URL); code != 200 {
		t.Errorf("organiser history: %d", code)
	}
	if code, out := setter.do("POST", post, nil, r.s.srv.URL); code != 200 {
		t.Errorf("organiser bring back: %d %v", code, out)
	}
}
