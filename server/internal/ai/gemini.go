package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// geminiBase is the public API; tests point BaseURL at a stand-in.
const geminiBase = "https://generativelanguage.googleapis.com/v1beta"

// Gemini calls the Gemini API's generateContent with net/http and the
// standard library (D111). Structured output uses responseSchema, an
// OpenAPI subset, translated from the wizard's JSON schema. The key goes
// in the x-goog-api-key header, never in the URL.
type Gemini struct {
	Key   Secret
	Model string
	// BaseURL and HTTPClient are for tests; empty means the public API.
	BaseURL    string
	HTTPClient *http.Client
}

func (g *Gemini) url(path string) string {
	base := g.BaseURL
	if base == "" {
		base = geminiBase
	}
	return strings.TrimRight(base, "/") + path
}

func (g *Gemini) header() map[string]string {
	return map[string]string{"x-goog-api-key": g.Key.reveal()}
}

// geminiRetryable: only overload is tried again. Gemini answers 429 for a
// used up quota and for a rate limit alike, and retrying a quota wastes
// the admin's time.
func geminiRetryable(f *httpFailure) bool { return f.status >= 500 }

type geminiErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Reason string `json:"reason"`
		} `json:"details"`
	} `json:"error"`
}

// mapError turns a failure into the provider's sentence. The body is
// read for its status and reason only and is never shown.
func (g *Gemini) mapError(ctx context.Context, err error) error {
	f, ok := err.(*httpFailure)
	if !ok {
		if e, ok := err.(*Error); ok {
			return e
		}
		return transportError(ctx)
	}
	var b geminiErrorBody
	json.Unmarshal(f.body, &b)
	badKey := strings.Contains(b.Error.Message, "API key not valid") || strings.Contains(b.Error.Message, "API key expired")
	for _, d := range b.Error.Details {
		if d.Reason == "API_KEY_INVALID" {
			badKey = true
		}
	}
	switch {
	case badKey || f.status == http.StatusUnauthorized:
		return &Error{Code: CodeKeyRefused, Message: "Google refused the key. Check it in Google AI Studio and paste it again."}
	case f.status == http.StatusTooManyRequests || b.Error.Status == "RESOURCE_EXHAUSTED":
		return &Error{Code: CodeNoQuota, Message: "Google says this key has used up its Gemini quota. Check the plan in Google AI Studio, or try again later."}
	case f.status == http.StatusNotFound:
		return &Error{Code: CodeModelNotFound, Message: fmt.Sprintf("Gemini does not offer the model %s to this key. Choose another model.", g.Model)}
	case f.status == http.StatusForbidden:
		return &Error{Code: CodeNoAccess, Message: fmt.Sprintf("Google says this key cannot use %s. Choose another model or key.", g.Model)}
	case f.status >= 500:
		return errUnavailable()
	}
	return &Error{Code: CodeBadRequest, Message: "Gemini could not accept the request. Try a shorter text or another model."}
}

type geminiModel struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

// geminiExcluded are id parts of models that answer generateContent but
// not with a JSON text answer.
var geminiExcluded = []string{"embedding", "image", "tts", "audio", "live", "gemma", "aqa"}

// geminiUsable is the Gemini filter rule (D111).
func geminiUsable(m geminiModel) bool {
	if !slices.Contains(m.SupportedGenerationMethods, "generateContent") {
		return false
	}
	id := strings.TrimPrefix(m.Name, "models/")
	for _, x := range geminiExcluded {
		if strings.Contains(id, x) {
			return false
		}
	}
	return true
}

// ListModels implements Provider: every page of GET /models, filtered,
// the "models/" prefix stripped, in the API's order.
func (g *Gemini) ListModels(ctx context.Context) ([]Model, error) {
	var out []Model
	token := ""
	for pages := 0; pages < maxListed/1000+1; pages++ {
		q := url.Values{"pageSize": {"1000"}}
		if token != "" {
			q.Set("pageToken", token)
		}
		var page struct {
			Models        []geminiModel `json:"models"`
			NextPageToken string        `json:"nextPageToken"`
		}
		if err := doJSON(ctx, apiClient(g.HTTPClient), "GET", g.url("/models?"+q.Encode()), g.header(), nil, &page, geminiRetryable); err != nil {
			return nil, g.mapError(ctx, err)
		}
		for _, m := range page.Models {
			if geminiUsable(m) {
				name := m.DisplayName
				id := strings.TrimPrefix(m.Name, "models/")
				if name == "" {
					name = id
				}
				out = append(out, Model{ID: id, Name: name})
			}
		}
		if page.NextPageToken == "" || page.NextPageToken == token {
			break
		}
		token = page.NextPageToken
	}
	return out, nil
}

