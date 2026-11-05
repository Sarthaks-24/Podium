package httpapi

// Stage F: the outside-AI pack prompt route and the wizard's custom-prompt
// flag.

import (
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

func TestPackPromptRoute(t *testing.T) {
	s := newSite(t)
	owner := s.browser()
	owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/")
	student := s.browser()
	student.signIn(authtest.Person{Email: "asha@college.edu", Verified: true}, "/")

	if code, _ := student.do("GET", "/api/admin/problems/pack-prompt", nil, ""); code != 403 {
		t.Errorf("student: %d", code)
	}
	code, out := owner.do("GET", "/api/admin/problems/pack-prompt", nil, "")
	if code != 200 {
		t.Fatalf("owner: %d %v", code, out)
	}
	text, _ := out["prompt"].(string)
	if !strings.Contains(text, "problem.yaml") || !strings.Contains(text, "=== PROBLEM DESCRIPTION ===") {
		t.Errorf("prompt: %q", text)
	}
}

func TestWizardReportsACustomPrompt(t *testing.T) {
	t.Cleanup(func() { ai.SetRules("") })
	s := newSite(t)
	owner := s.browser()
	owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/")
	get := func() map[string]any {
		code, out := owner.do("GET", "/api/admin/ai/key", nil, "")
		if code != 200 {
			t.Fatalf("key status: %d", code)
		}
		return out
	}
	if get()["custom_prompt"] != false {
		t.Error("flag set with no override")
	}
	ai.SetRules("- secret rules text")
	out := get()
	if out["custom_prompt"] != true {
		t.Error("flag not set with an override")
	}
	for _, v := range out {
		if s, ok := v.(string); ok && strings.Contains(s, "secret rules") {
			t.Error("the rules text leaked into the answer")
		}
	}
}
