package config

import (
	"strings"
	"testing"
)

// The judge sizes itself: with neither setting given, lanes and budget stay
// zero and the sizing package decides. A value set by hand is kept as given.
func TestJudgeSizeSettings(t *testing.T) {
	tok := strings.Repeat("j", 40)
	c, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok}))
	if err != nil || c.Lanes != 0 || c.BudgetMB != 0 {
		t.Fatalf("defaults: lanes %d budget %d, %v", c.Lanes, c.BudgetMB, err)
	}
	c, err = LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok, "PODIUM_JUDGE_LANES": "3", "PODIUM_JUDGE_MEMORY_BUDGET_MB": "3000"}))
	if err != nil || c.Lanes != 3 || c.BudgetMB != 3000 {
		t.Fatalf("hand-set: lanes %d budget %d, %v", c.Lanes, c.BudgetMB, err)
	}
	for _, bad := range []string{"0", "-5", "lots", "1048577"} {
		_, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok, "PODIUM_JUDGE_MEMORY_BUDGET_MB": bad}))
		if err == nil || !strings.Contains(err.Error(), "PODIUM_JUDGE_MEMORY_BUDGET_MB must be a whole number from 1 to 1048576") {
			t.Errorf("budget %q: %v", bad, err)
		}
	}
}

// A budget under 1,024 MB, the smallest Size ever gives, would put the
// slice cap under what one Java box is allowed, so the slice could kill a
// correct program. It is refused, in the worker and in calibrate's loader.
func TestAHandSetBudgetUnderTheSmallestIsRefused(t *testing.T) {
	tok := strings.Repeat("j", 40)
	for _, bad := range []string{"300", "1023"} {
		want := "PODIUM_JUDGE_MEMORY_BUDGET_MB=" + bad + " is less than the minimum of 1024 MB"
		_, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok, "PODIUM_JUDGE_MEMORY_BUDGET_MB": bad}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("worker, budget %s: %v", bad, err)
		}
		_, err = LoadJudge(env(map[string]string{"PODIUM_JUDGE_MEMORY_BUDGET_MB": bad}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("judge, budget %s: %v", bad, err)
		}
	}
	c, err := LoadWorker(env(map[string]string{"PODIUM_JUDGE_TOKEN": tok, "PODIUM_JUDGE_MEMORY_BUDGET_MB": "1024"}))
	if err != nil || c.BudgetMB != 1024 {
		t.Errorf("1024: %d %v", c.BudgetMB, err)
	}
}

func TestJudgeVarsListTheBudgetAfterTheLanes(t *testing.T) {
	var names []string
	for _, v := range JudgeVars {
		names = append(names, v.Name)
	}
	got := strings.Join(names, " ")
	if !strings.Contains(got, "PODIUM_JUDGE_LANES PODIUM_JUDGE_MEMORY_BUDGET_MB PODIUM_JUDGE_BOX_BASE") {
		t.Errorf("JudgeVars = %s", got)
	}
}

// The lane range is only known once the judge is sized, so the box check
// that LoadWorker makes for a hand-set count is also available after it.
func TestBoxRangeKeepsClearOfTheSelftestBox(t *testing.T) {
	if err := CheckBoxRange(78, 3); err == nil || !strings.Contains(err.Error(), "boxes 78 to 80") {
		t.Errorf("78 + 3 lanes: %v", err)
	}
	for _, ok := range [][2]int{{0, 2}, {100, 4}, {81, 16}, {64, 16}} {
		if err := CheckBoxRange(ok[0], ok[1]); err != nil {
			t.Errorf("base %d lanes %d: %v", ok[0], ok[1], err)
		}
	}
}
