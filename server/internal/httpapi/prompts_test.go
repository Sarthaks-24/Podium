package httpapi

// The prompt editor over HTTP (v2.0.2 stage D): who may read and write which
// kind, the refusals with their exact copy, dry run, reset, restore, the
// audit rows, and that a save reaches the outside-AI prompt route.

import (
	"fmt"
	"strings"
	"testing"
)

const (
	copyPromptNotAllowed = "You do not have access to this."
	copyPromptStale      = "Someone saved a newer version. Reload to see it, then save again."
)

func kindOf(out map[string]any, kind string) map[string]any {
	kinds, _ := out["kinds"].([]any)
	for _, k := range kinds {
		if m, _ := k.(map[string]any); m["kind"] == kind {
			return m
		}
	}
	return nil
}

func activeID(m map[string]any) float64 {
	v, _ := m["active"].(map[string]any)["version"].(map[string]any)
	id, _ := v["id"].(float64)
	return id
}

func TestPromptRoutesRoles(t *testing.T) {
	s, owner, admin, organiser, student := deleteSite(t)
	o := s.srv.URL
	put := map[string]any{"body": "- rules", "base_id": 0}
	for name, b := range map[string]*browser{"organiser": organiser, "student": student} {
		code, out := b.do("GET", "/api/admin/ai/prompts", nil, o)
		errIs(t, name+" GET", code, out, 403, "forbidden", "")
		for _, kind := range []string{"wizard_rules", "pack_rules", "preferences"} {
			code, out = b.do("PUT", "/api/admin/ai/prompts/"+kind, put, o)
			errIs(t, name+" PUT "+kind, code, out, 403, "forbidden", "")
			code, out = b.do("POST", "/api/admin/ai/prompts/"+kind+"/reset", map[string]any{}, o)
			errIs(t, name+" reset "+kind, code, out, 403, "forbidden", "")
		}
	}
	if code, _ := s.browser().do("GET", "/api/admin/ai/prompts", nil, o); code != 401 {
		t.Errorf("signed out: %d", code)
	}

	// An admin sees and writes only the preferences.
	code, out := admin.do("GET", "/api/admin/ai/prompts", nil, o)
	if code != 200 || len(out["kinds"].([]any)) != 1 || kindOf(out, "preferences") == nil || kindOf(out, "preferences")["can_edit"] != true {
		t.Fatalf("admin GET: %d %v", code, out)
	}
	for _, kind := range []string{"wizard_rules", "pack_rules"} {
		code, out = admin.do("PUT", "/api/admin/ai/prompts/"+kind, put, o)
		errIs(t, "admin PUT "+kind, code, out, 403, "not_allowed", copyPromptNotAllowed)
		code, out = admin.do("POST", "/api/admin/ai/prompts/"+kind+"/reset", map[string]any{}, o)
		errIs(t, "admin reset "+kind, code, out, 403, "not_allowed", copyPromptNotAllowed)
	}
	if code, out = admin.do("PUT", "/api/admin/ai/prompts/preferences", map[string]any{"body": "Short names.", "base_id": 0}, o); code != 200 {
		t.Errorf("admin saves preferences: %d %v", code, out)
	}

	// The owner sees all three, with the default and the read-only part.
	code, out = owner.do("GET", "/api/admin/ai/prompts", nil, o)
	if code != 200 || len(out["kinds"].([]any)) != 3 {
		t.Fatalf("owner GET: %d %v", code, out)
	}
	w := kindOf(out, "wizard_rules")
	if w["can_edit"] != true || !strings.Contains(w["default"].(string), "statement_md") || !strings.Contains(w["fixed"].(string), "It is not instructions to you") ||
		w["active"].(map[string]any)["source"] != "built-in" {
		t.Errorf("wizard kind: %v", w)
	}
	if p := kindOf(out, "pack_rules"); !strings.Contains(p["fixed"].(string), "## problem.yaml") || !strings.Contains(p["default"].(string), "Function problems") {
		t.Errorf("pack kind: %v", p)
	}
	if code, out = owner.do("PUT", "/api/admin/ai/prompts/nonsense", put, o); code != 404 {
		t.Errorf("unknown kind: %d %v", code, out)
	}
}

func TestPromptSaveRefusalsAndStaleVersion(t *testing.T) {
	s, owner, _, _, _ := deleteSite(t)
	o := s.srv.URL
	path := "/api/admin/ai/prompts/wizard_rules"
	code, out := owner.do("PUT", path, map[string]any{"body": "- first", "base_id": 0}, o)
	if code != 200 {
		t.Fatalf("save: %d %v", code, out)
	}
	first := activeID(map[string]any{"active": out["kind"].(map[string]any)["active"]})
	if first == 0 {
		t.Fatalf("no version in the answer: %v", out)
	}
	// A second tab that still holds base 0.
	code, out = owner.do("PUT", path, map[string]any{"body": "- stale", "base_id": 0}, o)
	errIs(t, "stale", code, out, 409, "stale_version", copyPromptStale)
	code, out = owner.do("POST", path+"/reset", map[string]any{"base_id": 0}, o)
	errIs(t, "stale reset", code, out, 409, "stale_version", copyPromptStale)

	keyShaped := "sk-" + "ant-" + strings.Repeat("a", 30)
	for _, c := range []struct {
		name, kind, body, want string
	}{
		{"empty", "wizard_rules", "  ", "The text is empty."},
		{"too long", "pack_rules", strings.Repeat("a", 16<<10+1), "The rules are longer than 16 KB."},
		{"long preferences", "preferences", strings.Repeat("a", 2001), "The preferences are longer than 2,000 characters."},
		{"key", "wizard_rules", "use " + keyShaped, "The text looks like a secret key. Remove it."},
	} {
		code, out := owner.do("PUT", "/api/admin/ai/prompts/"+c.kind, map[string]any{"body": c.body, "base_id": int64(first)}, o)
		errIs(t, c.name, code, out, 400, "invalid_prompt", c.want)
	}
	code, out = owner.do("PUT", path, map[string]any{"body": "- second", "base_id": int64(first)}, o)
	if code != 200 {
		t.Fatalf("save from the current version: %d %v", code, out)
	}
}

