package ai

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/db"
)

var owner = auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner}

func newPrompts(t *testing.T) (*Prompts, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'owner@college.edu', 'owner', 1)`); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	p := &Prompts{DB: d, Keys: &Keys{}, Now: func() time.Time { clock = clock.Add(time.Minute); return clock }}
	t.Cleanup(func() { SetRules("") })
	return p, d
}

func ctxOwner() context.Context { return auth.WithActor(context.Background(), owner) }

func promptErr(err error) string {
	var pe *PromptError
	if errors.As(err, &pe) {
		return pe.Message
	}
	if err == nil {
		return "no error"
	}
	return "other: " + err.Error()
}

func TestPromptSaveWithAStaleBaseIsRefused(t *testing.T) {
	p, _ := newPrompts(t)
	v1, err := p.Save(ctxOwner(), KindWizardRules, "- first", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Save(ctxOwner(), KindWizardRules, "- second", 0); !errors.Is(err, ErrStale) {
		t.Errorf("a save from an editor that loaded no version after one exists: %v", err)
	}
	if _, err := p.Save(ctxOwner(), KindWizardRules, "- second", v1.ID); err != nil {
		t.Errorf("a save from the current version: %v", err)
	}
	if _, err := p.Save(ctxOwner(), KindWizardRules, "- third", v1.ID); !errors.Is(err, ErrStale) {
		t.Errorf("a save from the old version: %v", err)
	}
	// Kinds are independent: the pack rules have no versions yet.
	if _, err := p.Save(ctxOwner(), KindPackRules, "- pack", 0); err != nil {
		t.Errorf("pack rules: %v", err)
	}
}

func TestPromptBodyChecks(t *testing.T) {
	p, _ := newPrompts(t)
	p.Keys.Put("session-1", KeyEntry{Provider: ProviderAnthropic, Key: NewSecret("held-test-key-0123456789")})
	anthropicShape := "sk-" + "ant-" + strings.Repeat("a", 30)
	for _, c := range []struct {
		name, kind, body, want string
	}{
		{"empty", KindWizardRules, " \n", "The text is empty."},
		{"empty preferences", KindPreferences, "", "The text is empty."},
		{"not utf-8", KindPackRules, "- bad \xff byte", "The text is not valid UTF-8."},
		{"rules too long", KindWizardRules, strings.Repeat("a", 16<<10+1), "The rules are longer than 16 KB."},
		{"preferences too long", KindPreferences, strings.Repeat("é", 2001), "The preferences are longer than 2,000 characters."},
		{"key shape", KindPreferences, "use " + anthropicShape, "The text looks like a secret key. Remove it."},
		{"private key", KindPackRules, "-----BEGIN " + "PRIVATE KEY-----", "The text looks like a secret key. Remove it."},
		{"held key", KindWizardRules, "- note: held-test-key-0123456789 works", "The text looks like a secret key. Remove it."},
	} {
		_, err := p.Save(ctxOwner(), c.kind, c.body, 0)
		if got := promptErr(err); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	var n int
	if err := p.DB.QueryRow(`SELECT count(*) FROM ai_prompt_versions`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a refused save left %d rows (%v)", n, err)
	}
	if _, err := p.Save(ctxOwner(), KindPreferences, strings.Repeat("é", 2000), 0); err != nil {
		t.Errorf("preferences at the limit: %v", err)
	}
	if _, err := p.Save(ctxOwner(), KindWizardRules, strings.Repeat("a", 16<<10), 0); err != nil {
		t.Errorf("rules at the limit: %v", err)
	}
}

// The Go list of key shapes is the list in tools/check-secrets.sh, so the
// two cannot drift apart.
func TestSecretPatternsMatchTheCommitHook(t *testing.T) {
	b, err := os.ReadFile("../../../tools/check-secrets.sh")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`(?m)^  '[^|']+\|(.+)'\r?$`)
	found := line.FindAllStringSubmatch(string(b), -1)
	if len(found) != len(secretPatterns) {
		t.Fatalf("check-secrets.sh has %d patterns, the server has %d", len(found), len(secretPatterns))
	}
	for i, m := range found {
		if secretPatterns[i].String() != m[1] {
			t.Errorf("pattern %d: server %q, hook %q", i, secretPatterns[i], m[1])
		}
	}
}

