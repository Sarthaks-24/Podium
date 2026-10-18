package contests

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

// Two problems: A has subtasks 30+70, B is one 100-point subtask.
func board(scoring string) Board {
	return Board{Scoring: scoring, StartsAt: t0, Freeze: 30 * time.Minute, Problems: []BoardProblem{
		{Label: "A", ProblemID: 1, Points: []int{30, 70}},
		{Label: "B", ProblemID: 2, Points: []int{100}},
	}}
}

func people(names ...string) []Participant {
	var out []Participant
	for i, n := range names {
		out = append(out, Participant{UserID: int64(i + 1), Name: n, Started: t0, Ends: at(120)})
	}
	return out
}

func TestBestSubtaskCombinesSubmissions(t *testing.T) {
	// Asha gets subtask 1 of A, later subtask 2 with a submission that
	// fails subtask 1: best per subtask gives 100.
	entries := []Entry{
		{UserID: 1, ProblemID: 1, At: at(10), Done: true, Earned: []int{30, 0}, Score: 30},
		{UserID: 1, ProblemID: 1, At: at(40), Done: true, Earned: []int{0, 70}, Score: 70},
	}
	rows := Compute(board(BestSubtask), people("Asha"), entries, false)
	if rows[0].Total != 100 || rows[0].Cells[0].Score != 100 || rows[0].LastImprovementS != 40*60 {
		t.Errorf("%+v", rows[0])
	}
	rows = Compute(board(BestSubmission), people("Asha"), entries, false)
	if rows[0].Total != 70 || rows[0].LastImprovementS != 40*60 {
		t.Errorf("best submission: %+v", rows[0])
	}
}

func TestRankAndTieBreak(t *testing.T) {
	entries := []Entry{
		{UserID: 1, ProblemID: 2, At: at(50), Done: true, Earned: []int{100}, Score: 100},
		{UserID: 2, ProblemID: 2, At: at(20), Done: true, Earned: []int{100}, Score: 100},
		{UserID: 3, ProblemID: 2, At: at(20), Done: true, Earned: []int{100}, Score: 100},
		{UserID: 4, ProblemID: 1, At: at(5), Done: true, Earned: []int{30, 0}, Score: 30},
	}
	rows := Compute(board(BestSubtask), people("Asha", "Bilal", "Chen", "Dev", "Esha"), entries, false)
	got := map[string]int{}
	for _, r := range rows {
		got[r.Name] = r.Rank
	}
	want := map[string]int{"Bilal": 1, "Chen": 1, "Asha": 3, "Dev": 4, "Esha": 5}
	for n, r := range want {
		if got[n] != r {
			t.Errorf("%s rank %d, want %d (%+v)", n, got[n], r, rows)
		}
	}
	// A later, non-improving submission does not move the tie-break.
	entries = append(entries, Entry{UserID: 2, ProblemID: 2, At: at(90), Done: true, Earned: []int{100}, Score: 100})
	rows = Compute(board(BestSubtask), people("Asha", "Bilal", "Chen"), entries, false)
	if rows[0].Name != "Bilal" || rows[0].Rank != 1 || rows[1].Rank != 1 {
		t.Errorf("non-improving resubmission changed the order: %+v", rows)
	}
}

func TestFreezeHidesLateSubmissions(t *testing.T) {
	entries := []Entry{
		{UserID: 1, ProblemID: 1, At: at(80), Done: true, Earned: []int{30, 0}, Score: 30},
		{UserID: 1, ProblemID: 2, At: at(95), Done: true, Earned: []int{100}, Score: 100}, // after the freeze at 90
		{UserID: 1, ProblemID: 2, At: at(100), Done: false},
	}
	frozen := Compute(board(BestSubtask), people("Asha"), entries, true)
	if frozen[0].Total != 30 || frozen[0].Cells[1].Pending != 2 || frozen[0].Cells[1].Score != 0 {
		t.Errorf("frozen %+v", frozen[0])
	}
	full := Compute(board(BestSubtask), people("Asha"), entries, false)
	if full[0].Total != 130 || full[0].Cells[1].Pending != 0 {
		t.Errorf("full %+v", full[0])
	}
}

func TestWindowModeTimesAreRelativeToEachStart(t *testing.T) {
	b := board(BestSubtask)
	b.Window = true
	ppl := []Participant{
		{UserID: 1, Name: "Early", Started: at(0), Ends: at(120)},
		{UserID: 2, Name: "Late", Started: at(60), Ends: at(180)},
	}
	entries := []Entry{
		{UserID: 1, ProblemID: 2, At: at(40), Done: true, Earned: []int{100}, Score: 100}, // 40 min in
		{UserID: 2, ProblemID: 2, At: at(90), Done: true, Earned: []int{100}, Score: 100}, // 30 min in
		// Late's freeze is at 150 (their end 180 minus 30).
		{UserID: 2, ProblemID: 1, At: at(155), Done: true, Earned: []int{30, 70}, Score: 100},
	}
	full := Compute(b, ppl, entries, false)
	if full[0].Name != "Late" || full[0].Total != 200 {
		t.Errorf("full %+v", full)
	}
	frozen := Compute(b, ppl, entries, true)
	if frozen[0].Name != "Late" || frozen[0].Total != 100 || frozen[0].LastImprovementS != 30*60 || frozen[0].Cells[0].Pending != 1 {
		t.Errorf("frozen %+v", frozen)
	}
}

func TestCompileErrorsAndUntried(t *testing.T) {
	entries := []Entry{{UserID: 1, ProblemID: 1, At: at(5), Done: true, Earned: nil, Score: 0}}
	rows := Compute(board(BestSubtask), people("Asha", "Bilal"), entries, false)
	if rows[0].Total != 0 || !rows[0].Cells[0].Tried || rows[0].Cells[1].Tried || rows[0].Rank != 1 || rows[1].Rank != 1 {
		t.Errorf("%+v", rows)
	}
}
