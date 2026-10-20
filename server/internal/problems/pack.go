package problems

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// Pack import limits (decision D46 as amended: sizes are checked against
// the bytes actually decompressed rather than a compression ratio, which
// would refuse legitimate repetitive tests). Packs come from setters and,
// from M5, from a model; the server reads them outside any sandbox.
const (
	MaxPackBytes      = 32 << 20  // the zip itself
	MaxPackUnpacked   = 128 << 20 // all files once decompressed
	MaxPackEntries    = 2000
	MaxPackFile       = 16 << 20
	MaxProblemYAML    = 64 << 10
	defaultTimeLimit  = 1000
	defaultMemoryMB   = 256
	packCheckerSource = "checker.cpp"
)

// PackError lists everything wrong with a pack, so a setter can fix it in
// one pass.
type PackError struct{ Problems []string }

func (e *PackError) Error() string { return strings.Join(e.Problems, " ") }

// Pack is a parsed problem pack.
type Pack struct {
	Problem Problem
	// Inputs and Outputs hold the test files by test number.
	Inputs  map[int][]byte
	Outputs map[int][]byte
	// Ignored lists files the pack carried that podium does not use yet.
	Ignored []string
}

// PackFormat is the newest pack format this server reads and writes
// (B1.7). A problem.yaml without a format line is format 1; one with a
// higher number was written by a newer Podium and is refused, rather than
// read with fields silently ignored. Format 2 (v1.2.0, D115) adds open
// tests; exports write format 1 when a problem has none, so an older
// server can still import them.
const PackFormat = 2

// packFormatOpen is the first format with open tests.
const packFormatOpen = 2

// packMeta is problem.yaml.
type packMeta struct {
	Format        int           `yaml:"format,omitempty"`
	Title         string        `yaml:"title"`
	Slug          string        `yaml:"slug"`
	TimeLimitMS   int           `yaml:"time_limit_ms"`
	MemoryLimitMB int           `yaml:"memory_limit_mb"`
	Checker       string        `yaml:"checker"`
	Epsilon       float64       `yaml:"epsilon,omitempty"`
	Tags          []string      `yaml:"tags,omitempty"`
	Difficulty    string        `yaml:"difficulty,omitempty"`
	Reference     string        `yaml:"reference"`
	Brute         string        `yaml:"brute,omitempty"`
	Validator     string        `yaml:"validator,omitempty"`
	Generator     string        `yaml:"generator,omitempty"`
	Samples       []int         `yaml:"samples,flow"`
	Open          []int         `yaml:"open,omitempty,flow"`
	Subtasks      []packSubtask `yaml:"subtasks"`
}

type packSubtask struct {
	Points        int      `yaml:"points"`
	Title         string   `yaml:"title,omitempty"`
	Tests         string   `yaml:"tests"`
	GeneratorArgs []string `yaml:"generator_args,omitempty,flow"`
}

// packPlanFile lists the generator's planned tests: a YAML list of
// {test, subtask, seed, args}.
const packPlanFile = "tests.yaml"

