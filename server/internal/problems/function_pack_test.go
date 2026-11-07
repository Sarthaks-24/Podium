package problems

import (
	"archive/zip"
	"bytes"
	"io"
	"maps"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// functionPackProblem is a-plus-b turned into a function problem in C++
// and Python, with its test hashes filled in, and a blob reader for export.
func functionPackProblem(t *testing.T) (*Problem, func(string) ([]byte, error)) {
	t.Helper()
	pk, err := ReadPack(zipDir(t, filepath.Join(examples, "a-plus-b")))
	if err != nil {
		t.Fatal(err)
	}
	p := pk.Problem
	blobs := map[string][]byte{}
	for i := range p.Tests {
		in, out := pk.Inputs[p.Tests[i].Idx], pk.Outputs[p.Tests[i].Idx]
		p.Tests[i].InputSHA, p.Tests[i].OutputSHA = SHA(in), SHA(out)
		blobs[SHA(in)], blobs[SHA(out)] = in, out
	}
	p.Mode, p.LockedTop, p.LockedBottom, p.Starters = ModeFunction, maps.Clone(fnTop), maps.Clone(fnBottom), maps.Clone(fnStubs)
	p.Reference = Source{"cpp17", "long long twice(long long n) { return 2 * n; }\n"}
	p.Brute = &Source{"python3", "def twice(n):\n    return n + n\n"}
	p.Validator = nil
	return &p, func(sha string) ([]byte, error) { return blobs[sha], nil }
}

func packYAML(t *testing.T, data []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if path.Base(f.Name) == "problem.yaml" {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatal("no problem.yaml")
	return ""
}

// A function problem exports as format 4 with its mode, locked parts and
// stubs, and imports back to the same problem and hash; exporting it again
// gives the same bytes.
func TestPackFormat4RoundTrip(t *testing.T) {
	p, read := functionPackProblem(t)
	var buf bytes.Buffer
	if err := WritePack(&buf, p, read); err != nil {
		t.Fatal(err)
	}
	y := packYAML(t, buf.Bytes())
	for _, want := range []string{"format: 4\n", "mode: function\n", "locked_top: [top.cpp, top.py]\n",
		"locked_bottom: [bottom.cpp, bottom.py]\n", "starters: [starter.cpp, starter.py]\n"} {
		if !strings.Contains(y, want) {
			t.Errorf("problem.yaml lacks %q:\n%s", want, y)
		}
	}
	if !strings.HasPrefix(y, "format: 4\n") {
		t.Errorf("problem.yaml does not start with the format:\n%s", y)
	}
	again, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	q := again.Problem
	if q.Mode != ModeFunction || !maps.Equal(q.LockedTop, p.LockedTop) || !maps.Equal(q.LockedBottom, p.LockedBottom) ||
		!maps.Equal(q.Starters, p.Starters) {
		t.Errorf("round trip: mode %q top %v bottom %v starters %v", q.Mode, q.LockedTop, q.LockedBottom, q.Starters)
	}
	for i := range q.Tests {
		q.Tests[i].InputSHA, q.Tests[i].OutputSHA = SHA(again.Inputs[q.Tests[i].Idx]), SHA(again.Outputs[q.Tests[i].Idx])
	}
	if ContentHash(&q) != ContentHash(p) {
		t.Error("round trip changed the content hash")
	}
	var buf2 bytes.Buffer
	if err := WritePack(&buf2, &q, read); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), buf2.Bytes()) {
		t.Error("export is not deterministic")
	}
}

// Malformed function packs are refused with a sentence saying what to fix.
func TestPackFormat4Refusals(t *testing.T) {
	head := "format: 4\nmode: function\nlocked_top: [top.cpp]\nlocked_bottom: [bottom.cpp]\nstarters: [starter.cpp]\n"
	files := []entry{
		{name: "solution.cpp", body: "int twice(int n) { return 2 * n; }"},
		{name: "top.cpp", body: "#include <cstdio>\n"}, {name: "bottom.cpp", body: "int main() {}\n"},
		{name: "starter.cpp", body: "int twice(int n) {}\n"},
		{name: "tests/01.in", body: "1\n"}, {name: "tests/01.out", body: "2\n"},
		{name: "tests/02.in", body: "2\n"}, {name: "tests/02.out", body: "4\n"},
	}
	if _, err := ReadPack(zipEntries(t, append([]entry{{name: "problem.yaml", body: head + minimalYAML}}, files...)...)); err != nil {
		t.Fatalf("the valid pack: %v", err)
	}
	for name, tc := range map[string]struct {
		yaml  string
		extra []entry
		want  string
	}{
		"locked parts in format 3": {strings.Replace(head, "format: 4", "format: 3", 1), nil,
			"problem.yaml says mode: function, which needs format: 4. Add that line at the top."},
		"locked parts without the mode": {strings.Replace(head, "mode: function\n", "", 1), nil,
			"problem.yaml lists locked_top, which needs mode: function."},
		"unknown mode": {strings.Replace(head, "mode: function", "mode: half", 1), nil,
			`problem.yaml: mode must be whole or function, not "half".`},
		"missing top file": {strings.Replace(head, "[top.cpp]", "[top2.cpp]", 1), nil,
			`problem.yaml names locked_top "top2.cpp", but the pack has no such file.`},
		"two tops for one language": {strings.Replace(head, "[top.cpp]", "[top.cpp, more.cpp]", 1),
			[]entry{{name: "more.cpp", body: "#include <vector>\n"}}, "problem.yaml lists two locked_top files for cpp17; keep one."},
		"top without bottom": {strings.Replace(head, "[top.cpp]", "[top.cpp, top.py]", 1), []entry{{name: "top.py", body: "import sys\n"}},
			"problem.yaml locked_bottom: Add a locked bottom for python3, or remove its locked top and starter."},
		"top in a folder": {strings.Replace(head, "[top.cpp]", "[parts/top.cpp]", 1), nil,
			`problem.yaml: locked_top "parts/top.cpp" must be a file name ending in .cpp, .java, .py or .js, at the top of the pack.`},
	} {
		entries := append([]entry{{name: "problem.yaml", body: tc.yaml + minimalYAML}}, files...)
		_, err := ReadPack(zipEntries(t, append(entries, tc.extra...)...))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", name, err, tc.want)
		}
	}
}

