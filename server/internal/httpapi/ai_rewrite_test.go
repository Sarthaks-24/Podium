package httpapi

// Rewrite with AI over HTTP (v2.1.0 stage W): the wizard run that starts
// from a problem and what the model is sent, the old problem untouched until
// Replace, Replace as one revision with an archived copy, its refusals with
// their exact copy, and roles and bad input on both routes.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
)

// Hidden tests of the source problem carry these, so a request that holds
// either has leaked a hidden test.
const (
	hiddenIn  = "HIDDEN-INPUT-MARKER-5"
	hiddenOut = "HIDDEN-OUTPUT-MARKER-6"
)

type rewriteRig struct {
	s     *site
	org   *browser
	stand *standIn
	key   string
	old   int64
}

// newRewriteRig is a production-mode site with an Anthropic stand-in that
// answers every request with the wizard's canned pack, an organiser who
// holds a key for it and one validated problem whose hidden tests carry
// markers.
func newRewriteRig(t *testing.T) *rewriteRig {
	t.Helper()
	s := newSite(t)
	s.ai.BaseURLs = map[string]string{}
	var rb [12]byte
	rand.Read(rb[:])
	key := "sk-ant-api03-PODIUMTEST" + hex.EncodeToString(rb[:])
	answer, _ := json.Marshal(ai.FakeMaxSubarray())
	stand := &standIn{provider: ai.ProviderAnthropic, key: key, answer: string(answer)}
	api := httptest.NewServer(stand)
	t.Cleanup(api.Close)
	s.ai.BaseURLs[ai.ProviderAnthropic] = api.URL
	org := s.signedIn("owner@college.edu", "Owner")
	if code, out := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": ai.ProviderAnthropic, "model": "claude-new-1", "key": key}, s.srv.URL); code != 200 {
		t.Fatalf("key: %d %v", code, out)
	}
	code, out := org.raw("POST", "/api/admin/problems/import", "application/zip",
		packZip(t, "rw-source", map[string]string{"tests/05.in": hiddenIn + "\n", "tests/06.out": hiddenOut + "\n"}, false), s.srv.URL)
	if code != 201 {
		t.Fatalf("import: %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	s.validate(id)
	return &rewriteRig{s: s, org: org, stand: stand, key: key, old: id}
}

func (r *rewriteRig) startRewrite(t *testing.T, oldID int64, change string) (int, map[string]any) {
	t.Helper()
	return startWizardWhenFree(t, r.org, r.s.srv.URL, map[string]any{"mode": "paste", "text": change, "rewrite_of": oldID})
}

// rewrite runs a rewrite to its end and returns the run's JSON.
func (r *rewriteRig) rewrite(t *testing.T, oldID int64, change string) map[string]any {
	t.Helper()
	code, started := r.startRewrite(t, oldID, change)
	if code != 202 {
		t.Fatalf("start rewrite: %d %v", code, started)
	}
	w := pollWizard(t, r.org, started["wizard_id"].(string))
	if w["state"] != "draft_created" {
		t.Fatalf("rewrite run: %v", w)
	}
	return w
}

func (r *rewriteRig) export(t *testing.T, id int64) []byte {
	t.Helper()
	resp, err := r.org.client.Get(fmt.Sprintf("%s/api/admin/problems/%d/export", r.s.srv.URL, id))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("export %d: %d", id, resp.StatusCode)
	}
	return b
}

