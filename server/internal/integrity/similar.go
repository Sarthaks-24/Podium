package integrity

import (
	"math"
	"sort"
)

// Subject is one program to compare: a student's best submission on a
// problem, or their last wrong one.
type Subject struct {
	SubmissionID int64
	UserID       int64
	Language     string
	Source       string
}

// Span is a byte range in a source that matched the other program.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Pair is two students whose programs share more than chance explains.
type Pair struct {
	A, B Subject
	// Score is containment: shared fingerprints over the smaller
	// program's fingerprints, after the common ones are dropped.
	Score float64
	// Shared is the number of fingerprints in common, Fingerprints the
	// smaller program's count (the score's denominator) and Tokens the
	// shorter program's length in tokens: how much evidence is behind the
	// score.
	Shared       int
	Fingerprints int
	Tokens       int
	Strength     string
	SpansA       []Span
	SpansB       []Span
}

// Settings tune the comparison; Defaults are the council's values.
type Settings struct {
	// MinTokens skips programs too short to tell apart (A plus B).
	MinTokens int
	// MinFingerprints skips programs left with too few fingerprints after
	// the common ones are dropped: a tiny denominator makes any overlap
	// look total.
	MinFingerprints int
	// Threshold is the lowest score reported.
	Threshold float64
	// CommonShare and CommonFloor drop a fingerprint that more than
	// max(CommonShare of the students in that language, CommonFloor)
	// students have: shared idioms, not copying. The share is counted per
	// language, so the input lines of a language few students use are
	// still dropped.
	CommonShare float64
	CommonFloor int
	// ExtraBase holds more programs everyone may share, added to the
	// reference and brute force: an organiser's template or starter code.
	// Nothing fills it yet (a per-contest list of templates is a known gap,
	// docs/INTEGRITY.md); the comparison is ready for it.
	ExtraBase []Subject
}

// Defaults are the integrity plan's settings (cache/v1/integrity-plan.md).
var Defaults = Settings{MinTokens: 40, MinFingerprints: 8, Threshold: 0.6, CommonShare: 0.1, CommonFloor: 5}

// withDefaults takes the numeric settings from Defaults when none is set
// and keeps ExtraBase, so a caller can set ExtraBase alone.
func (s Settings) withDefaults() Settings {
	if s.MinTokens == 0 && s.MinFingerprints == 0 && s.Threshold == 0 && s.CommonShare == 0 && s.CommonFloor == 0 {
		extra := s.ExtraBase
		s = Defaults
		s.ExtraBase = extra
	}
	return s
}

// Strength levels of similar code. A high score from a handful of
// fingerprints is what short programs and textbook algorithms produce
// without copying (integrity review 2026-10-18: honest pairs reached 1.00
// with 14 to 29 shared fingerprints, real copies had 81 to 108), so each
// level also needs an amount of shared code.
const (
	StrongScore    = 0.9
	StrongShared   = 40
	ModerateScore  = 0.75
	ModerateShared = 25
)

// Strength words a score for reviewers, who see the number beside it:
// strong needs a score of at least 0.9 and 40 shared fingerprints,
// moderate 0.75 and 25; anything else that is reported is weak.
func Strength(score float64, shared int) string {
	switch {
	case score >= StrongScore && shared >= StrongShared:
		return "strong"
	case score >= ModerateScore && shared >= ModerateShared:
		return "moderate"
	default:
		return "weak"
	}
}