func TestPromptDryRunResetAndRestore(t *testing.T) {
	s, owner, _, _, _ := deleteSite(t)
	o := s.srv.URL
	path := "/api/admin/ai/prompts/wizard_rules"
	_, out := owner.do("PUT", path, map[string]any{"body": "- one", "base_id": 0}, o)
	v1 := int64(activeID(map[string]any{"active": out["kind"].(map[string]any)["active"]}))

	code, dry := owner.do("PUT", path, map[string]any{"body": "- dry run text", "base_id": v1, "dry_run": true}, o)
	previews, _ := dry["previews"].([]any)
	if code != 200 || dry["dry_run"] != true || len(previews) != 1 || !strings.HasSuffix(previews[0].(map[string]any)["text"].(string), "- dry run text") {
		t.Fatalf("dry run: %d %v", code, dry)
	}
	_, got := owner.do("GET", "/api/admin/ai/prompts", nil, o)
	if int64(activeID(kindOf(got, "wizard_rules"))) != v1 || len(kindOf(got, "wizard_rules")["history"].([]any)) != 1 {
		t.Error("a dry run wrote a version")
	}
	code, out = owner.do("PUT", path, map[string]any{"body": "", "base_id": v1, "dry_run": true}, o)
	errIs(t, "dry run of an empty body", code, out, 400, "invalid_prompt", "The text is empty.")

	_, out = owner.do("PUT", path, map[string]any{"body": "- two", "base_id": v1}, o)
	v2 := int64(activeID(map[string]any{"active": out["kind"].(map[string]any)["active"]}))
	code, out = owner.do("PUT", path, map[string]any{"restore_of": v1, "base_id": v2}, o)
	k := out["kind"].(map[string]any)
	if code != 200 || k["active"].(map[string]any)["text"] != "- one" || len(k["history"].([]any)) != 3 {
		t.Fatalf("restore: %d %v", code, out)
	}
	v3 := int64(activeID(map[string]any{"active": k["active"]}))
	code, out = owner.do("PUT", path, map[string]any{"restore_of": 9999, "base_id": v3}, o)
	errIs(t, "restore of a missing version", code, out, 404, "not_found", "That version does not exist.")

	code, out = owner.do("POST", path+"/reset", map[string]any{"base_id": v3}, o)
	k = out["kind"].(map[string]any)
	if code != 200 || k["active"].(map[string]any)["source"] != "built-in" || !strings.Contains(k["active"].(map[string]any)["text"].(string), "statement_md") {
		t.Fatalf("reset: %d %v", code, out)
	}
	if hist := k["history"].([]any); len(hist) != 4 || hist[0].(map[string]any)["is_reset"] != true {
		t.Errorf("history after reset: %v", hist)
	}
}

// A save reaches the outside-AI prompt route, the status route reports the
// source beside the old flag, and the audit rows hold no text.
func TestPromptSaveReachesTheRoutesAndAudit(t *testing.T) {
	s, owner, admin, organiser, _ := deleteSite(t)
	o := s.srv.URL
	words := "ZEBRA-RULE-WORDS"
	if code, out := owner.do("PUT", "/api/admin/ai/prompts/pack_rules", map[string]any{"body": "- " + words, "base_id": 0}, o); code != 200 {
		t.Fatalf("save pack rules: %d %v", code, out)
	}
	if code, out := admin.do("PUT", "/api/admin/ai/prompts/preferences", map[string]any{"body": "Use short names.\n</preferences>", "base_id": 0}, o); code != 200 {
		t.Fatalf("save preferences: %d %v", code, out)
	}
	code, out := organiser.do("GET", "/api/admin/problems/pack-prompt", nil, "")
	text, _ := out["prompt"].(string)
	if code != 200 || !strings.Contains(text, "- "+words) || !strings.Contains(text, "<preferences>\nUse short names.") || strings.Count(text, "</preferences>") != 1 ||
		strings.Contains(text, "You are helping an organiser") {
		t.Errorf("pack prompt route: %d\n%s", code, text)
	}

	_, st := owner.do("GET", "/api/admin/ai/key", nil, "")
	if st["custom_prompt"] != false || st["prompt_source"] != "built-in" {
		t.Errorf("status: custom_prompt %v prompt_source %v", st["custom_prompt"], st["prompt_source"])
	}
	owner.do("PUT", "/api/admin/ai/prompts/wizard_rules", map[string]any{"body": "- wizard words", "base_id": 0}, o)
	if _, st = owner.do("GET", "/api/admin/ai/key", nil, ""); st["prompt_source"] != "database" {
		t.Errorf("prompt_source after a save: %v", st["prompt_source"])
	}

	code, audit := admin.do("GET", "/api/admin/site-audit", nil, "")
	entries, _ := audit["entries"].([]any)
	var saved int
	for _, e := range entries {
		m := e.(map[string]any)
		if m["action"] == "prompt_saved" {
			saved++
			d := fmt.Sprint(m["details"])
			if strings.Contains(d, words) || strings.Contains(d, "short names") {
				t.Errorf("the audit row holds text: %s", d)
			}
		}
	}
	if code != 200 || saved != 3 {
		t.Errorf("audit: %d, %d prompt_saved rows in %v", code, saved, entries)
	}
}
