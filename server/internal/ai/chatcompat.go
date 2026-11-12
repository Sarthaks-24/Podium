package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// The Chat Completions client: one implementation for the hosts that speak
// that shape, from a fixed table in this file. There is no typed address:
// the host comes from the table row chosen by provider id, and Config.BaseURL
// stays test-only, so no request can point a key at another host.

// chatHost is one row of the table.
type chatHost struct {
	id, name string
	// base is the API root; paths below are appended to it.
	base string
	// jsonSchema says the host takes response_format json_schema; false
	// means json_object. Either way the schema is also written into the
	// system message, because json_object never carries it.
	jsonSchema bool
	// hasList says GET {base}/models lists the host's models.
	hasList bool
	// checkPath is a GET path that proves the key and costs no tokens;
	// empty means the model listing is the check. A listing that is
	// public would not prove a key, so a host with such a listing needs a path.
	checkPath string
}

// chatHosts is the table. Each row was read from the host's own
// documentation on 2026-11-13; the pages are named above the row.
var chatHosts = []chatHost{
	// https://openrouter.ai/docs/api-reference/overview (POST /api/v1/chat/completions,
	// Bearer key, finish_reason stop/length/content_filter/error),
	// https://openrouter.ai/docs/guides/features/structured-outputs (json_schema; support
	// is "determined per endpoint", and a model without it ignores the parameter unless
	// require_parameters is set, which this client never sets; the schema is in the
	// system message as well),
	// https://openrouter.ai/docs/api-reference/models/get-models (GET /api/v1/models,
	// data[].id, name, supported_parameters; the spec lists a bearer key but does not say
	// a missing key is refused, so the listing is not used as the key check),
	// https://openrouter.ai/docs/api-reference/api-keys/get-current-key (GET /api/v1/key,
	// 401 for a bad key),
	// https://openrouter.ai/docs/api-reference/errors (402 credits, 403 moderation,
	// 413 payload too large, 429 rate limit).
	{id: ProviderOpenRouter, name: "OpenRouter", base: "https://openrouter.ai/api/v1", jsonSchema: true, hasList: true, checkPath: "/key"},
	// https://console.groq.com/docs/api-reference (base https://api.groq.com/openai/v1,
	// Bearer key, GET /models with data[].id, created, active; the page's example sends
	// the key but does not say a missing key is refused, so the listing is trusted as the
	// check: UNCONFIRMED until the owner's check with a real key),
	// https://console.groq.com/docs/structured-outputs (json_schema only on four named
	// models, json_object on any model, so json_object here),
	// https://console.groq.com/docs/errors (401 key, 413 request too large, 429 rate limit).
	{id: ProviderGroq, name: "Groq", base: "https://api.groq.com/openai/v1", hasList: true},
}

func chatHostFor(id string) (chatHost, bool) {
	for _, h := range chatHosts {
		if h.id == id {
			return h, true
		}
	}
	return chatHost{}, false
}

// ChatCompat calls a Chat Completions host from the table.
type ChatCompat struct {
	Host  chatHost
	Key   Secret
	Model string
	// BaseURL and HTTPClient are for tests; empty means the host's own API.
	BaseURL    string
	HTTPClient *http.Client
}

func (c *ChatCompat) url(path string) string {
	base := c.BaseURL
	if base == "" {
		base = c.Host.base
	}
	return strings.TrimRight(base, "/") + path
}

func (c *ChatCompat) header() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.Key.reveal()}
}

// chatRetryable retries a server fault only. A rate limit on these hosts is
// usually a used-up free limit, which waiting a few seconds does not fix.
func chatRetryable(f *httpFailure) bool { return f.status >= 500 }

