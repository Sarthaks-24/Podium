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

// fakeAPI stands in for the Claude API. It records each request and
// answers with the scripted status and body.
type fakeAPI struct {
	mu     sync.Mutex
	status int
	body   string
	sse    bool
	reqs   []recorded
}

type recorded struct {
	path   string
	header http.Header
	body   map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.reqs = append(f.reqs, recorded{r.URL.Path, r.Header.Clone(), body})
	status, out, sse := f.status, f.body, f.sse
	f.mu.Unlock()
	if sse {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	io.WriteString(w, out)
}

func newFakeAPI(t *testing.T, status int, body string, sse bool) (*fakeAPI, *Anthropic) {
	t.Helper()
	f := &fakeAPI{status: status, body: body, sse: sse}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, &Anthropic{Key: NewSecret(testKey), Model: DefaultModel, BaseURL: srv.URL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second}, MaxRetries: 0}
}

// stream builds an SSE answer with one text block and the given stop.
func stream(text, stopReason, stopDetails string) string {
	ev := func(name string, v any) string {
		b, _ := json.Marshal(v)
		return "event: " + name + "\ndata: " + string(b) + "\n\n"
	}
	delta := map[string]any{"stop_reason": stopReason, "stop_sequence": nil, "stop_details": nil}
	if stopDetails != "" {
		var d any
		json.Unmarshal([]byte(stopDetails), &d)
		delta["stop_details"] = d
	}
	return ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": DefaultModel, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 1}}}) +
		ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}) +
		ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}}) +
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}) +
		ev("message_delta", map[string]any{"type": "message_delta", "delta": delta, "usage": map[string]any{"output_tokens": 50}}) +
		ev("message_stop", map[string]any{"type": "message_stop"})
}

func TestAnthropicRequestShape(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-"+"ant-from-the-environment-must-not-be-used")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	answer, _ := json.Marshal(FakeMaxSubarray())
	f, a := newFakeAPI(t, 200, stream(string(answer), "end_turn", ""), true)
	pack, err := a.Generate(context.Background(), Request{Pasted: "Find the maximum subarray sum."})
	if err != nil {
		t.Fatal(err)
	}
	if pack.Slug != "best-stretch" || len(Check(pack)) != 0 {
		t.Fatalf("pack %+v", pack)
	}
	r := f.reqs[0]
	if r.path != "/v1/messages" {
		t.Errorf("path %s", r.path)
	}
	if got := r.header.Get("X-Api-Key"); got != testKey {
		t.Errorf("x-api-key %q: the key must come from the pasted value, never the environment", got)
	}
	if !strings.Contains(r.header.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("anthropic-beta %q", r.header.Get("Anthropic-Beta"))
	}
	b := r.body
	if b["model"] != DefaultModel || b["max_tokens"] != float64(64000) || b["stream"] != true || b["fallbacks"] != "default" {
		t.Errorf("body model %v max_tokens %v stream %v fallbacks %v", b["model"], b["max_tokens"], b["stream"], b["fallbacks"])
	}
	if _, ok := b["thinking"]; ok {
		t.Error("thinking was sent; Opus 5.5 must leave it unset")
	}
	oc, _ := b["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if oc["effort"] != "high" || format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("output_config %v", oc)
	}
	msgs, _ := b["messages"].([]any)
	if len(msgs) != 1 || !strings.Contains(fmt.Sprint(msgs[0]), "<problem_text>") {
		t.Errorf("messages %v", msgs)
	}

	// The retry sends the previous answer and the reasons as two more turns.
	if _, err := a.Generate(context.Background(), Request{Pasted: "x", Previous: `{"a":1}`, Fix: []string{"Fix this."}}); err != nil {
		t.Fatal(err)
	}
	msgs, _ = f.reqs[1].body["messages"].([]any)
	if len(msgs) != 3 || !strings.Contains(fmt.Sprint(msgs[1]), "assistant") || !strings.Contains(fmt.Sprint(msgs[2]), "Fix this.") {
		t.Errorf("retry messages %v", msgs)
	}

	// Haiku 5.5 has no server-side fallback.
	a.Model = "claude-haiku-5-5"
	a.Generate(context.Background(), Request{Pasted: "x"})
	if _, ok := f.reqs[2].body["fallbacks"]; ok || f.reqs[2].header.Get("Anthropic-Beta") != "" {
		t.Error("fallbacks sent for Haiku")
	}
}

