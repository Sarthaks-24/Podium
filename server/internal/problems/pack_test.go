package problems

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// examples is the repository's example packs directory.
const examples = "../../../problems"

// zipDir zips a pack folder the way a setter would, from inside it.
func zipDir(t *testing.T, dir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}

type entry struct {
	name string
	body string
	mode fs.FileMode
}

func zipEntries(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	return buf.Bytes()
}

const minimalYAML = `title: Tiny
slug: tiny
reference: solution.cpp
samples: [1]
subtasks:
  - points: 100
    tests: "2"
`

func minimalPack(extra ...entry) []entry {
	return append([]entry{
		{name: "problem.yaml", body: minimalYAML},
		{name: "solution.cpp", body: "int main(){}"},
		{name: "tests/01.in", body: "1\n"}, {name: "tests/01.out", body: "1\n"},
		{name: "tests/2.in", body: "2\n"}, {name: "tests/002.out", body: "2\n"},
	}, extra...)
}

func TestExamplePacksImport(t *testing.T) {
	for slug, want := range map[string]struct {
		checker, lang string
		tests         int
		brute, valid  bool
	}{
		"a-plus-b":      {"tokens", "cpp17", 6, true, true},
		"average-speed": {"float", "java21", 7, true, true},
		"pair-with-sum": {"custom", "cpp17", 6, true, false},
	} {
		pk, err := ReadPack(zipDir(t, filepath.Join(examples, slug)))
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		p := pk.Problem
		if p.Slug != slug || p.Checker != want.checker || p.Reference.Language != want.lang || len(p.Tests) != want.tests ||
			(p.Brute != nil) != want.brute || (p.Validator != nil) != want.valid {
			t.Errorf("%s: got slug %s checker %s lang %s tests %d brute %v validator %v", slug, p.Slug, p.Checker,
				p.Reference.Language, len(p.Tests), p.Brute != nil, p.Validator != nil)
		}
		if probs := p.Problems(); len(probs) != 0 {
			t.Errorf("%s: problems %v", slug, probs)
		}
		if p.Checker == CheckerCustom && !strings.Contains(p.CheckerSource, "registerTestlibCmd") {
			t.Errorf("%s: checker source not loaded", slug)
		}
	}
}

func TestPackInsideOneFolder(t *testing.T) {
	var entries []entry
	for _, e := range minimalPack() {
		e.name = "tiny/" + e.name
		entries = append(entries, e)
	}
	entries = append(entries, entry{name: "__MACOSX/tiny/._problem.yaml", body: "junk"})
	pk, err := ReadPack(zipEntries(t, entries...))
	if err != nil {
		t.Fatal(err)
	}
	if len(pk.Problem.Tests) != 2 || pk.Problem.Tests[1].Idx != 2 {
		t.Errorf("tests %+v", pk.Problem.Tests)
	}
}

