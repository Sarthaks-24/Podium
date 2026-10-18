// Package problems is the problem bank: problems with their statements,
// limits, checkers, solutions and tests; the problem pack format for import
// and export; the content hash that decides whether a problem is
// validated; and the validation gate, which runs on the real judge through
// the jobs queue (decisions D44 to D46). Docs: docs/AUTHORING.md for the
// pack format and rules, docs/API.md for the routes.
package problems

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// Limits on what a problem may hold.
const (
	MinTimeLimitMS   = 100
	MaxTimeLimitMS   = 10000
	MinMemoryLimitMB = 16
	MaxMemoryLimitMB = 1024
	MaxTitle         = 120
	MaxStatement     = 64 << 10
	MaxSource        = 64 << 10
	MaxTests         = 999
	MaxSubtasks      = 50
	MaxPoints        = 1000
	MaxTags          = 10
	MaxTag           = 32
)

// Checker names.
const (
	CheckerTokens = string(judge.CheckTokens)
	CheckerExact  = string(judge.CheckExact)
	CheckerFloat  = string(judge.CheckFloat)
	CheckerCustom = string(judge.CheckCustom)
)

// Origins.
const (
	OriginManual = "manual"
	OriginImport = "import"
	// OriginAI marks a draft the AI wizard made (M5).
	OriginAI = "ai"
)