// completeDraft imports a validated problem with its own statement and marks
// it an AI draft, as a rewrite looks once the judge has made its tests.
func (r *rewriteRig) completeDraft(t *testing.T) int64 {
	t.Helper()
	code, out := r.org.raw("POST", "/api/admin/problems/import", "application/zip",
		packZip(t, "rw-full", map[string]string{"statement.md": "The rewritten statement.\n\n## Input\n\nTwo integers.\n\n## Output\n\nTheir sum.\n\n## Constraints\n\nSmall.\n"}, false), r.s.srv.URL)
	if code != 201 {
		t.Fatalf("import draft: %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	r.s.db.Exec("UPDATE problems SET origin = 'ai' WHERE id = ?", id)
	r.s.validate(id)
	return id
}

func (r *rewriteRig) replace(t *testing.T, oldID, draftID int64, base string) (int, map[string]any) {
	t.Helper()
	return r.org.do("POST", fmt.Sprintf("/api/admin/problems/%d/replace-with-rewrite", oldID), map[string]any{"draft_id": draftID, "base": base}, r.s.srv.URL)
}

func rewriteOf(t *testing.T, w map[string]any) (oldID int64, base string, draftID int64) {
	t.Helper()
	link, _ := w["rewrite_of"].(map[string]any)
	if link == nil || link["base"] == "" || link["title"] == "" {
		t.Fatalf("the run does not remember what it rewrites: %v", w)
	}
	return int64(link["problem_id"].(float64)), link["base"].(string), int64(w["problem_id"].(float64))
}

func TestRewriteRequestHasTheProblemAndNoHiddenTests(t *testing.T) {
	r := newRewriteRig(t)
	before := r.export(t, r.old)
	_, problemBefore := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", r.old), nil, "")
	w := r.rewrite(t, r.old, "The limits are wrong.")

	oldID, base, draftID := rewriteOf(t, w)
	if oldID != r.old || base == "" || draftID == r.old {
		t.Errorf("link: old %d base %q draft %d", oldID, base, draftID)
	}
	r.stand.mu.Lock()
	posts := append([]string{}, r.stand.posts...)
	r.stand.mu.Unlock()
	if len(posts) != 1 {
		t.Fatalf("%d model requests, want 1", len(posts))
	}
	// The body is JSON, so read the user text back out of it.
	var req struct {
		Messages []struct{ Content any } `json:"messages"`
	}
	if err := json.Unmarshal([]byte(posts[0]), &req); err != nil || len(req.Messages) != 1 {
		t.Fatalf("request: %v %s", err, posts[0])
	}
	user := fmt.Sprint(req.Messages[0].Content)
	for _, want := range []string{
		"You are given $t$ pairs of integers", // the statement
		"Reference solution (cpp17)",          // the reference
		"Sample 1 input:", "Sample 2 output:", // the samples
		"The limits are wrong.", // what should change
		"Time limit: 1000 ms",   // the settings
		"Students write: a whole program",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("the request lacks %q", want)
		}
	}
	for _, leak := range []string{hiddenIn, hiddenOut, "Sample 3", r.key} {
		if strings.Contains(posts[0], leak) {
			t.Errorf("the request holds %q", leak)
		}
	}

	// The old problem is exactly as it was, and the rewrite is a problem of its own.
	_, problemAfter := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", r.old), nil, "")
	if fmt.Sprint(problemBefore) != fmt.Sprint(problemAfter) || string(before) != string(r.export(t, r.old)) || revisions(t, r.s, r.old) != 1 {
		t.Error("the old problem changed before Replace")
	}
	_, draft := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", draftID), nil, "")
	if draft["origin"] != "ai" || draft["slug"] == problemAfter["slug"] {
		t.Errorf("the rewrite draft: origin %v slug %v", draft["origin"], draft["slug"])
	}
}

func TestRewriteStartRolesAndBadInput(t *testing.T) {
	r := newRewriteRig(t)
	student := r.s.signedIn("asha@college.edu", "Asha")
	body := func(m map[string]any) map[string]any { m["mode"] = "paste"; return m }
	url := "/api/admin/ai/wizard"
	if code, _ := student.do("POST", url, body(map[string]any{"text": "x", "rewrite_of": r.old}), r.s.srv.URL); code != 403 {
		t.Errorf("student: %d", code)
	}
	if code, _ := r.s.browser().do("POST", url, body(map[string]any{"text": "x", "rewrite_of": r.old}), r.s.srv.URL); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
	for _, c := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
		msg    string
	}{
		{"empty change", body(map[string]any{"text": "  ", "rewrite_of": r.old}), 400, "bad_request", "Say what should change."},
		{"long change", body(map[string]any{"text": strings.Repeat("a", ai.MaxInstruction+1), "rewrite_of": r.old}), 400, "bad_request", fmt.Sprintf("Keep what should change under %d characters.", ai.MaxInstruction)},
		{"brief mode", map[string]any{"mode": "brief", "brief": map[string]any{"topic": "x", "difficulty": "easy"}, "rewrite_of": r.old}, 400, "bad_request", "Choose paste to rewrite a problem."},
		{"no such problem", body(map[string]any{"text": "x", "rewrite_of": 99999}), 404, "not_found", "There is no such problem."},
	} {
		code, out := r.org.do("POST", url, c.body, r.s.srv.URL)
		errIs(t, c.name, code, out, c.status, c.code, c.msg)
	}
	r.s.db.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", r.old)
	code, out := r.org.do("POST", url, body(map[string]any{"text": "x", "rewrite_of": r.old}), r.s.srv.URL)
	errIs(t, "deleted", code, out, 409, "deleted", "")
	r.s.db.Exec("UPDATE problems SET deleted = 0 WHERE id = ?", r.old)
	if code, _ := r.org.do("DELETE", fmt.Sprintf("/api/admin/problems/%d", r.old), nil, r.s.srv.URL); code != 204 {
		t.Fatalf("archive: %d", code)
	}
	code, out = r.org.do("POST", url, body(map[string]any{"text": "x", "rewrite_of": r.old}), r.s.srv.URL)
	errIs(t, "archived", code, out, 409, "archived", "A plus B (rw-source) is archived. Restore it first, then rewrite it.")
	r.stand.mu.Lock()
	n := len(r.stand.posts)
	r.stand.mu.Unlock()
	if n != 0 {
		t.Errorf("a refused start made %d model requests", n)
	}
}