func TestPackRefusals(t *testing.T) {
	big := strings.Repeat("0", MaxPackFile+1)
	for name, tc := range map[string]struct {
		entries []entry
		want    string
	}{
		"zip slip":             {minimalPack(entry{name: "../evil.txt", body: "x"}), "not a clean path"},
		"absolute path":        {minimalPack(entry{name: "/etc/passwd", body: "x"}), "not a relative path"},
		"backslash":            {minimalPack(entry{name: `tests\..\x`, body: "x"}), "not a relative path"},
		"drive letter":         {minimalPack(entry{name: "C:/x", body: "x"}), "not a relative path"},
		"symlink":              {minimalPack(entry{name: "link", body: "/etc/shadow", mode: fs.ModeSymlink | 0o777}), "not a regular file"},
		"oversized file":       {minimalPack(entry{name: "tests/03.in", body: big}), "larger than 16 MB"},
		"no problem.yaml":      {[]entry{{name: "solution.cpp", body: "x"}}, "no problem.yaml"},
		"unknown yaml key":     {[]entry{{name: "problem.yaml", body: minimalYAML + "generator: gen.py\n"}}, "generator"},
		"output without input": {minimalPack(entry{name: "tests/09.out", body: "9"}), "output but no input"},
		"test in no subtask":   {minimalPack(entry{name: "tests/03.in", body: "3"}, entry{name: "tests/03.out", body: "3"}), "Test 3 is in no subtask"},
		"missing reference": {[]entry{{name: "problem.yaml", body: strings.Replace(minimalYAML, "solution.cpp", "sol.cpp", 1)},
			{name: "tests/01.in", body: "1"}, {name: "tests/01.out", body: "1"}}, "no such file"},
		"bad slug": {[]entry{{name: "problem.yaml", body: strings.Replace(minimalYAML, "slug: tiny", "slug: Tiny Problem", 1)},
			{name: "solution.cpp", body: "x"}, {name: "tests/01.in", body: "1"}, {name: "tests/01.out", body: "1"},
			{name: "tests/02.in", body: "2"}, {name: "tests/02.out", body: "2"}}, "slug"},
		"duplicate test number": {minimalPack(entry{name: "tests/1.in", body: "x"}), "two in files"},
		// B1.7: a pack from a newer Podium is refused by its format, with
		// the version named, even though it also has fields unknown here.
		"newer format": {[]entry{{name: "problem.yaml", body: "format: 3\n" + minimalYAML + "hints: [3]\n"}}, "pack format 3, and this server reads format 2"},
	} {
		_, err := ReadPack(zipEntries(t, tc.entries...))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want an error containing %q", name, err, tc.want)
		}
	}
	if _, err := ReadPack([]byte("not a zip")); err == nil || !strings.Contains(err.Error(), "not a zip") {
		t.Errorf("garbage: %v", err)
	}
}

func TestPackRefusesTooMuchUnpacked(t *testing.T) {
	// Nine 15 MB files compress to almost nothing but unpack past 128 MB.
	chunk := strings.Repeat("0", 15<<20)
	entries := minimalPack()
	for i := 3; i <= 11; i++ {
		entries = append(entries, entry{name: "extra" + string(rune('a'+i)) + ".txt", body: chunk})
	}
	_, err := ReadPack(zipEntries(t, entries...))
	if err == nil || !strings.Contains(err.Error(), "128 MB") {
		t.Errorf("got %v", err)
	}
}

func TestPackRoundTrip(t *testing.T) {
	for _, slug := range []string{"a-plus-b", "average-speed", "pair-with-sum"} {
		pk, err := ReadPack(zipDir(t, filepath.Join(examples, slug)))
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
		var buf bytes.Buffer
		if err := WritePack(&buf, &p, func(sha string) ([]byte, error) { return blobs[sha], nil }); err != nil {
			t.Fatal(err)
		}
		again, err := ReadPack(buf.Bytes())
		if err != nil {
			t.Fatalf("%s: re-import: %v", slug, err)
		}
		q := again.Problem
		for i := range q.Tests {
			q.Tests[i].InputSHA, q.Tests[i].OutputSHA = SHA(again.Inputs[q.Tests[i].Idx]), SHA(again.Outputs[q.Tests[i].Idx])
		}
		if ContentHash(&p) != ContentHash(&q) || p.Title != q.Title || p.Statement != q.Statement {
			t.Errorf("%s: round trip changed the problem", slug)
		}
		var buf2 bytes.Buffer
		WritePack(&buf2, &q, func(sha string) ([]byte, error) {
			for n, b := range again.Inputs {
				if SHA(b) == sha {
					return again.Inputs[n], nil
				}
			}
			for n, b := range again.Outputs {
				if SHA(b) == sha {
					return again.Outputs[n], nil
				}
			}
			return nil, os.ErrNotExist
		})
		if !bytes.Equal(buf.Bytes(), buf2.Bytes()) {
			t.Errorf("%s: export is not deterministic", slug)
		}
		// Exports write the format: 1, or 2 for a problem with open tests.
		wantFormat := "format: 1\n"
		if len(p.Open) > 0 {
			wantFormat = "format: 2\n"
		}
		zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		for _, f := range zr.File {
			if path.Base(f.Name) == "problem.yaml" {
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				if !strings.HasPrefix(string(b), wantFormat) {
					t.Errorf("%s: exported problem.yaml starts %q", slug, clip(string(b)))
				}
			}
		}
	}
}

