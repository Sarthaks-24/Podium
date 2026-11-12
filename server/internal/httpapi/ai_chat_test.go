package httpapi

// The Chat Completions hosts over HTTP (v2.1.0 stage K): a stand-in for a
// host answers the key check, a model list and completions. The tests add a
// key for OpenRouter and Groq, run a draft, a function draft, a fix and an
// edit (each with a repair retry where it can), and look for the keys in every
// log line, the SQLite files and the blob store. A provider that refuses a key
// echoes it in its body; the routes must answer without it. No request field
// can name a host, and a redirect is never followed.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
)

// chatStand is a stand-in for a Chat Completions host. A wrong key gets a 401
// whose body echoes the key.
type chatStand struct {
	key    string
	answer string
	// first, when set, answers the first completion instead of answer.
	first string
	// status, when set, is the status of every completion.
	status int

	mu    sync.Mutex
	seen  []string
	posts []string
}

func (s *chatStand) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	s.seen = append(s.seen, got)
	if r.Method == "POST" {
		s.posts = append(s.posts, string(body))
	}
	n := len(s.posts)
	s.mu.Unlock()
	if got != s.key {
		writeJSONBody(w, 401, fmt.Sprintf(`{"error":{"message":"No auth credentials found for %s","code":401}}`, got))
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/key":
		writeJSONBody(w, 200, `{"data":{"label":"test"}}`)
	case r.Method == "GET" && r.URL.Path == "/models":
		writeJSONBody(w, 200, `{"data":[
{"id":"vendor/chat-new:free","name":"Chat New","created":9,"supported_parameters":["response_format"]},
{"id":"vendor/chat-old","created":8,"supported_parameters":["response_format"]},
{"id":"vendor/no-format","created":7,"supported_parameters":["temperature"]},
{"id":"whisper-large","created":6}]}`)
	case r.Method == "POST" && r.URL.Path == "/chat/completions":
		if s.status != 0 {
			writeJSONBody(w, s.status, fmt.Sprintf(`{"error":{"message":"limit for key %s"}}`, got))
			return
		}
		text := s.answer
		if s.first != "" && n == 1 {
			text = s.first
		}
		content, _ := json.Marshal(text)
		writeJSONBody(w, 200, `{"id":"c","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":`+string(content)+`}}]}`)
	default:
		w.WriteHeader(404)
	}
}

func (s *chatStand) postCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.posts)
}

func chatKey(prefix string) string {
	var rb [12]byte
	rand.Read(rb[:])
	return prefix + "PODIUMTEST" + hex.EncodeToString(rb[:])
}

func providerEntry(st map[string]any, id string) map[string]any {
	for _, p := range st["providers"].([]any) {
		if p.(map[string]any)["id"] == id {
			return p.(map[string]any)
		}
	}
	return nil
}

func TestAIKeyStatusListsTheOtherProviders(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	code, st := owner.do("GET", "/api/admin/ai/key", nil, "")
	var ids []string
	for _, p := range st["providers"].([]any) {
		ids = append(ids, p.(map[string]any)["id"].(string))
	}
	if code != 200 || strings.Join(ids, ",") != "anthropic,openai,gemini,openrouter,groq" {
		t.Fatalf("providers: %d %v", code, ids)
	}
	for _, id := range ids {
		p := providerEntry(st, id)
		wantOther := id == "openrouter" || id == "groq"
		if p["other"] != wantOther || p["has_list"] != true || p["name"] == "" {
			t.Errorf("%s: %v", id, p)
		}
	}
	if providerEntry(st, "openrouter")["name"] != "OpenRouter" || providerEntry(st, "groq")["name"] != "Groq" {
		t.Errorf("names: %v", st["providers"])
	}
	_, pv := owner.do("GET", "/api/admin/ai/providers", nil, "")
	if len(pv["providers"].([]any)) != 5 {
		t.Errorf("providers route: %v", pv)
	}
}