var (
	testFile   = regexp.MustCompile(`^tests/0*([1-9][0-9]{0,2})\.(in|out)$`)
	sourceFile = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}\.(cpp|java|py|js)$`)
)

// ReadPack parses a pack zip.
func ReadPack(data []byte) (*Pack, error) {
	files, err := readZip(data)
	if err != nil {
		return nil, err
	}
	return parsePackFiles(files)
}

// readZip unpacks a zip into memory. Every entry must be a regular file or
// a directory at a clean relative path; sizes are checked against what is
// actually decompressed, never against the zip's own headers. A zip made
// from one top-level folder is read as if zipped from inside it. Hidden
// files and macOS resource forks are skipped.
func readZip(data []byte) (map[string][]byte, error) {
	if len(data) > MaxPackBytes {
		return nil, &PackError{[]string{"The pack is larger than 32 MB."}}
	}
	if n, ok := zipEntryCount(data); ok && n > MaxPackEntries {
		return nil, &PackError{[]string{fmt.Sprintf("The pack has more than %d files.", MaxPackEntries)}}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, &PackError{[]string{"The file is not a zip archive."}}
	}
	if len(zr.File) > MaxPackEntries {
		return nil, &PackError{[]string{fmt.Sprintf("The pack has more than %d files.", MaxPackEntries)}}
	}
	var bad []string
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		name := f.Name
		mode := f.Mode()
		switch {
		case strings.Contains(name, `\`), strings.HasPrefix(name, "/"), strings.Contains(name, ":"):
			bad = append(bad, fmt.Sprintf("%q is not a relative path.", name))
		case path.Clean(strings.TrimSuffix(name, "/")) != strings.TrimSuffix(name, "/") || hasDotDot(name):
			bad = append(bad, fmt.Sprintf("%q is not a clean path.", name))
		case !mode.IsRegular() && !mode.IsDir():
			bad = append(bad, fmt.Sprintf("%q is not a regular file (links and devices are refused).", name))
		}
		names = append(names, name)
	}
	if len(bad) > 0 {
		return nil, &PackError{bad}
	}
	prefix := commonFolder(names)

	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		if f.Mode().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, prefix)
		if strings.HasPrefix(path.Base(name), ".") || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, &PackError{[]string{fmt.Sprintf("%q cannot be read: %v.", name, err)}}
		}
		// Read one byte past the cap to notice a file that lies about its
		// size in the zip headers.
		b, err := io.ReadAll(io.LimitReader(rc, MaxPackFile+1))
		rc.Close()
		if err != nil {
			return nil, &PackError{[]string{fmt.Sprintf("%q is damaged: %v.", name, err)}}
		}
		if len(b) > MaxPackFile {
			return nil, &PackError{[]string{fmt.Sprintf("%q is larger than 16 MB.", name)}}
		}
		total += int64(len(b))
		if total > MaxPackUnpacked {
			return nil, &PackError{[]string{"The pack holds more than 128 MB once unzipped."}}
		}
		if _, dup := files[name]; dup {
			return nil, &PackError{[]string{fmt.Sprintf("%q appears twice.", name)}}
		}
		files[name] = b
	}
	return files, nil
}

func parsePackFiles(files map[string][]byte) (*Pack, error) {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	raw, ok := files["problem.yaml"]
	if !ok {
		return nil, &PackError{[]string{"The pack has no problem.yaml at its top level."}}
	}
	if len(raw) > MaxProblemYAML {
		return nil, &PackError{[]string{"problem.yaml is larger than 64 KB."}}
	}
	// The format is read first and alone: a newer pack's unknown fields
	// would otherwise be reported one by one, hiding the real cause.
	var head struct {
		Format int `yaml:"format"`
	}
	if err := yaml.Unmarshal(raw, &head); err == nil && head.Format > PackFormat {
		return nil, &PackError{[]string{fmt.Sprintf("problem.yaml is pack format %d, and this server reads format %d and older. Upgrade Podium, or export the problem again from a server of this version.", head.Format, PackFormat)}}
	}
	var meta packMeta
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&meta); err != nil {
		return nil, &PackError{[]string{"problem.yaml: " + yamlMessage(err)}}
	}
	if meta.Format < 0 {
		return nil, &PackError{[]string{fmt.Sprintf("problem.yaml: format must be 1 or %d (or left out).", PackFormat)}}
	}
	if len(meta.Open) > 0 && meta.Format < packFormatOpen {
		return nil, &PackError{[]string{fmt.Sprintf("problem.yaml lists open tests, which need format: %d. Add that line at the top.", packFormatOpen)}}
	}

	pk := &Pack{Inputs: map[int][]byte{}, Outputs: map[int][]byte{}}
	p := &pk.Problem
	p.Title, p.Slug, p.Origin = meta.Title, meta.Slug, OriginImport
	p.TimeLimitMS = orDefault(meta.TimeLimitMS, defaultTimeLimit)
	p.MemoryLimitMB = orDefault(meta.MemoryLimitMB, defaultMemoryMB)
	p.Checker = meta.Checker
	if p.Checker == "" {
		p.Checker = CheckerTokens
	}
	p.Epsilon = meta.Epsilon
	if p.Epsilon == 0 {
		p.Epsilon = judge.DefaultEpsilon
	}
	p.Tags = meta.Tags
	p.Difficulty = meta.Difficulty
	p.Samples = meta.Samples
	p.Open = nonNilInts(meta.Open)
	for _, st := range meta.Subtasks {
		p.Subtasks = append(p.Subtasks, Subtask{Points: st.Points, Title: st.Title, Tests: st.Tests, GeneratorArgs: st.GeneratorArgs})
	}
	if s, ok := files["statement.md"]; ok {
		p.Statement = string(s)
	}

	used := map[string]bool{"problem.yaml": true, "statement.md": true}
	source := func(field, name string) *Source {
		if name == "" {
			return nil
		}
		if !sourceFile.MatchString(name) {
			add("problem.yaml: %s %q must be a file name ending in .cpp, .java, .py or .js, at the top of the pack.", field, name)
			return nil
		}
		body, ok := files[name]
		if !ok {
			add("problem.yaml names %s %q, but the pack has no such file.", field, name)
			return nil
		}
		used[name] = true
		return &Source{Language: LanguageByExt[path.Ext(name)], Source: string(body)}
	}
	if meta.Reference == "" {
		add("problem.yaml must name the reference solution, for example reference: solution.cpp.")
	} else if s := source("reference", meta.Reference); s != nil {
		p.Reference = *s
	}
	p.Brute = source("brute", meta.Brute)
	p.Validator = source("validator", meta.Validator)
	p.Generator = source("generator", meta.Generator)
	if raw, ok := files[packPlanFile]; ok {
		used[packPlanFile] = true
		if len(raw) > MaxProblemYAML {
			add("%s is larger than 64 KB.", packPlanFile)
		} else {
			dec := yaml.NewDecoder(bytes.NewReader(raw))
			dec.KnownFields(true)
			if err := dec.Decode(&p.Plan); err != nil && err != io.EOF {
				add("%s: %s", packPlanFile, yamlMessage(err))
			}
		}
	}
	if p.Checker == CheckerCustom {
		if s, ok := files[packCheckerSource]; ok {
			p.CheckerSource = string(s)
			used[packCheckerSource] = true
		} else {
			add("The checker is custom, so the pack needs checker.cpp.")
		}
	}

	for name, body := range files {
		m := testFile.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		used[name] = true
		n, _ := strconv.Atoi(m[1])
		target := pk.Inputs
		if m[2] == "out" {
			target = pk.Outputs
		}
		if _, dup := target[n]; dup {
			add("Test %d has two %s files (check zero padding).", n, m[2])
		}
		target[n] = body
	}
	for n := range pk.Outputs {
		if _, ok := pk.Inputs[n]; !ok {
			add("Test %d has an output but no input.", n)
		}
	}
	if len(pk.Inputs) == 0 && len(p.Plan) == 0 {
		add("The pack has no tests. Put them in tests/ as 01.in and 01.out, or plan them in tests.yaml with a generator.")
	}
	// A test may arrive without its output; "Generate outputs" fills it
	// from the reference solution before validation.
	for n, in := range pk.Inputs {
		t := Test{Idx: n, InputSHA: SHA(in), InputSize: int64(len(in))}
		if out, ok := pk.Outputs[n]; ok {
			t.OutputSHA, t.OutputSize = SHA(out), int64(len(out))
		}
		p.Tests = append(p.Tests, t)
	}
	sort.Slice(p.Tests, func(i, j int) bool { return p.Tests[i].Idx < p.Tests[j].Idx })

	for name := range files {
		if !used[name] {
			pk.Ignored = append(pk.Ignored, name)
		}
	}
	sort.Strings(pk.Ignored)

	if errs := checkFields(p); errs != nil {
		keys := make([]string, 0, len(errs))
		for k := range errs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			add("problem.yaml %s: %s", packField(k), errs[k])
		}
	}
	if len(problems) == 0 {
		// Missing outputs are filled after import, and planned tests are
		// made by Generate inputs; everything else must be right in the
		// pack.
		problems = append(problems, withPlanned(p).StructureProblems()...)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, &PackError{problems}
	}
	return pk, nil
}

// withPlanned returns a copy of p in which every planned test exists, for
// checking the structure of a pack whose generator has not run yet.
func withPlanned(p *Problem) *Problem {
	q := *p
	q.Tests = append([]Test{}, p.Tests...)
	have := map[int]bool{}
	for _, t := range p.Tests {
		have[t.Idx] = true
	}
	for _, t := range p.Plan {
		if !have[t.Test] {
			q.Tests = append(q.Tests, Test{Idx: t.Test})
		}
	}
	sort.Slice(q.Tests, func(i, j int) bool { return q.Tests[i].Idx < q.Tests[j].Idx })
	return &q
}

// packField names a field the way problem.yaml spells it.
func packField(k string) string {
	switch {
	case k == "statement_md":
		return "statement.md"
	case k == "checker_source":
		return "checker.cpp"
	case k == "plan":
		return packPlanFile
	case strings.HasPrefix(k, "subtasks."):
		parts := strings.Split(k, ".")
		n, _ := strconv.Atoi(parts[1])
		return fmt.Sprintf("subtask %d %s", n+1, parts[2])
	}
	return k
}

func yamlMessage(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "yaml: ")
	return strings.ReplaceAll(msg, "unmarshal errors:\n  ", "")
}

func hasDotDot(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." {
			return true
		}
	}
	return false
}

// commonFolder returns "dir/" when every entry sits under one top-level
// folder and there is no problem.yaml at the root.
func commonFolder(names []string) string {
	first := ""
	for _, n := range names {
		if n == "problem.yaml" {
			return ""
		}
		top, _, nested := strings.Cut(n, "/")
		if !nested && !strings.HasSuffix(n, "/") {
			return ""
		}
		if strings.HasPrefix(top, "__MACOSX") {
			continue
		}
		if first == "" {
			first = top
		} else if top != first {
			return ""
		}
	}
	if first == "" {
		return ""
	}
	return first + "/"
}

func orDefault(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}

// WritePack writes p as a pack. read returns a blob's bytes. The archive is
// deterministic: fixed file order and timestamps, so exporting twice gives
// the same bytes.
func WritePack(w io.Writer, p *Problem, read func(sha string) ([]byte, error)) error {
	zw := zip.NewWriter(w)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	put := func(name string, body []byte) error {
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: stamp})
		if err != nil {
			return err
		}
		_, err = fw.Write(body)
		return err
	}
	meta := packMeta{
		Format: 1,
		Title:  p.Title, Slug: p.Slug, TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB,
		Checker: p.Checker, Tags: p.Tags, Difficulty: p.Difficulty, Samples: p.Samples,
		Reference: "solution" + ExtByLanguage[p.Reference.Language],
	}
	if len(p.Open) > 0 {
		meta.Format, meta.Open = packFormatOpen, p.Open
	}
	if p.Checker == CheckerFloat {
		meta.Epsilon = p.Epsilon
	}
	files := []struct {
		name string
		body []byte
	}{}
	files = append(files, struct {
		name string
		body []byte
	}{meta.Reference, []byte(p.Reference.Source)})
	if p.Brute != nil {
		meta.Brute = "brute" + ExtByLanguage[p.Brute.Language]
		files = append(files, packFile{meta.Brute, []byte(p.Brute.Source)})
	}
	if p.Validator != nil {
		meta.Validator = "validator" + ExtByLanguage[p.Validator.Language]
		files = append(files, packFile{meta.Validator, []byte(p.Validator.Source)})
	}
	if p.Checker == CheckerCustom {
		files = append(files, packFile{packCheckerSource, []byte(p.CheckerSource)})
	}
	if p.Generator != nil {
		meta.Generator = "generator" + ExtByLanguage[p.Generator.Language]
		files = append(files, packFile{meta.Generator, []byte(p.Generator.Source)})
		if len(p.Plan) > 0 {
			var plan bytes.Buffer
			penc := yaml.NewEncoder(&plan)
			penc.SetIndent(2)
			if err := penc.Encode(p.Plan); err != nil {
				return err
			}
			penc.Close()
			files = append(files, packFile{packPlanFile, plan.Bytes()})
		}
	}
	for _, st := range p.Subtasks {
		meta.Subtasks = append(meta.Subtasks, packSubtask{Points: st.Points, Title: st.Title, Tests: st.Tests, GeneratorArgs: st.GeneratorArgs})
	}
	var y bytes.Buffer
	enc := yaml.NewEncoder(&y)
	enc.SetIndent(2)
	if err := enc.Encode(meta); err != nil {
		return err
	}
	enc.Close()
	if err := put("problem.yaml", y.Bytes()); err != nil {
		return err
	}
	if err := put("statement.md", []byte(p.Statement)); err != nil {
		return err
	}
	for _, f := range files {
		if err := put(f.name, f.body); err != nil {
			return err
		}
	}
	width := 2
	if len(p.Tests) > 0 && p.Tests[len(p.Tests)-1].Idx >= 100 {
		width = 3
	}
	for _, t := range p.Tests {
		for _, part := range []struct{ ext, sha string }{{"in", t.InputSHA}, {"out", t.OutputSHA}} {
			if part.sha == "" {
				continue // output not generated yet
			}
			body, err := read(part.sha)
			if err != nil {
				return fmt.Errorf("test %d %s: %w", t.Idx, part.ext, err)
			}
			if err := put(fmt.Sprintf("tests/%0*d.%s", width, t.Idx, part.ext), body); err != nil {
				return err
			}
		}
	}
	return zw.Close()
}

type packFile struct {
	name string
	body []byte
}
