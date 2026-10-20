package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stand-ins for the OpenAI and Gemini APIs (D111). Each test scripts the
// answers in order; the last one repeats.

type scripted struct {
	status int
	body   string
}

type standIn struct {
	mu      sync.Mutex
	answers []scripted
	reqs    []recorded
	urls    []string
}

func (s *standIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.reqs = append(s.reqs, recorded{r.URL.Path, r.Header.Clone(), body})
	s.urls = append(s.urls, r.URL.String())
	a := s.answers[min(len(s.reqs)-1, len(s.answers)-1)]
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(a.status)
	io.WriteString(w, a.body)
}

func newStandIn(t *testing.T, answers ...scripted) (*standIn, string) {
	t.Helper()
	retryDelay = func(int) time.Duration { return time.Millisecond }
	s := &standIn{answers: answers}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv.URL
}

func ok(body string) scripted { return scripted{200, body} }

func openAIAnswer(text string) string {
	b, _ := json.Marshal(text)
	return `{"status":"completed","output":[{"type":"reasoning","summary":[]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":` + string(b) + `}]}]}`
}

func geminiAnswer(text, finish string) string {
	b, _ := json.Marshal(text)
	return `{"candidates":[{"content":{"role":"model","parts":[{"text":"draft thoughts","thought":true},{"text":` + string(b) + `}]},"finishReason":"` + finish + `"}]}`
}

// walk calls f on every object node of a schema.
func walk(s map[string]any, f func(map[string]any)) {
	f(s)
	if props, ok := s["properties"].(map[string]any); ok {
		for _, v := range props {
			if sub, ok := v.(map[string]any); ok {
				walk(sub, f)
			}
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		walk(items, f)
	}
}

func TestOpenAIRequestShape(t *testing.T) {
	answer, _ := json.Marshal(FakeMaxSubarray())
	s, base := newStandIn(t, ok(openAIAnswer(string(answer))))
	o := &OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: base}
	pack, err := o.Generate(context.Background(), Request{Pasted: "Find the maximum subarray sum.", Previous: `{"a":1}`, Fix: []string{"Fix this."}})
	if err != nil || pack.Slug != "best-stretch" {
		t.Fatalf("pack %v %v", pack, err)
	}
	r := s.reqs[0]
	if r.path != "/responses" || r.header.Get("Authorization") != "Bearer "+testKey || strings.Contains(s.urls[0], testKey) {
		t.Errorf("path %s auth %q url %s", r.path, r.header.Get("Authorization"), s.urls[0])
	}
	b := r.body
	if b["model"] != "gpt-x" || b["store"] != false || !strings.Contains(fmt.Sprint(b["instructions"]), "Answer with one JSON object") {
		t.Errorf("body %v", b)
	}
	input, _ := b["input"].([]any)
	if len(input) != 3 || input[1].(map[string]any)["role"] != "assistant" || !strings.Contains(fmt.Sprint(input[2]), "Fix this.") {
		t.Errorf("input %v", input)
	}
	format := b["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["strict"] != true || format["name"] != "wizard_pack" {
		t.Errorf("format %v", format)
	}
	// Strict mode: every object lists all its properties as required and
	// allows no others.
	objects := 0
	walk(format["schema"].(map[string]any), func(o map[string]any) {
		props, ok := o["properties"].(map[string]any)
		if !ok {
			return
		}
		objects++
		req, _ := o["required"].([]any)
		if o["additionalProperties"] != false || len(req) != len(props) {
			t.Errorf("object not strict: %v", o)
		}
	})
	if objects != 3 {
		t.Errorf("%d objects in the schema", objects)
	}
}

