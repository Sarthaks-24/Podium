package submissions

import (
	"testing"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// payload: samples 1-2, subtask 1 (30 points) tests 3-4, subtask 2 (70) tests 5-6.
func payload() judgeapi.SubmitPayload {
	return judgeapi.SubmitPayload{
		Subtasks: []judgeapi.Subtask{{Idx: 1, Points: 30}, {Idx: 2, Points: 70}},
		Tests: []judgeapi.SubmitTest{
			{Idx: 1, Sample: true}, {Idx: 2, Sample: true},
			{Idx: 3, Subtask: 1}, {Idx: 4, Subtask: 1},
			{Idx: 5, Subtask: 2}, {Idx: 6, Subtask: 2},
		},
	}
}

func results(verdicts map[int]string) []judgeapi.TestResult {
	var out []judgeapi.TestResult
	for idx, v := range verdicts {
		out = append(out, judgeapi.TestResult{Idx: idx, Role: judgeapi.RoleProgram, Verdict: v, TimeMS: idx * 10, MemoryKB: 100})
	}
	return out
}

var compiled = []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}

func TestFullScore(t *testing.T) {
	s := ScoreSubmission(payload(), compiled, results(map[int]string{1: "AC", 2: "AC", 3: "AC", 4: "AC", 5: "AC", 6: "AC"}), true)
	if s.Verdict != "AC" || s.Score != 100 || s.MaxScore != 100 || s.MaxTimeMS != 60 || len(s.Samples) != 2 {
		t.Errorf("%+v", s)
	}
}

func TestPartialScoreAndFirstFailure(t *testing.T) {
	// Subtask 2 stopped at test 5 (TLE), test 6 skipped.
	s := ScoreSubmission(payload(), compiled, results(map[int]string{1: "AC", 2: "AC", 3: "AC", 4: "AC", 5: "TLE", 6: "SK"}), true)
	if s.Verdict != "TLE" || s.Score != 30 || s.Subtasks[0].Earned != 30 || s.Subtasks[1].Verdict != "TLE" || s.Subtasks[1].Earned != 0 {
		t.Errorf("%+v", s)
	}
}

func TestFailingSampleStopsEverything(t *testing.T) {
	s := ScoreSubmission(payload(), compiled, results(map[int]string{1: "AC", 2: "WA", 3: "SK", 4: "SK", 5: "SK", 6: "SK"}), true)
	if s.Verdict != "WA" || s.Score != 0 || s.Subtasks[0].Verdict != "SK" || s.Subtasks[1].Verdict != "SK" {
		t.Errorf("%+v", s)
	}
}

func TestCompileError(t *testing.T) {
	s := ScoreSubmission(payload(), []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "CE", Output: "error"}}, nil, true)
	if s.Verdict != "CE" || s.Score != 0 || s.Subtasks[0].Verdict != "CE" || s.Samples[0].Verdict != "SK" {
		t.Errorf("%+v", s)
	}
}

func TestSilentJudgeEarnsNothing(t *testing.T) {
	// Final, but test 4 was never reported.
	s := ScoreSubmission(payload(), compiled, results(map[int]string{1: "AC", 2: "AC", 3: "AC", 5: "AC", 6: "AC"}), true)
	if s.Score != 70 || s.Subtasks[0].Verdict != "IE" || s.Verdict != "IE" {
		t.Errorf("%+v", s)
	}
}

func TestPendingWhileJudging(t *testing.T) {
	s := ScoreSubmission(payload(), compiled, results(map[int]string{1: "AC", 2: "AC", 3: "AC", 4: "AC"}), false)
	if s.Verdict != "" || s.Score != 30 || s.Subtasks[1].Verdict != "" || s.Subtasks[0].Verdict != "AC" {
		t.Errorf("%+v", s)
	}
}
