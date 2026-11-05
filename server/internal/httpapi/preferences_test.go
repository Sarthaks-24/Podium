package httpapi

import (
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
)

// Display preferences follow the account, so a second browser (another lab
// machine) gets the theme chosen on the first (decision D42).
func TestPreferencesFollowTheAccount(t *testing.T) {
	s := newSite(t)
	asha := authtest.Person{Email: "asha@college.edu", Verified: true, Name: "Asha"}

	first := s.browser()
	first.signIn(asha, "/")
	code, body := first.do("GET", "/api/me", nil, "")
	if code != 200 || body["theme"] != "" || body["verdicts"] != "standard" {
		t.Fatalf("defaults: %d theme=%v verdicts=%v", code, body["theme"], body["verdicts"])
	}

	code, body = first.do("PUT", "/api/me/preferences", map[string]string{"theme": "hc-dark", "verdicts": "signal"}, s.srv.URL)
	if code != 200 || body["theme"] != "hc-dark" || body["verdicts"] != "signal" {
		t.Fatalf("save: %d %v", code, body)
	}

	second := s.browser()
	second.signIn(asha, "/")
	_, body = second.do("GET", "/api/me", nil, "")
	if body["theme"] != "hc-dark" || body["verdicts"] != "signal" {
		t.Errorf("second machine: theme=%v verdicts=%v", body["theme"], body["verdicts"])
	}

	// Back to following the system.
	if code, _ := first.do("PUT", "/api/me/preferences", map[string]string{"theme": "", "verdicts": "standard"}, s.srv.URL); code != 200 {
		t.Errorf("reset: %d", code)
	}

	for _, bad := range []map[string]string{
		{"theme": "Dark", "verdicts": "standard"},
		{"theme": "../x", "verdicts": "standard"},
		{"theme": "a-theme-id-that-is-far-too-long-to-be-real", "verdicts": "standard"},
		{"theme": "dark", "verdicts": "neon"},
		{"theme": "dark"},
	} {
		if code, _ := first.do("PUT", "/api/me/preferences", bad, s.srv.URL); code != 400 {
			t.Errorf("%v: %d, want 400", bad, code)
		}
	}

	if code, _ := s.browser().do("PUT", "/api/me/preferences", map[string]string{"theme": "dark", "verdicts": "standard"}, s.srv.URL); code != 401 {
		t.Errorf("signed out: %d, want 401", code)
	}
}

// The editor colour scheme and a custom theme's three inputs follow the
// account too (v2.0.1 stage E). The server checks the shape only; it never
// derives a colour.
func TestEditorColoursAndCustomTheme(t *testing.T) {
	s := newSite(t)
	asha := authtest.Person{Email: "asha@college.edu", Verified: true, Name: "Asha"}
	b := s.browser()
	b.signIn(asha, "/")

	code, body := b.do("GET", "/api/me", nil, "")
	if code != 200 || body["editor_colours"] != "night" || body["theme_custom"] != nil {
		t.Fatalf("defaults: %d editor=%v custom=%v", code, body["editor_colours"], body["theme_custom"])
	}

	custom := map[string]any{"background": "#1A2B3C", "accent": "#E07A5F", "contrast": "high"}
	code, body = b.do("PUT", "/api/me/preferences", map[string]any{"theme": "custom", "verdicts": "standard", "editor_colours": "dusk", "theme_custom": custom}, s.srv.URL)
	if code != 200 {
		t.Fatalf("save: %d %v", code, body)
	}
	second := s.browser()
	second.signIn(asha, "/")
	_, body = second.do("GET", "/api/me", nil, "")
	saved, _ := body["theme_custom"].(map[string]any)
	if body["theme"] != "custom" || body["editor_colours"] != "dusk" || saved["background"] != "#1a2b3c" || saved["accent"] != "#e07a5f" || saved["contrast"] != "high" {
		t.Errorf("second machine: %v", body)
	}

	// A request that leaves the optional fields out keeps them (an older page).
	if code, _ := b.do("PUT", "/api/me/preferences", map[string]string{"theme": "dark", "verdicts": "signal"}, s.srv.URL); code != 200 {
		t.Fatalf("old client: %d", code)
	}
	_, body = second.do("GET", "/api/me", nil, "")
	if body["editor_colours"] != "dusk" || body["theme_custom"] == nil || body["theme"] != "dark" {
		t.Errorf("kept: %v", body)
	}

	// Choosing "custom" again needs no inputs in the request while one is saved.
	if code, _ := b.do("PUT", "/api/me/preferences", map[string]string{"theme": "custom", "verdicts": "standard"}, s.srv.URL); code != 200 {
		t.Errorf("custom from saved: %d", code)
	}

	// Every refusal names its field.
	bad := []struct {
		field string
		body  map[string]any
	}{
		{"editor_colours", map[string]any{"theme": "", "verdicts": "standard", "editor_colours": "neon"}},
		{"editor_colours", map[string]any{"theme": "", "verdicts": "standard", "editor_colours": ""}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": map[string]any{"background": "#1A2B3C", "accent": "red", "contrast": "high"}}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": map[string]any{"background": "#1A2B3", "accent": "#E07A5F", "contrast": "high"}}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": map[string]any{"background": "#1A2B3C", "accent": "#E07A5F", "contrast": "extreme"}}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": map[string]any{"background": "#1A2B3C", "accent": "#E07A5F", "contrast": "high", "second": "#000000"}}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": map[string]any{"background": "#1A2B3C", "accent": "#E07A5F"}}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": "#1A2B3C"}},
		{"theme_custom", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": map[string]any{"background": "url(x)", "accent": "#E07A5F", "contrast": "high"}}},
	}
	for _, c := range bad {
		code, body := b.do("PUT", "/api/me/preferences", c.body, s.srv.URL)
		errObj, _ := body["error"].(map[string]any)
		fields, _ := errObj["fields"].(map[string]any)
		if code != 400 || fields[c.field] == nil {
			t.Errorf("%v: %d %v, want a 400 naming %s", c.body, code, body, c.field)
		}
	}

	// Clearing the custom theme with null, then choosing "custom" is refused.
	if code, _ := b.do("PUT", "/api/me/preferences", map[string]any{"theme": "", "verdicts": "standard", "theme_custom": nil}, s.srv.URL); code != 200 {
		t.Fatalf("clear: %d", code)
	}
	_, body = b.do("GET", "/api/me", nil, "")
	if body["theme_custom"] != nil {
		t.Errorf("cleared: %v", body["theme_custom"])
	}
	code, body = b.do("PUT", "/api/me/preferences", map[string]string{"theme": "custom", "verdicts": "standard"}, s.srv.URL)
	errObj, _ := body["error"].(map[string]any)
	if fields, _ := errObj["fields"].(map[string]any); code != 400 || fields["theme_custom"] == nil {
		t.Errorf("custom with none saved: %d %v", code, body)
	}
}