// Compare finds similar pairs among one problem's subjects. base holds
// programs everyone may legitimately share (reference, brute force), and
// s.ExtraBase more of them (a template); their fingerprints never count. A
// pair of students appears once, with their most similar programs. Zero
// numeric settings take the Defaults.
func Compare(subjects []Subject, base []Subject, s Settings) []Pair {
	s = s.withDefaults()
	ignore := map[uint64]bool{}
	for _, b := range append(append([]Subject{}, base...), s.ExtraBase...) {
		for h := range Prepare(b.Language, b.Source).Set {
			ignore[h] = true
		}
	}
	docs := make([]Doc, len(subjects))
	users := map[string]map[int64]bool{} // language to students
	for i, sub := range subjects {
		docs[i] = Prepare(sub.Language, sub.Source)
		if users[sub.Language] == nil {
			users[sub.Language] = map[int64]bool{}
		}
		users[sub.Language][sub.UserID] = true
	}
	// How many students of each language have each fingerprint.
	type langHash struct {
		lang string
		h    uint64
	}
	holders := map[langHash]map[int64]bool{}
	for i, d := range docs {
		for h := range d.Set {
			k := langHash{subjects[i].Language, h}
			if holders[k] == nil {
				holders[k] = map[int64]bool{}
			}
			holders[k][subjects[i].UserID] = true
		}
	}
	limits := map[string]int{}
	for lang, us := range users {
		limits[lang] = max(int(math.Ceil(s.CommonShare*float64(len(us)))), s.CommonFloor)
	}
	keep := func(lang string, h uint64) bool {
		return !ignore[h] && len(holders[langHash{lang, h}]) <= limits[lang]
	}

	eff := make([]map[uint64]bool, len(docs))
	index := map[uint64][]int{}
	for i, d := range docs {
		if d.Tokens < s.MinTokens {
			continue
		}
		e := map[uint64]bool{}
		for h := range d.Set {
			if keep(subjects[i].Language, h) {
				e[h] = true
			}
		}
		if len(e) < s.MinFingerprints {
			continue
		}
		eff[i] = e
		for h := range e {
			index[h] = append(index[h], i)
		}
	}
	shared := map[[2]int]int{}
	for _, list := range index {
		for x := 0; x < len(list); x++ {
			for y := x + 1; y < len(list); y++ {
				a, b := list[x], list[y]
				if subjects[a].UserID == subjects[b].UserID {
					continue
				}
				shared[[2]int{a, b}]++
			}
		}
	}
	best := map[[2]int64]Pair{}
	for k, n := range shared {
		a, b := k[0], k[1]
		smaller := min(len(eff[a]), len(eff[b]))
		score := float64(n) / float64(smaller)
		if score < s.Threshold {
			continue
		}
		ua, ub := subjects[a].UserID, subjects[b].UserID
		if ua > ub {
			a, b, ua, ub = b, a, ub, ua
		}
		key := [2]int64{ua, ub}
		if old, ok := best[key]; ok && old.Score >= score {
			continue
		}
		common := map[uint64]bool{}
		for h := range eff[a] {
			if eff[b][h] {
				common[h] = true
			}
		}
		best[key] = Pair{A: subjects[a], B: subjects[b], Score: score, Shared: n, Fingerprints: smaller,
			Tokens: min(docs[a].Tokens, docs[b].Tokens), Strength: Strength(score, n),
			SpansA: spans(docs[a], common), SpansB: spans(docs[b], common)}
	}
	out := make([]Pair, 0, len(best))
	for _, p := range best {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].A.UserID != out[j].A.UserID {
			return out[i].A.UserID < out[j].A.UserID
		}
		return out[i].B.UserID < out[j].B.UserID
	})
	return out
}

// spans merges the source ranges of a document's fingerprints in common.
func spans(d Doc, common map[uint64]bool) []Span {
	var out []Span
	for _, f := range d.Fingerprints {
		if !common[f.Hash] {
			continue
		}
		if n := len(out); n > 0 && f.Start <= out[n-1].End {
			if f.End > out[n-1].End {
				out[n-1].End = f.End
			}
			continue
		}
		out = append(out, Span{f.Start, f.End})
	}
	return out
}

// Clusters groups students linked by pairs, largest group first; each
// group lists user ids in ascending order.
func Clusters(pairs []Pair) [][]int64 {
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if parent[x] == x {
			return x
		}
		parent[x] = find(parent[x])
		return parent[x]
	}
	for _, p := range pairs {
		for _, u := range []int64{p.A.UserID, p.B.UserID} {
			if _, ok := parent[u]; !ok {
				parent[u] = u
			}
		}
		ra, rb := find(p.A.UserID), find(p.B.UserID)
		if ra != rb {
			if ra < rb {
				parent[rb] = ra
			} else {
				parent[ra] = rb
			}
		}
	}
	groups := map[int64][]int64{}
	for u := range parent {
		r := find(u)
		groups[r] = append(groups[r], u)
	}
	out := make([][]int64, 0, len(groups))
	for _, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i][0] < out[j][0]
	})
	return out
}
