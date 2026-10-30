package fakeai

import (
	"context"
	"testing"

	"github.com/Sarthaks-24/podium/internal/ai"
)

func TestFakeProvider(t *testing.T) {
	f := &Fake{}
	ctx := context.Background()
	p, err := ai.Generate(ctx, f, ai.Request{Brief: &ai.Brief{Topic: "Maximum subarray sum"}})
	if err != nil || p.Slug != "best-stretch" {
		t.Fatalf("%v %v", p, err)
	}
	p, err = ai.Generate(ctx, f, ai.Request{Pasted: "Add two numbers a and b."})
	if err != nil || p.Slug != "pair-sums" {
		t.Fatalf("%v %v", p, err)
	}
	if f.Calls() != 2 {
		t.Errorf("calls %d", f.Calls())
	}
}

func TestImportRegistersTheProvider(t *testing.T) {
	if p, err := ai.NewProvider(ai.Config{Provider: ai.ProviderFake}, true); err != nil {
		t.Fatal(err)
	} else if _, ok := p.(*Fake); !ok {
		t.Errorf("testing mode got %T", p)
	}
	if _, err := ai.NewProvider(ai.Config{Provider: ai.ProviderFake}, false); err == nil {
		t.Error("fake provider outside testing mode")
	}
}