func TestAIChatHostsKeyNeverStored(t *testing.T) {
	s := newSite(t)
	s.logLevel.Set(slog.LevelDebug)
	s.ai.BaseURLs = map[string]string{}
	owner := s.signedIn("owner@college.edu", "Owner")
	fixID := importFunction(t, s, owner, true)
	editID := s.importProblem(owner, "leak-chat")

	var keys []string
	for _, c := range []struct{ provider, prefix, refused, typed string }{
		{ai.ProviderOpenRouter, "sk-or-v1-", "OpenRouter refused the key", "vendor/typed-model:free"},
		{ai.ProviderGroq, "gsk_", "Groq refused the key", "typed-model-2"},
	} {
		key := chatKey(c.prefix)
		keys = append(keys, key)
		pack, _ := json.Marshal(ai.FakeMaxSubarray())
		stand := &chatStand{key: key, answer: string(pack), first: "Sure, here is the problem: " + string(pack)}
		api := httptest.NewServer(stand)
		defer api.Close()
		s.ai.BaseURLs[c.provider] = api.URL

		// A refused key: the refusal in words, never the key its body echoes.
		code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": c.provider, "key": key + "x"}, s.srv.URL)
		if code != 400 || errCode(body) != "bad_key" || !strings.Contains(fmt.Sprint(body), c.refused) || strings.Contains(fmt.Sprint(body), key) {
			t.Fatalf("%s wrong key: %d %v", c.provider, code, body)
		}
		// Right key, no model: the picker gets the filtered list.
		code, st := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": c.provider, "key": key}, s.srv.URL)
		if code != 200 || st["provider"] != c.provider || strings.Contains(fmt.Sprint(st), key) {
			t.Fatalf("%s add key: %d %v", c.provider, code, st)
		}
		var ids []string
		for _, m := range providerEntry(st, c.provider)["models"].([]any) {
			ids = append(ids, m.(map[string]any)["id"].(string))
		}
		if strings.Join(ids, ",") != "vendor/chat-new:free,vendor/chat-old" {
			t.Errorf("%s picker: %v", c.provider, ids)
		}
		// A typed id, saved without the key; a slash is allowed on these hosts only.
		code, st = owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": c.provider, "model": c.typed}, s.srv.URL)
		if code != 200 || st["model"] != c.typed {
			t.Fatalf("%s typed model: %d %v", c.provider, code, st)
		}
		if code, _ := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": c.provider, "model": "a/../b"}, s.srv.URL); code != 400 {
			t.Errorf("%s model with ..: %d", c.provider, code)
		}

		// A draft whose first answer is loose text: the retry runs, and neither
		// request holds the key.
		code, started := owner.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": "Find the best stretch of days."}, s.srv.URL)
		if code != 202 {
			t.Fatalf("%s wizard: %d %v", c.provider, code, started)
		}
		w := pollWizard(t, owner, started["wizard_id"].(string))
		if w["state"] != "draft_created" || w["provider"] != c.provider || w["model"] != c.typed || strings.Contains(fmt.Sprint(w), key) {
			t.Fatalf("%s wizard: %v", c.provider, w)
		}
		stand.mu.Lock()
		posts := slices.Clone(stand.posts)
		used := strings.Join(stand.seen, ",")
		stand.mu.Unlock()
		if len(posts) != 2 || !strings.Contains(posts[0], `\"slug\"`) || !strings.Contains(posts[1], "Sure, here is the problem") || strings.Contains(posts[0]+posts[1], key) {
			t.Fatalf("%s: the draft requests are wrong or hold the key (%d posts)", c.provider, len(posts))
		}
		if !strings.Contains(used, key) {
			t.Fatalf("%s: the stand-in never saw the key: the test proves nothing", c.provider)
		}

		// A function draft.
		fn, _ := json.Marshal(ai.FakeFunctionArraySum())
		stand.mu.Lock()
		stand.answer, stand.first = string(fn), ""
		before := len(stand.posts)
		stand.mu.Unlock()
		code, started = startWizardWhenFree(t, owner, s.srv.URL, map[string]any{"mode": "paste", "text": "Sum an array.", "students_write": "function", "java_lists": "arraylist"})
		if code != 202 {
			t.Fatalf("%s function wizard: %d %v", c.provider, code, started)
		}
		w = pollWizard(t, owner, started["wizard_id"].(string))
		stand.mu.Lock()
		fnPosts := stand.posts[before:]
		stand.mu.Unlock()
		if w["state"] != "draft_created" || len(fnPosts) != 1 || !strings.Contains(fnPosts[0], "brute_cpp") || strings.Contains(fnPosts[0], key) {
			t.Fatalf("%s function wizard: %v (%d posts)", c.provider, w, len(fnPosts))
		}

		// An AI fix, then the same call with a refused key.
		fixed, _ := json.Marshal(&ai.FixAnswer{LockedTop: "import java.util.*;", Starter: "static long solve(int[] a) { return 0; }",
			LockedBottom: "class Main {}", Solution: "static long solve(int[] a) { return 1; }"})
		stand.mu.Lock()
		stand.answer = string(fixed)
		before = len(stand.posts)
		stand.mu.Unlock()
		fix := func() (int, map[string]any) {
			for i := 0; i < 100; i++ {
				code, out := owner.do("POST", langPath(fixID, "java21", "ai-fix"), map[string]string{"provider": c.provider}, s.srv.URL)
				if code != 429 {
					return code, out
				}
				time.Sleep(20 * time.Millisecond)
			}
			t.Fatal("the AI slot was never released")
			return 0, nil
		}
		code, fixedOut := fix()
		stand.mu.Lock()
		fixPosts := stand.posts[before:]
		stand.mu.Unlock()
		if code != 202 || strings.Contains(fmt.Sprint(fixedOut), key) || len(fixPosts) != 1 || !strings.Contains(fixPosts[0], "locked_bottom") || strings.Contains(fixPosts[0], key) {
			t.Fatalf("%s ai-fix: %d %v", c.provider, code, fixedOut)
		}
		closeRound(t, s)
		stand.key = key + "x"
		code, refusedFix := fix()
		stand.key = key
		if code != 502 || strings.Contains(fmt.Sprint(refusedFix), key) {
			t.Errorf("%s ai-fix with a refused key: %d %v", c.provider, code, refusedFix)
		}

		// An edit whose first answer breaks problem.yaml: the retry runs.
		edit, _ := json.Marshal(ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md", Content: "Edited by the " + c.provider + " stand-in.\n"}}, Summary: "Changed it."})
		broken, _ := json.Marshal(ai.EditAnswer{Files: []ai.EditFile{{Path: "problem.yaml", Content: "title: [\n"}}, Summary: "Broken."})
		stand.mu.Lock()
		stand.answer, stand.first = string(edit), string(broken)
		stand.posts = nil
		stand.mu.Unlock()
		code, out := owner.do("POST", editPath(editID, ""), map[string]any{"instruction": "Change it.", "provider": c.provider}, s.srv.URL)
		if code != 200 || strings.Contains(fmt.Sprint(out), key) || out["summary"] != "Changed it." {
			t.Fatalf("%s edit: %d %v", c.provider, code, out)
		}
		stand.mu.Lock()
		posts = slices.Clone(stand.posts)
		stand.mu.Unlock()
		if len(posts) != 2 || !strings.Contains(posts[1], "title: [") || strings.Contains(posts[0]+posts[1], key) {
			t.Fatalf("%s: the edit retry is wrong or carries the key (%d posts)", c.provider, len(posts))
		}
		if code, acc := owner.do("POST", editPath(editID, "/"+out["draft_id"].(string)+"/accept"), nil, s.srv.URL); code != 200 || strings.Contains(fmt.Sprint(acc), key) {
			t.Fatalf("%s accept: %d %v", c.provider, code, acc)
		}
		// The same edit with a refused key: the stand-in echoes the key in its 401
		// body, and the route must answer without it (guard test, no red run).
		stand.key = key + "x"
		code, refusedEdit := owner.do("POST", editPath(editID, ""), map[string]any{"instruction": "Change it again.", "provider": c.provider}, s.srv.URL)
		stand.key = key
		if code != 502 || strings.Contains(fmt.Sprint(refusedEdit), key) {
			t.Errorf("%s edit with a refused key: %d %v", c.provider, code, refusedEdit)
		}
		closeRound(t, s)
	}

	// An error body that echoes the key during a draft: the run fails with a
	// sentence, and the run, the log and the database stay clean.
	stand := &chatStand{key: keys[0], status: 413}
	api := httptest.NewServer(stand)
	defer api.Close()
	s.ai.BaseURLs[ai.ProviderOpenRouter] = api.URL
	if code, _ := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "openrouter", "model": "vendor/chat-old", "key": keys[0]}, s.srv.URL); code != 200 {
		t.Fatal("key for the 413 run")
	}
	code, started := startWizardWhenFree(t, owner, s.srv.URL, map[string]any{"mode": "paste", "text": "Find the best stretch of days.", "provider": "openrouter"})
	if code != 202 {
		t.Fatalf("413 wizard: %d %v", code, started)
	}
	w := pollWizard(t, owner, started["wizard_id"].(string))
	if w["state"] != "error" || strings.Contains(fmt.Sprint(w), keys[0]) || !strings.Contains(fmt.Sprint(w), "OpenRouter says the text is too large for this model or plan.") {
		t.Errorf("413 wizard: %v", w)
	}

	if code, _ := owner.do("POST", "/api/auth/logout", nil, s.srv.URL); code != 204 {
		t.Errorf("logout: %d", code)
	}
	logs := s.logs.String()
	if !strings.Contains(logs, `"msg":"ai draft created"`) || !strings.Contains(logs, `"msg":"ai edit accepted"`) {
		t.Fatalf("debug logging was not on; the check below would prove nothing:\n%s", logs)
	}
	for _, key := range keys {
		if strings.Contains(logs, key) {
			t.Errorf("a key reached the log")
		}
	}
	if strings.Contains(logs, "PODIUMTEST") {
		t.Errorf("a key reached the log:\n%s", logs)
	}
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	files := []string{s.dbPath, s.dbPath + "-wal", s.dbPath + "-shm"}
	filepath.WalkDir(s.bank.Blobs.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	read := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		read++
		if strings.Contains(string(b), "PODIUMTEST") {
			t.Errorf("a key is in %s", f)
		}
	}
	if read < 3 {
		t.Errorf("read only %d files", read)
	}
}

