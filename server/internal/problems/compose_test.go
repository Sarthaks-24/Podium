package problems

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

// composeFixture is the file the editor's join reads too (V202-34).
const composeFixture = "../../../web/tests/fixtures/compose.json"

type composeCase struct {
	Name      string `json:"name"`
	Language  string `json:"language"`
	Top       string `json:"top"`
	Code      string `json:"code"`
	Bottom    string `json:"bottom"`
	Composed  string `json:"composed"`
	TopLines  int    `json:"top_lines"`
	CodeLines int    `json:"code_lines"`
}

func readComposeFixture(t *testing.T) []composeCase {
	t.Helper()
	b, err := os.ReadFile(composeFixture)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []composeCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) < 4 {
		t.Fatalf("only %d cases", len(f.Cases))
	}
	return f.Cases
}

// The shared fixture: Compose and the editor's join give the same file and
// offsets for every case.
func TestComposeMatchesTheSharedFixture(t *testing.T) {
	for _, c := range readComposeFixture(t) {
		got := Compose(c.Top, c.Code, c.Bottom)
		if got.Source != c.Composed || got.TopLines != c.TopLines || got.CodeLines != c.CodeLines {
			t.Errorf("%s: got %q (%d, %d), want %q (%d, %d)", c.Name, got.Source, got.TopLines, got.CodeLines,
				c.Composed, c.TopLines, c.CodeLines)
		}
		if n := CodeLines(c.Code); n != c.CodeLines {
			t.Errorf("%s: CodeLines %d, want %d", c.Name, n, c.CodeLines)
		}
		p := &Problem{Mode: ModeFunction, LockedTop: map[string]string{c.Language: c.Top}, LockedBottom: map[string]string{c.Language: c.Bottom}}
		if got := p.ComposeFor(c.Language, c.Code); got.Source != c.Composed || got.TopLines != c.TopLines {
			t.Errorf("%s: ComposeFor %q (%d)", c.Name, got.Source, got.TopLines)
		}
	}
}

// A whole problem judges the student's text exactly as sent, with no
// offset: v2.0.1 behaviour.
func TestComposeLeavesAWholeProblemAlone(t *testing.T) {
	for _, mode := range []string{"", ModeWhole} {
		p := &Problem{Mode: mode, LockedTop: map[string]string{"cpp17": "x\n"}, LockedBottom: map[string]string{"cpp17": "y\n"}}
		code := "int main() {}\r\n\n\n"
		if got := p.ComposeFor("cpp17", code); got.Source != code || got.TopLines != 0 || got.CodeLines != 0 {
			t.Errorf("mode %q: %+v", mode, got)
		}
	}
}

// checkComposed is the property every composed file has: its lines are the
// top's, then the code's, then the bottom's, none lost, none repeated, each
// part cut only after its last line, and the offsets count them.
func checkComposed(t *testing.T, top, code, bottom string) {
	t.Helper()
	got := Compose(top, code, bottom)
	lines := func(s string) []string {
		s = strings.TrimRight(s, "\r\n")
		if s == "" {
			return nil
		}
		return strings.Split(s, "\n")
	}
	want := append(append(append([]string{}, lines(top)...), lines(code)...), lines(bottom)...)
	have := lines(got.Source)
	if strings.Join(have, "\n") != strings.Join(want, "\n") || len(have) != len(want) {
		t.Fatalf("Compose(%q, %q, %q) = %q: lines %q, want %q", top, code, bottom, got.Source, have, want)
	}
	if got.TopLines != len(lines(top)) || got.CodeLines != len(lines(code)) {
		t.Fatalf("Compose(%q, %q, %q): offsets %d, %d, want %d, %d", top, code, bottom, got.TopLines, got.CodeLines,
			len(lines(top)), len(lines(code)))
	}
	if got.Source != "" && (!strings.HasSuffix(got.Source, "\n") || strings.HasSuffix(got.Source, "\n\n") || strings.HasSuffix(got.Source, "\r\n")) {
		t.Fatalf("Compose(%q, %q, %q) = %q does not end in one newline", top, code, bottom, got.Source)
	}
	if got.Source == "" && len(want) > 0 {
		t.Fatalf("Compose(%q, %q, %q) lost everything", top, code, bottom)
	}
}

// Random parts built from line ends, blanks and text, with and without a
// final newline: the property holds for every one. The seed is fixed, so a
// failure repeats.
func TestComposeNeverLosesOrRepeatsALine(t *testing.T) {
	pieces := []string{"a", "bc", " ", "\t", "\n", "\r\n", "\n\n", "é", "}", "\r"}
	rng := rand.New(rand.NewPCG(2026, 1107))
	part := func() string {
		var b strings.Builder
		for range rng.IntN(12) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		return b.String()
	}
	for range 5000 {
		checkComposed(t, part(), part(), part())
	}
}

// FuzzCompose runs the same property on fuzzed parts. Its seeds run in
// every go test; longer runs are by hand: go test -fuzz FuzzCompose
// -fuzztime 60s ./internal/problems/.
func FuzzCompose(f *testing.F) {
	for _, s := range [][3]string{
		{"", "", ""}, {"a", "b", "c"}, {"a\n", "b\n", "c\n"}, {"a\r\n", "\r\n", "c"}, {"\n\n", "x\n\n", "\n"},
		{"import sys\n", "def f():\n    return 1", "print(f())"}, {"a\rb", "c\r", "\r"},
	} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, top, code, bottom string) {
		checkComposed(t, top, code, bottom)
	})
}

// A whole file that already holds the locked top or bottom is cut back to
// the student's part, on an exact match only (V202-34).
func TestStripLocked(t *testing.T) {
	p := &Problem{Mode: ModeFunction, LockedTop: map[string]string{"python3": "import sys\n"}, LockedBottom: map[string]string{"python3": "print(f(1))"}}
	body := "def f(n):\n    return n\n"
	for name, c := range map[string]struct{ in, want string }{
		"top and bottom":     {"import sys\n" + body + "print(f(1))\n", body},
		"bottom has no end":  {"import sys\n" + body + "print(f(1))", body},
		"top only":           {"import sys\n" + body, body},
		"bottom only":        {body + "print(f(1))\n", body},
		"neither":            {body, body},
		"near miss kept":     {"import os\n" + body, "import os\n" + body},
		"top inside a line":  {"x import sys\n" + body, "x import sys\n" + body},
		"bottom on one line": {body + "x print(f(1))\n", body + "x print(f(1))\n"},
	} {
		if got := p.StripLocked("python3", c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
	whole := &Problem{Mode: ModeWhole}
	if got := whole.StripLocked("python3", "import sys\n"); got != "import sys\n" {
		t.Errorf("whole problem changed the code: %q", got)
	}
}
