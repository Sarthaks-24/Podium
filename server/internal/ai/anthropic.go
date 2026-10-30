package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// anthropicBase is the public API; tests point BaseURL at a stand-in.
const anthropicBase = "https://api.anthropic.com"

// anthropicVersion is the API version every request names.
const anthropicVersion = "2023-06-01"

// maxTokens leaves room for thinking plus a whole pack; requests stream,
// so a large value does not risk an HTTP timeout.
const maxTokens = 64000

// fallbackBeta turns on server-side refusal fallbacks in their "default"
// form, which routes by refusal category.
const fallbackBeta = "server-side-fallback-2026-07-01"

// fallbackModels accept server-side refusal fallbacks ("default" routing
// by refusal category). This is a table of a feature, not a choice of
// model: any other model, Haiku included, is sent without them, and the
// admin picks the model.
var fallbackModels = map[string]bool{
	"claude-fable-5-1": true, "claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true,
}

// Anthropic calls the Claude API with net/http and the standard library,
// like the other two providers. The key goes only in the X-Api-Key header
// of each request; nothing is read from the environment.
type Anthropic struct {
	Key   Secret
	Model string
	// BaseURL and HTTPClient are for tests; empty means the public API.
	BaseURL    string
	HTTPClient *http.Client
	// MaxRetries is how often a 408, 409, 429 or 5xx is tried again;
	// negative means httpRetries.
	MaxRetries int
}

func (a *Anthropic) url(path string) string {
	base := a.BaseURL
	if base == "" {
		base = anthropicBase
	}
	return strings.TrimRight(base, "/") + path
}

func (a *Anthropic) header(beta string) map[string]string {
	h := map[string]string{"X-Api-Key": a.Key.reveal(), "Anthropic-Version": anthropicVersion}
	if beta != "" {
		h["Anthropic-Beta"] = beta
	}
	return h
}

func (a *Anthropic) retries() int {
	if a.MaxRetries < 0 {
		return httpRetries
	}
	return a.MaxRetries
}

// anthropicRetryable: timeouts, conflicts, rate limits and server errors
// (529 is overload) are tried again.
func anthropicRetryable(f *httpFailure) bool {
	switch f.status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	}
	return f.status >= 500
}

func (a *Anthropic) do(ctx context.Context, method, path, beta string, body, out any) error {
	resp, err := send(ctx, apiClient(a.HTTPClient), method, a.url(path), a.header(beta), body, a.retries(), anthropicRetryable)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(limitBody(resp)).Decode(out); err != nil {
		if ctx.Err() != nil {
			return transportError(ctx)
		}
		return errUnavailable()
	}
	return nil
}