// Check implements Provider: GET /models/{id}, or a listing without a
// model. Neither costs tokens.
func (g *Gemini) Check(ctx context.Context) error {
	if g.Model == "" {
		_, err := g.ListModels(ctx)
		return err
	}
	var m geminiModel
	if err := doJSON(ctx, apiClient(g.HTTPClient), "GET", g.url("/models/"+url.PathEscape(g.Model)), g.header(), nil, &m, geminiRetryable); err != nil {
		return g.mapError(ctx, err)
	}
	if !slices.Contains(m.SupportedGenerationMethods, "generateContent") {
		return &Error{Code: CodeUnsupportedModel, Message: fmt.Sprintf("%s cannot write problems here: it does not generate text. Choose another model.", g.Model)}
	}
	return nil
}

type geminiPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

// geminiStops are finishReason values that mean the answer was withheld.
var geminiStops = []string{"SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "LANGUAGE"}

// call sends one structured generateContent request and returns the text.
func (g *Gemini) call(ctx context.Context, system string, msgs []geminiContent, schema map[string]any) (string, error) {
	if g.Model == "" {
		return "", needModel(ProviderGemini)
	}
	body := map[string]any{
		"systemInstruction": geminiContent{Parts: []geminiPart{{Text: system}}},
		"contents":          msgs,
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseSchema":   geminiSchema(schema),
		},
	}
	var resp struct {
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Candidates []struct {
			Content      geminiContent `json:"content"`
			FinishReason string        `json:"finishReason"`
		} `json:"candidates"`
	}
	path := "/models/" + url.PathEscape(g.Model) + ":generateContent"
	if err := doJSON(ctx, apiClient(g.HTTPClient), "POST", g.url(path), g.header(), body, &resp, geminiRetryable); err != nil {
		return "", g.mapError(ctx, err)
	}
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return "", geminiRefusal(resp.PromptFeedback.BlockReason)
	}
	if len(resp.Candidates) == 0 {
		return "", errUnavailable()
	}
	c := resp.Candidates[0]
	switch {
	case c.FinishReason == "MAX_TOKENS":
		return "", &Error{Code: CodeCutOff, Message: "The answer was cut off before it was complete."}
	case slices.Contains(geminiStops, c.FinishReason):
		return "", geminiRefusal(c.FinishReason)
	}
	var text strings.Builder
	for _, p := range c.Content.Parts {
		if !p.Thought {
			text.WriteString(p.Text)
		}
	}
	return text.String(), nil
}

func geminiRefusal(reason string) *Error {
	cat := strings.ToLower(reason)
	return &Error{Code: CodeRefused, Category: cat,
		Message: fmt.Sprintf("The model declined this request (category: %s). Rephrase it and try again.", cat)}
}

// geminiSchema translates a JSON schema into Gemini's responseSchema, an
// OpenAPI subset: types are upper case, additionalProperties does not
// exist (every property is listed and required instead), and
// propertyOrdering fixes the order the model writes the fields in.
func geminiSchema(s map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"description", "enum", "format", "nullable"} {
		if v, ok := s[k]; ok {
			out[k] = v
		}
	}
	if t, ok := s["type"].(string); ok {
		out["type"] = strings.ToUpper(t)
	}
	if props, ok := s["properties"].(map[string]any); ok {
		cp := make(map[string]any, len(props))
		order := make([]string, 0, len(props))
		for k, v := range props {
			if sub, ok := v.(map[string]any); ok {
				v = geminiSchema(sub)
			}
			cp[k] = v
			order = append(order, k)
		}
		sort.Strings(order)
		out["properties"], out["required"], out["propertyOrdering"] = cp, order, order
	}
	if items, ok := s["items"].(map[string]any); ok {
		out["items"] = geminiSchema(items)
	}
	return out
}

// Generate implements Provider.
func (g *Gemini) Generate(ctx context.Context, req Request) (*WizardPack, error) {
	msgs := []geminiContent{{Role: "user", Parts: []geminiPart{{Text: UserPrompt(req)}}}}
	if req.Previous != "" && len(req.Fix) > 0 {
		msgs = append(msgs,
			geminiContent{Role: "model", Parts: []geminiPart{{Text: req.Previous}}},
			geminiContent{Role: "user", Parts: []geminiPart{{Text: FixPrompt(req.Fix)}}})
	}
	text, err := g.call(ctx, systemPrompt, msgs, PackSchema())
	if err != nil {
		return nil, err
	}
	return DecodePack(text)
}

// Solve implements Provider.
func (g *Gemini) Solve(ctx context.Context, statement string) (string, error) {
	text, err := g.call(ctx, solveSystemPrompt, []geminiContent{{Role: "user", Parts: []geminiPart{{Text: SolvePrompt(statement)}}}}, SolveSchema())
	if err != nil {
		return "", err
	}
	return decodeSolution(text)
}