// Nothing in a request names or changes the host: the form's fields are
// fixed, unknown fields are refused, and the stand-in behind BaseURLs (the
// test-only map) is the only host that is ever called.
func TestAIChatNoRequestFieldSetsAHost(t *testing.T) {
	s := newSite(t)
	s.ai.BaseURLs = map[string]string{}
	owner := s.signedIn("owner@college.edu", "Owner")
	var evilHits int
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { evilHits++ }))
	defer evil.Close()
	key := chatKey("sk-or-v1-")
	good := httptest.NewServer(&chatStand{key: key})
	defer good.Close()
	s.ai.BaseURLs[ai.ProviderOpenRouter] = good.URL

	for _, extra := range []string{"base_url", "url", "host", "endpoint", "api_base"} {
		code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "openrouter", "key": key, extra: evil.URL}, s.srv.URL)
		if code != 400 {
			t.Errorf("field %s: %d %v", extra, code, body)
		}
	}
	// A provider id that is an address is not a provider.
	for _, p := range []string{evil.URL, "https://openrouter.ai/api/v1", "openrouter@evil.example", "OpenRouter", "openrouter/", "fake"} {
		code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": p, "key": key}, s.srv.URL)
		if code != 400 || errCode(body) != "bad_request" {
			t.Errorf("provider %q on PUT: %d %v", p, code, body)
		}
		if code, body := owner.do("GET", "/api/admin/ai/models?provider="+p, nil, ""); code != 400 {
			t.Errorf("provider %q on models: %d %v", p, code, body)
		}
	}
	code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": evil.URL, "key": key}, s.srv.URL)
	if e, _ := body["error"].(map[string]any); code != 400 || e["message"] != ai.ChooseProvider {
		t.Errorf("copy: %d %v", code, body)
	}
	// The same key on the real provider works, through the stand-in only.
	if code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "openrouter", "key": key}, s.srv.URL); code != 200 {
		t.Fatalf("good key: %d %v", code, body)
	}
	if evilHits != 0 {
		t.Errorf("%d requests reached the other host", evilHits)
	}
}