func TestRanges(t *testing.T) {
	for in, want := range map[string]string{"3-5, 8": "3-5,8", "1,2,3": "1-3", "7": "7", "": "", " 2 - 4 ,9-9": "2-4,9"} {
		nums, err := ParseRanges(in)
		if err != nil || FormatRanges(nums) != want {
			t.Errorf("%q: %v %q", in, err, FormatRanges(nums))
		}
	}
	for _, bad := range []string{"5-3", "0", "1000", "a", "1-", "1,1", "2-4,3"} {
		if _, err := ParseRanges(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestContentHashCoversJudgingOnly(t *testing.T) {
	pk, err := ReadPack(zipEntries(t, minimalPack()...))
	if err != nil {
		t.Fatal(err)
	}
	p := pk.Problem
	base := ContentHash(&p)
	same := p
	same.Title, same.Statement, same.Tags = "Other", "New text", []string{"x"}
	if ContentHash(&same) != base {
		t.Error("statement edits changed the hash")
	}
	for name, change := range map[string]func(q *Problem){
		"time limit": func(q *Problem) { q.TimeLimitMS++ },
		"test":       func(q *Problem) { q.Tests = append([]Test{}, q.Tests...); q.Tests[0].OutputSHA = SHA([]byte("x")) },
		"reference":  func(q *Problem) { q.Reference.Source += " " },
		"subtask":    func(q *Problem) { q.Subtasks = []Subtask{{Idx: 1, Points: 50, Tests: "2"}} },
		"samples":    func(q *Problem) { q.Samples = []int{} },
		"brute":      func(q *Problem) { q.Brute = &Source{"python3", "print()"} },
	} {
		q := p
		change(&q)
		if ContentHash(&q) == base {
			t.Errorf("%s did not change the hash", name)
		}
	}
}

func TestPackRefusesHugeDirectoryBeforeReadingIt(t *testing.T) {
	entries := minimalPack()
	for i := 0; i < MaxPackEntries; i++ {
		entries = append(entries, entry{name: "f/" + strings.Repeat("x", 1) + jsonInt(i)})
	}
	data := zipEntries(t, entries...)
	if n, ok := zipEntryCount(data); !ok || n != uint64(len(entries)) {
		t.Fatalf("entry count %d %v, want %d", n, ok, len(entries))
	}
	if _, err := ReadPack(data); err == nil || !strings.Contains(err.Error(), "more than 2000 files") {
		t.Errorf("got %v", err)
	}
}

func jsonInt(i int) string { return strconv.Itoa(i) }

func TestPackMayOmitOutputs(t *testing.T) {
	pk, err := ReadPack(zipEntries(t, entry{name: "problem.yaml", body: minimalYAML}, entry{name: "solution.cpp", body: "x"},
		entry{name: "tests/01.in", body: "1"}, entry{name: "tests/01.out", body: "1"}, entry{name: "tests/02.in", body: "2"}))
	if err != nil {
		t.Fatal(err)
	}
	if pk.Problem.Tests[1].OutputSHA != "" || pk.Problem.Tests[0].OutputSHA != SHA([]byte("1")) {
		t.Errorf("tests %+v", pk.Problem.Tests)
	}
	probs := strings.Join(pk.Problem.Problems(), " ")
	if !strings.Contains(probs, "Test 2 has no expected output") {
		t.Errorf("missing output not listed: %s", probs)
	}
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

// A pack without a format line is format 1, and so is one that says so.
func TestPackFormatOne(t *testing.T) {
	for _, head := range []string{"", "format: 1\n"} {
		if _, err := ReadPack(zipEntries(t, minimalPack()...)); err != nil {
			t.Fatalf("no format: %v", err)
		}
		entries := minimalPack()
		for i := range entries {
			if entries[i].name == "problem.yaml" {
				entries[i].body = head + entries[i].body
			}
		}
		if _, err := ReadPack(zipEntries(t, entries...)); err != nil {
			t.Errorf("%q: %v", head, err)
		}
	}
}