// mapError turns a failure into the host's sentence. The body is never read.
func (c *ChatCompat) mapError(ctx context.Context, err error) error {
	f, ok := err.(*httpFailure)
	if !ok {
		if e, ok := err.(*Error); ok {
			return e
		}
		return transportError(ctx)
	}
	name := c.Host.name
	switch {
	case f.status == http.StatusUnauthorized:
		return &Error{Code: CodeKeyRefused, Message: name + " refused the key. Check it and paste it again."}
	case f.status == http.StatusPaymentRequired:
		return &Error{Code: CodeNoQuota, Message: name + " says this key's account has no credit left. Check the plan on " + name + ", or use another key."}
	case f.status == http.StatusNotFound:
		return &Error{Code: CodeModelNotFound, Message: fmt.Sprintf("%s does not offer the model %s to this key. Choose another model.", name, c.Model)}
	case f.status == http.StatusForbidden:
		return &Error{Code: CodeNoAccess, Message: fmt.Sprintf("%s says this key cannot use %s. Choose another model or key.", name, c.Model)}
	case f.status == http.StatusRequestEntityTooLarge:
		return &Error{Code: CodeBadRequest, Message: name + " says the text is too large for this model or plan. Use a shorter text or choose another model."}
	case f.status == http.StatusTooManyRequests:
		return &Error{Code: CodeRateLimited, Message: name + " is busy or your free limit is used up. Wait a minute and ask again, or choose another model."}
	case f.status == http.StatusRequestTimeout || f.status >= 500:
		return errUnavailable()
	}
	return &Error{Code: CodeBadRequest, Message: name + " could not accept the request. Try a shorter text or another model."}
}

// chatExcluded are id parts of models that do not write text answers.
var chatExcluded = []string{"audio", "realtime", "tts", "transcribe", "whisper", "image", "embed", "moderation", "guard", "orpheus"}

// chatUsable keeps a text model. When the host lists the parameters a model
// takes, it must take a response format.
func chatUsable(id string, params []string) bool {
	for _, x := range chatExcluded {
		if strings.Contains(id, x) {
			return false
		}
	}
	if len(params) == 0 {
		return true
	}
	for _, p := range params {
		if p == "response_format" || p == "structured_outputs" {
			return true
		}
	}
	return false
}

// verify makes the host's own key check.
func (c *ChatCompat) verify(ctx context.Context) error {
	var out json.RawMessage
	if err := doJSON(ctx, apiClient(c.HTTPClient), "GET", c.url(c.Host.checkPath), c.header(), nil, &out, chatRetryable); err != nil {
		return c.mapError(ctx, err)
	}
	return nil
}

