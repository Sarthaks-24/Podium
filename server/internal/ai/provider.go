// Package ai is the AI wizard's model side (M5, cache/v1/m5-plan.md; three
// providers since D111): the in-memory key store, the provider clients
// (Anthropic, OpenAI, Google Gemini), the WizardPack schema and prompts,
// and the checks that turn a model's answer into a problem draft.
// A provider key is pasted by an admin, held in memory for two hours and
// never written to the database, logs, errors, fixtures or job payloads
// (CLAUDE.md 3a, decision D50). The model's code is never run on the
// server: it goes to the judge, in isolate, like any setter's code.
package ai

import (
	"context"
	"regexp"
	"strings"
)

// Provider is one model API.
type Provider interface {
	// Generate asks for a problem. A refused, cut off or unusable answer
	// comes back as *Error.
	Generate(ctx context.Context, req Request) (*WizardPack, error)
	// Check makes the cheapest call that proves the key works: a lookup
	// of the model, or a model listing when no model is chosen yet.
	Check(ctx context.Context) error
	// ListModels returns the provider's models that can write a problem,
	// after the provider's filter rule (D111). No model id is written in
	// the code; the rules are.
	ListModels(ctx context.Context) ([]Model, error)
}

// Model is one entry of a provider's filtered model list.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	CodeKeyRefused       = "key_refused"
	CodeNoQuota          = "no_quota"
	CodeNoAccess         = "no_access"
	CodeModelNotFound    = "model_not_found"
	CodeUnsupportedModel = "unsupported_model"
	CodeRateLimited      = "rate_limited"
	CodeUnavailable      = "unavailable"
	CodeBadRequest       = "bad_request"
	CodeRefused          = "refused"
	CodeCutOff           = "cut_off"
	CodeInvalidAnswer    = "invalid_answer"
	CodeTimeout          = "timeout"
	CodeCanceled         = "canceled"
	CodeInput            = "input"
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

// safeCategory keeps a provider's refusal category to what a fixed
// sentence may carry: at most 40 lower-case letters, digits and
// underscores, or "other".
func safeCategory(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > 40 || strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
		return "other"
	}
	return s
}

// Provider names.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGemini    = "gemini"
	// ProviderFake exists only in testing mode (decision D50).
	ProviderFake = "fake"
)

// Providers lists the real providers in the order the key form shows them.
var Providers = []string{ProviderAnthropic, ProviderOpenAI, ProviderGemini}

// ProviderName is the name the key form and the sentences use.
func ProviderName(p string) string {
	switch p {
	case ProviderAnthropic:
		return "Anthropic"
	case ProviderOpenAI:
		return "OpenAI"
	case ProviderGemini:
		return "Google Gemini"
	case ProviderFake:
		return "Test provider (canned problems)"
	}
	return "The provider"
}

// Known reports whether p is a provider this server offers; the fake one
// only in testing mode.
func Known(p string, testing bool) bool {
	switch p {
	case ProviderAnthropic, ProviderOpenAI, ProviderGemini:
		return true
	case ProviderFake:
		return testing
	}
	return false
}

// FakeModel is the one model of the testing-mode provider.
const FakeModel = "canned"

// MaxModelID bounds a typed model id.
const MaxModelID = 128

var modelIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// CleanModelID trims a typed model id and drops Gemini's "models/" prefix.
// It reports false for an id with characters no provider uses, which
// keeps a typed id from changing the path of the provider request.
func CleanModelID(id string) (string, bool) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
	if id == "" {
		return "", true
	}
	if len(id) > MaxModelID || !modelIDRe.MatchString(id) || strings.Contains(id, "..") {
		return "", false
	}
	return id, true
}

// Config chooses a provider for one admin's key.
type Config struct {
	Provider string
	// Model may be empty: the client can then only Check (by listing) and
	// ListModels.
	Model string
	Key   Secret
	// BaseURL points the client at a stand-in API. Tests only: it is
	// never taken from a request, so an admin cannot aim the key at
	// another host.
	BaseURL string
}

// NewProvider returns the client for c. The fake provider exists only when
// testing is set (PODIUM_MODE=testing).
func NewProvider(c Config, testing bool) (Provider, error) {
	if c.Provider == ProviderFake {
		if !testing {
			return nil, &Error{Code: CodeInput, Message: "The test provider exists only in testing mode."}
		}
		return &Fake{}, nil
	}
	if !Known(c.Provider, testing) {
		return nil, &Error{Code: CodeInput, Message: "Choose Anthropic, OpenAI or Google Gemini as the provider."}
	}
	model, ok := CleanModelID(c.Model)
	if !ok {
		return nil, &Error{Code: CodeInput, Message: "A model id has only letters, digits and . _ - : and at most 128 characters."}
	}
	if c.Key.IsZero() {
		return nil, &Error{Code: CodeInput, Message: "Paste the provider key."}
	}
	switch c.Provider {
	case ProviderOpenAI:
		return &OpenAI{Key: c.Key, Model: model, BaseURL: c.BaseURL}, nil
	case ProviderGemini:
		return &Gemini{Key: c.Key, Model: model, BaseURL: c.BaseURL}, nil
	}
	return &Anthropic{Key: c.Key, Model: model, BaseURL: c.BaseURL, MaxRetries: 2}, nil
}

// needModel is the error of a Generate on a client built without
// a model; the routes refuse earlier with 409 no_model.
func needModel(provider string) error {
	return &Error{Code: CodeInput, Message: "Choose a model for " + ProviderName(provider) + " in AI assistant first."}
}
