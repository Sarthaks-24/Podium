package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// openAIBase is the public API; tests point BaseURL at a stand-in.
const openAIBase = "https://api.openai.com/v1"

// OpenAI calls the Responses API with net/http and the standard library
// (D111). Structured output uses a JSON schema in strict mode; the
// response is not stored at OpenAI (store: false).
type OpenAI struct {
	Key   Secret
	Model string
	// BaseURL and HTTPClient are for tests; empty means the public API.
	BaseURL    string
	HTTPClient *http.Client
}

func (o *OpenAI) url(path string) string {
	base := o.BaseURL
	if base == "" {
		base = openAIBase
	}
	return strings.TrimRight(base, "/") + path
}

func (o *OpenAI) header() map[string]string {
	return map[string]string{"Authorization": "Bearer " + o.Key.reveal()}
}

// openAIRetryable: overload and plain rate limits are tried again; a used
// up quota is not, since waiting does not help.
func openAIRetryable(f *httpFailure) bool {
	if f.status == http.StatusTooManyRequests {
		return openAIErrorCode(f.body) != "insufficient_quota"
	}
	return f.status >= 500
}

func openAIErrorCode(body []byte) string {
	var e struct {
		Error struct {
			Code any    `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	json.Unmarshal(body, &e)
	if c, ok := e.Error.Code.(string); ok && c != "" {
		return c
	}
	return e.Error.Type
}

// mapError turns a failure into the provider's sentence. The body is
// read for its error code only and is never shown.
func (o *OpenAI) mapError(ctx context.Context, err error) error {
	f, ok := err.(*httpFailure)
	if !ok {
		if e, ok := err.(*Error); ok {
			return e
		}
		return transportError(ctx)
	}
	code := openAIErrorCode(f.body)
	switch {
	case f.status == http.StatusUnauthorized || code == "invalid_api_key":
		return &Error{Code: CodeKeyRefused, Message: "OpenAI refused the key. Check it and paste it again."}
	case code == "insufficient_quota" || f.status == http.StatusPaymentRequired:
		return &Error{Code: CodeNoQuota, Message: "OpenAI says this key's account has used up its quota. Check the plan and billing on the OpenAI platform, or use another key."}
	case f.status == http.StatusNotFound || code == "model_not_found":
		return &Error{Code: CodeModelNotFound, Message: fmt.Sprintf("OpenAI does not offer the model %s to this key. Choose another model.", o.Model)}
	case f.status == http.StatusForbidden:
		return &Error{Code: CodeNoAccess, Message: fmt.Sprintf("OpenAI says this key cannot use %s. Choose another model or key.", o.Model)}
	case f.status == http.StatusTooManyRequests:
		return errRateLimited()
	case f.status >= 500:
		return errUnavailable()
	}
	return &Error{Code: CodeBadRequest, Message: "OpenAI could not accept the request. Try a shorter text or another model."}
}

// openAIExcluded are id parts of models that cannot answer a text request
// with a JSON schema.
var openAIExcluded = []string{"audio", "realtime", "tts", "transcribe", "image", "search", "embedding", "moderation", "instruct"}

var openAIReasoning = regexp.MustCompile(`^o[0-9]`)

// openAIUsable is the OpenAI filter rule (D111): chat model families by
// their id, without the ones before structured outputs.
func openAIUsable(id string) bool {
	if !strings.HasPrefix(id, "gpt-") && !openAIReasoning.MatchString(id) {
		return false
	}
	if strings.HasPrefix(id, "gpt-3.5") || id == "gpt-4" || strings.HasPrefix(id, "gpt-4-") {
		return false
	}
	for _, x := range openAIExcluded {
		if strings.Contains(id, x) {
			return false
		}
	}
	return true
}

// ListModels implements Provider: GET /models, filtered, newest first.
func (o *OpenAI) ListModels(ctx context.Context) ([]Model, error) {
	var page struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := doJSON(ctx, apiClient(o.HTTPClient), "GET", o.url("/models"), o.header(), nil, &page, openAIRetryable); err != nil {
		return nil, o.mapError(ctx, err)
	}
	sort.SliceStable(page.Data, func(i, j int) bool { return page.Data[i].Created > page.Data[j].Created })
	var out []Model
	for _, m := range page.Data {
		if openAIUsable(m.ID) && len(out) < maxListed {
			out = append(out, Model{ID: m.ID, Name: m.ID})
		}
	}
	return out, nil
}

// Check implements Provider: GET /models/{id}, or a listing without a
// model. Neither costs tokens.
func (o *OpenAI) Check(ctx context.Context) error {
	if o.Model == "" {
		_, err := o.ListModels(ctx)
		return err
	}
	var m struct {
		ID string `json:"id"`
	}
	if err := doJSON(ctx, apiClient(o.HTTPClient), "GET", o.url("/models/"+url.PathEscape(o.Model)), o.header(), nil, &m, openAIRetryable); err != nil {
		return o.mapError(ctx, err)
	}
	return nil
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// call sends one structured request to /responses and returns the text.
func (o *OpenAI) call(ctx context.Context, system string, msgs []openAIMessage, name string, schema map[string]any) (string, error) {
	if o.Model == "" {
		return "", needModel(ProviderOpenAI)
	}
	body := map[string]any{
		"model":        o.Model,
		"instructions": system,
		"input":        msgs,
		"store":        false,
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": name, "strict": true, "schema": strictSchema(schema),
		}},
	}
	var resp struct {
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := doJSON(ctx, apiClient(o.HTTPClient), "POST", o.url("/responses"), o.header(), body, &resp, openAIRetryable); err != nil {
		return "", o.mapError(ctx, err)
	}
	var text strings.Builder
	for _, item := range resp.Output {
		if item.Type != "message" {
			continue
		}
		for _, c := range item.Content {
			switch c.Type {
			case "refusal":
				return "", &Error{Code: CodeRefused, Message: "The model declined this request. Rephrase it and try again."}
			case "output_text":
				text.WriteString(c.Text)
			}
		}
	}
	if resp.Status == "incomplete" {
		if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason == "content_filter" {
			return "", &Error{Code: CodeRefused, Category: "content_filter", Message: "The model declined this request (category: content_filter). Rephrase it and try again."}
		}
		return "", &Error{Code: CodeCutOff, Message: "The answer was cut off before it was complete."}
	}
	if resp.Status != "" && resp.Status != "completed" {
		return "", errUnavailable()
	}
	return text.String(), nil
}

// Generate implements Provider.
func (o *OpenAI) Generate(ctx context.Context, req Request) (*WizardPack, error) {
	msgs := []openAIMessage{{Role: "user", Content: UserPrompt(req)}}
	if req.Previous != "" && len(req.Fix) > 0 {
		msgs = append(msgs, openAIMessage{Role: "assistant", Content: req.Previous}, openAIMessage{Role: "user", Content: FixPrompt(req.Fix)})
	}
	text, err := o.call(ctx, systemPrompt, msgs, "wizard_pack", PackSchema())
	if err != nil {
		return nil, err
	}
	return DecodePack(text)
}
