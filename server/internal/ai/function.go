package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// Values of the wizard's students_write and java_lists fields.
const (
	StudentsWriteProgram  = "program"
	StudentsWriteFunction = "function"
	JavaListsArray        = "array"
	JavaListsArrayList    = "arraylist"
)

// functionLanguages are the languages a function pack may carry.
var functionLanguages = []string{"cpp17", "java21", "python3", "node22"}

// FunctionPack is the model's answer for a function problem: the student
// writes one function between locked code.
type FunctionPack struct {
	Title         string             `json:"title"`
	Slug          string             `json:"slug"`
	StatementMD   string             `json:"statement_md"`
	Samples       []WizardSample     `json:"samples"`
	Subtasks      []WizardSubtask    `json:"subtasks"`
	Reference     string             `json:"reference_cpp"`    // a bare C++ function
	Brute         string             `json:"brute_cpp"`        // a bare C++ function, slow is fine
	Generator     string             `json:"generator_python"` // a whole program
	Validator     string             `json:"validator_python"` // a whole program
	Checker       string             `json:"checker"`
	Epsilon       float64            `json:"epsilon"`
	TimeLimitMS   int                `json:"time_limit_ms"`
	MemoryLimitMB int                `json:"memory_limit_mb"`
	Tags          []string           `json:"tags"`
	Difficulty    string             `json:"difficulty"`
	Languages     []FunctionLanguage `json:"languages"`
}

// FunctionLanguage is one language's four parts.
type FunctionLanguage struct {
	Language     string `json:"language"`
	LockedTop    string `json:"locked_top"`
	Starter      string `json:"starter"`
	LockedBottom string `json:"locked_bottom"`
	// Solution is a full solution function in this language. The C++ entry's
	// is not used: the reference is the C++ solution.
	Solution string `json:"solution"`
}

// FunctionSchema is the JSON schema sent with every function generation. It
// follows the rules of PackSchema: every property required, no lengths or
// ranges.
func FunctionSchema() map[string]any {
	props := PackSchema()["properties"].(map[string]any)
	delete(props, "brute_python")
	props["reference_cpp"] = str("Correct, efficient C++17 solution as a bare function, with no main and no input or output.")
	props["brute_cpp"] = str("Obviously correct slow C++17 solution as a bare function with the same signature.")
	props["languages"] = map[string]any{"type": "array", "description": "One to four languages; cpp17 is required.", "items": object(map[string]any{
		"language":      map[string]any{"type": "string", "enum": functionLanguages},
		"locked_top":    str("Locked code before the student's function: imports and the base code the language needs."),
		"starter":       str("The function a student fills in: a signature and a placeholder body that compiles."),
		"locked_bottom": str("Locked code after the student's function: reads the input, calls the function once, prints the result and nothing else."),
		"solution":      str("A full, efficient solution function in the same shape as the starter."),
	})}
	return object(props)
}

// DecodeFunction parses a model's JSON answer strictly: an unknown key, a
// missing key, a wrong type and a cut-off answer are each refused.
func DecodeFunction(text string) (*FunctionPack, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.DisallowUnknownFields()
	var p FunctionPack
	err := dec.Decode(&p)
	if err == nil {
		err = requireKeys(json.RawMessage(text), FunctionSchema(), "the answer")
	}
	if err != nil {
		return nil, &Error{Code: CodeInvalidAnswer, Message: "The model's answer could not be used.",
			Reasons: []string{"The answer was not in the format Podium asked for: " + clipText(err.Error(), 200)}, answer: text}
	}
	return &p, nil
}