func TestActiveTextPrecedence(t *testing.T) {
	p, _ := newPrompts(t)
	ctx := ctxOwner()
	active := func(kind string) ActivePrompt {
		t.Helper()
		a, err := p.Active(ctx, kind)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	// Wizard rules: database, then file, then built-in.
	if a := active(KindWizardRules); a.Source != SourceBuiltIn || a.Text != DefaultRules {
		t.Errorf("wizard with nothing set: %+v", a)
	}
	SetRules("- from the file")
	if a := active(KindWizardRules); a.Source != SourceFile || a.Text != "- from the file" || a.FileSHA == "" {
		t.Errorf("wizard with a file: %+v", a)
	}
	v1, _ := p.Save(ctx, KindWizardRules, "- edit one", 0)
	if a := active(KindWizardRules); a.Source != SourceDatabase || a.Text != "- edit one" || a.Version == nil || a.Version.ID != v1.ID || a.Version.By != "owner@college.edu" {
		t.Errorf("wizard with a database row: %+v", a)
	}
	v2, _ := p.Save(ctx, KindWizardRules, "- edit two", v1.ID)
	r, err := p.Reset(ctx, KindWizardRules, v2.ID)
	if err != nil || !r.IsReset {
		t.Fatalf("reset: %v %+v", err, r)
	}
	if a := active(KindWizardRules); a.Source != SourceFile || a.Text != "- from the file" {
		t.Errorf("reset must give the file text, never an older edit: %+v", a)
	}
	SetRules("")
	if a := active(KindWizardRules); a.Source != SourceBuiltIn || a.Text != DefaultRules {
		t.Errorf("reset with no file must give the built-in text: %+v", a)
	}

	// Pack rules: database, then built-in. The file is never read.
	SetRules("- from the file")
	if a := active(KindPackRules); a.Source != SourceBuiltIn || a.Text == "- from the file" || a.Text == "" {
		t.Errorf("pack rules with a wizard file: %+v", a)
	}
	pv, _ := p.Save(ctx, KindPackRules, "- pack edit", 0)
	if a := active(KindPackRules); a.Source != SourceDatabase || a.Text != "- pack edit" {
		t.Errorf("pack with a row: %+v", a)
	}
	if _, err := p.Reset(ctx, KindPackRules, pv.ID); err != nil {
		t.Fatal(err)
	}
	if a := active(KindPackRules); a.Source != SourceBuiltIn || a.Text == "- pack edit" {
		t.Errorf("pack reset: %+v", a)
	}

	// Preferences: empty unless a non-reset row is newest.
	if a := active(KindPreferences); a.Text != "" || a.Source != SourceBuiltIn {
		t.Errorf("preferences with nothing set: %+v", a)
	}
	fv, _ := p.Save(ctx, KindPreferences, "Prefer short names.", 0)
	if a := active(KindPreferences); a.Text != "Prefer short names." || a.Source != SourceDatabase {
		t.Errorf("preferences: %+v", a)
	}
	p.Reset(ctx, KindPreferences, fv.ID)
	if a := active(KindPreferences); a.Text != "" {
		t.Errorf("preferences after reset: %+v", a)
	}
}

func TestRestoreInsertsANewRowWithTheOldBody(t *testing.T) {
	p, _ := newPrompts(t)
	ctx := ctxOwner()
	v1, _ := p.Save(ctx, KindPackRules, "- one", 0)
	v2, _ := p.Save(ctx, KindPackRules, "- two", v1.ID)
	v3, err := p.Restore(ctx, KindPackRules, v1.ID, v2.ID)
	if err != nil || v3.Body != "- one" || v3.ID <= v2.ID {
		t.Fatalf("restore: %v %+v", err, v3)
	}
	hist, err := p.History(ctx, KindPackRules, 10)
	if err != nil || len(hist) != 3 || hist[0].ID != v3.ID {
		t.Errorf("history: %v %+v", err, hist)
	}
	if _, err := p.Restore(ctx, KindPackRules, 9999, v3.ID); !errors.Is(err, ErrNoVersion) {
		t.Errorf("restore of a missing version: %v", err)
	}
	if _, err := p.Restore(ctx, KindWizardRules, v1.ID, 0); !errors.Is(err, ErrNoVersion) {
		t.Errorf("restore of another kind's version: %v", err)
	}
}

func TestDefaultChangedWarning(t *testing.T) {
	p, d := newPrompts(t)
	ctx := ctxOwner()
	v, _ := p.Save(ctx, KindWizardRules, "- mine", 0)
	if p.DefaultChanged(v) {
		t.Error("warned right after a save")
	}
	if _, err := d.Exec(`INSERT INTO ai_prompt_versions (kind, body, default_sha, created_at) VALUES ('wizard_rules', '- older', 'sha-of-an-old-default', 1)`); err != nil {
		t.Fatal(err)
	}
	latest, ok, err := p.Latest(ctx, KindWizardRules)
	if err != nil || !ok || !p.DefaultChanged(latest) {
		t.Errorf("no warning when the stored default differs: %v %v %+v", err, ok, latest)
	}
	// A row with no stored default never warns.
	if p.DefaultChanged(PromptVersion{Kind: KindWizardRules, DefaultSHA: ""}) {
		t.Error("warned for a row with no default sha")
	}
}

// Each save, reset and restore writes one audit row with the version id,
// kind and hash; nothing else of the text.
func TestPromptAuditHoldsNoText(t *testing.T) {
	p, d := newPrompts(t)
	ctx := ctxOwner()
	words := "UNIQUE-RULE-WORDS-abc"
	v1, _ := p.Save(ctx, KindWizardRules, "- "+words, 0)
	v2, _ := p.Restore(ctx, KindWizardRules, v1.ID, v1.ID)
	if _, err := p.Reset(ctx, KindWizardRules, v2.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Query(`SELECT action, actor_id, details FROM site_audit ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, details string
		var actor sql.NullInt64
		if err := rows.Scan(&action, &actor, &details); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
		if strings.Contains(details, words) || strings.Contains(details, "UNIQUE") {
			t.Errorf("the audit row holds text: %s", details)
		}
		for _, key := range []string{`"version_id"`, `"kind":"wizard_rules"`, `"sha"`} {
			if !strings.Contains(details, key) {
				t.Errorf("%s lacks %s: %s", action, key, details)
			}
		}
		if actor.Int64 != 1 {
			t.Errorf("%s: actor %v", action, actor)
		}
	}
	if strings.Join(actions, ",") != "prompt_saved,prompt_restored,prompt_reset" {
		t.Errorf("actions %v", actions)
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM site_audit WHERE details LIKE ?`, "%"+words+"%").Scan(&n)
	if n != 0 {
		t.Error("the body text is in site_audit")
	}
}

func TestPackPromptInputsFollowTheNewestRows(t *testing.T) {
	p, _ := newPrompts(t)
	ctx := ctxOwner()
	rules, prefs, err := p.PackPromptInputs(ctx)
	if err != nil || rules != "" || prefs != "" {
		t.Fatalf("nothing saved: %q %q %v", rules, prefs, err)
	}
	r, _ := p.Save(ctx, KindPackRules, "- pack rules", 0)
	p.Save(ctx, KindPreferences, "Short names.", 0)
	rules, prefs, _ = p.PackPromptInputs(ctx)
	if rules != "- pack rules" || prefs != "Short names." {
		t.Errorf("after saves: %q %q", rules, prefs)
	}
	p.Reset(ctx, KindPackRules, r.ID)
	if rules, _, _ = p.PackPromptInputs(ctx); rules != "" {
		t.Errorf("after a reset the built-in text applies: %q", rules)
	}
}

// An admin's dry run of the preferences shows only the block as inserted,
// never the owner's active rules.
func TestPreferencesPreviewForANonOwnerHidesTheRules(t *testing.T) {
	p, _ := newPrompts(t)
	ctx := ctxOwner()
	p.Save(ctx, KindWizardRules, "- SECRET-OWNER-RULES", 0)
	p.Save(ctx, KindPackRules, "- SECRET-PACK-RULES", 0)
	got, err := p.Preview(ctx, KindPreferences, "Short names.", false)
	if err != nil || len(got) != 1 {
		t.Fatalf("preview: %v %v", err, got)
	}
	if strings.Contains(got[0].Text, "SECRET-") || strings.Contains(got[0].Text, "You write competitive") || !strings.Contains(got[0].Text, "<preferences>\nShort names.\n</preferences>") {
		t.Errorf("admin preview:\n%s", got[0].Text)
	}
	full, _ := p.Preview(ctx, KindPreferences, "Short names.", true)
	if len(full) != 2 || !strings.Contains(full[0].Text, "SECRET-OWNER-RULES") {
		t.Errorf("owner preview: %v", full)
	}
}

// A held key pasted with spaces or line breaks inside it is still refused.
func TestHeldKeyWithWhitespaceIsRefused(t *testing.T) {
	p, _ := newPrompts(t)
	p.Keys.Put("s", KeyEntry{Provider: ProviderAnthropic, Key: NewSecret("held-test-key-0123456789")})
	_, err := p.Save(ctxOwner(), KindPreferences, "note: held-test-\nkey-0123 456789", 0)
	if got := promptErr(err); got != "The text looks like a secret key. Remove it." {
		t.Errorf("got %q", got)
	}
}

func TestVersionLookupByID(t *testing.T) {
	p, _ := newPrompts(t)
	v, _ := p.Save(ctxOwner(), KindPackRules, "- one", 0)
	got, err := p.Version(ctxOwner(), KindPackRules, v.ID)
	if err != nil || got.Body != "- one" || got.ByID != 1 {
		t.Errorf("%v %+v", err, got)
	}
	if _, err := p.Version(ctxOwner(), KindWizardRules, v.ID); !errors.Is(err, ErrNoVersion) {
		t.Errorf("other kind: %v", err)
	}
}
