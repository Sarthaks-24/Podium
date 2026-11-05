package problems

import (
	"bytes"
	"embed"
	"fmt"
	"net/http"
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

var packPromptTmpl = template.Must(template.ParseFS(packPromptFS, "packprompt/prompt.tmpl"))

// PackPrompt returns the prompt an organiser gives to any AI to get a pack
// for Import pack. Every number and name comes from the importer's own
// constants and from the example pack, not from retyped text. The first
// line names this build's release and the pack format (V201-16).
func PackPrompt() (string, error) {
	return packPrompt(buildinfo.Version)
}

// packPromptStamp is the prompt's first line. A plain dev build has no
// release to name.
func packPromptStamp(version string) string {
	if version == "" || version == "dev" {
		return fmt.Sprintf("Podium pack prompt, pack format %d", promptPackFormat)
	}
	return fmt.Sprintf("Podium %s pack prompt, pack format %d", version, promptPackFormat)
}

func packPrompt(version string) (string, error) {
	yml, err := packPromptFS.ReadFile("packprompt/example/problem.yaml")
	if err != nil {
		return "", err
	}
	data := map[string]any{
		"Stamp":          packPromptStamp(version),
		"Format":         promptPackFormat,
		"ExampleSlug":    "double-it",
		"ExampleYAML":    string(yml),
		"ReferenceName":  "solution.cpp",
		"MaxStatementKB": MaxStatement >> 10,
		"MaxSourceKB":    MaxSource >> 10,
		"MaxTests":       MaxTests,
		"MaxTitle":       MaxTitle,
		"MinTimeLimit":   MinTimeLimitMS,
		"MaxTimeLimit":   MaxTimeLimitMS,
		"MinMemory":      MinMemoryLimitMB,
		"MaxMemory":      MaxMemoryLimitMB,
		"MaxTags":        MaxTags,
		"MaxTag":         MaxTag,
		"MaxPoints":      MaxPoints,
		"MaxSubtasks":    MaxSubtasks,
		"MaxPackMB":      MaxPackBytes >> 20,
		"MaxEntries":     MaxPackEntries,
		"MaxYAMLKB":      MaxProblemYAML >> 10,
		"MaxFileMB":      MaxPackFile >> 20,
		"CheckerTokens":  CheckerTokens,
		"CheckerExact":   CheckerExact,
		"CheckerFloat":   CheckerFloat,
		"CheckerCustom":  CheckerCustom,
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
	text, err := PackPrompt()
	if err != nil {
		d.Log.Error("pack prompt", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The prompt could not be made. Try again.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, packPromptJSON{Prompt: text})
}
