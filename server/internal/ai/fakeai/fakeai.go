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

// EditHook lets a test choose the fake's answer to an edit. Nil means the
// default: the instruction is appended to statement.md.
var EditHook func(req ai.EditRequest) (*ai.EditAnswer, error)

// Edit implements ai.Provider. The default answer appends the instruction to
// the statement, so a page driven in testing mode has something to show.
func (f *Fake) Edit(ctx context.Context, req ai.EditRequest) (*ai.EditAnswer, error) {
	f.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, &ai.Error{Code: ai.CodeCanceled, Message: "The request was cancelled."}
	}
	if EditHook != nil {
		return EditHook(req)
	}
	for _, file := range req.Files {
		if file.Path == "statement.md" {
			return &ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md",
				Content: strings.TrimRight(file.Content, "\n") + "\n\nNote from the test provider: " + req.Instruction + "\n"}},
				Summary: "Added a note to the statement."}, nil
		}
	}
	return &ai.EditAnswer{Unsupported: "There is no statement to edit."}, nil
}

// Check implements ai.Provider.
func (f *Fake) Check(context.Context) error { return nil }

// ListModels implements ai.Provider: the one canned model.
func (f *Fake) ListModels(context.Context) ([]ai.Model, error) {
	return []ai.Model{{ID: ai.FakeModel, Name: "Canned problems"}}, nil
}
