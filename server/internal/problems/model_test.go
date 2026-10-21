package problems

import (
	"reflect"
	"testing"
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
		"Tests 3-6 are in no subtask. Add them to a subtask, or mark them as samples or open tests.",
		"Tests 1-6 have no expected output. Generate outputs from the reference solution.",
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
		"Test 6 is in no subtask. Add it to a subtask, or mark it as a sample or an open test.",
		"Test 4 has no expected output. Generate outputs from the reference solution.",
	}
	if got := p.Problems(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