func TestAIChatRedirectIsNotFollowed(t *testing.T) {
	s := newSite(t)
	s.ai.BaseURLs = map[string]string{}
	owner := s.signedIn("owner@college.edu", "Owner")
	var hits int
	away := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer away.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, away.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	for _, p := range []string{"openrouter", "groq"} {
		s.ai.BaseURLs[p] = redirect.URL
		code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": p, "model": "m", "key": chatKey("k-")}, s.srv.URL)
		if code != 400 || errCode(body) != "bad_key" {
			t.Errorf("%s: %d %v", p, code, body)
		}
	}
	if hits != 0 {
		t.Errorf("%d requests reached the redirect target", hits)
	}
}

func TestAIChatRateLimitSentenceOnARun(t *testing.T) {
	s := newSite(t)
	s.ai.BaseURLs = map[string]string{}
	owner := s.signedIn("owner@college.edu", "Owner")
	key := chatKey("gsk_")
	stand := &chatStand{key: key, status: 429}
	api := httptest.NewServer(stand)
	defer api.Close()
	s.ai.BaseURLs[ai.ProviderGroq] = api.URL
	if code, body := owner.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "groq", "model": "llama-x", "key": key}, s.srv.URL); code != 200 {
		t.Fatalf("key: %d %v", code, body)
	}
	code, started := owner.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": "Find the best stretch of days."}, s.srv.URL)
	if code != 202 {
		t.Fatalf("wizard: %d %v", code, started)
	}
	w := pollWizard(t, owner, started["wizard_id"].(string))
	e, _ := w["error"].(map[string]any)
	if w["state"] != "error" || e["code"] != "rate_limited" ||
		e["message"] != "Groq is busy or your free limit is used up. Wait a minute and ask again, or choose another model." || strings.Contains(fmt.Sprint(w), key) {
		t.Errorf("wizard: %v", w)
	}
	if stand.postCount() != 1 {
		t.Errorf("a rate limit was retried: %d requests", stand.postCount())
	}
}

