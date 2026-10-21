package problems

import (
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// bruteRun is a validation of n tests where the reference passes every test
// and the brute force gets brute[i] on test i+1.
func bruteRun(brute ...string) (judgeapi.ValidatePayload, jobs.Result) {
	pl := judgeapi.ValidatePayload{TimeLimitMS: 1000, Points: 100,
		Programs: judgeapi.Programs{Reference: judgeapi.Program{Language: "cpp17"}, Brute: &judgeapi.Program{Language: "python3"},
			Validator: &judgeapi.Program{Language: "python3"}}}
	res := jobs.Result{Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleReference, Verdict: "OK"},
		{Role: judgeapi.RoleBrute, Verdict: "OK"}, {Role: judgeapi.RoleValidator, Verdict: "OK"}}}
	for i, v := range brute {
		idx := i + 1
		pl.Tests = append(pl.Tests, judgeapi.Test{Idx: idx})
		res.Tests = append(res.Tests,
			judgeapi.TestResult{Idx: idx, Role: judgeapi.RoleReference, Verdict: "AC", TimeMS: 10},
			judgeapi.TestResult{Idx: idx, Role: judgeapi.RoleValidator, Verdict: "AC"},
			judgeapi.TestResult{Idx: idx, Role: judgeapi.RoleBrute, Verdict: v})
	}
	return pl, res
}

// The brute force's time-outs are not cross-checks (security council S10):
// they are counted and named, and fewer than half cross-checked fails.
func TestBruteForceTimeoutsAreNotCrossChecks(t *testing.T) {
	sm := summarise(bruteRun("AC", "AC", "AC", "AC", "TLE", "SK"))
	if !sm.Passed || sm.BruteUnchecked != 2 || len(sm.BruteUncheckedTests) != 2 || sm.BruteUncheckedTests[1] != 6 {
		t.Fatalf("4 of 6 cross-checked: passed %v, unchecked %d %v, problems %v", sm.Passed, sm.BruteUnchecked, sm.BruteUncheckedTests, sm.Problems)
	}
	if w := strings.Join(sm.Warnings, "\n"); !strings.Contains(w, "2 of 6 tests were not cross-checked") || !strings.Contains(w, "tests 5 and 6") {
		t.Errorf("warnings do not name the unchecked tests: %v", sm.Warnings)
	}

	sm = summarise(bruteRun("AC", "AC", "TLE", "TLE", "TLE", "SK"))
	if sm.Passed || sm.BruteUnchecked != 4 {
		t.Fatalf("2 of 6 cross-checked passed: %+v", sm)
	}
	if p := strings.Join(sm.Problems, "\n"); !strings.Contains(p, "Only 2 of 6 tests were cross-checked against the brute force") {
		t.Errorf("problems: %v", sm.Problems)
	}

	// Exactly half is enough; a wrong answer is still a cross-check (and a
	// failure of its own).
	if sm := summarise(bruteRun("AC", "AC", "AC", "TLE", "TLE", "TLE")); !sm.Passed {
		t.Errorf("3 of 6 cross-checked failed: %v", sm.Problems)
	}
	if sm := summarise(bruteRun("AC", "WA", "AC", "TLE", "TLE", "TLE")); sm.Passed || strings.Contains(strings.Join(sm.Problems, " "), "Only") {
		t.Errorf("a WA counts as a cross-check: %v", sm.Problems)
	}

	// Without a brute force there is nothing to count.
	pl, res := bruteRun("AC", "AC")
	pl.Programs.Brute = nil
	if sm := summarise(pl, res); !sm.Passed || sm.BruteUnchecked != 0 {
		t.Errorf("no brute force: %+v", sm)
	}
}

// The summary names the reference language and its factor, so the report
// can say why the limit it shows is not the problem's (D323).
func TestSummaryNamesTheReferenceTimeFactor(t *testing.T) {
	pl, res := bruteRun("AC")
	pl.Programs.Reference.Language = "java21"
	sm := summarise(pl, res)
	if sm.TimeLimitMS != 2000 || sm.ReferenceLanguage != "java21" || sm.TimeFactor != 2 {
		t.Errorf("java reference: limit %d, language %q, factor %v", sm.TimeLimitMS, sm.ReferenceLanguage, sm.TimeFactor)
	}
}