// FuzzReadPackFormat4 feeds mangled problem.yaml files to the parser next to
// a function pack's files: it refuses or accepts, and never panics. Seeds
// run in every go test; longer runs are by hand (-fuzz FuzzReadPackFormat4).
func FuzzReadPackFormat4(f *testing.F) {
	head := "format: 4\nmode: function\nlocked_top: [top.cpp]\nlocked_bottom: [bottom.cpp]\nstarters: [starter.cpp]\n"
	for _, s := range []string{
		head + minimalYAML, "format: 4\nmode: function\n" + minimalYAML, "format: 4\nlocked_top: [top.cpp]\n" + minimalYAML,
		"format: 4\nmode: [function]\n" + minimalYAML, "format: 4\nlocked_top: top.cpp\n" + minimalYAML,
		"format: 4\nlocked_bottom: [bottom.cpp, bottom.cpp]\nmode: function\n" + minimalYAML, "format: 4\nmode: function\nlocked_top: [{a: 1}]\n",
		"format: 4\nmode: function\nlocked_top: [../top.cpp]\nlocked_bottom: [bottom.cpp]\n" + minimalYAML, "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, yaml string) {
		data := zipEntries(t, entry{name: "problem.yaml", body: yaml}, entry{name: "solution.cpp", body: "int f() { return 1; }"},
			entry{name: "top.cpp", body: "#include <cstdio>\n"}, entry{name: "bottom.cpp", body: "int main() {}\n"},
			entry{name: "starter.cpp", body: "int f() {}\n"}, entry{name: "tests/01.in", body: "1\n"},
			entry{name: "tests/01.out", body: "1\n"}, entry{name: "tests/02.in", body: "2\n"})
		pk, err := ReadPack(data)
		if err == nil && pk.Problem.IsFunction() && len(pk.Problem.LockedBottom) == 0 {
			t.Errorf("accepted a function problem with no locked code: %q", yaml)
		}
	})
}

// The example function pack (problems/array-sum) offers all four
// languages, its Java bottom is public class Main and its stub a
// non-public class Solution, every part composes into one file, and it
// survives an export and an import as format 4.
func TestExampleFunctionPack(t *testing.T) {
	pk, err := ReadPack(zipDir(t, filepath.Join(examples, "array-sum")))
	if err != nil {
		t.Fatal(err)
	}
	p := &pk.Problem
	if !p.IsFunction() || strings.Join(p.Offered(), ",") != "cpp17,java21,node22,python3" {
		t.Fatalf("mode %q offers %v", p.Mode, p.Offered())
	}
	if !strings.Contains(p.LockedBottom["java21"], "public class Main") || !strings.Contains(p.Starters["java21"], "class Solution") ||
		strings.Contains(p.Starters["java21"], "public class") || !strings.Contains(p.LockedBottom["java21"], "new Scanner(System.in)") {
		t.Errorf("java parts: bottom %q stub %q", p.LockedBottom["java21"], p.Starters["java21"])
	}
	for _, lang := range p.Offered() {
		c := p.ComposeFor(lang, p.Starters[lang])
		if c.TopLines < 1 || !strings.HasPrefix(c.Source, p.LockedTop[lang]) || !strings.HasSuffix(c.Source, p.LockedBottom[lang]) {
			t.Errorf("%s composes as %q", lang, c.Source)
		}
	}
	blobs := map[string][]byte{}
	for i := range p.Tests {
		in, out := pk.Inputs[p.Tests[i].Idx], pk.Outputs[p.Tests[i].Idx]
		p.Tests[i].InputSHA, p.Tests[i].OutputSHA = SHA(in), SHA(out)
		blobs[SHA(in)], blobs[SHA(out)] = in, out
	}
	var buf bytes.Buffer
	if err := WritePack(&buf, p, func(sha string) ([]byte, error) { return blobs[sha], nil }); err != nil {
		t.Fatal(err)
	}
	if y := packYAML(t, buf.Bytes()); !strings.HasPrefix(y, "format: 4\nmode: function\n") {
		t.Errorf("exported problem.yaml:\n%s", y)
	}
	again, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	q := again.Problem
	for i := range q.Tests {
		q.Tests[i].InputSHA, q.Tests[i].OutputSHA = SHA(again.Inputs[q.Tests[i].Idx]), SHA(again.Outputs[q.Tests[i].Idx])
	}
	if ContentHash(&q) != ContentHash(p) {
		t.Error("export and import changed the content hash")
	}
}
