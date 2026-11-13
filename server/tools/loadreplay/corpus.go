package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Program is one entry of the replay corpus.
type Program struct {
	ID        string            `json:"id"`
	Problem   string            `json:"problem"` // pack slug
	Language  string            `json:"language"`
	File      string            `json:"file"` // relative to the repository root
	Heavy     bool              `json:"heavy"`
	NearLimit bool              `json:"near_limit"`
	Runs      int               `json:"runs"`    // per round
	Submits   int               `json:"submits"` // per round
	Expected  map[string]string `json:"expected"`
	Source    string            `json:"-"`
}

// Corpus is the fixed list of programs the replay judges.
type Corpus struct {
	Programs []Program `json:"programs"`
}

// LoadCorpus reads the manifest and every program file it names. It refuses
// a program that is sent without an expected verdict for that kind.
func LoadCorpus(root, manifest string) (*Corpus, error) {
	b, err := os.ReadFile(manifest)
	if err != nil {
		return nil, err
	}
	var c Corpus
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", manifest, err)
	}
	seen := map[string]bool{}
	for i := range c.Programs {
		p := &c.Programs[i]
		if p.ID == "" || seen[p.ID] {
			return nil, fmt.Errorf("program %d: missing or repeated id %q", i, p.ID)
		}
		seen[p.ID] = true
		if (p.Runs > 0 && p.Expected["run"] == "") || (p.Submits > 0 && p.Expected["submit"] == "") {
			return nil, fmt.Errorf("%s: a kind it is sent as has no expected verdict", p.ID)
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p.File)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.ID, err)
		}
		p.Source = string(src)
	}
	return &c, nil
}

// Only returns the corpus cut down to the programs with these ids.
func (c *Corpus) Only(ids []string) (*Corpus, error) {
	out := &Corpus{}
	for _, id := range ids {
		found := false
		for _, p := range c.Programs {
			if p.ID == id {
				out.Programs, found = append(out.Programs, p), true
			}
		}
		if !found {
			return nil, fmt.Errorf("no program %q in the corpus", id)
		}
	}
	return out, nil
}

// Item is one request of the replay.
type Item struct {
	Seq     int
	Program *Program
	Kind    string // run or submit
}

// Sequence deals out rounds of the corpus. Inside a round the programs are
// visited in manifest order, pass after pass, each pass sending a Run and
// then a submission for every program that has some left. The list depends
// on nothing but the manifest and rounds.
func (c *Corpus) Sequence(rounds int) []Item {
	var out []Item
	for r := 0; r < rounds; r++ {
		runs := make([]int, len(c.Programs))
		subs := make([]int, len(c.Programs))
		for i, p := range c.Programs {
			runs[i], subs[i] = p.Runs, p.Submits
		}
		for left := true; left; {
			left = false
			for i := range c.Programs {
				for _, k := range []struct {
					kind string
					n    *int
				}{{"run", &runs[i]}, {"submit", &subs[i]}} {
					if *k.n > 0 {
						*k.n--
						left = true
						out = append(out, Item{Seq: len(out), Program: &c.Programs[i], Kind: k.kind})
					}
				}
			}
		}
	}
	return out
}

// burstFactor is how many times denser the requests are in the burst than
// before it.
const burstFactor = 3

// Schedule gives each of n requests its offset from the start of a contest
// of length total, and the index of the first request of the burst: the
// last burst of the contest holds requests burstFactor times as dense. The
// offsets depend on nothing else, so two runs send the same list at the
// same moments.
func Schedule(n int, total, burst time.Duration) (offsets []time.Duration, firstBurst int) {
	if burst > total {
		burst = total
	}
	steady := total - burst
	nBurst := int(float64(n)*float64(burstFactor*burst)/float64(burstFactor*burst+steady) + 0.5)
	firstBurst = n - nBurst
	offsets = make([]time.Duration, n)
	for i := range offsets {
		if i < firstBurst {
			offsets[i] = time.Duration(int64(steady) * int64(i) / int64(max(firstBurst, 1)))
		} else {
			offsets[i] = steady + time.Duration(int64(burst)*int64(i-firstBurst)/int64(max(nBurst, 1)))
		}
	}
	return offsets, firstBurst
}
