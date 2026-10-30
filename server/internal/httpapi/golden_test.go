package httpapi

// Golden files: the JSON of the core-path routes as TestMiniContest sees
// them, kept in web/tests/fixtures. The web app's types and its test mocks
// are checked against these files, so a change to what the server sends
// shows up there instead of drifting quietly.
//
// PODIUM_GOLDEN=1 rewrites the files. A normal run fails when a response no
// longer matches its file. Times are moved onto a fixed contest start and
// cut to the minute, and the server's URL, the submit keys and the board's
// version are replaced, so the files stay the same from run to run.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goldenDir is web/tests/fixtures, seen from this package.
const goldenDir = "../../../web/tests/fixtures"

// goldenStart is the contest start every time in a golden file is moved
// onto: 2027-01-15 08:00 UTC.
const goldenStart = int64(1_800_000_000_000)

// golden writes or compares the golden files of one test.
type golden struct {
	t     *testing.T
	start time.Time
	url   string
}

// check normalises body and compares it with web/tests/fixtures/name.json,
// or writes the file when PODIUM_GOLDEN=1.
func (g *golden) check(name string, code int, body any) {
	g.t.Helper()
	if code < 200 || code > 299 {
		g.t.Fatalf("golden %s: status %d %v", name, code, body)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g.normalise("", body)); err != nil {
		g.t.Fatal(err)
	}
	path := filepath.Join(goldenDir, name+".json")
	if os.Getenv("PODIUM_GOLDEN") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			g.t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			g.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		g.t.Errorf("golden %s: %v; run PODIUM_GOLDEN=1 go test -run TestMiniContest ./internal/httpapi to write it", name, err)
		return
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(want, buf.Bytes()) {
		g.t.Errorf("golden %s: the response no longer matches %s (%s); if the change is meant, run PODIUM_GOLDEN=1 go test -run TestMiniContest ./internal/httpapi",
			name, path, firstDiff(string(want), buf.String()))
	}
}

// normalise rewrites the values that change from run to run.
func (g *golden) normalise(key string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = g.normalise(k, val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = g.normalise(key, val)
		}
		return out
	case float64:
		// A board's version counts its recomputes, which depend on timing.
		if key == "version" {
			return 1
		}
		// Unix milliseconds: on the fixed start, to the minute.
		if x >= 1e12 && x < 1e13 {
			rel := int64(x) - g.start.UnixMilli()
			minutes := rel / 60_000
			if rel < 0 && rel%60_000 != 0 {
				minutes--
			}
			return goldenStart + minutes*60_000
		}
		return x
	case string:
		if key == "client_key" {
			return "client-key"
		}
		return strings.ReplaceAll(x, g.url, "http://podium.test")
	}
	return v
}

// firstDiff names the first line where two texts differ.
func firstDiff(want, got string) string {
	w, h := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(h); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(h) {
			b = h[i]
		}
		if a != b {
			return fmt.Sprintf("line %d: file has %s, server sent %s", i+1, strings.TrimSpace(a), strings.TrimSpace(b))
		}
	}
	return "same lines"
}
