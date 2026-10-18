package judge

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// checkerLimits bound a custom checker run. The checker is setter code (and
// from M5 possibly model-written), so it runs in a fresh box like any
// submission.
var checkerLimits = Limits{CPU: 10 * time.Second, Wall: 20 * time.Second, MemoryKB: 256 * 1024,
	Processes: 1, FileSizeKB: DefaultFileSizeKB}

// checkerMessageLimit bounds the checker comment kept with a result.
const checkerMessageLimit = 256

// testlib's exit codes (testlib.h: OK, WA, PE, FAIL, DIRT, POINTS,
// UNEXPECTED_EOF; partial scores start at 50).
const (
	testlibOK            = 0
	testlibWA            = 1
	testlibPE            = 2
	testlibFail          = 3
	testlibDirt          = 4
	testlibPoints        = 7
	testlibUnexpectedEOF = 8
	testlibPartialBase   = 50
)

// CheckerResult is a custom checker's judgement.
type CheckerResult struct {
	// Verdict is Accepted, WrongAnswer, or InternalError when the checker
	// itself failed (crashed, ran out of time, or reported FAIL).
	Verdict Verdict
	// Message is the start of the checker's comment.
	Message string
	Meta    Meta
}

// RunChecker runs a compiled testlib checker in a fresh box as
// "checker input.txt output.txt answer.txt", the testlib convention:
// the test input, the program's output, and the expected answer. Partial
// scores (POINTS and the partial range) count as wrong answer until
// per-test scoring arrives in v2.
func (r *Runner) RunChecker(ctx context.Context, box int, checker Program, inputPath, outputPath, answerPath string) (CheckerResult, error) {
	release, err := r.reserve(ctx, checkerLimits.MemoryKB)
	if err != nil {
		return CheckerResult{}, err
	}
	defer release()
	dir, err := r.Iso.Init(ctx, box)
	if err != nil {
		return CheckerResult{}, err
	}
	defer r.Iso.Cleanup(ctx, box)

	if err := copyRegularFiles(checker.Dir, dir, nil); err != nil {
		return CheckerResult{}, err
	}
	for name, src := range map[string]string{"input.txt": inputPath, "output.txt": outputPath, "answer.txt": answerPath} {
		if err := copyFile(src, filepath.Join(dir, name), 0o644, -1); err != nil {
			return CheckerResult{}, err
		}
	}
	args := append(checker.Lang.RunCommand(checkerLimits.MemoryKB/1024), "input.txt", "output.txt", "answer.txt")
	meta, err := r.Iso.Run(ctx, RunRequest{
		Box: box, Limits: checkerLimits, Args: args,
		Stdout: "checker.txt", StderrToStdout: true,
		Env: sandboxEnv, Dirs: checker.Lang.Dirs,
	})
	if err != nil {
		return CheckerResult{}, err
	}
	msg, _ := readBoxHead(filepath.Join(dir, "checker.txt"), checkerMessageLimit)
	res := CheckerResult{Message: strings.ToValidUTF8(strings.TrimSpace(string(msg)), ""), Meta: meta}
	switch {
	case meta.Status == "XX":
		res.Verdict, res.Message = InternalError, "The judge could not run the checker."
	case meta.OOMKilled || meta.Status == "TO" || meta.Status == "SG" || meta.ExitSig != 0:
		res.Verdict, res.Message = InternalError, "The checker crashed or ran out of time or memory."
	case meta.ExitCode == testlibOK:
		res.Verdict = Accepted
	case meta.ExitCode == testlibWA, meta.ExitCode == testlibPE, meta.ExitCode == testlibDirt,
		meta.ExitCode == testlibUnexpectedEOF, meta.ExitCode == testlibPoints, meta.ExitCode >= testlibPartialBase:
		res.Verdict = WrongAnswer
	case meta.ExitCode == testlibFail:
		res.Verdict = InternalError
		res.Message = strings.TrimSpace("The checker reported a failure. " + res.Message)
	default:
		res.Verdict = InternalError
		res.Message = strings.TrimSpace("The checker exited in an unexpected way. " + res.Message)
	}
	if len(res.Message) > checkerMessageLimit {
		res.Message = strings.ToValidUTF8(res.Message[:checkerMessageLimit], "")
	}
	return res, nil
}
