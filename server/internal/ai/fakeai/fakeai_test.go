package fakeai

import (
	"context"
	"reflect"
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

func TestFakeFunctionAnswers(t *testing.T) {
	f := &Fake{}
	ctx := context.Background()
	p, err := ai.GenerateFunction(ctx, f, ai.Request{Brief: &ai.Brief{Topic: "Sum of an array"}})
	if err != nil || p.Slug != "array-sum" || len(p.Languages) != 4 {
		t.Fatalf("%v %v", p, err)
	}
	for _, v := range []string{ai.VariantJavaWrong, ai.VariantStarterBroken, ai.VariantExtraOutput, ai.VariantIgnoresFunction, ai.VariantReferenceFails} {
		got, err := ai.GenerateFunction(ctx, f, ai.Request{Pasted: "Sum an array. variant:" + v})
		if err != nil || got.Slug != "array-sum" || reflect.DeepEqual(got, p) {
			t.Errorf("%s: %v %v", v, got, err)
		}
	}
	// An unknown variant name falls back to the plain pack.
	if got, err := ai.GenerateFunction(ctx, f, ai.Request{Pasted: "variant:nope"}); err != nil || !reflect.DeepEqual(got, p) {
		t.Errorf("unknown variant: %v %v", got, err)
	}
	if f.Calls() != 7 {
		t.Errorf("calls %d", f.Calls())
	}
}
