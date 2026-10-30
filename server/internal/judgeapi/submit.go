package judgeapi

// Job kinds for student code: a submission and a run.
const (
	KindSubmit   = "submit"
	KindRun      = "run"
	KindGenerate = "generate"
)

// RoleProgram is the role of the one program in a submit, run or generate
// job (a student's code, or the reference for generate).
const RoleProgram = "program"

// MaxRunOutput bounds the output excerpt a run reports per input. Submit
// jobs report output only with Reveal, on the first failing hidden test;
// otherwise hidden tests stay hidden.
const MaxRunOutput = 4 << 10

// SubmitTest is one test of a submission, in judging order.
type SubmitTest struct {
	Idx       int    `json:"idx"`
	Sample    bool   `json:"sample"`
	Subtask   int    `json:"subtask"`
	InputSHA  string `json:"input_sha"`
	OutputSHA string `json:"output_sha"`
}

// Subtask is a group of tests scored all or nothing.
type Subtask struct {
	Idx    int `json:"idx"`
	Points int `json:"points"`
}

// SubmitPayload is everything a submission's judging needs. Tests come
// samples first, then each subtask's tests in order. The worker stops
// everything at a failing sample when StopOnSampleFail is set, and stops a
// subtask at its first failing test; skipped tests are reported as SK.
type SubmitPayload struct {
	SubmissionID     int64        `json:"submission_id"`
	Program          Program      `json:"program"`
	TimeLimitMS      int          `json:"time_limit_ms"`
	MemoryLimitMB    int          `json:"memory_limit_mb"`
	Checker          Checker      `json:"checker"`
	Tests            []SubmitTest `json:"tests"`
	Subtasks         []Subtask    `json:"subtasks"`
	StopOnSampleFail bool         `json:"stop_on_sample_fail"`
	// Reveal is set for a submission that never counts (practice, the
	// organisers' workspace): the worker reports the
	// program's output (at most MaxRunOutput) on the first hidden test
	// that fails, so the author can see it (protocol 3).
	Reveal bool `json:"reveal,omitempty"`
}

// RunInput is one input of a run: a sample or an open test (with its
// expected output) or the student's custom input (without).
type RunInput struct {
	Idx         int    `json:"idx"`
	InputSHA    string `json:"input_sha"`
	ExpectedSHA string `json:"expected_sha,omitempty"`
	// Open marks an open test (protocol 3): the worker runs it only
	// while every earlier input with an expected output passed, and
	// reports SK otherwise.
	Open bool `json:"open,omitempty"`
}

// RunPayload runs a program on samples or a custom input and reports its
// output.
type RunPayload struct {
	SubmissionID  int64      `json:"submission_id"`
	Program       Program    `json:"program"`
	TimeLimitMS   int        `json:"time_limit_ms"`
	MemoryLimitMB int        `json:"memory_limit_mb"`
	Checker       Checker    `json:"checker"`
	Inputs        []RunInput `json:"inputs"`
}

// GeneratePayload runs the reference solution on inputs and uploads each
// output, which become the tests' expected outputs.
type GeneratePayload struct {
	ProblemID     int64      `json:"problem_id"`
	ContentHash   string     `json:"content_hash"`
	Program       Program    `json:"program"`
	TimeLimitMS   int        `json:"time_limit_ms"`
	MemoryLimitMB int        `json:"memory_limit_mb"`
	Inputs        []RunInput `json:"inputs"`
	// All replaces every output; otherwise only missing outputs are
	// filled.
	All bool `json:"all"`
	// ThenValidate asks the server to queue a validation when every
	// output arrived (set when generate_inputs queued this job). The
	// worker ignores it.
	ThenValidate bool `json:"then_validate,omitempty"`
}