func TestOpenAIAnswers(t *testing.T) {
	cases := []struct {
		body string
		code string
	}{
		{`{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"I can't help with that."}]}]}`, CodeRefused},
		{`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}`, CodeCutOff},
		{`{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`, CodeRefused},
		{openAIAnswer("not json"), CodeInvalidAnswer},
	}
	for _, c := range cases {
		_, base := newStandIn(t, ok(c.body))
		o := &OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: base}
		if _, err := o.Generate(context.Background(), Request{Pasted: "x"}); err == nil || err.(*Error).Code != c.code {
			t.Errorf("%s: %v", c.body, err)
		}
	}
	_, base := newStandIn(t, ok(openAIAnswer(`{"source":"int main(){}"}`)))
	o := &OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: base}
	if src, err := o.Solve(context.Background(), "Sum."); err != nil || src != "int main(){}" {
		t.Errorf("solve %q %v", src, err)
	}
}

func TestOpenAIModelsAndErrors(t *testing.T) {
	raw := `{"data":[{"id":"gpt-x","created":2},{"id":"text-embedding-3-small","created":3},{"id":"o9","created":4},
{"id":"gpt-x-realtime-preview","created":5},{"id":"gpt-3.5-turbo","created":1},{"id":"gpt-4-turbo","created":1},{"id":"dall-e-3","created":9}]}`
	_, base := newStandIn(t, ok(raw))
	o := &OpenAI{Key: NewSecret(testKey), BaseURL: base}
	models, err := o.ListModels(context.Background())
	if err != nil || fmt.Sprint(models) != "[{o9 o9} {gpt-x gpt-x}]" {
		t.Errorf("models %v %v", models, err)
	}

	echo := func(code string) string {
		return fmt.Sprintf(`{"error":{"message":"bad key %s","type":"x","code":%q}}`, testKey, code)
	}
	cases := []struct {
		status int
		code   string
		want   string
		says   string
	}{
		{401, "invalid_api_key", CodeKeyRefused, "OpenAI refused the key"},
		{429, "insufficient_quota", CodeNoQuota, "used up its quota"},
		{404, "model_not_found", CodeModelNotFound, "OpenAI does not offer the model gpt-x"},
		{403, "unsupported_country_region_territory", CodeNoAccess, "cannot use gpt-x"},
		{429, "rate_limit_exceeded", CodeRateLimited, "Rate limited"},
		{503, "", CodeUnavailable, "unavailable"},
		{400, "invalid_json_schema", CodeBadRequest, "OpenAI could not accept"},
	}
	for _, c := range cases {
		s, base := newStandIn(t, scripted{c.status, echo(c.code)})
		o := &OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: base}
		e, isErr := o.Check(context.Background()).(*Error)
		if !isErr || e.Code != c.want || !strings.Contains(e.Message, c.says) {
			t.Errorf("%d %s: %v", c.status, c.code, e)
			continue
		}
		if b, _ := json.Marshal(e); strings.Contains(e.Error()+string(b)+fmt.Sprintf("%+v", e), testKey) {
			t.Errorf("%d: the key reached the error", c.status)
		}
		// Only overload and plain rate limits are tried again.
		retried := c.status >= 500 || c.code == "rate_limit_exceeded"
		if n := len(s.reqs); (retried && n != 1+httpRetries) || (!retried && n != 1) {
			t.Errorf("%d %s: %d requests", c.status, c.code, n)
		}
	}
	// A 5xx then an answer: the retry succeeds.
	_, base = newStandIn(t, scripted{502, `{}`}, ok(`{"id":"gpt-x"}`))
	if err := (&OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: base}).Check(context.Background()); err != nil {
		t.Errorf("retry: %v", err)
	}
	// A redirect is not followed with the key.
	s, base := newStandIn(t, scripted{307, `{}`})
	if err := (&OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: base}).Check(context.Background()); err == nil || len(s.reqs) != 1 {
		t.Errorf("redirect followed: %v %d", err, len(s.reqs))
	}
	// Nothing listening.
	if e := (&OpenAI{Key: NewSecret(testKey), Model: "gpt-x", BaseURL: "http://127.0.0.1:1"}).Check(context.Background()); e == nil || e.(*Error).Code != CodeUnavailable {
		t.Errorf("network failure: %v", e)
	}
}

