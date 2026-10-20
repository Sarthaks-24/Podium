package problems

import (
	"encoding/json"
)

// ContentHash is the SHA-256 of everything validation depends on
// (decision D45): limits, checker, solutions, samples, open tests,
// subtasks and tests.
// Title, slug, statement, tags and subtask titles are left out, so fixing
// a typo in the statement keeps a problem validated. The judge build is
// recorded with each validation instead of hashed, so upgrading the judge
// does not unvalidate the whole bank.
func ContentHash(p *Problem) string {
	type src struct {
		Language string `json:"language"`
		SHA      string `json:"sha"`
	}
	type subtask struct {
		Idx    int    `json:"idx"`
		Points int    `json:"points"`
		Tests  string `json:"tests"`
	}
	type test struct {
		Idx       int    `json:"idx"`
		InputSHA  string `json:"input_sha"`
		OutputSHA string `json:"output_sha"`
	}
	opt := func(s *Source) *src {
		if s == nil {
			return nil
		}
		return &src{s.Language, SHA([]byte(s.Source))}
	}
	m := struct {
		V          int     `json:"v"`
		TimeLimit  int     `json:"time_limit_ms"`
		Memory     int     `json:"memory_limit_mb"`
		Checker    string  `json:"checker"`
		Epsilon    float64 `json:"epsilon"`
		CheckerSHA string  `json:"checker_sha"`
		Reference  src     `json:"reference"`
		Brute      *src    `json:"brute"`
		Validator  *src    `json:"validator"`
		Samples    []int   `json:"samples"`
		// Open is left out when empty, so the hash of every problem
		// without open tests is what it was before D115.
		Open     []int     `json:"open,omitempty"`
		Subtasks []subtask `json:"subtasks"`
		Tests    []test    `json:"tests"`
	}{
		V: 1, TimeLimit: p.TimeLimitMS, Memory: p.MemoryLimitMB, Checker: p.Checker, Epsilon: p.Epsilon,
		Reference: src{p.Reference.Language, SHA([]byte(p.Reference.Source))},
		Brute:     opt(p.Brute), Validator: opt(p.Validator),
		Samples: append([]int{}, p.Samples...), Open: append([]int(nil), p.Open...), Subtasks: []subtask{}, Tests: []test{},
	}
	if p.Checker == CheckerCustom {
		m.CheckerSHA = SHA([]byte(p.CheckerSource))
	}
	for _, st := range p.Subtasks {
		m.Subtasks = append(m.Subtasks, subtask{st.Idx, st.Points, st.Tests})
	}
	for _, t := range p.Tests {
		m.Tests = append(m.Tests, test{t.Idx, t.InputSHA, t.OutputSHA})
	}
	b, _ := json.Marshal(m)
	return SHA(b)
}
