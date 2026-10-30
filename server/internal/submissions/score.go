// Package submissions is the student side of judging: running code on
// samples or a custom input, submitting it for a score, drafts saved as
// students type, rate limits, the integrity counts, and the job kinds that carry runs
// and submissions to the judge. Scores are always computed here from the
// per-test results and the problem's subtasks, never taken from the judge.
package submissions

import (
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// Verdict codes, matching the judge's.
const (
	verdictAccepted = "AC"
	verdictCompile  = "CE"
	verdictSkipped  = "SK"
	verdictInternal = "IE"
)

// TestOutcome is one test's result as students see it: never its input or
// output.
type TestOutcome struct {
	Idx      int    `json:"idx"`
	Verdict  string `json:"verdict"`
	TimeMS   int    `json:"time_ms"`
	MemoryKB int    `json:"memory_kb"`
}

// SubtaskResult is one subtask: all or nothing.
type SubtaskResult struct {
	Idx     int           `json:"idx"`
	Points  int           `json:"points"`
	Earned  int           `json:"earned"`
	Verdict string        `json:"verdict"`
	Tests   []TestOutcome `json:"tests"`
}

// Score is a submission's result, final or so far.
type Score struct {
	Verdict     string          `json:"verdict"`
	Score       int             `json:"score"`
	MaxScore    int             `json:"max_score"`
	MaxTimeMS   int             `json:"max_time_ms"`
	MaxMemoryKB int             `json:"max_memory_kb"`
	Samples     []TestOutcome   `json:"samples"`
	Subtasks    []SubtaskResult `json:"subtasks"`
}

// ScoreSubmission scores a submission from what the judge reported. With
// final false it gives the score so far (pending tests have an empty
// verdict). With final true a test the judge never reported counts as an
// internal error, so a silent judge can never earn points.
func ScoreSubmission(pl judgeapi.SubmitPayload, compile []judgeapi.CompileResult, results []judgeapi.TestResult, final bool) Score {
	s := Score{Samples: []TestOutcome{}, Subtasks: []SubtaskResult{}}
	for _, st := range pl.Subtasks {
		s.MaxScore += st.Points
	}
	compileFailed := false
	for _, c := range compile {
		if c.Verdict == verdictCompile {
			compileFailed = true
		}
	}
	got := map[int]judgeapi.TestResult{}
	for _, r := range results {
		got[r.Idx] = r
	}
	outcome := func(t judgeapi.SubmitTest) TestOutcome {
		r, ok := got[t.Idx]
		switch {
		case ok:
			return TestOutcome{Idx: t.Idx, Verdict: r.Verdict, TimeMS: r.TimeMS, MemoryKB: r.MemoryKB}
		case compileFailed:
			return TestOutcome{Idx: t.Idx, Verdict: verdictSkipped}
		case final:
			return TestOutcome{Idx: t.Idx, Verdict: verdictInternal}
		}
		return TestOutcome{Idx: t.Idx}
	}

	byIdx := map[int]*SubtaskResult{}
	for _, st := range pl.Subtasks {
		s.Subtasks = append(s.Subtasks, SubtaskResult{Idx: st.Idx, Points: st.Points, Tests: []TestOutcome{}})
	}
	for i := range s.Subtasks {
		byIdx[s.Subtasks[i].Idx] = &s.Subtasks[i]
	}
	firstFailure := ""
	allPassed := !compileFailed
	pending := false
	for _, t := range pl.Tests {
		o := outcome(t)
		if o.Verdict == "" {
			pending, allPassed = true, false
		} else if o.Verdict != verdictAccepted {
			allPassed = false
			if firstFailure == "" && o.Verdict != verdictSkipped {
				firstFailure = o.Verdict
			}
		}
		s.MaxTimeMS = max(s.MaxTimeMS, o.TimeMS)
		s.MaxMemoryKB = max(s.MaxMemoryKB, o.MemoryKB)
		if t.Sample {
			s.Samples = append(s.Samples, o)
		} else if st := byIdx[t.Subtask]; st != nil {
			st.Tests = append(st.Tests, o)
		}
	}
	for i := range s.Subtasks {
		st := &s.Subtasks[i]
		st.Verdict = subtaskVerdict(st.Tests, compileFailed)
		if st.Verdict == verdictAccepted {
			st.Earned = st.Points
			s.Score += st.Points
		}
	}
	switch {
	case compileFailed:
		s.Verdict = verdictCompile
	case firstFailure != "":
		s.Verdict = firstFailure
	case allPassed:
		s.Verdict = verdictAccepted
	case pending:
		s.Verdict = ""
	default:
		s.Verdict = verdictSkipped
	}
	return s
}

// subtaskVerdict is AC when every test passed, the first real failure
// otherwise, SK when its tests were skipped (a failing sample stopped
// judging), and empty while tests are pending.
func subtaskVerdict(tests []TestOutcome, compileFailed bool) string {
	if compileFailed {
		return verdictCompile
	}
	if len(tests) == 0 {
		return verdictSkipped // a subtask with no tests earns nothing
	}
	skipped := false
	for _, t := range tests {
		switch t.Verdict {
		case verdictAccepted:
		case "":
			return ""
		case verdictSkipped:
			skipped = true
		default:
			return t.Verdict
		}
	}
	if skipped {
		return verdictSkipped
	}
	return verdictAccepted
}