func TestReplaceOverHTTPMakesOneRevisionAndRefusesTheSecond(t *testing.T) {
	r := newRewriteRig(t)
	w := r.rewrite(t, r.old, "Tighten the limits.")
	oldID, base, draftID := rewriteOf(t, w)
	before := r.export(t, oldID)

	// Before Validate has passed the rewrite is refused and nothing moves.
	code, out := r.replace(t, oldID, draftID, base)
	errIs(t, "not validated", code, out, 409, "not_validated", "Validate this rewrite first.")
	if revisions(t, r.s, oldID) != 1 || string(before) != string(r.export(t, oldID)) {
		t.Error("a refused replace changed the old problem")
	}

	// The wizard's canned draft has no tests until the judge makes them, so
	// the replace uses a complete AI draft with a different statement.
	draftID = r.completeDraft(t)
	_, draft := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", draftID), nil, "")
	code, out = r.replace(t, oldID, draftID, base)
	if code != 200 || int64(out["problem_id"].(float64)) != oldID || out["validation_queued"] != true || out["copy_id"] == nil ||
		!strings.HasPrefix(out["copy_title"].(string), "A plus B (rw-source) (before rewrite, ") {
		t.Fatalf("replace: %d %v", code, out)
	}
	if revisions(t, r.s, oldID) != 2 {
		t.Errorf("%d revisions, want 2", revisions(t, r.s, oldID))
	}
	_, now := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", oldID), nil, "")
	if now["slug"] != "rw-source" || now["title"] != draft["title"] || now["origin"] != "ai" || now["validated"] == true ||
		!strings.Contains(fmt.Sprint(now["tags"]), "ai draft") {
		t.Errorf("replaced problem: slug %v title %v origin %v validated %v tags %v", now["slug"], now["title"], now["origin"], now["validated"], now["tags"])
	}
	if code, v := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d/validation", oldID), nil, ""); code != 200 || v["state"] != "queued" {
		t.Errorf("validation after replace: %d %v", code, v)
	}
	// The copy is the old problem, archived, and the rewrite is archived too.
	copyID := int64(out["copy_id"].(float64))
	_, kept := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", copyID), nil, "")
	if kept["archived"] != true || kept["statement_md"] == now["statement_md"] {
		t.Errorf("copy: archived %v", kept["archived"])
	}
	_, gone := r.org.do("GET", fmt.Sprintf("/api/admin/problems/%d", draftID), nil, "")
	if gone["archived"] != true {
		t.Error("the rewrite is not archived")
	}

	// Pressing Replace again finds the rewrite used up: no second revision, no second copy.
	code, out = r.replace(t, oldID, draftID, base)
	errIs(t, "second replace", code, out, 409, "rewrite_gone", "This rewrite is archived or deleted, so it cannot replace A plus B (rw-full). Rewrite it again.")
	if revisions(t, r.s, oldID) != 2 {
		t.Error("a second replace made a revision")
	}
	var problems int
	r.s.db.QueryRow("SELECT count(*) FROM problems").Scan(&problems)
	if problems != 4 {
		t.Errorf("%d problems, want the old problem, two rewrites and one copy", problems)
	}
}

