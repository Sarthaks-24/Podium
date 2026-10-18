// Package ai is the AI wizard's model side (M5, cache/v1/m5-plan.md): the
// in-memory key store, the provider clients, the WizardPack schema and
// prompts, and the checks that turn a model's answer into a problem draft.
// The provider key is pasted by an admin, held in memory for two hours
// and never written to the database, logs, errors, fixtures or job
// payloads (CLAUDE.md 3a, decision D50). The model's code is never run on
// the server: it goes to the judge, in isolate, like any setter's code.
package ai

import (
	"context"
	"fmt"
	"slices"
)

// Provider is one model API.
type Provider interface {
	// Generate asks for a problem. A refused, cut off or unusable answer
	// comes back as *Error.
	Generate(ctx context.Context, req Request) (*WizardPack, error)
	// Solve asks for a C++17 solution to a statement ("Can a model solve
	// this?") and returns the source.
	Solve(ctx context.Context, statement string) (string, error)
	// Check makes the cheapest call that proves the key and model work.
	Check(ctx context.Context) error
}

// Brief describes a problem to write from scratch.
type Brief struct {
	Topic        string `json:"topic"`
	Difficulty   string `json:"difficulty"`
	Constraints  string `json:"constraints"`
	SubtaskIdeas string `json:"subtask_ideas"`
}

// Request is one generation. Exactly one of Brief and Pasted is set.
type Request struct {
	Brief  *Brief
	Pasted string
	// The one retry: the previous answer and what was wrong with it, or
	// Shorter after an answer was cut off.
	Previous string
	Fix      []string
	Shorter  bool
}

// Error codes a caller can branch on.
const (
	CodeKeyRefused    = "key_refused"
	CodeNoAccess      = "no_access"
	CodeModelNotFound = "model_not_found"
	CodeRateLimited   = "rate_limited"
	CodeUnavailable   = "unavailable"
	CodeBadRequest    = "bad_request"
	CodeRefused       = "refused"
	CodeCutOff        = "cut_off"
	CodeInvalidAnswer = "invalid_answer"
	CodeTimeout       = "timeout"
	CodeCanceled      = "canceled"
	CodeInput         = "input"
)

// Error is what the admin sees. Message is a fixed sentence: it never
// carries the key or the provider's raw response body. Reasons lists what
// was wrong with an unusable answer.
type Error struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Category string   `json:"category,omitempty"`
	Reasons  []string `json:"reasons,omitempty"`
	// answer is the unusable answer, kept for the one retry.
	answer string
}

func (e *Error) Error() string { return e.Message }

func providerName(p string) string {
	switch p {
	case ProviderAnthropic:
		return "Anthropic"
	case ProviderFake:
		return "The test provider"
	}
	return "The provider"
}

// Config chooses a provider for one admin's key.
type Config struct {
	Provider string
	Model    string
	Key      Secret
	// BaseURL points the Anthropic client at a stand-in API. Tests only:
	// it is never taken from a request, so an admin cannot aim the key
	// at another host.
	BaseURL string
}

// NewProvider returns the client for c. The fake provider exists only when
// testing is set (PODIUM_MODE=testing).
func NewProvider(c Config, testing bool) (Provider, error) {
	switch c.Provider {
	case ProviderAnthropic:
		model := c.Model
		if model == "" {
			model = DefaultModel
		}
		if !slices.Contains(AnthropicModels, model) {
			return nil, &Error{Code: CodeInput, Message: fmt.Sprintf("Choose one of the offered models: %v.", AnthropicModels)}
		}
		if c.Key.IsZero() {
			return nil, &Error{Code: CodeInput, Message: "Paste the provider key."}
		}
		return &Anthropic{Key: c.Key, Model: model, BaseURL: c.BaseURL, MaxRetries: 2}, nil
	case ProviderFake:
		if !testing {
			return nil, &Error{Code: CodeInput, Message: "The test provider exists only in testing mode."}
		}
		return &Fake{}, nil
	}
	return nil, &Error{Code: CodeInput, Message: "Choose Anthropic as the provider."}
}
