// Package fakeai is the testing-mode AI provider: no key, no network. It
// answers with one of two canned packs (ai.FakePairSums, ai.FakeMaxSubarray)
// whose programs really work on the judge, so the whole pipeline (draft,
// generate inputs, generate outputs, validate) can be checked end to end.
// A brief or pasted text mentioning a subarray or a stretch gets the
// maximum subarray sum pack; anything else gets the pair sums pack.
//
// Importing the package registers the provider with package ai. Tests
// import it, and so does podium-server built with the tag fakeai (make dev
// and make dev-test); the release binary does not, so neither the provider
// nor the canned packs ship to the VM.
package fakeai

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/Sarthaks-24/podium/internal/ai"
)

func init() {
	ai.RegisterFake(func() ai.Provider { return &Fake{} })
}

// Fake is the provider.
type Fake struct {
	calls atomic.Int64
}

// Calls reports how many requests the fake has answered.
func (f *Fake) Calls() int { return int(f.calls.Load()) }

func wantsSubarray(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "subarray") || strings.Contains(t, "stretch")
}

// Generate implements ai.Provider.
func (f *Fake) Generate(ctx context.Context, req ai.Request) (*ai.WizardPack, error) {
	f.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, &ai.Error{Code: ai.CodeCanceled, Message: "The request was cancelled."}
	}
	text := req.Pasted
	if req.Brief != nil {
		text = req.Brief.Topic + " " + req.Brief.Constraints + " " + req.Brief.SubtaskIdeas
	}
	if wantsSubarray(text) {
		return ai.FakeMaxSubarray(), nil
	}
	return ai.FakePairSums(), nil
}

// Check implements ai.Provider.
func (f *Fake) Check(context.Context) error { return nil }

// ListModels implements ai.Provider: the one canned model.
func (f *Fake) ListModels(context.Context) ([]ai.Model, error) {
	return []ai.Model{{ID: ai.FakeModel, Name: "Canned problems"}}, nil
}
