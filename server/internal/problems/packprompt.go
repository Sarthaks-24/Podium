package problems

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"text/template"

	"github.com/Sarthaks-24/podium/internal/buildinfo"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

// packPromptFS holds the prompt template and the example pack it quotes.
// The example is imported by a test, so a change to the pack format that
// breaks it fails the build before the prompt can drift.
//
//go:embed packprompt
var packPromptFS embed.FS

// promptPackFormat is the one pack format the outside-AI prompt asks for.
// It is the format the exporter writes for a problem without starters.
const promptPackFormat = 1

// promptFunctionFormat is the pack format of a function problem.
const promptFunctionFormat = 4

var packPromptTmpl = template.Must(template.ParseFS(packPromptFS, "packprompt/prompt.tmpl"))

// DefaultPackRules is the built-in rules text of the outside-AI prompt: the
// authoring advice the owner can edit (packprompt/rules.txt), then the
// function-problem teaching (packprompt/rules_function.txt, a separate file
// so its wording can follow the function pack format without touching the
// rest). Everything that carries a limit or a format name stays in
// prompt.tmpl and is never editable.
func DefaultPackRules() string {
	rules, _ := packPromptFS.ReadFile("packprompt/rules.txt")
	function, _ := packPromptFS.ReadFile("packprompt/rules_function.txt")
	return strings.TrimRight(string(rules), "\n") + "\n\n" + strings.TrimRight(string(function), "\n")
}

// PromptSource gives the outside-AI prompt the owner's edited rules and the
// admin's preferences. Both come back "" when none were saved.
type PromptSource interface {
	PackPromptInputs(ctx context.Context) (rules, preferences string, err error)
}

// PackPrompt returns the prompt an organiser gives to any AI to get a pack
// for Import pack, with the built-in rules and no preferences. Every number
// and name comes from the importer's own constants and from the example
// pack, not from retyped text. The first line names this build's release
// and the pack format (V201-16).
func PackPrompt() (string, error) {
	return PackPromptWith("", "")
}

// PackPromptWith is PackPrompt with the owner's rules ("" for the built-in
// text) and the admin's preferences. Both are data: they are inserted into
// the finished prompt, never parsed as template source, so a "{{" in them
// prints as typed.
func PackPromptWith(rules, preferences string) (string, error) {
	return packPromptWith(buildinfo.Version, rules, preferences)
}

// PackPromptGenerated is the part of the prompt the owner cannot edit,
// with a marker where the rules go. The owner editor shows it read-only.
func PackPromptGenerated() (string, error) {
	return packPromptWith(buildinfo.Version, "(the rules text goes here)", "")
}

// defusePreferences matches an opening or closing preferences tag typed
// inside the text, so the text cannot end its own block early.
var defusePreferences = regexp.MustCompile(`(?i)<(/?)preferences\b`)

// preferencesBlock is the admin's preferences as a delimited data block,
// or "" when there are none.
func preferencesBlock(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = defusePreferences.ReplaceAllString(text, "&lt;$1preferences")
	return "## Preferences\n\nThe organisation's style choices follow. Apply them unless they conflict with the format, the limits or the fixed rules below.\n\n<preferences>\n" +
		text + "\n</preferences>\n\n"
}

// packPromptStamp is the prompt's first line. A plain dev build has no
// release to name.
func packPromptStamp(version string) string {
	if version == "" || version == "dev" {
		return fmt.Sprintf("Podium pack prompt, pack formats %d and %d", promptPackFormat, promptFunctionFormat)
	}
	return fmt.Sprintf("Podium %s pack prompt, pack formats %d and %d", version, promptPackFormat, promptFunctionFormat)
}

func packPrompt(version string) (string, error) { return packPromptWith(version, "", "") }

func packPromptWith(version, rules, preferences string) (string, error) {
	if strings.TrimSpace(rules) == "" {
		rules = DefaultPackRules()
	}
	yml, err := packPromptFS.ReadFile("packprompt/example/problem.yaml")
	if err != nil {
		return "", err
	}
	fn := map[string]string{}
	for _, f := range []string{"problem.yaml", "solution.cpp", "bottom.py"} {
		b, err := packPromptFS.ReadFile("packprompt/example_function/" + f)
		if err != nil {
			return "", err
		}
		fn[f] = string(b)
	}
	data := map[string]any{
		"FunctionFormat":   promptFunctionFormat,
		"FunctionYAML":     fn["problem.yaml"],
		"FunctionSolution": fn["solution.cpp"],
		"FunctionBottom":   fn["bottom.py"],
		"MaxStarterKB":     MaxStarter >> 10,
		"Stamp":            packPromptStamp(version),
		"Rules":            strings.TrimRight(rules, "\n"),
		"PreferencesBlock": preferencesBlock(preferences),
		"Format":           promptPackFormat,
		"ExampleSlug":      "double-it",
		"ExampleYAML":      string(yml),
		"ReferenceName":    "solution.cpp",
		"MaxStatementKB":   MaxStatement >> 10,
		"MaxSourceKB":      MaxSource >> 10,
		"MaxTests":         MaxTests,
		"MaxTitle":         MaxTitle,
		"MinTimeLimit":     MinTimeLimitMS,
		"MaxTimeLimit":     MaxTimeLimitMS,
		"MinMemory":        MinMemoryLimitMB,
		"MaxMemory":        MaxMemoryLimitMB,
		"MaxTags":          MaxTags,
		"MaxTag":           MaxTag,
		"MaxPoints":        MaxPoints,
		"MaxSubtasks":      MaxSubtasks,
		"MaxPackMB":        MaxPackBytes >> 20,
		"MaxEntries":       MaxPackEntries,
		"MaxYAMLKB":        MaxProblemYAML >> 10,
		"MaxFileMB":        MaxPackFile >> 20,
		"CheckerTokens":    CheckerTokens,
		"CheckerExact":     CheckerExact,
		"CheckerFloat":     CheckerFloat,
		"CheckerCustom":    CheckerCustom,
	}
	var b bytes.Buffer
	if err := packPromptTmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("pack prompt: %w", err)
	}
	return b.String(), nil
}

type packPromptJSON struct {
	Prompt string `json:"prompt"`
}

// GetPackPrompt serves GET /api/admin/problems/pack-prompt.
func (d *Handlers) GetPackPrompt(w http.ResponseWriter, r *http.Request) {
	var rules, prefs string
	if d.Prompts != nil {
		var err error
		if rules, prefs, err = d.Prompts.PackPromptInputs(r.Context()); err != nil {
			d.Log.Error("pack prompt", "err", err)
			httpx.WriteError(w, http.StatusInternalServerError, "internal", "The prompt could not be made. Try again.")
			return
		}
	}
	text, err := PackPromptWith(rules, prefs)
	if err != nil {
		d.Log.Error("pack prompt", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The prompt could not be made. Try again.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, packPromptJSON{Prompt: text})
}