type anthropicText struct {
	Type         string            `json:"type"`
	Text         string            `json:"text"`
	CacheControl map[string]string `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content []anthropicText `json:"content"`
}

func anthropicTurn(role, text string) anthropicMessage {
	return anthropicMessage{Role: role, Content: []anthropicText{{Type: "text", Text: text}}}
}

// streamEvent is the part of a server-sent event that call reads.
type streamEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		StopReason  string `json:"stop_reason"`
		StopDetails *struct {
			Category string `json:"category"`
		} `json:"stop_details"`
	} `json:"delta"`
	Error struct {
		Type string `json:"type"`
	} `json:"error"`
}

// call streams one structured request and returns the answer's text.
func (a *Anthropic) call(ctx context.Context, system string, msgs []anthropicMessage, schema map[string]any) (string, error) {
	if a.Model == "" {
		return "", needModel(ProviderAnthropic)
	}
	body := map[string]any{
		"model":      a.Model,
		"max_tokens": maxTokens,
		"stream":     true,
		// The system prompt is fixed text (the pack format rules), so it
		// carries the cache breakpoint; the schema in output_config is
		// cached by the API on its own. A prompt under the model's minimum
		// silently does not cache, which costs nothing.
		"system":   []anthropicText{{Type: "text", Text: system, CacheControl: map[string]string{"type": "ephemeral"}}},
		"messages": msgs,
		// Adaptive thinking, sent explicitly: leaving it unset means no
		// thinking on some models the live list offers (Opus 4.8 and 4.7).
		// Every listed model supports it (anthropicUsable). The thinking
		// text is not read, so it is omitted from the stream.
		"thinking": map[string]string{"type": "adaptive", "display": "omitted"},
		// Effort is set explicitly because Opus 5.5 and Haiku 5.5 default
		// to medium.
		"output_config": map[string]any{
			"effort": "high",
			"format": map[string]any{"type": "json_schema", "schema": schema},
		},
	}
	beta := ""
	if fallbackModels[a.Model] {
		body["fallbacks"], beta = "default", fallbackBeta
	}
	resp, err := send(ctx, apiClient(a.HTTPClient), "POST", a.url("/v1/messages"), a.header(beta), body, a.retries(), anthropicRetryable)
	if err != nil {
		return "", a.mapError(ctx, err)
	}
	defer resp.Body.Close()

	// The answer's content blocks in order, and how it stopped.
	type block struct {
		kind string
		text strings.Builder
	}
	var blocks []*block
	var stopReason, category string
	size := 0
	sc := bufio.NewScanner(limitBody(resp))
	sc.Buffer(make([]byte, 64<<10), maxAnswer)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue // event names, comments and the blank lines between events
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &ev); err != nil {
			return "", errUnavailable()
		}
		switch ev.Type {
		case "content_block_start":
			if ev.Index != len(blocks) {
				return "", errUnavailable()
			}
			b := &block{kind: ev.ContentBlock.Type}
			b.text.WriteString(ev.ContentBlock.Text)
			blocks = append(blocks, b)
		case "content_block_delta":
			if ev.Index < 0 || ev.Index >= len(blocks) {
				return "", errUnavailable()
			}
			if ev.Delta.Type == "text_delta" {
				if size += len(ev.Delta.Text); size > maxAnswer {
					return "", errUnavailable()
				}
				blocks[ev.Index].text.WriteString(ev.Delta.Text)
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Delta.StopDetails != nil {
				category = ev.Delta.StopDetails.Category
			}
		case "error":
			// An error after the stream opened, such as an overload.
			if ev.Error.Type == "rate_limit_error" {
				return "", errRateLimited()
			}
			return "", errUnavailable()
		}
	}
	if err := sc.Err(); err != nil {
		return "", transportError(ctx)
	}
	switch stopReason {
	case "":
		// The stream ended before the answer did.
		return "", transportError(ctx)
	case "refusal":
		cat := safeCategory(category)
		m := "The model declined this request."
		if cat != "" {
			m = fmt.Sprintf("The model declined this request (category: %s).", cat)
		}
		return "", &Error{Code: CodeRefused, Category: cat, Message: m + " Rephrase it and try again."}
	case "max_tokens":
		return "", &Error{Code: CodeCutOff, Message: "The answer was cut off before it was complete."}
	}
	var text strings.Builder
	for _, b := range blocks {
		switch b.kind {
		case "text":
			text.WriteString(b.text.String())
		case "fallback":
			// A mid-stream refusal fallback: what came before was the
			// first model's partial answer.
			text.Reset()
		}
	}
	return text.String(), nil
}

// Generate implements Provider.
func (a *Anthropic) Generate(ctx context.Context, req Request) (*WizardPack, error) {
	msgs := []anthropicMessage{anthropicTurn("user", UserPrompt(req))}
	if req.Previous != "" && len(req.Fix) > 0 {
		msgs = append(msgs, anthropicTurn("assistant", req.Previous), anthropicTurn("user", FixPrompt(req.Fix)))
	}
	text, err := a.call(ctx, systemPrompt, msgs, PackSchema())
	if err != nil {
		return nil, err
	}
	return DecodePack(text)
}

// claudeModel is a model as /v1/models describes it.
type claudeModel struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Capabilities struct {
		StructuredOutputs supported `json:"structured_outputs"`
		Thinking          struct {
			Types struct {
				Adaptive supported `json:"adaptive"`
			} `json:"types"`
		} `json:"thinking"`
		Effort struct {
			High supported `json:"high"`
		} `json:"effort"`
	} `json:"capabilities"`
}

type supported struct {
	Supported bool `json:"supported"`
}

// Check implements Provider with a model lookup: it costs no tokens and
// proves the key, the model name and that the model has what the wizard
// sends (structured outputs, adaptive thinking, effort high). Without a
// model it lists the models instead.
func (a *Anthropic) Check(ctx context.Context) error {
	if a.Model == "" {
		_, err := a.ListModels(ctx)
		return err
	}
	var m claudeModel
	if err := a.do(ctx, "GET", "/v1/models/"+url.PathEscape(a.Model), "", nil, &m); err != nil {
		return a.mapError(ctx, err)
	}
	if !anthropicUsable(&m) {
		return unsupportedModel(a.Model)
	}
	return nil
}

// ListModels implements Provider: every page of /v1/models, newest first
// as the API lists them, kept when the model supports what the wizard
// sends.
func (a *Anthropic) ListModels(ctx context.Context) ([]Model, error) {
	var out []Model
	after, seen := "", 0
	for {
		var page struct {
			Data    []claudeModel `json:"data"`
			HasMore bool          `json:"has_more"`
			LastID  string        `json:"last_id"`
		}
		path := "/v1/models?limit=1000"
		if after != "" {
			path += "&after_id=" + url.QueryEscape(after)
		}
		if err := a.do(ctx, "GET", path, "", nil, &page); err != nil {
			return nil, a.mapError(ctx, err)
		}
		for i := range page.Data {
			if seen++; seen > maxListed {
				return out, nil
			}
			if m := &page.Data[i]; anthropicUsable(m) {
				out = append(out, Model{ID: m.ID, Name: m.DisplayName})
			}
		}
		if !page.HasMore || page.LastID == "" || page.LastID == after {
			return out, nil
		}
		after = page.LastID
	}
}

// anthropicUsable is the Anthropic filter rule: the request shape in call
// needs structured outputs, adaptive thinking and effort high.
func anthropicUsable(m *claudeModel) bool {
	c := m.Capabilities
	return c.StructuredOutputs.Supported && c.Thinking.Types.Adaptive.Supported && c.Effort.High.Supported
}

func unsupportedModel(model string) error {
	return &Error{Code: CodeUnsupportedModel, Message: fmt.Sprintf("%s cannot write problems here: it lacks structured outputs, adaptive thinking or effort. Choose another model.", model)}
}

// mapError turns a failure into a fixed sentence. The body is read for
// its error type only; it is never wrapped, formatted or logged, since a
// careless proxy could echo the key in it.
func (a *Anthropic) mapError(ctx context.Context, err error) error {
	f, ok := err.(*httpFailure)
	if !ok {
		if e, ok := err.(*Error); ok {
			return e
		}
		return transportError(ctx)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	json.Unmarshal(f.body, &body)
	switch code := f.status; {
	case code == http.StatusPaymentRequired || body.Error.Type == "billing_error" ||
		(code == http.StatusBadRequest && strings.Contains(string(f.body), "credit balance")):
		return &Error{Code: CodeNoQuota, Message: "Anthropic says this key's account has no credit left. Add credit in the Claude Console, or use another key."}
	case code == http.StatusUnauthorized:
		return &Error{Code: CodeKeyRefused, Message: "Anthropic refused the key. Check it and paste it again."}
	case code == http.StatusForbidden:
		return &Error{Code: CodeNoAccess, Message: fmt.Sprintf("Anthropic says this key cannot use %s. Choose another model or key.", a.Model)}
	case code == http.StatusNotFound:
		return &Error{Code: CodeModelNotFound, Message: fmt.Sprintf("Anthropic does not offer the model %s to this key. Choose another model.", a.Model)}
	case code == http.StatusTooManyRequests:
		return errRateLimited()
	case code >= 500:
		return errUnavailable()
	case code >= 400:
		return &Error{Code: CodeBadRequest, Message: "Anthropic could not accept the request. Try a shorter text or another model."}
	}
	return transportError(ctx)
}