// Roles and refusals on the routes that take a provider: the new ids change
// nothing about who may call them.
func TestAIChatRoutesRolesAndOldClients(t *testing.T) {
	s := newSiteMode(t, "testing")
	s.ai.BaseURLs = map[string]string{}
	org := s.testSignIn("ta@college.edu", "organiser")
	student := s.testSignIn("asha@college.edu", "student")
	anon := s.browser()
	key := chatKey("sk-or-v1-")
	api := httptest.NewServer(&chatStand{key: key})
	defer api.Close()
	s.ai.BaseURLs[ai.ProviderOpenRouter] = api.URL

	put := map[string]string{"provider": "openrouter", "key": key}
	for name, b := range map[string]*browser{"student": student} {
		if code, _ := b.do("PUT", "/api/admin/ai/key", put, s.srv.URL); code != 403 {
			t.Errorf("%s PUT key: %d", name, code)
		}
		if code, _ := b.do("GET", "/api/admin/ai/models?provider=openrouter", nil, ""); code != 403 {
			t.Errorf("%s models: %d", name, code)
		}
		if code, _ := b.do("DELETE", "/api/admin/ai/key?provider=openrouter", nil, s.srv.URL); code != 403 {
			t.Errorf("%s DELETE key: %d", name, code)
		}
	}
	if code, _ := anon.do("PUT", "/api/admin/ai/key", put, s.srv.URL); code != 401 {
		t.Errorf("anonymous PUT key: %d", code)
	}
	if code, _ := anon.do("GET", "/api/admin/ai/models?provider=openrouter", nil, ""); code != 401 {
		t.Errorf("anonymous models: %d", code)
	}
	if code, body := org.do("GET", "/api/admin/ai/models?provider=openrouter", nil, ""); code != 409 || errCode(body) != "no_key" ||
		!strings.Contains(fmt.Sprint(body), "Add an OpenRouter key first.") {
		t.Errorf("models without a key: %d %v", code, body)
	}
	if code, body := org.do("PUT", "/api/admin/ai/key", put, s.srv.URL); code != 200 {
		t.Fatalf("organiser PUT key: %d %v", code, body)
	}
	if code, body := org.do("GET", "/api/admin/ai/models?provider=openrouter&refresh=1", nil, ""); code != 200 || len(body["models"].([]any)) != 2 {
		t.Errorf("models: %d %v", code, body)
	}
	// A wizard run that names a provider the session holds no key for is refused as before.
	if code, body := org.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": "x", "provider": "groq"}, s.srv.URL); code != 409 || errCode(body) != "no_key" {
		t.Errorf("wizard on a provider without a key: %d %v", code, body)
	}
	// An old client names one of the three old providers, or none, as before.
	if code, body := org.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": "x", "provider": "openai"}, s.srv.URL); code != 409 || errCode(body) != "no_key" {
		t.Errorf("old provider without a key: %d %v", code, body)
	}
	if code, body := org.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": "x"}, s.srv.URL); code != 409 || errCode(body) != "no_model" {
		t.Errorf("no provider named, openrouter active without a model: %d %v", code, body)
	}
	if code, _ := org.do("DELETE", "/api/admin/ai/key?provider=openrouter", nil, s.srv.URL); code != 204 {
		t.Errorf("delete key: %d", code)
	}
}
