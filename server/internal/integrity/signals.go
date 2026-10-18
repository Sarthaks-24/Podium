package integrity

import (
	"sort"
	"strings"
	"unicode"
)

// TestOutput is one judged hidden test of a submission: its verdict and
// the hash of the program's output (reported by the judge since M3).
type TestOutput struct {
	UserID       int64
	SubmissionID int64
	ProblemID    int64
	TestIdx      int
	Verdict      string
	OutputSHA    string
}

// emptySHA is the SHA-256 of no output, which many wrong programs share.
const emptySHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// WrongPair is two students who printed the same wrong output on the same
// hidden tests. Programs in different languages that fail identically
// are the strongest sign of a shared source (integrity council).
type WrongPair struct {
	UserA, UserB int64
	ProblemID    int64
	Tests        []int
	Strength     string
}

// maxWrongHolders: a wrong output more students than this printed is a
// common mistake (an overflow everyone makes), not a shared source.
const maxWrongHolders = 3

// IdenticalWrong finds pairs sharing at least two wrong outputs on one
// problem; four or more is strong.
func IdenticalWrong(outputs []TestOutput) []WrongPair {
	type key struct {
		problem int64
		test    int
		sha     string
	}
	holders := map[key]map[int64]bool{}
	for _, o := range outputs {
		if o.Verdict != "WA" || o.OutputSHA == "" || o.OutputSHA == emptySHA {
			continue
		}
		k := key{o.ProblemID, o.TestIdx, o.OutputSHA}
		if holders[k] == nil {
			holders[k] = map[int64]bool{}
		}
		holders[k][o.UserID] = true
	}
	type pairKey struct {
		a, b, problem int64
	}
	shared := map[pairKey]map[int]bool{}
	for k, users := range holders {
		if len(users) < 2 || len(users) > maxWrongHolders {
			continue
		}
		ids := sortedIDs(users)
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				pk := pairKey{ids[i], ids[j], k.problem}
				if shared[pk] == nil {
					shared[pk] = map[int]bool{}
				}
				shared[pk][k.test] = true
			}
		}
	}
	var out []WrongPair
	for pk, tests := range shared {
		if len(tests) < 2 {
			continue
		}
		p := WrongPair{UserA: pk.a, UserB: pk.b, ProblemID: pk.problem, Strength: "moderate"}
		for t := range tests {
			p.Tests = append(p.Tests, t)
		}
		sort.Ints(p.Tests)
		if len(p.Tests) >= 4 {
			p.Strength = "strong"
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Tests) != len(out[j].Tests) {
			return len(out[i].Tests) > len(out[j].Tests)
		}
		if out[i].UserA != out[j].UserA {
			return out[i].UserA < out[j].UserA
		}
		return out[i].UserB < out[j].UserB
	})
	return out
}

func sortedIDs(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Expected is a hidden test's expected output.
type Expected struct {
	TestIdx int
	Output  string
}

// Hardcoded names the hidden tests whose expected output appears in a
// source. Samples are visible to everyone, so callers pass hidden tests
// only. Outputs shorter than 8 bytes or a single number of up to 6 digits
// are skipped: they appear in honest code by chance.
func Hardcoded(source string, hidden []Expected) (tests []int, strength string) {
	src := squash(source)
	longest := 0
	for _, e := range hidden {
		out := squash(e.Output)
		if len(out) < 8 || smallNumber(out) {
			continue
		}
		if strings.Contains(src, out) {
			tests = append(tests, e.TestIdx)
			longest = max(longest, len(out))
		}
	}
	switch {
	case len(tests) == 0:
		return nil, ""
	case len(tests) >= 2 || longest >= 32:
		return tests, "strong"
	default:
		return tests, "moderate"
	}
}

// squash collapses runs of whitespace to one space and trims the ends.
func squash(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

func smallNumber(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if len(s) == 0 || len(s) > 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Attempt is one submission of a student on a problem, in order.
type Attempt struct {
	SubmissionID int64
	Language     string
	Source       string
	// Verdicts is the verdict of each hidden test, in test order.
	Verdicts []string
}

// Probing counts how many different hidden-test results a student drew
// from near-identical code: someone flipping a branch to learn the hidden
// tests one bit at a time. It returns the largest count among runs of
// consecutive attempts whose code barely changed, and a strength when
// the count reaches 6 (10 is strong).
func Probing(attempts []Attempt) (distinct int, strength string) {
	best := 0
	seen := map[string]bool{}
	var prev Doc
	for i, a := range attempts {
		d := Prepare(a.Language, a.Source)
		if i == 0 || !nearIdentical(prev, d) {
			seen = map[string]bool{}
		}
		seen[strings.Join(a.Verdicts, ",")] = true
		best = max(best, len(seen))
		prev = d
	}
	switch {
	case best >= 10:
		return best, "strong"
	case best >= 6:
		return best, "moderate"
	default:
		return best, ""
	}
}

// nearIdentical: at least 90% of fingerprints in common (Jaccard), or
// both too short to fingerprint and equal in length within a few tokens.
func nearIdentical(a, b Doc) bool {
	if len(a.Set) == 0 || len(b.Set) == 0 {
		return a.Tokens > 0 && abs(a.Tokens-b.Tokens) <= 3
	}
	both := 0
	for h := range a.Set {
		if b.Set[h] {
			both++
		}
	}
	union := len(a.Set) + len(b.Set) - both
	return float64(both) >= 0.9*float64(union)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
