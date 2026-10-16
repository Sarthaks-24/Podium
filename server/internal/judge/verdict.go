package judge

import "bytes"

// Verdict is the outcome of one test or one submission. The short codes are
// stored in the database; the UI maps them to sentence-case copy.
type Verdict string

const (
	// OK means the program finished inside every limit; the checker has
	// not judged its output yet.
	OK            Verdict = "OK"
	Accepted      Verdict = "AC"
	WrongAnswer   Verdict = "WA"
	TimeLimit     Verdict = "TLE"
	MemoryLimit   Verdict = "MLE"
	RuntimeError  Verdict = "RE"
	OutputLimit   Verdict = "OLE"
	CompileError  Verdict = "CE"
	InternalError Verdict = "IE"
	Skipped       Verdict = "SK"
)

// sigXFSZ is the signal sent when a program exceeds --fsize (Linux x86-64
// and arm64).
const sigXFSZ = 25

// Classify turns a finished run into a verdict before any output checking.
// stderrTail is the end of the program's stderr, used only to recognise a
// runtime that reported its own heap exhaustion.
func Classify(m Meta, lim Limits, lang Language, stderrTail []byte) Verdict {
	switch {
	case m.Status == "XX":
		return InternalError
	case m.OOMKilled:
		return MemoryLimit
	case m.Status == "TO" || (lim.CPU > 0 && m.Time > lim.CPU):
		return TimeLimit
	case m.ExitSig == sigXFSZ:
		return OutputLimit
	case m.Status == "RE" || m.Status == "SG":
		for _, marker := range lang.OOMMarkers {
			if bytes.Contains(stderrTail, []byte(marker)) {
				return MemoryLimit
			}
		}
		return RuntimeError
	}
	return OK
}