// Problem is one problem in the bank.
type Problem struct {
	ID            int64
	Slug          string
	Title         string
	Statement     string
	TimeLimitMS   int
	MemoryLimitMB int
	Checker       string
	Epsilon       float64
	CheckerSource string
	Reference     Source
	// Brute and Validator are nil when the problem has none.
	Brute     *Source
	Validator *Source
	// Generator, when set, makes the inputs of the tests in Plan (M5). It
	// is setter code like the others and runs only inside isolate.
	Generator  *Source
	Plan       []PlannedTest
	Tags       []string
	Difficulty string
	Origin     string
	AuthorID   int64
	Archived   bool
	// Samples are test numbers shown in the statement; they carry no points.
	Samples  []int
	Subtasks []Subtask
	Tests    []Test

	ContentHash   string
	ValidatedHash string
	ValidatedAt   time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Validated reports whether the last passing validation covers the problem
// as it is now.
func (p *Problem) Validated() bool {
	return p.ValidatedHash != "" && p.ValidatedHash == p.ContentHash
}

// Source is a program in one of the judge's languages.
type Source struct {
	Language string
	Source   string
}

// Subtask is a group of tests scored all or nothing. Tests is the range
// list as written ("3-5,8"). GeneratorArgs are the arguments the
// generator gets when tests are planned for this subtask.
type Subtask struct {
	Idx           int
	Points        int
	Title         string
	Tests         string
	GeneratorArgs []string
}

// Test is one test case, by the hashes of its files. OutputSHA is empty
// while the expected output is missing (generated from the reference).
type Test struct {
	Idx        int
	InputSHA   string
	OutputSHA  string
	InputSize  int64
	OutputSize int64
}

// LanguageByExt maps a source file extension to a judge language id.
var LanguageByExt = map[string]string{
	".cpp":  "cpp17",
	".java": "java21",
	".py":   "python3",
	".js":   "node22",
}

// ExtByLanguage is the reverse of LanguageByExt, used for export.
var ExtByLanguage = map[string]string{
	"cpp17":   ".cpp",
	"java21":  ".java",
	"python3": ".py",
	"node22":  ".js",
}

// FieldErrors maps a field name to a sentence saying what to fix.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	parts := make([]string, 0, len(f))
	for _, msg := range f {
		parts = append(parts, msg)
	}
	return strings.Join(parts, " ")
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// checkFields applies the per-field rules shared by the editor and pack
// import. Cross-checks against the tests are in Problems().
func checkFields(p *Problem) FieldErrors {
	errs := FieldErrors{}
	p.Title = strings.TrimSpace(p.Title)
	switch {
	case p.Title == "":
		errs["title"] = "Enter a title."
	case utf8.RuneCountInString(p.Title) > MaxTitle:
		errs["title"] = fmt.Sprintf("Keep the title under %d characters.", MaxTitle)
	}
	if len(p.Slug) > 64 || !slugPattern.MatchString(p.Slug) {
		errs["slug"] = "Use lowercase letters, digits and single hyphens, like two-sum."
	}
	if len(p.Statement) > MaxStatement {
		errs["statement_md"] = "The statement is longer than 64 KB. Shorten it or move data into tests."
	}
	if p.TimeLimitMS < MinTimeLimitMS || p.TimeLimitMS > MaxTimeLimitMS {
		errs["time_limit_ms"] = fmt.Sprintf("Set a time limit from %d to %d ms.", MinTimeLimitMS, MaxTimeLimitMS)
	}
	if p.MemoryLimitMB < MinMemoryLimitMB || p.MemoryLimitMB > MaxMemoryLimitMB {
		errs["memory_limit_mb"] = fmt.Sprintf("Set a memory limit from %d to %d MB.", MinMemoryLimitMB, MaxMemoryLimitMB)
	}
	switch p.Checker {
	case CheckerTokens, CheckerExact:
	case CheckerFloat:
		if !(p.Epsilon > 0 && p.Epsilon <= 0.1) {
			errs["epsilon"] = "Set the allowed error above 0 and at most 0.1, for example 1e-6."
		}
	case CheckerCustom:
		if strings.TrimSpace(p.CheckerSource) == "" {
			errs["checker_source"] = "Paste the checker's C++ source, or choose another checker."
		}
	default:
		errs["checker"] = "Choose tokens, exact, float or custom."
	}
	if p.Checker != CheckerCustom {
		p.CheckerSource = ""
	}
	if p.Checker != CheckerFloat {
		p.Epsilon = judge.DefaultEpsilon
	}
	if len(p.CheckerSource) > MaxSource {
		errs["checker_source"] = "The checker is longer than 64 KB."
	}
	checkSource(errs, "reference", &p.Reference, true)
	if p.Brute != nil {
		checkSource(errs, "brute", p.Brute, false)
	}
	if p.Validator != nil {
		checkSource(errs, "validator", p.Validator, false)
	}
	if p.Generator != nil {
		checkSource(errs, "generator", p.Generator, false)
	}
	if len(p.Tags) > MaxTags {
		errs["tags"] = fmt.Sprintf("Use at most %d tags.", MaxTags)
	}
	for i, t := range p.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		p.Tags[i] = t
		if t == "" || len(t) > MaxTag {
			errs["tags"] = fmt.Sprintf("Keep each tag between 1 and %d characters.", MaxTag)
		}
	}
	switch p.Difficulty {
	case "", "easy", "medium", "hard":
	default:
		errs["difficulty"] = "Choose easy, medium, hard or none."
	}
	checkStructure(errs, p)
	checkPlan(errs, p)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func checkSource(errs FieldErrors, field string, s *Source, required bool) {
	if _, ok := judge.Languages[s.Language]; !ok {
		errs[field] = "Choose C++17, Java 21, Python 3 or JavaScript."
		return
	}
	switch {
	case required && strings.TrimSpace(s.Source) == "":
		errs[field] = "Paste the reference solution."
	case len(s.Source) > MaxSource:
		errs[field] = "The source is longer than 64 KB."
	case !utf8.ValidString(s.Source):
		errs[field] = "The source is not valid UTF-8 text."
	}
}

// checkStructure checks the samples and subtasks on their own: range
// syntax, points, duplicates. Whether they match the tests is a warning.
func checkStructure(errs FieldErrors, p *Problem) {
	if len(p.Subtasks) > MaxSubtasks {
		errs["subtasks"] = fmt.Sprintf("Use at most %d subtasks.", MaxSubtasks)
		return
	}
	sort.Ints(p.Samples)
	seen := map[int]bool{}
	for _, n := range p.Samples {
		if n < 1 || n > MaxTests || seen[n] {
			errs["samples"] = "List each sample test number once, from 1 to 999."
			break
		}
		seen[n] = true
	}
	for i := range p.Subtasks {
		st := &p.Subtasks[i]
		st.Idx = i + 1
		st.Title = strings.TrimSpace(st.Title)
		if st.Points < 1 || st.Points > MaxPoints {
			errs[fmt.Sprintf("subtasks.%d.points", i)] = fmt.Sprintf("Give subtask %d from 1 to %d points.", st.Idx, MaxPoints)
		}
		if utf8.RuneCountInString(st.Title) > MaxTitle {
			errs[fmt.Sprintf("subtasks.%d.title", i)] = "Keep the subtask title short."
		}
		nums, err := ParseRanges(st.Tests)
		if err != nil {
			errs[fmt.Sprintf("subtasks.%d.tests", i)] = fmt.Sprintf("Subtask %d: %s", st.Idx, err)
			continue
		}
		if len(nums) == 0 {
			errs[fmt.Sprintf("subtasks.%d.tests", i)] = fmt.Sprintf("Subtask %d: list its tests, like 3-5.", st.Idx)
		}
		st.Tests = FormatRanges(nums)
	}
}

// Problems lists what stops the problem from being validated as it stands:
// tests missing, tests in no subtask or in two, ranges naming tests that do
// not exist. An empty list means the structure is complete.
func (p *Problem) Problems() []string {
	return append(p.StructureProblems(), p.missingOutputs()...)
}

// missingOutputs lists tests still waiting for their expected output.
func (p *Problem) missingOutputs() []string {
	var out []string
	for _, t := range p.Tests {
		if t.OutputSHA == "" {
			out = append(out, fmt.Sprintf("Test %d has no expected output. Generate outputs from the reference solution.", t.Idx))
		}
	}
	return out
}

// StructureProblems is Problems without missing outputs: tests, samples and
// subtasks that do not fit together.
func (p *Problem) StructureProblems() []string {
	var out []string
	if len(p.Tests) == 0 {
		return []string{"Add tests before validating."}
	}
	exists := map[int]bool{}
	for _, t := range p.Tests {
		exists[t.Idx] = true
	}
	owner := map[int]string{}
	for _, n := range p.Samples {
		if !exists[n] {
			out = append(out, fmt.Sprintf("Sample %d does not exist. Remove it from the samples.", n))
			continue
		}
		owner[n] = "the samples"
	}
	if len(p.Subtasks) == 0 {
		out = append(out, "Add at least one subtask.")
	}
	for _, st := range p.Subtasks {
		nums, err := ParseRanges(st.Tests)
		if err != nil {
			out = append(out, fmt.Sprintf("Subtask %d: %s", st.Idx, err))
			continue
		}
		for _, n := range nums {
			switch {
			case !exists[n]:
				out = append(out, fmt.Sprintf("Subtask %d lists test %d, which does not exist.", st.Idx, n))
			case owner[n] != "":
				out = append(out, fmt.Sprintf("Test %d is in subtask %d and in %s. Put each test in one place.", n, st.Idx, owner[n]))
			default:
				owner[n] = fmt.Sprintf("subtask %d", st.Idx)
			}
		}
	}
	for _, t := range p.Tests {
		if owner[t.Idx] == "" {
			out = append(out, fmt.Sprintf("Test %d is in no subtask. Add it to a subtask or mark it as a sample.", t.Idx))
		}
	}
	return out
}

// Warnings are advice that does not block validation.
func (p *Problem) Warnings() []string {
	var out []string
	total := 0
	for _, st := range p.Subtasks {
		total += st.Points
	}
	if len(p.Subtasks) > 0 && total != 100 {
		out = append(out, fmt.Sprintf("Subtask points add up to %d. Most contests expect 100.", total))
	}
	if p.Brute == nil {
		out = append(out, "No brute force solution: validation cannot cross-check the reference.")
	}
	if p.Validator == nil {
		out = append(out, "No input validator: validation cannot check inputs against the constraints.")
	}
	if n := len(p.Tests) - len(p.Samples); n > 10 {
		out = append(out, fmt.Sprintf("%d hidden tests. Keep it to about 10 so contests judge quickly.", n))
	}
	return append(out, p.planWarnings()...)
}

// SubtaskOf returns the subtask containing test n, or 0.
func (p *Problem) SubtaskOf(n int) int {
	for _, st := range p.Subtasks {
		nums, _ := ParseRanges(st.Tests)
		for _, m := range nums {
			if m == n {
				return st.Idx
			}
		}
	}
	return 0
}

// IsSample reports whether test n is a sample.
func (p *Problem) IsSample(n int) bool {
	for _, s := range p.Samples {
		if s == n {
			return true
		}
	}
	return false
}
