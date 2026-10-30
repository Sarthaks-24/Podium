package integrity

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// TestOutput is one judged hidden test of a submission: its verdict and
// the hash of the program's output, as the judge reports it.
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
// are the strongest sign of a shared source.
type WrongPair struct {
	UserA, UserB int64
	ProblemID    int64
	Tests        []int
	Strength     string
}

// wrongHolderFloor and wrongHolderShare: a wrong output more than
// max(wrongHolderFloor, wrongHolderShare of the students) printed is a
// common mistake (an overflow everyone makes), not a shared source.
const (
	wrongHolderFloor = 3
	wrongHolderShare = 0.02
)

// WrongHolderLimit is the most students who may share a wrong output for
// it to still count, in a problem with this many students.
func WrongHolderLimit(students int) int {
	return max(wrongHolderFloor, int(math.Ceil(wrongHolderShare*float64(students))))
}

// IdenticalWrong finds pairs sharing at least two wrong outputs on one
// problem; four or more is strong. students is how many students submitted
// to the problem, for the common-mistake limit.
func IdenticalWrong(outputs []TestOutput, students int) []WrongPair {
	limit := WrongHolderLimit(students)
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
		if len(users) < 2 || len(users) > limit {
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

// Program is one submission searched for hard-coded answers.
type Program struct {
	UserID       int64
	SubmissionID int64
	Source       string
}

// HardcodedHit is a student whose program contains the expected output of
// hidden tests.
type HardcodedHit struct {
	UserID       int64
	SubmissionID int64
	Tests        []int
	Strength     string
}

// hardcodedFloor and hardcodedShare: an expected output found in the
// programs of more than max(hardcodedFloor, hardcodedShare of the
// students) is a word or constant the problem makes everyone write
// (IMPOSSIBLE, 1000000000), not an answer pasted in.
const (
	hardcodedFloor = 3
	hardcodedShare = 0.1
)

// FindHardcoded reports, per student, the submission containing the most
// hidden expected outputs. Samples are visible to everyone, so callers
// pass hidden tests only. An output is never looked for when:
//
//   - it is shorter than 8 bytes or a single number of up to 6 digits: it
//     appears in honest code by chance;
//   - one of the shared texts contains it (the reference, the brute force,
//     the statement): the problem itself names it;
//   - the programs of more than max(3, 10% of the students) contain it.
//
// Each distinct source is squashed once and every output is looked for in
// it, so the cost is one pass per distinct source and output.
func FindHardcoded(progs []Program, hidden []Expected, shared []string) []HardcodedHit {
	type output struct {
		test int
		text string
	}
	var texts []string
	for _, t := range shared {
		if t != "" {
			texts = append(texts, squash(t))
		}
	}
	var outs []output
next:
	for _, e := range hidden {
		out := squash(e.Output)
		if len(out) < 8 || smallNumber(out) {
			continue
		}
		for _, t := range texts {
			if strings.Contains(t, out) {
				continue next
			}
		}
		outs = append(outs, output{e.TestIdx, out})
	}
	if len(outs) == 0 {
		return nil
	}
	// The outputs each distinct source contains, as indexes into outs.
	found := map[string][]int{}
	students := map[int64]bool{}
	for _, p := range progs {
		students[p.UserID] = true
		if _, done := found[p.Source]; done {
			continue
		}
		src := squash(p.Source)
		in := []int{}
		for i, o := range outs {
			if strings.Contains(src, o.text) {
				in = append(in, i)
			}
		}
		found[p.Source] = in
	}
	holders := make([]map[int64]bool, len(outs))
	for _, p := range progs {
		for _, i := range found[p.Source] {
			if holders[i] == nil {
				holders[i] = map[int64]bool{}
			}
			holders[i][p.UserID] = true
		}
	}
	limit := max(hardcodedFloor, int(math.Ceil(hardcodedShare*float64(len(students)))))
	best := map[int64]HardcodedHit{}
	for _, p := range progs {
		var tests []int
		longest := 0
		for _, i := range found[p.Source] {
			if len(holders[i]) > limit {
				continue
			}
			tests = append(tests, outs[i].test)
			longest = max(longest, len(outs[i].text))
		}
		if len(tests) == 0 {
			continue
		}
		if old, ok := best[p.UserID]; ok && len(old.Tests) >= len(tests) {
			continue
		}
		sort.Ints(tests)
		strength := "moderate"
		if len(tests) >= 2 || longest >= 32 {
			strength = "strong"
		}
		best[p.UserID] = HardcodedHit{UserID: p.UserID, SubmissionID: p.SubmissionID, Tests: tests, Strength: strength}
	}
	out := make([]HardcodedHit, 0, len(best))
	for _, h := range best {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
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
	// Verdicts is the verdict of each hidden test, in test order; SK marks
	// a test that did not run.
	Verdicts []string
}

// Probing counts how many different hidden-test results a student drew
// from near-identical code by flipping them: someone changing a branch to
// learn the hidden tests one bit at a time. Honest debugging moves
// forward, so only flips count:
//
//   - Attempts whose hidden tests all show SK (a compile error, a failed
//     sample) are ignored.
//   - Attempts form a run while their code stays near-identical to the
//     first attempt of the run, not to the previous one, so small edits
//     cannot chain without end.
//   - Within a run, an attempt counts when, compared with the attempt
//     before it, some hidden test went from AC back to a failure, or from
//     one kind of failure to another (WA to TLE). Tests that newly pass or
//     newly run are progress and never count.
//
// It returns the largest number of distinct counted results in one run,
// and a strength when that reaches 6 (10 is strong).
func Probing(attempts []Attempt) (distinct int, strength string) {
	best := 0
	seen := map[string]bool{}
	var anchor Doc
	var prev []string
	started := false
	for _, a := range attempts {
		if allSkipped(a.Verdicts) {
			continue
		}
		d := Prepare(a.Language, a.Source)
		if !started || !nearIdentical(anchor, d) {
			anchor, prev, started = d, a.Verdicts, true
			seen = map[string]bool{}
			continue
		}
		if flipped(prev, a.Verdicts) {
			seen[strings.Join(a.Verdicts, ",")] = true
			best = max(best, len(seen))
		}
		prev = a.Verdicts
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

func allSkipped(v []string) bool {
	for _, x := range v {
		if x != "SK" && x != "" {
			return false
		}
	}
	return true
}

// flipped reports whether some test went from AC to a failure, or from one
// failure to another, between two results. SK on either side carries no
// information: the judge stops a subtask at its first failure.
func flipped(before, after []string) bool {
	for i := 0; i < len(before) && i < len(after); i++ {
		b, a := before[i], after[i]
		if b == a || b == "SK" || a == "SK" || b == "" || a == "" || a == "AC" {
			continue
		}
		return true
	}
	return false
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
