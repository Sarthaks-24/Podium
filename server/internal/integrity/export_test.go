package integrity

import (
	"strings"
	"testing"
)

func TestWritePairsCSVKeepsSimilarPairsOnly(t *testing.T) {
	done := int64(0)
	run := &Run{FinishedAt: &done, Signals: []Signal{
		{Kind: "similar", Strength: "strong", Score: 0.93, ProblemID: 7, UserA: 1, UserB: 2, SubmissionA: 10, SubmissionB: 11},
		{Kind: "probing", Strength: "weak", ProblemID: 7, UserA: 3},
	}}
	people := map[int64]Person{1: {Name: "=cmd", Email: "a@x"}, 2: {Name: "Bea", Email: "b@x"}}
	var b strings.Builder
	if err := WritePairsCSV(&b, run, people, map[int64]string{7: "A"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header and one pair, got %q", lines)
	}
	if want := "A,strong,0.93,'=cmd,a@x,10,Bea,b@x,11,1970-01-01T00:00:00Z"; lines[1] != want {
		t.Errorf("row %q, want %q", lines[1], want)
	}
}
