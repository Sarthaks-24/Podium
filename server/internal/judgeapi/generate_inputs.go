package judgeapi

import "regexp"

// KindGenerateInputs runs a problem's generator once per planned test and
// uploads each output, which becomes that test's input (M5,
// cache/v1/m5-plan.md section 3).
const KindGenerateInputs = "generate_inputs"

// RoleGenerator is the role of the generator program in a generate_inputs
// job.
const RoleGenerator = "generator"

// Bounds on a planned test's generator arguments.
const (
	MaxGeneratorArgs = 16
	MaxGeneratorArg  = 64
	// MaxGeneratorSeed keeps a seed inside a signed 32-bit integer, so
	// generators in any language can parse it.
	MaxGeneratorSeed = 1<<31 - 1
)

// generatorArg allows letters, digits and a few separators. Braces are
// left out on purpose: the judge substitutes "{mem}" in run commands, and
// an argument must reach the generator exactly as written.
var generatorArg = regexp.MustCompile(`^[A-Za-z0-9_.,:=+-]{1,64}$`)

// ValidGeneratorArg reports whether s may be passed to a generator.
func ValidGeneratorArg(s string) bool { return generatorArg.MatchString(s) }

// PlannedInput is one test a generator makes: the generator runs with
// argv seed followed by Args, reads nothing, and its stdout is the input.
type PlannedInput struct {
	Idx     int      `json:"idx"`
	Subtask int      `json:"subtask"`
	Seed    int64    `json:"seed"`
	Args    []string `json:"args"`
}

// GenerateInputsPayload is everything a generate_inputs job needs. The
// worker reports one RoleGenerator result per test, with the hash of the
// uploaded input in OutputSHA.
type GenerateInputsPayload struct {
	ProblemID   int64  `json:"problem_id"`
	ContentHash string `json:"content_hash"`
	// PlanHash covers the generator and the plan, which the content hash
	// leaves out; results are applied only if both still match.
	PlanHash      string         `json:"plan_hash"`
	Generator     Program        `json:"generator"`
	TimeLimitMS   int            `json:"time_limit_ms"`
	MemoryLimitMB int            `json:"memory_limit_mb"`
	Tests         []PlannedInput `json:"tests"`
}