func TestReplaceOverHTTPRefusals(t *testing.T) {
	const title = "A plus B (rw-source)"
	cases := []struct {
		name   string
		setup  func(t *testing.T, r *rewriteRig, w map[string]any)
		status int
		code   string
		msg    string
	}{
		{"changed", func(t *testing.T, r *rewriteRig, _ map[string]any) {
			r.s.db.Exec("UPDATE problems SET statement_md = statement_md || 'x' WHERE id = ?", r.old)
		}, 409, "changed", title + " changed after this rewrite began. Rewrite it again, or keep this rewrite as its own problem."},
		{"students used it", func(t *testing.T, r *rewriteRig, _ map[string]any) {
			r.s.db.Exec(`INSERT INTO drafts (problem_id, user_id, language, source, updated_at) VALUES (?, ?, 'cpp17', 'x', 0)`, r.old, r.org.userID())
		}, 409, "in_use", "Students have submitted or saved code for " + title + ", so it cannot be replaced. Keep this rewrite as its own problem and use it in new contests."},
		{"archived", func(t *testing.T, r *rewriteRig, _ map[string]any) {
			if code, _ := r.org.do("DELETE", fmt.Sprintf("/api/admin/problems/%d", r.old), nil, r.s.srv.URL); code != 204 {
				t.Fatalf("archive: %d", code)
			}
		}, 409, "archived", title + " is archived, so it cannot be replaced. Restore it first, or keep this rewrite as its own problem."},
		{"deleted", func(t *testing.T, r *rewriteRig, _ map[string]any) {
			r.s.db.Exec("UPDATE problems SET deleted = 1 WHERE id = ?", r.old)
		}, 409, "deleted", title + " was deleted, so it cannot be replaced. Keep this rewrite as its own problem."},
		{"running contest", func(t *testing.T, r *rewriteRig, _ map[string]any) {
			start := time.Now().Add(time.Hour).Truncate(time.Minute)
			r.s.createContest(r.org, "wk", start, map[string]int64{"A": r.old}, nil)
			r.s.at(start, 10)
		}, 409, "contest_running", "A contest that is running uses " + title + ", so it cannot be replaced now. Try again after the contest ends, or keep this rewrite as its own problem."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRewriteRig(t)
			w := r.rewrite(t, r.old, "Fix it.")
			oldID, base, draftID := rewriteOf(t, w)
			r.s.validate(draftID)
			c.setup(t, r, w)
			revs := revisions(t, r.s, oldID)
			code, out := r.replace(t, oldID, draftID, base)
			errIs(t, c.name, code, out, c.status, c.code, c.msg)
			var problems int
			r.s.db.QueryRow("SELECT count(*) FROM problems").Scan(&problems)
			if revisions(t, r.s, oldID) != revs || problems != 2 {
				t.Errorf("a refused replace changed the bank: %d revisions, %d problems", revisions(t, r.s, oldID), problems)
			}
			if strings.Contains(fmt.Sprint(out), r.key) {
				t.Error("the key is in the response")
			}
		})
	}
}

func TestReplaceRoutesRolesAndBadInput(t *testing.T) {
	r := newRewriteRig(t)
	w := r.rewrite(t, r.old, "Fix it.")
	oldID, base, draftID := rewriteOf(t, w)
	r.s.validate(draftID)
	path := fmt.Sprintf("/api/admin/problems/%d/replace-with-rewrite", oldID)
	good := map[string]any{"draft_id": draftID, "base": base}

	if code, _ := r.s.signedIn("asha@college.edu", "Asha").do("POST", path, good, r.s.srv.URL); code != 403 {
		t.Errorf("student: %d", code)
	}
	if code, _ := r.s.browser().do("POST", path, good, r.s.srv.URL); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
	for _, c := range []struct {
		name string
		path string
		body map[string]any
		code int
		want string
	}{
		{"no body fields", path, map[string]any{}, 400, "bad_request"},
		{"no base", path, map[string]any{"draft_id": draftID}, 400, "bad_request"},
		{"no draft", path, map[string]any{"base": base}, 400, "bad_request"},
		{"problem as its own rewrite", path, map[string]any{"draft_id": oldID, "base": base}, 400, "bad_request"},
		{"unknown draft", path, map[string]any{"draft_id": 99999, "base": base}, 404, "not_found"},
		{"unknown problem", "/api/admin/problems/99999/replace-with-rewrite", good, 404, "not_found"},
		{"bad id", "/api/admin/problems/x/replace-with-rewrite", good, 404, "not_found"},
	} {
		code, out := r.org.do("POST", c.path, c.body, r.s.srv.URL)
		errIs(t, c.name, code, out, c.code, c.want, "")
	}
	if revisions(t, r.s, oldID) != 1 {
		t.Error("bad input made a revision")
	}
	// A rewrite that is not an AI draft cannot replace anything.
	r.s.db.Exec("UPDATE problems SET origin = 'manual' WHERE id = ?", draftID)
	code, out := r.replace(t, oldID, draftID, base)
	errIs(t, "not an AI draft", code, out, 400, "bad_request", "")
}
