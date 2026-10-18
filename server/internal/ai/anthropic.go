package ai

import (
	"context"
	"encoding/json"
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

// fallbackModels get server-side refusal fallbacks ("default" routing by
// refusal category). Haiku 5.5 has none.
var fallbackModels = map[string]bool{"claude-opus-5-5": true, "claude-sonnet-5-5": true}

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
		hc = &http.Client{}
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
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.Model),
		MaxTokens: maxTokens,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages:  msgs,
		// Thinking is left unset: it is always on for Opus 5.5 and
		// adaptive by default on the others. Effort is set explicitly
		// because Opus 5.5 defaults to medium.
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
		cat := string(msg.StopDetails.Category)
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
	var out struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return "", &Error{Code: CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: []string{"The answer is not valid JSON."}}
	}
	return out.Source, nil
}

// Check implements Provider with a model lookup: it costs no tokens and
// proves both the key and the model name.
func (a *Anthropic) Check(ctx context.Context) error {
	client := a.client()
	if _, err := client.Models.Get(ctx, a.Model, anthropic.ModelGetParams{}); err != nil {
		return a.mapError(ctx, err)
	}
	return nil
}

// mapError turns an SDK error into a fixed sentence. The SDK's error holds
// the request, whose headers carry the key, and the raw response body; it
// is never wrapped, formatted or logged.
func (a *Anthropic) mapError(ctx context.Context, err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch code := apierr.StatusCode; {
		case code == http.StatusUnauthorized:
			return &Error{Code: CodeKeyRefused, Message: "Anthropic refused the key. Check it and paste it again."}
		case code == http.StatusForbidden:
			return &Error{Code: CodeNoAccess, Message: fmt.Sprintf("Anthropic says this key cannot use %s. Choose another model or key.", a.Model)}
		case code == http.StatusNotFound:
			return &Error{Code: CodeModelNotFound, Message: fmt.Sprintf("Anthropic does not offer the model %s to this key. Choose another model.", a.Model)}
		case code == http.StatusTooManyRequests:
			return &Error{Code: CodeRateLimited, Message: "Rate limited by the provider; try again in a minute."}
		case code >= 500:
			return &Error{Code: CodeUnavailable, Message: "The provider is unavailable. Try again in a few minutes."}
		case code >= 400:
			return &Error{Code: CodeBadRequest, Message: "Anthropic could not accept the request. Try a shorter text or another model."}
		}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &Error{Code: CodeTimeout, Message: "The provider took too long to answer. Try again."}
	case ctx.Err() != nil:
		return &Error{Code: CodeCanceled, Message: "The request was cancelled."}
	}
	return &Error{Code: CodeUnavailable, Message: "The provider is unavailable. Try again in a few minutes."}
}
