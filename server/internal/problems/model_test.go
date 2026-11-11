package problems

import (
	"reflect"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judge"
)

// A zip of six inputs without outputs and no subtasks yet read as twelve
// lines in the readiness panel (walk 2026-10-21): each kind is one line.
func TestProblemsGroupTestsInOneLine(t *testing.T) {
	p := &Problem{Samples: []int{1}}
	for i := 1; i <= 6; i++ {
		p.Tests = append(p.Tests, Test{Idx: i})
	}
	p.Subtasks = []Subtask{{Idx: 1, Points: 100, Tests: "2"}}
	want := []string{
		"Tests 3-6 are in no subtask. Add them to a subtask, or mark them as samples.",
		"Tests 1-6 have no expected output yet. Validate makes them from the reference solution.",
	}
	if got := p.Problems(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// One test keeps the singular sentence.
	p.Subtasks[0].Tests = "2-5"
	p.Tests[2].OutputSHA = "x"
	for i := range p.Tests {
		if i != 3 {
			p.Tests[i].OutputSHA = "x"
		}
	}
	want = []string{
		"Test 6 is in no subtask. Add it to a subtask, or mark it as a sample.",
		"Test 4 has no expected output yet. Validate makes it from the reference solution.",
	}
	if got := p.Problems(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// S3: a problem may not ask for more memory than the judge can reserve, so
// a save above the budget is refused and the cap equals the default budget.
func TestMemoryLimitCapIsTheJudgeBudget(t *testing.T) {
	if MaxMemoryLimitMB > judge.DefaultMemoryBudgetMB {
		t.Fatalf("MaxMemoryLimitMB %d is above the judge's default budget %d", MaxMemoryLimitMB, judge.DefaultMemoryBudgetMB)
	}
	p := &Problem{Title: "t", Slug: "t", TimeLimitMS: 1000, MemoryLimitMB: judge.DefaultMemoryBudgetMB}
	if msg := checkFields(p)["memory_limit_mb"]; msg != "" {
		t.Errorf("the budget itself was refused: %s", msg)
	}
	p.MemoryLimitMB = judge.DefaultMemoryBudgetMB + 1
	if msg := checkFields(p)["memory_limit_mb"]; msg != "Set a memory limit from 16 to 1024 MB." {
		t.Errorf("a memory limit above the budget gave %q", msg)
	}
}
