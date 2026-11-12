package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Stand-ins for a Chat Completions host. No real host and no real key.

var (
	hostSchemaRow = chatHost{id: "testhost", name: "Testhost", base: "https://example.invalid/v1", jsonSchema: true, hasList: true, checkPath: "/key"}
	hostObjectRow = chatHost{id: "objhost", name: "Objhost", base: "https://example.invalid/v1", hasList: true}
	hostNoList    = chatHost{id: "nolist", name: "Nolist", base: "https://example.invalid/v1"}
)

func chatClient(host chatHost, base, model string) *ChatCompat {
	return &ChatCompat{Host: host, Key: NewSecret(testKey), Model: model, BaseURL: base}
}

// chatReply is a completion with one choice.
func chatReply(content, finish string) string {
	b, _ := json.Marshal(content)
	return `{"id":"c1","choices":[{"index":0,"finish_reason":"` + finish + `","message":{"role":"assistant","content":` + string(b) + `}}]}`
}

func packJSON(t *testing.T) string {
	t.Helper()
	b, err := json.Marshal(FakeMaxSubarray())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// schemaAware answers correctly only when the first message is a system
// message that carries the schema as text (it holds `marker`); otherwise it
// answers with something that is not a pack.
type schemaAware struct {
	marker, good string
	mu           sync.Mutex
	bodies       []map[string]any
	paths        []string
}

func (s *schemaAware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.bodies = append(s.bodies, body)
	s.paths = append(s.paths, r.URL.Path)
	s.mu.Unlock()
	msgs, _ := body["messages"].([]any)
	content := "{}"
	if len(msgs) > 0 {
		first := msgs[0].(map[string]any)
		if first["role"] == "system" && strings.Contains(fmt.Sprint(first["content"]), s.marker) {
			content = s.good
		}
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, chatReply(content, "stop"))
}

func TestChatGenerateAndEditNeedTheSchemaText(t *testing.T) {
	s := &schemaAware{marker: `"locked_bottom"`}
	srv := httptest.NewServer(s)
	defer srv.Close()
	c := chatClient(hostObjectRow, srv.URL, "m")

	// Wizard pack: the schema has "slug".
	s.marker, s.good = `"slug"`, packJSON(t)
	pack, err := c.Generate(context.Background(), Request{Pasted: "Find the maximum subarray sum."})
	if err != nil || pack.Slug != "best-stretch" {
		t.Fatalf("generate: %v %v", pack, err)
	}
	if s.paths[0] != "/chat/completions" {
		t.Errorf("path %s", s.paths[0])
	}

	// Edit: the schema has "unsupported".
	s.marker, s.good = `"unsupported"`, `{"files":[{"path":"statement.md","content":"New text."}],"summary":"Reworded.","unsupported":""}`
	ans, err := c.Edit(context.Background(), EditRequest{Instruction: "Reword it.", Files: []EditFile{{Path: "statement.md", Content: "Old."}}})
	if err != nil || len(ans.Files) != 1 || ans.Summary != "Reworded." {
		t.Fatalf("edit: %v %v", ans, err)
	}

	// Without the schema text the stand-in answers with a non-pack, which proves
	// the marker check bites.
	s.marker = "never in the request"
	if _, err := Generate(context.Background(), c, Request{Pasted: "x"}); err == nil {
		t.Error("the stand-in answered a pack although the schema text was missing")
	}
}

func TestChatFunctionPackAndFix(t *testing.T) {
	s := &schemaAware{}
	srv := httptest.NewServer(s)
	defer srv.Close()
	c := chatClient(hostObjectRow, srv.URL, "m")

	fn, _ := json.Marshal(FakeFunctionArraySum())
	s.marker, s.good = `"brute_cpp"`, string(fn)
	pack, err := c.GenerateFunction(context.Background(), Request{Pasted: "Sum an array.", JavaLists: "arraylist"})
	if err != nil || pack == nil || pack.Slug == "" {
		t.Fatalf("function: %v %v", pack, err)
	}

	fixed, _ := json.Marshal(&FixAnswer{LockedTop: "import java.util.*;", Starter: "static long solve(int[] a) { return 0; }", LockedBottom: "class Main {}", Solution: "static long solve(int[] a) { return 1; }"})
	s.marker, s.good = `"locked_bottom"`, string(fixed)
	ans, err := c.FixLanguage(context.Background(), sampleFixRequest())
	if err != nil || ans.LockedBottom != "class Main {}" {
		t.Fatalf("fix: %v %v", ans, err)
	}
	// The fix call's user message holds the problem and the system message the schema.
	last := s.bodies[len(s.bodies)-1]["messages"].([]any)
	if len(last) != 2 || !strings.Contains(fmt.Sprint(last[1]), "Sum the array.") {
		t.Errorf("fix messages %v", last)
	}
}

func TestChatJSONModeFollowsTheRow(t *testing.T) {
	for _, tc := range []struct {
		row  chatHost
		want string
	}{{hostSchemaRow, "json_schema"}, {hostObjectRow, "json_object"}} {
		s := &schemaAware{marker: `"slug"`, good: packJSON(t)}
		srv := httptest.NewServer(s)
		c := chatClient(tc.row, srv.URL, "m")
		if _, err := c.Generate(context.Background(), Request{Pasted: "x"}); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		format := s.bodies[0]["response_format"].(map[string]any)
		if format["type"] != tc.want {
			t.Errorf("%s: response_format %v, want %s", tc.row.id, format, tc.want)
		}
		if tc.want == "json_schema" {
			js := format["json_schema"].(map[string]any)
			if js["name"] != "wizard_pack" || js["strict"] != true || js["schema"] == nil {
				t.Errorf("json_schema %v", js)
			}
		} else if len(format) != 1 {
			t.Errorf("json_object carries more: %v", format)
		}
		if s.bodies[0]["model"] != "m" || strings.Contains(fmt.Sprint(s.bodies[0]), testKey) {
			t.Errorf("body %v", s.bodies[0])
		}
	}
}

func TestChatFencedAnswerIsUnwrapped(t *testing.T) {
	for _, wrapped := range []string{"```json\n" + packJSON(t) + "\n```", "```\n" + packJSON(t) + "\n```\n", "  ```JSON\n" + packJSON(t) + "```"} {
		_, base := newStandIn(t, ok(chatReply(wrapped, "stop")))
		pack, err := chatClient(hostObjectRow, base, "m").Generate(context.Background(), Request{Pasted: "x"})
		if err != nil || pack.Slug != "best-stretch" {
			t.Errorf("fenced %q: %v %v", wrapped[:12], pack, err)
		}
	}
}

func TestChatLooseJSONThenGoodRetry(t *testing.T) {
	loose := "Here is the problem you asked for: " + packJSON(t)
	s, base := newStandIn(t, ok(chatReply(loose, "stop")), ok(chatReply(packJSON(t), "stop")))
	pack, err := Generate(context.Background(), chatClient(hostObjectRow, base, "m"), Request{Pasted: "Find the maximum subarray sum."})
	if err != nil || pack.Slug != "best-stretch" {
		t.Fatalf("%v %v", pack, err)
	}
	if len(s.reqs) != 2 {
		t.Fatalf("%d requests", len(s.reqs))
	}
	msgs := s.reqs[1].body["messages"].([]any)
	if len(msgs) != 4 || msgs[2].(map[string]any)["role"] != "assistant" || !strings.Contains(fmt.Sprint(msgs[2]), "Here is the problem") ||
		!strings.Contains(fmt.Sprint(msgs[3]), "not in the format") {
		t.Errorf("retry messages %v", msgs)
	}
	// Two loose answers in a row end in invalid_answer, without the answer text.
	_, base = newStandIn(t, ok(chatReply(loose, "stop")))
	_, err = Generate(context.Background(), chatClient(hostObjectRow, base, "m"), Request{Pasted: "x"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInvalidAnswer {
		t.Errorf("two loose answers: %v", err)
	}
}

func TestChatStatusesMapToSentences(t *testing.T) {
	leak := `{"error":{"message":"bad key ` + testKey + `"}}`
	for _, tc := range []struct {
		status int
		code   string
		want   string
	}{
		{401, CodeKeyRefused, "Testhost refused the key. Check it and paste it again."},
		{402, CodeNoQuota, "Testhost says this key's account has no credit left. Check the plan on Testhost, or use another key."},
		{403, CodeNoAccess, "Testhost says this key cannot use m. Choose another model or key."},
		{404, CodeModelNotFound, "Testhost does not offer the model m to this key. Choose another model."},
		{413, CodeBadRequest, "Testhost says the text is too large for this model or plan. Use a shorter text or choose another model."},
		{429, CodeRateLimited, "Testhost is busy or your free limit is used up. Wait a minute and ask again, or choose another model."},
		{400, CodeBadRequest, "Testhost could not accept the request. Try a shorter text or another model."},
		{408, CodeUnavailable, "The provider is unavailable. Try again in a few minutes."},
		{503, CodeUnavailable, "The provider is unavailable. Try again in a few minutes."},
	} {
		s, base := newStandIn(t, scripted{tc.status, leak})
		_, err := chatClient(hostSchemaRow, base, "m").Generate(context.Background(), Request{Pasted: "x"})
		var e *Error
		if !errors.As(err, &e) || e.Code != tc.code || e.Message != tc.want {
			t.Errorf("%d: %v", tc.status, err)
		} else if strings.Contains(e.Message, testKey) || strings.Contains(fmt.Sprint(e), "bad key") {
			t.Errorf("%d: the body reached the message", tc.status)
		}
		// 429 is not retried; a server fault is tried again twice.
		wantReqs := 1
		if tc.status >= 500 {
			wantReqs = 3
		}
		if len(s.reqs) != wantReqs {
			t.Errorf("%d: %d requests, want %d", tc.status, len(s.reqs), wantReqs)
		}
	}
}

func TestChatCutOffRefusedAndBrokenAnswers(t *testing.T) {
	// A cut-off answer asks again for a shorter one, once.
	s, base := newStandIn(t, ok(chatReply(`{"slug":"x`, "length")), ok(chatReply(packJSON(t), "stop")))
	if _, err := Generate(context.Background(), chatClient(hostObjectRow, base, "m"), Request{Pasted: "x"}); err != nil {
		t.Fatalf("after a cut-off: %v", err)
	}
	if len(s.reqs) != 2 || !strings.Contains(fmt.Sprint(s.reqs[1].body["messages"]), "cut off") {
		t.Errorf("the retry did not ask for a shorter answer: %d requests", len(s.reqs))
	}
	_, base = newStandIn(t, ok(chatReply(`{"slug":"x`, "length")))
	_, err := Generate(context.Background(), chatClient(hostObjectRow, base, "m"), Request{Pasted: "x"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeCutOff || e.Message != "The answer was cut off before it was complete." {
		t.Errorf("two cut-offs: %v", err)
	}

	refusal := `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":null,"refusal":"I cannot help with that."}}]}`
	for name, body := range map[string]string{"refusal": refusal, "content_filter": chatReply("", "content_filter")} {
		s, base := newStandIn(t, ok(body))
		_, err := chatClient(hostObjectRow, base, "m").Generate(context.Background(), Request{Pasted: "x"})
		if !errors.As(err, &e) || e.Code != CodeRefused || e.Message != "The model declined this request. Rephrase it and try again." || strings.Contains(e.Message, "cannot help") {
			t.Errorf("%s: %v", name, err)
		}
		if len(s.reqs) != 1 {
			t.Errorf("%s: a refusal was retried", name)
		}
	}
	for name, body := range map[string]string{"error finish": chatReply("", "error"), "no choices": `{"choices":[]}`, "error body": `{"error":{"code":502,"message":"x"},"choices":[]}`, "not json": `<html>`} {
		_, base := newStandIn(t, ok(body))
		if _, err := chatClient(hostObjectRow, base, "m").Generate(context.Background(), Request{Pasted: "x"}); !errors.As(err, &e) || e.Code != CodeUnavailable {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestChatNeverFollowsARedirect(t *testing.T) {
	var hits int
	away := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer away.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, away.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	c := chatClient(hostSchemaRow, redirect.URL, "m")
	if _, err := c.Generate(context.Background(), Request{Pasted: "x"}); err == nil {
		t.Error("generate followed a redirect")
	}
	if _, err := c.ListModels(context.Background()); err == nil {
		t.Error("listing followed a redirect")
	}
	if err := c.Check(context.Background()); err == nil {
		t.Error("check followed a redirect")
	}
	if hits != 0 {
		t.Errorf("%d requests reached the redirect target", hits)
	}
}

func TestChatOversizeAnswer(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"`)
		chunk := strings.Repeat("a", 1<<20)
		for i := 0; i <= maxAnswer>>20; i++ {
			io.WriteString(w, chunk)
		}
		io.WriteString(w, `"}}]}`)
	}))
	defer big.Close()
	_, err := chatClient(hostObjectRow, big.URL, "m").Generate(context.Background(), Request{Pasted: "x"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeTooLarge || e.Message != "The answer was too large to use. Ask for a smaller problem." {
		t.Errorf("oversize: %v", err)
	}
}

func TestChatHostWithoutAModelList(t *testing.T) {
	c := chatClient(hostNoList, "http://127.0.0.1:1", "")
	_, err := c.ListModels(context.Background())
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInput || e.Message != "Enter the model id." {
		t.Errorf("listing a host without a list: %v", err)
	}
	if _, err := c.Generate(context.Background(), Request{Pasted: "x"}); !errors.As(err, &e) || !strings.Contains(e.Message, "Choose a model") {
		t.Errorf("generate without a model: %v", err)
	}
	// With a check path the key is checked without a model and the list stays empty.
	_, base := newStandIn(t, ok(`{"data":{}}`))
	withCheck := hostNoList
	withCheck.checkPath = "/key"
	models, err := chatClient(withCheck, base, "").ListModels(context.Background())
	if err != nil || models == nil || len(models) != 0 {
		t.Errorf("no-list host with a check: %v %v", models, err)
	}
}

// A public model listing does not prove a key: the check path does.
func TestChatKeyCheckUsesTheCheckPath(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/models":
			io.WriteString(w, `{"data":[{"id":"a/new","name":"New","created":2},{"id":"a/old","created":1}]}`)
		case "/key":
			if r.Header.Get("Authorization") != "Bearer "+testKey {
				w.WriteHeader(401)
				io.WriteString(w, `{"error":{"message":"bad `+r.Header.Get("Authorization")+`"}}`)
				return
			}
			io.WriteString(w, `{"data":{"label":"x"}}`)
		}
	}))
	defer srv.Close()
	bad := &ChatCompat{Host: hostSchemaRow, Key: NewSecret("wrong-key"), BaseURL: srv.URL}
	if _, err := bad.ListModels(context.Background()); err == nil || !strings.Contains(err.Error(), "refused the key") || strings.Contains(err.Error(), "wrong-key") {
		t.Errorf("wrong key: %v", err)
	}
	if err := bad.Check(context.Background()); err == nil {
		t.Error("check accepted a wrong key")
	}
	good := chatClient(hostSchemaRow, srv.URL, "")
	models, err := good.ListModels(context.Background())
	if err != nil || len(models) != 2 || models[0].ID != "a/new" || models[0].Name != "New" || models[1].Name != "a/old" {
		t.Errorf("models %v %v", models, err)
	}
	if err := good.Check(context.Background()); err != nil {
		t.Error(err)
	}
	// A host without a check path is checked by its listing.
	mu.Lock()
	paths = nil
	mu.Unlock()
	if err := chatClient(hostObjectRow, srv.URL, "m").Check(context.Background()); err != nil || len(paths) != 1 || paths[0] != "/models" {
		t.Errorf("listing check: %v %v", err, paths)
	}
}

func TestChatModelListFilter(t *testing.T) {
	_, base := newStandIn(t, ok(`{"data":[
{"id":"vendor/chat-a","created":5,"supported_parameters":["temperature","response_format"]},
{"id":"vendor/no-format","created":9,"supported_parameters":["temperature"]},
{"id":"vendor/chat-b","created":7,"supported_parameters":["structured_outputs"]},
{"id":"whisper-large-v3","created":8,"active":true},
{"id":"llama-guard-4","created":8},
{"id":"orpheus-tts","created":8},
{"id":"retired","created":6,"active":false},
{"id":"llama-fine","created":4,"active":true}]}`))
	models, err := chatClient(hostObjectRow, base, "").ListModels(context.Background())
	var ids []string
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	if err != nil || strings.Join(ids, ",") != "vendor/chat-b,vendor/chat-a,llama-fine" {
		t.Errorf("filtered %v %v", ids, err)
	}
}

func TestChatTableRowsAreFixed(t *testing.T) {
	or, ok1 := chatHostFor(ProviderOpenRouter)
	gq, ok2 := chatHostFor(ProviderGroq)
	if !ok1 || !ok2 || or.name != "OpenRouter" || gq.name != "Groq" {
		t.Fatalf("rows %v %v", or, gq)
	}
	if or.base != "https://openrouter.ai/api/v1" || gq.base != "https://api.groq.com/openai/v1" {
		t.Errorf("bases %s %s", or.base, gq.base)
	}
	if !or.jsonSchema || gq.jsonSchema || !or.hasList || !gq.hasList || or.checkPath != "/key" || gq.checkPath != "" {
		t.Errorf("rows %+v %+v", or, gq)
	}
	for _, h := range chatHosts {
		if !strings.HasPrefix(h.base, "https://") || strings.Contains(h.base, "?") || !Known(h.id, false) || ProviderName(h.id) != h.name || !IsOther(h.id) {
			t.Errorf("row %s", h.id)
		}
	}
	if _, ok := chatHostFor("https://evil.example"); ok || Known("https://evil.example", true) || IsOther("anthropic") {
		t.Error("a typed address was taken for a provider")
	}
	p, err := NewProvider(Config{Provider: ProviderGroq, Model: "openai/gpt-oss-20b", Key: NewSecret(testKey)}, false)
	if cc, ok := p.(*ChatCompat); err != nil || !ok || cc.Host.id != ProviderGroq || cc.Model != "openai/gpt-oss-20b" || cc.url("/models") != "https://api.groq.com/openai/v1/models" {
		t.Errorf("NewProvider: %v %v", p, err)
	}
	if _, err := NewProvider(Config{Provider: ProviderGroq, Model: "a/../b", Key: NewSecret(testKey)}, false); err == nil {
		t.Error("a model id with .. was taken")
	}
	if _, ok := CleanModelIDFor(ProviderOpenAI, "vendor/model"); ok {
		t.Error("a slash was allowed for a built-in provider")
	}
	if id, ok := CleanModelIDFor(ProviderOpenRouter, "vendor/model:free"); !ok || id != "vendor/model:free" {
		t.Errorf("slash id: %q %v", id, ok)
	}
	if _, ok := CleanModelIDFor(ProviderOpenRouter, "vendor/mo del"); ok {
		t.Error("a space was allowed")
	}
	if HasModelList(ProviderOpenAI) != true || !HasModelList(ProviderGroq) {
		t.Error("HasModelList")
	}
}