func TestGeminiRequestShape(t *testing.T) {
	answer, _ := json.Marshal(FakeMaxSubarray())
	s, base := newStandIn(t, ok(geminiAnswer(string(answer), "STOP")))
	g := &Gemini{Key: NewSecret(testKey), Model: "gemini-x-pro", BaseURL: base}
	pack, err := g.Generate(context.Background(), Request{Pasted: "Find the maximum subarray sum.", Previous: `{"a":1}`, Fix: []string{"Fix this."}})
	if err != nil || pack.Slug != "best-stretch" {
		t.Fatalf("pack %v %v", pack, err)
	}
	r := s.reqs[0]
	if r.path != "/models/gemini-x-pro:generateContent" || r.header.Get("X-Goog-Api-Key") != testKey || strings.Contains(s.urls[0], testKey) {
		t.Errorf("path %s key header %q url %s", r.path, r.header.Get("X-Goog-Api-Key"), s.urls[0])
	}
	b := r.body
	if !strings.Contains(fmt.Sprint(b["systemInstruction"]), "Answer with one JSON object") {
		t.Errorf("system %v", b["systemInstruction"])
	}
	contents, _ := b["contents"].([]any)
	if len(contents) != 3 || contents[1].(map[string]any)["role"] != "model" || !strings.Contains(fmt.Sprint(contents[2]), "Fix this.") {
		t.Errorf("contents %v", contents)
	}
	gc := b["generationConfig"].(map[string]any)
	if gc["responseMimeType"] != "application/json" {
		t.Errorf("generationConfig %v", gc)
	}
	// responseSchema is the OpenAPI subset: upper-case types, no
	// additionalProperties, every property required and ordered.
	nodes := 0
	walk(gc["responseSchema"].(map[string]any), func(o map[string]any) {
		nodes++
		if typ, _ := o["type"].(string); typ == "" || typ != strings.ToUpper(typ) {
			t.Errorf("type %v", o["type"])
		}
		if _, ok := o["additionalProperties"]; ok {
			t.Errorf("additionalProperties sent: %v", o)
		}
		if props, ok := o["properties"].(map[string]any); ok {
			req, _ := o["required"].([]any)
			order, _ := o["propertyOrdering"].([]any)
			if len(req) != len(props) || len(order) != len(props) {
				t.Errorf("object %v", o)
			}
		}
	})
	if nodes < 20 {
		t.Errorf("%d schema nodes", nodes)
	}
	if sub := gc["responseSchema"].(map[string]any)["properties"].(map[string]any)["checker"].(map[string]any); sub["enum"] == nil || sub["type"] != "STRING" {
		t.Errorf("enum lost: %v", sub)
	}
}

func TestGeminiAnswers(t *testing.T) {
	cases := []struct {
		body string
		code string
		cat  string
	}{
		{`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`, CodeRefused, "prohibited_content"},
		{geminiAnswer("", "SAFETY"), CodeRefused, "safety"},
		{geminiAnswer(`{"title":`, "MAX_TOKENS"), CodeCutOff, ""},
		{geminiAnswer("not json", "STOP"), CodeInvalidAnswer, ""},
		{`{"candidates":[]}`, CodeUnavailable, ""},
	}
	for _, c := range cases {
		_, base := newStandIn(t, ok(c.body))
		g := &Gemini{Key: NewSecret(testKey), Model: "gemini-x", BaseURL: base}
		_, err := g.Generate(context.Background(), Request{Pasted: "x"})
		if e, ok := err.(*Error); !ok || e.Code != c.code || e.Category != c.cat {
			t.Errorf("%s: %v", c.body, err)
		}
	}
	// The thought part is not part of the answer.
	_, base := newStandIn(t, ok(geminiAnswer(`{"source":"int main(){}"}`, "STOP")))
	g := &Gemini{Key: NewSecret(testKey), Model: "gemini-x", BaseURL: base}
	if src, err := g.Solve(context.Background(), "Sum."); err != nil || src != "int main(){}" {
		t.Errorf("solve %q %v", src, err)
	}
}

