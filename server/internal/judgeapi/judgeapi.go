// Package judgeapi holds the request and response bodies of the judge
// worker API (docs/API.md, decision D44), shared by podium-server and
// podium-judge so the two cannot drift. It has no behaviour beyond the
// small checks both sides need on the same values.
package judgeapi

import (
	"encoding/json"
	"regexp"
)

// Job kinds. M2 has only validation; M3 adds submissions and runs.
const KindValidate = "validate"

// LeaseSeconds is how long a claim lasts without a report.
const LeaseSeconds = 60

// Headers that carry the lease on blob downloads.
const (
	HeaderJob   = "X-Podium-Job"
	HeaderLease = "X-Podium-Lease"
)

// Roles a program plays in a validation run.
const (
	RoleReference = "reference"
	RoleBrute     = "brute"
	RoleValidator = "validator"
	RoleChecker   = "checker"
)

// Verdicts the judge reports, matching judge.Verdict.
var Verdicts = map[string]bool{
	"AC": true, "WA": true, "TLE": true, "MLE": true, "RE": true,
	"OLE": true, "CE": true, "IE": true, "SK": true, "OK": true,
}

// MaxMessage bounds a per-test message (a checker comment or the tail of
// a validator's stderr).
const MaxMessage = 256

// MaxCompileOutput bounds compiler output in a report.
const MaxCompileOutput = 16 << 10

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidSHA reports whether s is a lowercase hex SHA-256.
func ValidSHA(s string) bool { return shaPattern.MatchString(s) }

// ClaimRequest asks for work of the given kinds.
type ClaimRequest struct {
	Kinds   []string `json:"kinds"`
	Worker  string   `json:"worker"`
	Version string   `json:"version"`
	Isolate string   `json:"isolate"`
}

// Claim is a job leased to one worker. Lease is a secret the worker sends
// back with every report. Payload depends on Kind (ValidatePayload for
// KindValidate).
type Claim struct {
	JobID        int64           `json:"job_id"`
	Kind         string          `json:"kind"`
	Lease        string          `json:"lease"`
	LeaseSeconds int             `json:"lease_seconds"`
	Payload      json.RawMessage `json:"payload"`
}

// Program is one source to compile, fetched by hash.
type Program struct {
	Language  string `json:"language"`
	SourceSHA string `json:"source_sha"`
}

// Checker selects output comparison. SourceSHA is set for "custom".
type Checker struct {
	Type      string  `json:"type"`
	Epsilon   float64 `json:"epsilon"`
	SourceSHA string  `json:"source_sha,omitempty"`
}

// Programs are the solutions a validation compiles and runs.
type Programs struct {
	Reference Program  `json:"reference"`
	Brute     *Program `json:"brute"`
	Validator *Program `json:"validator"`
}

// Test is one test case, by the hashes of its files.
type Test struct {
	Idx       int    `json:"idx"`
	Sample    bool   `json:"sample"`
	InputSHA  string `json:"input_sha"`
	OutputSHA string `json:"output_sha"`
}

// ValidatePayload is everything a validation run needs.
type ValidatePayload struct {
	ProblemID     int64    `json:"problem_id"`
	ContentHash   string   `json:"content_hash"`
	TimeLimitMS   int      `json:"time_limit_ms"`
	MemoryLimitMB int      `json:"memory_limit_mb"`
	Checker       Checker  `json:"checker"`
	Programs      Programs `json:"programs"`
	Tests         []Test   `json:"tests"`
	// Points is the subtask total, for the summary's advice only.
	Points int `json:"points"`
}

// CompileResult reports one program's compile. TimedOut marks a compile
// error caused by the compiler running out of time or memory; the server
// uses it to slow down a student who keeps sending such a program, since
// every C++ and Java compile on the judge waits behind it.
type CompileResult struct {
	Role     string `json:"role"`
	Verdict  string `json:"verdict"`
	Output   string `json:"output"`
	TimedOut bool   `json:"timed_out,omitempty"`
}

// TestResult reports one program on one test. Output is set only by run
// jobs (at most MaxRunOutput); OutputSHA by generate jobs (the uploaded
// output) and by submit jobs (the hash of the program's output, for
// integrity checks: the text itself is never sent).
type TestResult struct {
	Idx       int    `json:"idx"`
	Role      string `json:"role"`
	Verdict   string `json:"verdict"`
	TimeMS    int    `json:"time_ms"`
	MemoryKB  int    `json:"memory_kb"`
	Message   string `json:"message"`
	Output    string `json:"output,omitempty"`
	OutputSHA string `json:"output_sha,omitempty"`
}

// Report adds results to a leased job. Final closes the job; Fatal closes
// it as an error the setter sees (the run could not happen, for example
// isolate failed), as opposed to a failed validation.
type Report struct {
	JobID   int64           `json:"job_id"`
	Lease   string          `json:"lease"`
	Compile []CompileResult `json:"compile"`
	Results []TestResult    `json:"results"`
	Final   bool            `json:"final"`
	Fatal   string          `json:"fatal"`
}

// ReportReply tells the worker how long its lease now lasts.
type ReportReply struct {
	LeaseSeconds int `json:"lease_seconds"`
}