// ListModels implements Provider: GET /models, filtered, newest first. A
// host with a check path of its own is checked first, because its listing
// may be public. A host without a list needs the model typed.
func (c *ChatCompat) ListModels(ctx context.Context) ([]Model, error) {
	if c.Host.checkPath != "" {
		if err := c.verify(ctx); err != nil {
			return nil, err
		}
	}
	if !c.Host.hasList {
		if c.Host.checkPath == "" {
			return nil, &Error{Code: CodeInput, Message: "Enter the model id."}
		}
		return []Model{}, nil
	}
	var page struct {
		Data []struct {
			ID      string   `json:"id"`
			Name    string   `json:"name"`
			Created int64    `json:"created"`
			Active  *bool    `json:"active"`
			Params  []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := doJSON(ctx, apiClient(c.HTTPClient), "GET", c.url("/models"), c.header(), nil, &page, chatRetryable); err != nil {
		return nil, c.mapError(ctx, err)
	}
	sort.SliceStable(page.Data, func(i, j int) bool { return page.Data[i].Created > page.Data[j].Created })
	out := []Model{}
	for _, m := range page.Data {
		if (m.Active != nil && !*m.Active) || !chatUsable(m.ID, m.Params) || len(out) >= maxListed {
			continue
		}
		if e, ok := listedModel(c.Host.id, m.ID, m.Name); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// Check implements Provider: the host's key check, or the listing.
func (c *ChatCompat) Check(ctx context.Context) error {
	if c.Host.checkPath != "" {
		return c.verify(ctx)
	}
	_, err := c.ListModels(ctx)
	return err
}

// schemaNote is the schema as text for the system message.
func schemaNote(schema map[string]any) string {
	b, _ := json.Marshal(strictSchema(schema))
	return "Reply with one JSON object and nothing else: no explanation and no code fence. It must match this JSON schema exactly:\n" + string(b)
}

// unfence strips a Markdown code fence a model put around its JSON.
func unfence(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return s
	}
	t = strings.TrimPrefix(t, "```")
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(t), "```"))
}

// call sends one request to /chat/completions and returns the answer text.
// openAIMessage is the same role and content pair this API uses.
func (c *ChatCompat) call(ctx context.Context, system string, msgs []openAIMessage, name string, schema map[string]any) (string, error) {
	if c.Model == "" {
		return "", needModel(c.Host.id)
	}
	format := map[string]any{"type": "json_object"}
	if c.Host.jsonSchema {
		format = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": name, "strict": true, "schema": strictSchema(schema)}}
	}
	body := map[string]any{
		"model":           c.Model,
		"messages":        append([]openAIMessage{{Role: "system", Content: system + "\n\n" + schemaNote(schema)}}, msgs...),
		"response_format": format,
	}
	resp, err := send(ctx, apiClient(c.HTTPClient), "POST", c.url("/chat/completions"), c.header(), body, httpRetries, chatRetryable)
	if err != nil {
		return "", c.mapError(ctx, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return "", transportError(ctx)
	}
	if len(raw) > maxAnswer {
		return "", &Error{Code: CodeTooLarge, Message: "The answer was too large to use. Ask for a smaller problem."}
	}
	var out struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &out) != nil || len(out.Choices) == 0 || (len(out.Error) > 0 && string(out.Error) != "null") {
		return "", errUnavailable()
	}
	ch := out.Choices[0]
	refused := &Error{Code: CodeRefused, Message: "The model declined this request. Rephrase it and try again."}
	switch {
	case ch.Message.Refusal != "":
		return "", refused
	case ch.FinishReason == "content_filter":
		refused.Category = "content_filter"
		return "", refused
	case ch.FinishReason == "length":
		return "", &Error{Code: CodeCutOff, Message: "The answer was cut off before it was complete."}
	case ch.FinishReason == "error":
		return "", errUnavailable()
	}
	return unfence(ch.Message.Content), nil
}

// retryMsgs is the user prompt and, on a retry, the previous answer and the
// reasons it was refused.
func retryMsgs(user, previous string, fix []string, fixPrompt func([]string) string) []openAIMessage {
	msgs := []openAIMessage{{Role: "user", Content: user}}
	if previous != "" && len(fix) > 0 {
		msgs = append(msgs, openAIMessage{Role: "assistant", Content: previous}, openAIMessage{Role: "user", Content: fixPrompt(fix)})
	}
	return msgs
}

// Generate implements Provider.
func (c *ChatCompat) Generate(ctx context.Context, req Request) (*WizardPack, error) {
	text, err := c.call(ctx, req.system(), retryMsgs(UserPrompt(req), req.Previous, req.Fix, FixPrompt), "wizard_pack", PackSchema())
	if err != nil {
		return nil, err
	}
	return DecodePack(text)
}

// GenerateFunction implements Provider.
func (c *ChatCompat) GenerateFunction(ctx context.Context, req Request) (*FunctionPack, error) {
	text, err := c.call(ctx, req.functionSystem(), retryMsgs(UserPrompt(req), req.Previous, req.Fix, FixPrompt), "function_pack", FunctionSchemaFor(req.JavaLists))
	if err != nil {
		return nil, err
	}
	return DecodeFunctionFor(text, req.JavaLists)
}

// Edit implements Provider.
func (c *ChatCompat) Edit(ctx context.Context, req EditRequest) (*EditAnswer, error) {
	text, err := c.call(ctx, req.System, retryMsgs(EditUserPrompt(req), req.Previous, req.Fix, EditFixPrompt), "pack_edit", EditSchema())
	if err != nil {
		return nil, err
	}
	return DecodeEdit(text)
}

// FixLanguage implements Provider.
func (c *ChatCompat) FixLanguage(ctx context.Context, req FixRequest) (*FixAnswer, error) {
	text, err := c.call(ctx, req.fixSystem(), retryMsgs(FixUserPrompt(req), req.Previous, req.Fix, FixRetryPrompt), "language_fix", FixSchema())
	if err != nil {
		return nil, err
	}
	return DecodeFix(text)
}
