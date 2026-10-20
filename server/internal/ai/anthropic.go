package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// maxTokens leaves room for thinking plus a whole pack; requests stream,
// so a large value does not risk an HTTP timeout.
const maxTokens = 64000

// fallbackModels accept server-side refusal fallbacks ("default" routing
// by refusal category). This is a table of a feature, not a choice of
// model: any other model, Haiku included, is sent without them, and the
// admin picks the model (D111).
var fallbackModels = map[string]bool{
	"claude-fable-5-1": true, "claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true,
}

// Anthropic calls the Claude API through the official Go SDK. The key is
// passed to each client explicitly; the SDK's environment and profile
// lookup is switched off, so no key is ever read from the environment.
type Anthropic struct {
	Key   Secret
	Model string
	// BaseURL and HTTPClient are for tests; empty means the public API.
	BaseURL    string
	HTTPClient *http.Client
	// MaxRetries is the SDK's retry count for 408, 409, 429 and 5xx;
	// negative means the SDK default (2).
	MaxRetries int
}

func (a *Anthropic) client() anthropic.Client {
	hc := a.HTTPClient
	if hc == nil {
		// No client timeout: the caller's context bounds each call (3
		// minutes for a generation), and a long stream must not be cut.
		// No redirects: Go would resend the X-Api-Key header to the new
		// host (it strips only Authorization and Cookie).
		hc = &http.Client{CheckRedirect: noRedirect}
	}
	opts := []option.RequestOption{
		option.WithoutEnvironmentDefaults(),
		option.WithHTTPClient(hc),
		option.WithAPIKey(a.Key.reveal()),
	}
	if a.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(a.BaseURL))
	}
	if a.MaxRetries >= 0 {
		opts = append(opts, option.WithMaxRetries(a.MaxRetries))
	}
	return anthropic.NewClient(opts...)
}

// call streams one structured request and returns the answer's text.
func (a *Anthropic) call(ctx context.Context, system string, msgs []anthropic.BetaMessageParam, schema map[string]any) (string, error) {
	if a.Model == "" {
		return "", needModel(ProviderAnthropic)
	}
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.Model),
		MaxTokens: maxTokens,
		// The system prompt is fixed text (the pack format rules), so it
		// carries the cache breakpoint; the schema in output_config is
		// cached by the API on its own. A prompt under the model's
		// minimum silently does not cache, which costs nothing.
		System:   []anthropic.BetaTextBlockParam{{Text: system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		Messages: msgs,
		// Adaptive thinking, sent explicitly: leaving it unset means no
		// thinking on some models the live list offers (Opus 4.8 and
		// 4.7). Every listed model supports it (anthropicUsable). The
		// thinking text is not read, so it is omitted from the stream.
		Thinking: anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{
			Display: anthropic.BetaThinkingConfigAdaptiveDisplayOmitted}},
		// Effort is set explicitly because Opus 5.5 and Haiku 5.5 default
		// to medium.
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortHigh,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
	}
	if fallbackModels[a.Model] {
		params.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	client := a.client()
	stream := client.Beta.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	msg := anthropic.BetaMessage{}
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return "", a.mapError(ctx, err)
		}
	}
	if err := stream.Err(); err != nil {
		return "", a.mapError(ctx, err)
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		cat := safeCategory(string(msg.StopDetails.Category))
		m := "The model declined this request."
		if cat != "" {
			m = fmt.Sprintf("The model declined this request (category: %s).", cat)
		}
		return "", &Error{Code: CodeRefused, Category: cat, Message: m + " Rephrase it and try again."}
	case anthropic.BetaStopReasonMaxTokens:
		return "", &Error{Code: CodeCutOff, Message: "The answer was cut off before it was complete."}
	}
	var text strings.Builder
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
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
	msgs := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(UserPrompt(req)))}
	if req.Previous != "" && len(req.Fix) > 0 {
		msgs = append(msgs,
			anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(req.Previous)}},
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(FixPrompt(req.Fix))))
	}
	text, err := a.call(ctx, systemPrompt, msgs, PackSchema())
	if err != nil {
		return nil, err
	}
	return DecodePack(text)
}

// Solve implements Provider.
func (a *Anthropic) Solve(ctx context.Context, statement string) (string, error) {
	msgs := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(SolvePrompt(statement)))}
	text, err := a.call(ctx, solveSystemPrompt, msgs, SolveSchema())
	if err != nil {
		return "", err
	}
	return decodeSolution(text)
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
	client := a.client()
	m, err := client.Models.Get(ctx, a.Model, anthropic.ModelGetParams{})
	if err != nil {
		return a.mapError(ctx, err)
	}
	if !anthropicUsable(m) {
		return unsupportedModel(a.Model)
	}
	return nil
}

// ListModels implements Provider: every page of /v1/models, newest first
// as the API lists them, kept when the model supports what the wizard
// sends.
func (a *Anthropic) ListModels(ctx context.Context) ([]Model, error) {
	client := a.client()
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1000)})
	var out []Model
	for n := 0; pager.Next() && n < maxListed; n++ {
		if m := pager.Current(); anthropicUsable(&m) {
			out = append(out, Model{ID: m.ID, Name: m.DisplayName})
		}
	}
	if err := pager.Err(); err != nil {
		return nil, a.mapError(ctx, err)
	}
	return out, nil
}

// anthropicUsable is the Anthropic filter rule (D111): the request shape
// in call needs structured outputs, adaptive thinking and effort high.
func anthropicUsable(m *anthropic.ModelInfo) bool {
	c := m.Capabilities
	return c.StructuredOutputs.Supported && c.Thinking.Types.Adaptive.Supported && c.Effort.High.Supported
}

func unsupportedModel(model string) error {
	return &Error{Code: CodeUnsupportedModel, Message: fmt.Sprintf("%s cannot write problems here: it lacks structured outputs, adaptive thinking or effort. Choose another model.", model)}
}

// mapError turns an SDK error into a fixed sentence. The SDK's error holds
// the request, whose headers carry the key, and the raw response body; it
// is never wrapped, formatted or logged.
func (a *Anthropic) mapError(ctx context.Context, err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch code := apierr.StatusCode; {
		case code == http.StatusPaymentRequired || apierr.Type() == "billing_error" ||
			(code == http.StatusBadRequest && strings.Contains(apierr.RawJSON(), "credit balance")):
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
	}
	return transportError(ctx)
}