func TestGeminiModelsAndErrors(t *testing.T) {
	page1 := `{"models":[{"name":"models/gemini-x-pro","displayName":"Gemini X Pro","supportedGenerationMethods":["generateContent","countTokens"]},
{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"t2"}`
	page2 := `{"models":[{"name":"models/gemini-x-flash-image","supportedGenerationMethods":["generateContent"]},
{"name":"models/gemma-9","supportedGenerationMethods":["generateContent"]},
{"name":"models/gemini-x-flash","displayName":"","supportedGenerationMethods":["generateContent"]}]}`
	s, base := newStandIn(t, ok(page1), ok(page2))
	g := &Gemini{Key: NewSecret(testKey), BaseURL: base}
	models, err := g.ListModels(context.Background())
	if err != nil || fmt.Sprint(models) != "[{gemini-x-pro Gemini X Pro} {gemini-x-flash gemini-x-flash}]" {
		t.Errorf("models %v %v", models, err)
	}
	if len(s.urls) != 2 || !strings.Contains(s.urls[1], "pageToken=t2") || strings.Contains(strings.Join(s.urls, " "), testKey) {
		t.Errorf("pages %v", s.urls)
	}

	cases := []struct {
		status int
		body   string
		want   string
		says   string
	}{
		{400, `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT","details":[{"reason":"API_KEY_INVALID"}]}}`, CodeKeyRefused, "Google refused the key"},
		{429, `{"error":{"code":429,"message":"You exceeded your current quota","status":"RESOURCE_EXHAUSTED"}}`, CodeNoQuota, "used up its Gemini quota"},
		{404, `{"error":{"code":404,"message":"models/gemini-x is not found","status":"NOT_FOUND"}}`, CodeModelNotFound, "Gemini does not offer the model gemini-x"},
		{403, `{"error":{"code":403,"message":"denied","status":"PERMISSION_DENIED"}}`, CodeNoAccess, "cannot use gemini-x"},
		{500, `{"error":{"code":500,"status":"INTERNAL"}}`, CodeUnavailable, "unavailable"},
		{400, `{"error":{"code":400,"message":"Invalid JSON payload","status":"INVALID_ARGUMENT"}}`, CodeBadRequest, "Gemini could not accept"},
	}
	for _, c := range cases {
		s, base := newStandIn(t, scripted{c.status, strings.Replace(c.body, `"code"`, `"echo":"`+testKey+`","code"`, 1)})
		g := &Gemini{Key: NewSecret(testKey), Model: "gemini-x", BaseURL: base}
		e, isErr := g.Check(context.Background()).(*Error)
		if !isErr || e.Code != c.want || !strings.Contains(e.Message, c.says) {
			t.Errorf("%d: %v", c.status, e)
			continue
		}
		if b, _ := json.Marshal(e); strings.Contains(e.Error()+string(b), testKey) {
			t.Errorf("%d: the key reached the error", c.status)
		}
		if n := len(s.reqs); (c.status >= 500 && n != 1+httpRetries) || (c.status < 500 && n != 1) {
			t.Errorf("%d: %d requests", c.status, n)
		}
	}
	// A model that does not generate text is refused by name.
	_, base = newStandIn(t, ok(`{"name":"models/emb","supportedGenerationMethods":["embedContent"]}`))
	if e, ok := (&Gemini{Key: NewSecret(testKey), Model: "emb", BaseURL: base}).Check(context.Background()).(*Error); !ok || e.Code != CodeUnsupportedModel {
		t.Errorf("unusable model: %v", e)
	}
	// A deadline is reported as such.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := (&Gemini{Key: NewSecret(testKey), Model: "gemini-x", BaseURL: slow.URL}).Generate(ctx, Request{Pasted: "x"}); err == nil || err.(*Error).Code != CodeTimeout {
		t.Errorf("deadline: %v", err)
	}
}
