package ai

// The prompt versions: the rules text of the wizard prompt and of the
// outside-AI pack prompt, and the admin's Preferences box, each kept as
// insert-only rows in ai_prompt_versions (migration 0015). The newest row of
// a kind is its active text. A reset row means "the default" and never
// brings an older edit back; restoring inserts a new row. Rules are data:
// they are placed in the prompt, never parsed as template source.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Prompt kinds, as stored.
const (
	KindWizardRules = "wizard_rules"
	KindPackRules   = "pack_rules"
	KindPreferences = "preferences"
)

// Where the active text of a kind comes from.
const (
	SourceDatabase = "database"
	SourceFile     = "file"
	SourceBuiltIn  = "built-in"
)

// Save limits: 16 KB for rules, 2,000 characters for preferences.
const (
	MaxRulesBytes       = 16 << 10
	MaxPreferencesChars = 2000
)

// ErrStale means the editor's base version is no longer the newest one.
var ErrStale = errors.New("someone saved a newer version")

// ErrNoVersion means a version to restore does not exist for that kind.
var ErrNoVersion = errors.New("no such version")

// PromptError is a refused body. Message is the sentence the owner sees.
type PromptError struct{ Message string }

func (e *PromptError) Error() string { return e.Message }

// IsPromptKind reports whether kind is one of the three stored kinds.
func IsPromptKind(kind string) bool {
	return kind == KindWizardRules || kind == KindPackRules || kind == KindPreferences
}

// secretPatterns are the key shapes of tools/check-secrets.sh, in its order;
// a test keeps the two lists equal. A prompt body that matches one is
// refused, because the table is append only and a pasted key could never be
// removed.
var secretPatterns = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range []string{
		`AKIA[0-9A-Z]{16}`,
		`sk-ant-[A-Za-z0-9_-]{20,}`,
		`sk-(proj-)?[A-Za-z0-9_-]{32,}`,
		`GOCSPX-[A-Za-z0-9_-]{20,}`,
		`AIza[0-9A-Za-z_-]{35}`,
		`gh[pousr]_[A-Za-z0-9]{36}`,
		`github_pat_[A-Za-z0-9_]{60,}`,
		`AGE-SECRET-KEY-1[0-9A-Z]{50,}`,
		`xox[baprs]-[A-Za-z0-9-]{10,}`,
		`-----BEGIN ([A-Z]+ )?PRIVATE KEY( BLOCK)?-----`,
		`(ntfy\.sh/[A-Za-z0-9_-]{16,}|hc-ping\.com/[0-9a-f-]{36})`,
		`(^|[^A-Za-z0-9/+])K[0-9]{3}[A-Za-z0-9/+]{27}([^A-Za-z0-9/+=]|$)`,
	} {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}()

// minHeldKey is the shortest held key worth matching in a body; shorter
// strings would match ordinary words.
const minHeldKey = 8

// contains reports whether text holds any key a live session holds.
func (k *Keys) contains(text string) bool {
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.now()
	for _, m := range k.m {
		for _, e := range m {
			if now.Before(e.Expires) {
				if key := e.Key.reveal(); len(key) >= minHeldKey && strings.Contains(text, key) {
					return true
				}
			}
		}
	}
	return false
}

// PromptVersion is one saved row. By is the saver's email, "" if unknown.
type PromptVersion struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Body       string `json:"body"`
	IsReset    bool   `json:"is_reset"`
	DefaultSHA string `json:"default_sha"`
	By         string `json:"by"`
	At         int64  `json:"at"`
}

// ActivePrompt is the text a kind uses now and where it comes from.
type ActivePrompt struct {
	Text string `json:"text"`
	// Source is SourceDatabase, SourceFile or SourceBuiltIn.
	Source string `json:"source"`
	// Version is the database row when the source is the database.
	Version *PromptVersion `json:"version,omitempty"`
	// FileSHA is the sha256 of the file text when the source is the file.
	FileSHA string `json:"file_sha,omitempty"`
}

// Preview is one full prompt a dry run shows.
type Preview struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Prompts reads and writes the prompt versions.
type Prompts struct {
	DB *sql.DB
	// Keys is checked so a held provider key cannot be saved by accident.
	Keys *Keys
	// Now is the clock; tests replace it.
	Now func() time.Time
}

func (p *Prompts) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// DefaultText is the built-in text of a kind: the rules the wizard ships
// with, the pack prompt's rules, or "" for preferences.
func DefaultText(kind string) string {
	switch kind {
	case KindWizardRules:
		return DefaultRules
	case KindPackRules:
		return problems.DefaultPackRules()
	}
	return ""
}

