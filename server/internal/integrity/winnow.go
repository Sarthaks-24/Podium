package integrity

import "hash/fnv"

// Winnowing parameters (Schleimer, Wilkerson and Aiken 2003; the integrity
// council's settings): hashes of K consecutive tokens, the minimum kept
// from every window of W hashes. Any run of at least W+K-1 = 8 tokens two
// programs share yields at least one shared fingerprint.
const (
	K = 5
	W = 4
)

// Fingerprint is one kept hash and the source bytes its tokens cover.
type Fingerprint struct {
	Hash       uint64
	Start, End int
}

// Doc is one program prepared for comparison.
type Doc struct {
	Tokens       int
	Fingerprints []Fingerprint
	// Set holds each distinct hash once.
	Set map[uint64]bool
}

// Prepare lexes and fingerprints a program.
func Prepare(lang, src string) Doc {
	toks := Lex(lang, src)
	fps := Winnow(toks)
	d := Doc{Tokens: len(toks), Fingerprints: fps, Set: make(map[uint64]bool, len(fps))}
	for _, f := range fps {
		d.Set[f.Hash] = true
	}
	return d
}

// Winnow returns the fingerprints of a token stream. A stream shorter than
// K tokens has none.
func Winnow(toks []Token) []Fingerprint {
	if len(toks) < K {
		return nil
	}
	hashes := make([]uint64, len(toks)-K+1)
	for i := range hashes {
		h := fnv.New64a()
		for _, t := range toks[i : i+K] {
			h.Write([]byte(t.Text))
			h.Write([]byte{0})
		}
		hashes[i] = h.Sum64()
	}
	span := func(i int) Fingerprint {
		return Fingerprint{Hash: hashes[i], Start: toks[i].Start, End: toks[i+K-1].End}
	}
	if len(hashes) <= W {
		return []Fingerprint{span(minIndex(hashes, 0, len(hashes)))}
	}
	var out []Fingerprint
	last := -1
	for start := 0; start+W <= len(hashes); start++ {
		m := minIndex(hashes, start, start+W)
		if m != last {
			out = append(out, span(m))
			last = m
		}
	}
	return out
}

// minIndex returns the position of the smallest hash in [from, to),
// preferring the rightmost on ties so a repeated minimum is kept once.
func minIndex(h []uint64, from, to int) int {
	m := from
	for i := from + 1; i < to; i++ {
		if h[i] <= h[m] {
			m = i
		}
	}
	return m
}