func TestAnthropicStopReasons(t *testing.T) {
	_, a := newFakeAPI(t, 200, stream("", "refusal", `{"type":"refusal","category":"cyber","explanation":"no"}`), true)
	_, err := a.Generate(context.Background(), Request{Pasted: "x"})
	if e, ok := err.(*Error); !ok || e.Code != CodeRefused || e.Category != "cyber" || !strings.Contains(e.Message, "category: cyber") {
		t.Errorf("refusal: %v", err)
	}
	_, a = newFakeAPI(t, 200, stream(`{"title":`, "max_tokens", ""), true)
	_, err = a.Generate(context.Background(), Request{Pasted: "x"})
	if e, ok := err.(*Error); !ok || e.Code != CodeCutOff {
		t.Errorf("max_tokens: %v", err)
	}
	_, a = newFakeAPI(t, 200, stream(`not json`, "end_turn", ""), true)
	_, err = a.Generate(context.Background(), Request{Pasted: "x"})
	if e, ok := err.(*Error); !ok || e.Code != CodeInvalidAnswer || e.answer != "not json" {
		t.Errorf("bad json: %v", err)
	}
	_, a = newFakeAPI(t, 200, stream(`{"source":"int main(){return 0;}"}`, "end_turn", ""), true)
	if src, err := a.Solve(context.Background(), "Sum two numbers."); err != nil || src != "int main(){return 0;}" {
		t.Errorf("solve %q %v", src, err)
	}
}

func TestAnthropicErrorMapping(t *testing.T) {
	apiErr := func(typ string) string {
		// The body echoes the key, as a careless proxy might: it must never
		// reach the admin.
		return fmt.Sprintf(`{"type":"error","error":{"type":%q,"message":"bad key %s"}}`, typ, testKey)
	}
	cases := []struct {
		status int
		typ    string
		code   string
		says   string
	}{
		{401, "authentication_error", CodeKeyRefused, "refused the key"},
		{403, "permission_error", CodeNoAccess, "cannot use claude-opus-5-5"},
		{404, "not_found_error", CodeModelNotFound, "does not offer the model"},
		{429, "rate_limit_error", CodeRateLimited, "Rate limited by the provider; try again in a minute."},
		{500, "api_error", CodeUnavailable, "The provider is unavailable"},
		{529, "overloaded_error", CodeUnavailable, "The provider is unavailable"},
		{400, "invalid_request_error", CodeBadRequest, "could not accept the request"},
	}
	for _, c := range cases {
		_, a := newFakeAPI(t, c.status, apiErr(c.typ), false)
		for name, call := range map[string]func() error{
			"generate": func() error { _, err := a.Generate(context.Background(), Request{Pasted: "x"}); return err },
			"check":    func() error { return a.Check(context.Background()) },
		} {
			err := call()
			e, ok := err.(*Error)
			if !ok || e.Code != c.code || !strings.Contains(e.Message, c.says) {
				t.Errorf("%d %s: %v", c.status, name, err)
				continue
			}
			if b, _ := json.Marshal(e); strings.Contains(e.Error()+string(b)+fmt.Sprintf("%+v", e), testKey) {
				t.Errorf("%d %s: the key reached the error", c.status, name)
			}
		}
	}

	// A provider that is not there at all.
	a := &Anthropic{Key: NewSecret(testKey), Model: DefaultModel, BaseURL: "http://127.0.0.1:1", MaxRetries: 0}
	if e, ok := a.Check(context.Background()).(*Error); !ok || e.Code != CodeUnavailable {
		t.Errorf("network failure: %v", e)
	}
	// A deadline is reported as such.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer slow.Close()
	a.BaseURL = slow.URL
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := a.Generate(ctx, Request{Pasted: "x"}); err == nil || err.(*Error).Code != CodeTimeout {
		t.Errorf("deadline: %v", err)
	}
}