// requireKeys checks that every object the schema describes has all its
// required keys, at any depth. Decoding into the Go type cannot tell a
// missing key from an empty one.
func requireKeys(raw json.RawMessage, schema map[string]any, path string) error {
	switch schema["type"] {
	case "object":
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("%s is not an object", path)
		}
		props, _ := schema["properties"].(map[string]any)
		for _, k := range schema["required"].([]string) {
			v, ok := m[k]
			if !ok {
				return fmt.Errorf("%s has no %q", path, k)
			}
			if sub, ok := props[k].(map[string]any); ok {
				if err := requireKeys(v, sub, path+"."+k); err != nil {
					return err
				}
			}
		}
	case "array":
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("%s is not a list", path)
		}
		sub, _ := schema["items"].(map[string]any)
		for i, it := range items {
			if err := requireKeys(it, sub, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckFunction lists everything wrong with a function pack: the wizard's
// shared rules, the language rules, then the editor's field rules on the draft
// it becomes. An empty list means the pack can become a draft.
func CheckFunction(p *FunctionPack) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	checkShape(add, p.Samples, p.Subtasks)
	for _, f := range []struct{ name, src string }{
		{"reference_cpp", p.Reference}, {"brute_cpp", p.Brute}, {"generator_python", p.Generator}, {"validator_python", p.Validator},
	} {
		if strings.TrimSpace(f.src) == "" {
			add("%s is empty.", f.name)
		}
	}
	checkLanguages(add, p.Languages)
	checkHeadings(add, p.StatementMD)
	if len(out) > 0 {
		return out
	}
	return draftReasons(ToDraftFunction(p), func(k string) string {
		if k == "brute" {
			return "brute_cpp"
		}
		return fieldName(k)
	})
}

func checkLanguages(add func(string, ...any), langs []FunctionLanguage) {
	if n := len(langs); n < 1 || n > len(functionLanguages) {
		add("Write one to four languages in languages.")
	}
	seen := map[string]bool{}
	for _, l := range langs {
		switch {
		case !slices.Contains(functionLanguages, l.Language):
			add("languages: there is no language %q. Use cpp17, java21, python3 or node22.", l.Language)
			continue
		case seen[l.Language]:
			add("languages lists %s twice.", l.Language)
		}
		seen[l.Language] = true
		for _, part := range []struct{ name, code string }{{"locked_top", l.LockedTop}, {"starter", l.Starter}, {"locked_bottom", l.LockedBottom}} {
			switch {
			case strings.TrimSpace(part.code) == "":
				add("%s: %s is empty.", l.Language, part.name)
			case len(part.code) > problems.MaxStarter:
				add("%s: %s is longer than 4 KB.", l.Language, part.name)
			}
		}
		if l.Language == "cpp17" {
			continue
		}
		switch {
		case strings.TrimSpace(l.Solution) == "":
			add("%s: solution is empty.", l.Language)
		case len(l.Solution) > problems.MaxSource:
			add("%s: solution is longer than 64 KB.", l.Language)
		}
	}
	if !seen["cpp17"] {
		add("languages must include cpp17: the reference and the brute force are C++.")
	}
}

// ToDraftFunction turns a function pack into a problem draft. The solutions
// are not part of it: Solutions hands them to the proof.
func ToDraftFunction(p *FunctionPack) problems.Draft {
	d := ToDraft(&WizardPack{Title: p.Title, Slug: p.Slug, StatementMD: p.StatementMD, Samples: p.Samples, Subtasks: p.Subtasks,
		Reference: p.Reference, Generator: p.Generator, Validator: p.Validator, Checker: p.Checker, Epsilon: p.Epsilon,
		TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB, Tags: p.Tags, Difficulty: p.Difficulty})
	pr := &d.Problem
	pr.Mode = problems.ModeFunction
	if strings.TrimSpace(p.Brute) != "" {
		pr.Brute = &problems.Source{Language: "cpp17", Source: p.Brute}
	}
	pr.LockedTop, pr.LockedBottom, pr.Starters = map[string]string{}, map[string]string{}, map[string]string{}
	for _, l := range p.Languages {
		pr.LockedTop[l.Language], pr.LockedBottom[l.Language], pr.Starters[l.Language] = l.LockedTop, l.LockedBottom, l.Starter
	}
	return d
}

// Solutions returns the solution function of each language except the
// reference language (cpp17), by language id.
func (p *FunctionPack) Solutions() map[string]string {
	out := map[string]string{}
	for _, l := range p.Languages {
		if l.Language != "cpp17" {
			out[l.Language] = l.Solution
		}
	}
	return out
}

// functionRules is the code-only part of a function request's system prompt.
// The owner-editable rules above it describe whole programs; this block says
// what changes.
func functionRules(javaLists string) string {
	java := "In Java, any list the function receives or returns is an array, such as `int[]`."
	if javaLists == JavaListsArrayList {
		java = "In Java, any list the function receives or returns is an `ArrayList`, such as `ArrayList<Integer>`."
	}
	return `This request is for a function problem: the student writes one function between locked code, not a whole program. These rules take precedence over the rules above where they differ.
- statement_md describes the function in language-neutral words: what it receives and what it returns. Keep the sections "## Input", "## Output" and "## Constraints", which describe the input the locked code reads. The statement never names a signature.
- reference_cpp and brute_cpp are bare C++ functions, not programs: no main and no input or output code. Where the rules above say "program", read "function". generator_python and validator_python stay whole Python programs.
- languages has one entry per language: cpp17 always, and java21, python3 and node22 when you can write them. Each entry has locked_top (imports only, plus the base code the language needs around the function), starter (the function a student fills in, with a signature and a placeholder body that compiles), locked_bottom (reads the input exactly as "## Input" describes, calls the student's function once, prints the result and prints nothing else) and solution (the full function, efficient, in the same shape as the starter). The cpp17 solution is not used; repeat reference_cpp there.
- ` + java + ` C++ uses vector, Python a list and JavaScript an Array either way.`
}

// BuildFunctionSystemPrompt is BuildSystemPrompt for a function request: the
// same fixed part, rules and preferences, with functionRules after the rules.
func BuildFunctionSystemPrompt(rules, preferences, javaLists string) string {
	return BuildSystemPrompt(rules+"\n\n"+functionRules(javaLists), preferences)
}

// functionSystem is the prompt a function request sends: its own System text,
// or the start-up rules with the function block.
func (r Request) functionSystem() string {
	if r.System != "" {
		return r.System
	}
	return BuildFunctionSystemPrompt(fileRules(), "", r.JavaLists)
}

// GenerateFunction runs one function generation like Generate: the call, the
// checks and at most one retry. The pack it returns passes CheckFunction.
func GenerateFunction(ctx context.Context, p Provider, req Request) (*FunctionPack, error) {
	return generateChecked(req, func(r Request) (*FunctionPack, error) { return p.GenerateFunction(ctx, r) }, CheckFunction)
}
