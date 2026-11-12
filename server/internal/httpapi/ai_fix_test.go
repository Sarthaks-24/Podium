package httpapi

// The three language routes (stage F, F6): GET .../proof, POST .../check and
// POST .../ai-fix. Roles, every refusal with its copy, the provider key and
// slot, and the promise that no hidden test byte reaches a model or a response.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/ai/fakeai"
)

const (
	fixHidden = "HIDDEN-TEST-MARKER-6b1d"
	fixSample = "SAMPLE-MARKER-0c9a"
)

// arraySumZip is the array-sum example with a marker written into every test:
// the samples (tests 1 and 2) get fixSample, the hidden tests fixHidden.
func arraySumZip(t *testing.T) []byte {
	t.Helper()
	dir := "../../../problems/array-sum"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		b, _ := os.ReadFile(p)
		if strings.HasPrefix(rel, "tests/") {
			marker := fixHidden
			if strings.HasPrefix(rel, "tests/01.") || strings.HasPrefix(rel, "tests/02.") {
				marker = fixSample
			}
			b = []byte(marker + " 1 2 3\n")
		}
		w, _ := zw.Create(rel)
		w.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}

// importFunction imports the marked array-sum pack and, when withoutJava,
// saves it again with Java 21 taken off.
func importFunction(t *testing.T, s *site, org *browser, withoutJava bool) int64 {
	t.Helper()
	code, out := org.raw("POST", "/api/admin/problems/import", "application/zip", arraySumZip(t), s.srv.URL)
	if code != 201 {
		t.Fatalf("import array-sum: %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	if withoutJava {
		path := "/api/admin/problems/" + strconv.FormatInt(id, 10)
		_, p := org.do("GET", path, nil, "")
		extra := map[string]any{}
		for _, k := range []string{"locked_top", "locked_bottom", "starters"} {
			m := map[string]any{}
			for lang, v := range p[k].(map[string]any) {
				if lang != "java21" {
					m[lang] = v
				}
			}
			extra[k] = m
		}
		if code, out := org.do("PUT", path, editBody(p, extra), s.srv.URL); code != 200 {
			t.Fatalf("remove java: %d %v", code, out)
		}
	}
	return id
}

func langPath(id int64, lang, route string) string {
	return fmt.Sprintf("/api/admin/problems/%d/languages/%s/%s", id, lang, route)
}

func proofPath(id int64) string { return fmt.Sprintf("/api/admin/problems/%d/proof", id) }

const copyJavaFixed = "The AI's Java 21 code could not be used, so nothing was changed. Press Fix again, or write the code yourself."

func errIs2(t *testing.T, name string, code int, body map[string]any, status int, ecode, msg string) {
	t.Helper()
	e, _ := body["error"].(map[string]any)
	if code != status || e["code"] != ecode || (msg != "" && e["message"] != msg) {
		t.Errorf("%s: %d %v, want %d %s %q", name, code, body, status, ecode, msg)
	}
}

func openRounds(t *testing.T, s *site, id int64) int {
	t.Helper()
	var n int
	s.db.QueryRow(`SELECT count(*) FROM jobs WHERE priority = 88 AND json_extract(payload, '$.problem_id') = ?`, id).Scan(&n)
	return n
}

// closeRound ends the open proof round without a judge, so a test can queue
// the next one.
func closeRound(t *testing.T, s *site) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE jobs SET state = 'failed' WHERE priority = 88 AND state IN ('queued', 'leased')`); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageRoutesRoles(t *testing.T) {
	s := newSiteMode(t, "testing")
	owner := s.testSignIn("owner2@college.edu", "owner")
	admin := s.testSignIn("admin@college.edu", "admin")
	org := s.testSignIn("ta@college.edu", "organiser")
	student := s.testSignIn("asha@college.edu", "student")
	out := s.browser()
	id := importFunction(t, s, org, true)
	routes := []struct {
		method, path string
		body         any
	}{
		{"GET", proofPath(id), nil},
		{"POST", langPath(id, "cpp17", "check"), map[string]string{"solution": "x"}},
		{"POST", langPath(id, "java21", "ai-fix"), map[string]string{}},
	}
	for _, r := range routes {
		for name, b := range map[string]*browser{"owner": owner, "admin": admin, "organiser": org} {
			if code, body := b.do(r.method, r.path, r.body, s.srv.URL); code == 401 || code == 403 {
				t.Errorf("%s %s as %s: %d %v", r.method, r.path, name, code, body)
			}
			closeRound(t, s)
		}
		if code, _ := student.do(r.method, r.path, r.body, s.srv.URL); code != 403 {
			t.Errorf("%s %s as a student: %d", r.method, r.path, code)
		}
		if code, _ := out.do(r.method, r.path, r.body, s.srv.URL); code != 401 {
			t.Errorf("%s %s signed out: %d", r.method, r.path, code)
		}
	}
}

func TestProofAndCheckRoutes(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	id := importFunction(t, s, org, true)
	whole := s.importProblem(org, "plain")

	code, st := org.do("GET", proofPath(id), nil, "")
	langs, _ := st["languages"].([]any)
	if code != 200 || st["checking"] != false || len(langs) != 4 || st["server_now"] == nil {
		t.Fatalf("proof: %d %v", code, st)
	}
	if java := langs[1].(map[string]any); java["language"] != "java21" || java["offered"] != false || java["result"] != "none" {
		t.Errorf("java21 %v", java)
	}
	if code, body := org.do("GET", proofPath(99999), nil, ""); code != 404 {
		t.Errorf("missing proof: %d %v", code, body)
	}

	chk := func(pid int64, lang string, body any) (int, map[string]any) {
		return org.do("POST", langPath(pid, lang, "check"), body, s.srv.URL)
	}
	code, body := chk(id, "cobol", map[string]string{"solution": "x"})
	errIs2(t, "language", code, body, 400, "bad_request", "Choose C++17, Java 21, Python 3 or JavaScript.")
	for name, b := range map[string]any{"empty": map[string]string{"solution": ""}, "none": map[string]string{},
		"long": map[string]string{"solution": strings.Repeat("a", 64<<10+1)}} {
		code, body = chk(id, "cpp17", b)
		errIs2(t, name, code, body, 400, "bad_request", "Paste a solution of at most 64 KB.")
	}
	if code, _ = chk(id, "cpp17", map[string]any{"solution": "x", "extra": 1}); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
	code, body = chk(99999, "cpp17", map[string]string{"solution": "x"})
	errIs2(t, "missing", code, body, 404, "not_found", "There is no such problem.")
	code, body = chk(whole, "cpp17", map[string]string{"solution": "x"})
	errIs2(t, "whole", code, body, 409, "not_function", "Only a function problem has languages to check.")
	code, body = chk(id, "java21", map[string]string{"solution": "x"})
	errIs2(t, "not offered", code, body, 409, "not_offered",
		"Java 21 is not offered. Write its locked code and starting code and save, or use Fix Java 21 with AI.")

	code, body = chk(id, "cpp17", map[string]string{"solution": "// " + fixSample + "\nlong long solve() {}"})
	if code != 202 || body["state"] != "checking" || body["language"] != "cpp17" || body["server_now"] == nil {
		t.Fatalf("check: %d %v", code, body)
	}
	code, body = chk(id, "cpp17", map[string]string{"solution": "x"})
	errIs2(t, "checking", code, body, 409, "checking", "Podium is still checking the languages of this problem.")
	if _, st = org.do("GET", proofPath(id), nil, ""); st["checking"] != true || st["progress"] == nil {
		t.Errorf("state while checking: %v", st)
	}
	if code, _ := org.do("POST", "/api/admin/problems/"+strconv.FormatInt(id, 10)+"/validate", nil, s.srv.URL); code != 409 {
		t.Errorf("validate during a check: %d", code)
	}
}

// The fix route needs a key and a slot, refuses what a fix cannot do, queues
// one round from the AI's code and answers with its provider.
func TestAIFixRoute(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	id := importFunction(t, s, org, true)
	whole := s.importProblem(org, "plain")
	fix := func(pid int64, lang string, body any) (int, map[string]any) {
		return org.do("POST", langPath(pid, lang, "ai-fix"), body, s.srv.URL)
	}

	code, body := fix(id, "java21", map[string]string{})
	errIs2(t, "no key", code, body, 409, "no_key", "Add a key first.")
	if code, out := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
		t.Fatalf("fake key: %d %v", code, out)
	}
	code, body = fix(id, "cobol", map[string]string{})
	errIs2(t, "language", code, body, 400, "bad_request", "Choose C++17, Java 21, Python 3 or JavaScript.")
	if code, _ = fix(id, "java21", map[string]any{"extra": 1}); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
	code, body = fix(99999, "java21", map[string]string{})
	errIs2(t, "missing", code, body, 404, "not_found", "There is no such problem.")
	code, body = fix(whole, "java21", map[string]string{})
	errIs2(t, "whole", code, body, 409, "not_function", "Only a function problem has languages to check.")
	code, body = fix(id, "cpp17", map[string]string{})
	errIs2(t, "offered", code, body, 409, "already_offered", "C++17 is already offered. Use Check C++17 to test it.")

	// The slot: one AI request at a time.
	var uid int64
	s.db.QueryRow("SELECT id FROM users WHERE email = 'ta@college.edu'").Scan(&uid)
	release, ok := s.ai.Slots.Take(uid)
	if !ok {
		t.Fatal("slot taken")
	}
	code, body = fix(id, "java21", map[string]string{})
	errIs2(t, "busy", code, body, 429, "busy", "")
	release()

	// Old client: an empty object, the active provider.
	code, body = fix(id, "java21", map[string]string{})
	if code != 202 || body["state"] != "checking" || body["language"] != "java21" || body["provider"] != "fake" || body["model"] != "canned" || body["server_now"] == nil {
		t.Fatalf("fix: %d %v", code, body)
	}
	var payload string
	rows, _ := s.db.Query(`SELECT payload FROM jobs WHERE priority = 88`)
	n := 0
	for rows.Next() {
		rows.Scan(&payload)
		n++
		var pl struct {
			Proof struct{ Source, Language string }
		}
		json.Unmarshal([]byte(payload), &pl)
		if pl.Proof.Source != "fix" || pl.Proof.Language != "java21" {
			t.Errorf("proof payload %s", payload)
		}
	}
	rows.Close()
	if n != 2 {
		t.Errorf("%d proof jobs, want a solution and a starter job", n)
	}
	// Nothing is saved until the round passes.
	if _, p := org.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, ""); len(p["locked_top"].(map[string]any)) != 3 {
		t.Errorf("the route changed the problem: %v", p["locked_top"])
	}
	code, body = fix(id, "java21", map[string]string{})
	errIs2(t, "checking", code, body, 409, "checking", "Podium is still checking the languages of this problem.")
}

func TestAIFixBadAnswersAndChangedProblem(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	id := importFunction(t, s, org, true)
	if code, out := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
		t.Fatalf("fake key: %d %v", code, out)
	}
	t.Cleanup(func() { fakeai.FixHook = nil })
	fix := func() (int, map[string]any) {
		return org.do("POST", langPath(id, "java21", "ai-fix"), map[string]string{}, s.srv.URL)
	}

	calls := 0
	var langs []string
	fakeai.FixHook = func(req ai.FixRequest) (*ai.FixAnswer, error) {
		calls++
		langs = append(langs, req.Language)
		return &ai.FixAnswer{LockedTop: "x", Starter: "", LockedBottom: "y", Solution: "z"}, nil
	}
	code, body := fix()
	errIs2(t, "bad answer", code, body, 422, "invalid_answer", copyJavaFixed)
	if calls != 2 || langs[0] != "java21" || openRounds(t, s, id) != 0 {
		t.Errorf("%d calls %v, %d jobs", calls, langs, openRounds(t, s, id))
	}
	if !strings.Contains(fmt.Sprint(body), "starter is empty") {
		t.Errorf("reasons missing: %v", body)
	}

	fakeai.FixHook = func(req ai.FixRequest) (*ai.FixAnswer, error) {
		return nil, &ai.Error{Code: ai.CodeRefused, Message: "The model declined this request. Rephrase it and try again."}
	}
	if code, body = fix(); code != 502 || openRounds(t, s, id) != 0 {
		t.Errorf("provider refusal: %d %v", code, body)
	}

	// The problem changes while the AI works: refused, nothing queued.
	fakeai.FixHook = func(req ai.FixRequest) (*ai.FixAnswer, error) {
		path := "/api/admin/problems/" + strconv.FormatInt(id, 10)
		_, p := org.do("GET", path, nil, "")
		p["time_limit_ms"] = p["time_limit_ms"].(float64) + 1
		if code, out := org.do("PUT", path, editBody(p, nil), s.srv.URL); code != 200 {
			t.Errorf("edit during the call: %d %v", code, out)
		}
		return &ai.FixAnswer{LockedTop: "t", Starter: "s", LockedBottom: "b", Solution: "c"}, nil
	}
	code, body = fix()
	errIs2(t, "changed", code, body, 409, "changed", "The problem changed while the AI was working. Press Fix again.")
	if openRounds(t, s, id) != 0 {
		t.Error("a round was queued for a stale fix")
	}
}

// standInFix is a stand-in per provider that answers a fix and records every
// request body.
func fixStandIns(t *testing.T, s *site) map[string]*standIn {
	t.Helper()
	answer, _ := json.Marshal(&ai.FixAnswer{LockedTop: "import java.util.*;", Starter: "static long solve(int[] a) { return 0; }",
		LockedBottom: "class Main {}", Solution: "static long solve(int[] a) { return 1; }"})
	out := map[string]*standIn{}
	s.ai.BaseURLs = map[string]string{}
	for provider, base := range map[string]string{ai.ProviderAnthropic: "", ai.ProviderOpenAI: "/v1", ai.ProviderGemini: "/v1beta"} {
		stand := &standIn{provider: provider, key: "PODIUMTESTKEY-" + provider, answer: string(answer)}
		srv := httptest.NewServer(stand)
		t.Cleanup(srv.Close)
		s.ai.BaseURLs[provider] = srv.URL + base
		out[provider] = stand
	}
	return out
}

// No hidden test byte reaches a model request or a response of the three
// routes; the samples do reach the model; the key reaches nothing.
func TestAIFixSendsOnlySamples(t *testing.T) {
	for _, provider := range []string{ai.ProviderAnthropic, ai.ProviderOpenAI, ai.ProviderGemini} {
		t.Run(provider, func(t *testing.T) {
			s := newSiteMode(t, "testing")
			org := s.testSignIn("ta@college.edu", "organiser")
			stands := fixStandIns(t, s)
			stand := stands[provider]
			id := importFunction(t, s, org, true)
			if code, out := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": provider, "model": "model-x", "key": stand.key}, s.srv.URL); code != 200 {
				t.Fatalf("key: %d %v", code, out)
			}
			var bodies []string
			code, fixed := org.do("POST", langPath(id, "java21", "ai-fix"), map[string]string{"provider": provider}, s.srv.URL)
			if code != 202 {
				t.Fatalf("fix: %d %v", code, fixed)
			}
			b, _ := json.Marshal(fixed)
			bodies = append(bodies, string(b))
			closeRound(t, s)
			_, st := org.do("GET", proofPath(id), nil, "")
			b, _ = json.Marshal(st)
			bodies = append(bodies, string(b))
			code, chk := org.do("POST", langPath(id, "cpp17", "check"), map[string]string{"solution": "x"}, s.srv.URL)
			if code != 202 {
				t.Fatalf("check: %d %v", code, chk)
			}
			b, _ = json.Marshal(chk)
			bodies = append(bodies, string(b))
			for _, b := range bodies {
				if strings.Contains(b, fixHidden) || strings.Contains(b, stand.key) {
					t.Errorf("a response holds hidden test data or the key: %s", b)
				}
			}
			stand.mu.Lock()
			posts := strings.Join(stand.posts, "\n")
			stand.mu.Unlock()
			if posts == "" || !strings.Contains(posts, fixSample) {
				t.Errorf("the samples did not reach the model: %.200s", posts)
			}
			if strings.Contains(posts, fixHidden) || strings.Contains(posts, stand.key) {
				t.Error("hidden test data or the key reached a model request")
			}
			if strings.Contains(s.logs.String(), fixHidden) || strings.Contains(s.logs.String(), stand.key) {
				t.Error("hidden test data or the key reached the log")
			}
		})
	}
}

// Accepting an AI edit while a proof is open is refused, so the proof does not
// find a changed problem and discard its result with no validation queued. The
// draft stays, and accepting works once the round has closed.
func TestAIEditAcceptIsRefusedWhileAProofIsOpen(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	if code, out := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
		t.Fatalf("fake key: %d %v", code, out)
	}
	id := importFunction(t, s, org, false)
	_, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "Add a note."}, s.srv.URL)
	draft, _ := out["draft_id"].(string)
	if draft == "" {
		t.Fatalf("no draft: %v", out)
	}
	if code, body := org.do("POST", langPath(id, "cpp17", "check"), map[string]string{"solution": "x"}, s.srv.URL); code != 202 {
		t.Fatalf("check: %d %v", code, body)
	}
	revs := revisions(t, s, id)
	_, before := org.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, "")

	code, body := org.do("POST", editPath(id, "/"+draft+"/accept"), nil, s.srv.URL)
	errIs2(t, "accept during a proof", code, body, 409, "checking", "Podium is still checking the languages of this problem.")
	_, after := org.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, "")
	if revisions(t, s, id) != revs || fmt.Sprint(before) != fmt.Sprint(after) {
		t.Error("the refused accept changed the problem")
	}

	closeRound(t, s)
	code, acc := org.do("POST", editPath(id, "/"+draft+"/accept"), nil, s.srv.URL)
	if code != 200 || acc["validation_queued"] != true {
		t.Fatalf("accept after the round: %d %v", code, acc)
	}
	if revisions(t, s, id) != revs+1 {
		t.Errorf("%d revisions, want %d", revisions(t, s, id), revs+1)
	}
}