// DefaultChanged reports whether the built-in text of the version's kind
// differs from the one it was saved against. A row with no stored hash
// never warns.
func (p *Prompts) DefaultChanged(v PromptVersion) bool {
	return !v.IsReset && v.DefaultSHA != "" && v.DefaultSHA != sha(DefaultText(v.Kind))
}

const versionColumns = `v.id, v.kind, v.body, v.is_reset, v.default_sha, COALESCE(u.email, ''), v.created_at`

func scanVersion(row interface{ Scan(...any) error }) (PromptVersion, error) {
	var v PromptVersion
	var reset int
	err := row.Scan(&v.ID, &v.Kind, &v.Body, &reset, &v.DefaultSHA, &v.By, &v.At)
	v.IsReset = reset == 1
	return v, err
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func latestIn(ctx context.Context, q queryer, kind string) (PromptVersion, bool, error) {
	v, err := scanVersion(q.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM ai_prompt_versions v
		LEFT JOIN users u ON u.id = v.created_by WHERE v.kind = ? ORDER BY v.id DESC LIMIT 1`, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return PromptVersion{}, false, nil
	}
	return v, err == nil, err
}

// Latest is the newest row of a kind, reset rows included.
func (p *Prompts) Latest(ctx context.Context, kind string) (PromptVersion, bool, error) {
	return latestIn(ctx, p.DB, kind)
}

// History is the newest rows of a kind, newest first.
func (p *Prompts) History(ctx context.Context, kind string, limit int) ([]PromptVersion, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+versionColumns+` FROM ai_prompt_versions v
		LEFT JOIN users u ON u.id = v.created_by WHERE v.kind = ? ORDER BY v.id DESC LIMIT ?`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PromptVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Active resolves the text a kind uses: the newest database row unless it
// is a reset or there is none; then, for the wizard rules only, the file
// from the environment; then the built-in text. Preferences are empty
// without a row.
func (p *Prompts) Active(ctx context.Context, kind string) (ActivePrompt, error) {
	v, ok, err := p.Latest(ctx, kind)
	if err != nil {
		return ActivePrompt{}, err
	}
	if ok && !v.IsReset {
		return ActivePrompt{Text: v.Body, Source: SourceDatabase, Version: &v}, nil
	}
	if kind == KindWizardRules {
		if c := customRules.Load(); c != nil {
			return ActivePrompt{Text: *c, Source: SourceFile, FileSHA: sha(*c)}, nil
		}
	}
	return ActivePrompt{Text: DefaultText(kind), Source: SourceBuiltIn}, nil
}

// WizardSystem builds the system prompt of a wizard run from the active
// rules and preferences. StartWizard calls it once and carries the result
// in the request.
func (p *Prompts) WizardSystem(ctx context.Context) (string, error) {
	rules, err := p.Active(ctx, KindWizardRules)
	if err != nil {
		return "", err
	}
	prefs, err := p.Active(ctx, KindPreferences)
	if err != nil {
		return "", err
	}
	return BuildSystemPrompt(rules.Text, prefs.Text), nil
}

// PackPromptInputs implements problems.PromptSource: the saved pack rules
// ("" when the built-in text applies) and the preferences.
func (p *Prompts) PackPromptInputs(ctx context.Context) (string, string, error) {
	rules, err := p.Active(ctx, KindPackRules)
	if err != nil {
		return "", "", err
	}
	prefs, err := p.Active(ctx, KindPreferences)
	if err != nil {
		return "", "", err
	}
	if rules.Source == SourceBuiltIn {
		rules.Text = ""
	}
	return rules.Text, prefs.Text, nil
}

// Preview builds the full prompt or prompts that would be sent if body
// became the active text of kind, through the builders the runs use.
// Nothing is written.
func (p *Prompts) Preview(ctx context.Context, kind, body string) ([]Preview, error) {
	if err := p.check(kind, body); err != nil {
		return nil, err
	}
	rules := map[string]string{}
	for _, k := range []string{KindWizardRules, KindPackRules, KindPreferences} {
		a, err := p.Active(ctx, k)
		if err != nil {
			return nil, err
		}
		rules[k] = a.Text
	}
	rules[kind] = body
	wizard := Preview{Label: "Wizard prompt", Text: BuildSystemPrompt(rules[KindWizardRules], rules[KindPreferences])}
	outside, err := problems.PackPromptWith(rules[KindPackRules], rules[KindPreferences])
	if err != nil {
		return nil, err
	}
	switch kind {
	case KindWizardRules:
		return []Preview{wizard}, nil
	case KindPackRules:
		return []Preview{{Label: "Outside-AI prompt", Text: outside}}, nil
	}
	return []Preview{wizard, {Label: "Outside-AI prompt", Text: outside}}, nil
}

// check refuses a body that must not be saved. Messages name the reason.
func (p *Prompts) check(kind, body string) error {
	switch {
	case !IsPromptKind(kind):
		return &PromptError{"This kind of prompt does not exist."}
	case !utf8.ValidString(body):
		return &PromptError{"The text is not valid UTF-8."}
	case strings.TrimSpace(body) == "":
		return &PromptError{"The text is empty."}
	case kind == KindPreferences && utf8.RuneCountInString(body) > MaxPreferencesChars:
		return &PromptError{"The preferences are longer than 2,000 characters."}
	case kind != KindPreferences && len(body) > MaxRulesBytes:
		return &PromptError{"The rules are longer than 16 KB."}
	}
	for _, re := range secretPatterns {
		if re.MatchString(body) {
			return &PromptError{"The text looks like a secret key. Remove it."}
		}
	}
	if p.Keys.contains(body) {
		return &PromptError{"The text looks like a secret key. Remove it."}
	}
	return nil
}

// Save inserts a new row with body as the active text of kind. baseID is
// the version the editor loaded (0 when it loaded none); if a newer row
// exists the save is refused with ErrStale.
func (p *Prompts) Save(ctx context.Context, kind, body string, baseID int64) (PromptVersion, error) {
	if err := p.check(kind, body); err != nil {
		return PromptVersion{}, err
	}
	return p.insert(ctx, kind, body, false, baseID, auth.AuditPromptSaved, nil)
}

// Reset inserts a reset row: the kind goes back to its default and an older
// edit never returns.
func (p *Prompts) Reset(ctx context.Context, kind string, baseID int64) (PromptVersion, error) {
	if !IsPromptKind(kind) {
		return PromptVersion{}, &PromptError{"This kind of prompt does not exist."}
	}
	return p.insert(ctx, kind, "", true, baseID, auth.AuditPromptReset, nil)
}

// Restore inserts a new row with the body of an older version of the same
// kind. A reset row cannot be restored; use Reset.
func (p *Prompts) Restore(ctx context.Context, kind string, versionID, baseID int64) (PromptVersion, error) {
	v, err := scanVersion(p.DB.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM ai_prompt_versions v
		LEFT JOIN users u ON u.id = v.created_by WHERE v.id = ? AND v.kind = ?`, versionID, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return PromptVersion{}, ErrNoVersion
	}
	if err != nil {
		return PromptVersion{}, err
	}
	if v.IsReset {
		return PromptVersion{}, &PromptError{"That version is a reset. Use Reset to default."}
	}
	if err := p.check(kind, v.Body); err != nil {
		return PromptVersion{}, err
	}
	return p.insert(ctx, kind, v.Body, false, baseID, auth.AuditPromptRestored, map[string]any{"restored_from": versionID})
}

// insert writes one row and its audit row in one transaction. The audit row
// names the version id, the kind and the hash of the body, never the text.
func (p *Prompts) insert(ctx context.Context, kind, body string, reset bool, baseID int64, action string, extra map[string]any) (PromptVersion, error) {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return PromptVersion{}, err
	}
	defer tx.Rollback()
	cur, _, err := latestIn(ctx, tx, kind)
	if err != nil {
		return PromptVersion{}, err
	}
	if want := cur.ID; baseID != want {
		return PromptVersion{}, ErrStale
	}
	actor, _ := auth.ActorFrom(ctx)
	var by any
	if actor.ID > 0 {
		by = actor.ID
	}
	at := p.now()
	isReset := 0
	if reset {
		isReset = 1
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO ai_prompt_versions (kind, body, is_reset, default_sha, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, kind, body, isReset, sha(DefaultText(kind)), by, at.UnixMilli())
	if err != nil {
		return PromptVersion{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return PromptVersion{}, err
	}
	details := map[string]any{"version_id": id, "kind": kind, "sha": sha(body)}
	for k, v := range extra {
		details[k] = v
	}
	if err := auth.RecordSite(ctx, tx, at, action, "", details); err != nil {
		return PromptVersion{}, fmt.Errorf("prompt audit: %w", err)
	}
	v, err := scanVersion(tx.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM ai_prompt_versions v
		LEFT JOIN users u ON u.id = v.created_by WHERE v.id = ?`, id))
	if err != nil {
		return PromptVersion{}, err
	}
	return v, tx.Commit()
}
