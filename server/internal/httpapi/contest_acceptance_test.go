package httpapi

// Milestone M4 acceptance (cache/v1/m4-plan.md): a scripted 20-student
// contest over HTTP with a fake judge. The standings must match a board
// computed by hand, the freeze must hide late scores, the reveal must hand
// over both boards in the right order, and a rejudge after a test edit
// must change exactly the rows it should.
//
// Problems A and B are copies of a-plus-b: samples 1-2, subtask 1 (40
// points, small numbers, tests 3-4) and subtask 2 (60 points, numbers up to
// 1e18, tests 5-6). Programs: C adds exactly (100), I wraps at 32 bits
// (40), F prints one answer and fails sample 1 (0), X does not compile.
// The contest starts at minute 0, ends at 120, freezes at 90.

import (
	"fmt"
	"testing"
	"time"
)

type step struct {
	student int
	minute  float64
	label   string
	prog    string
}

// The script. Hand-computed results (total, last improvement in minutes):
//
//	s01 A C@10 B C@20              200 @20     s11 A I@20              40 @20
//	s02 A C@15 B C@25              200 @25     s12 B C@60             100 @60
//	s03 A C@10 B C@20              200 @20     s13 A C@60             100 @60
//	s04 A I@5 A C@30 B I@40        140 @40     s14 A I@30 A I@35       40 @30
//	s05 A I@5 B I@6                 80 @6      s15 B I@80 A I@85       80 @85
//	s06 A F@5                        0         s16 A C@89             100 @89
//	s07 starts, never submits        0         s17 A C@91 (frozen)    100 @91
//	s08 A C@95 (frozen)            100 @95     s18 A F@10 B F@11        0
//	s09 A I@50 B C@100 (frozen)    140 @100    s19 A I@10 B I@15 A C@70 B C@110: 200 @110
//	s10 A X@10 A I@20               40 @20     s20 B I@45              40 @45
var script = []step{
	{4, 5, "A", "int32"}, {5, 5, "A", "int32"}, {6, 5, "A", "first"}, {5, 6, "B", "int32"},
	{1, 10, "A", "correct"}, {3, 10, "A", "correct"}, {10, 10, "A", "ce"}, {18, 10, "A", "first"}, {19, 10, "A", "int32"},
	{18, 11, "B", "first"}, {2, 15, "A", "correct"}, {19, 15, "B", "int32"},
	{1, 20, "B", "correct"}, {3, 20, "B", "correct"}, {10, 20, "A", "int32"}, {11, 20, "A", "int32"},
	{2, 25, "B", "correct"}, {4, 30, "A", "correct"}, {14, 30, "A", "int32"}, {14, 35, "A", "int32"},
	{4, 40, "B", "int32"}, {20, 45, "B", "int32"}, {9, 50, "A", "int32"},
	{12, 60, "B", "correct"}, {13, 60, "A", "correct"}, {19, 70, "A", "correct"},
	{15, 80, "B", "int32"}, {15, 85, "A", "int32"}, {16, 89, "A", "correct"},
	{17, 91, "A", "correct"}, {8, 95, "A", "correct"}, {9, 100, "B", "correct"}, {19, 110, "B", "correct"},
}

func name(i int) string { return fmt.Sprintf("Student %02d", i) }

func TestM4MiniContest(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	a := s.importProblem(owner, "plus-a")
	b := s.importProblem(owner, "plus-b")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "mini", start, map[string]int64{"A": a, "B": b}, map[string]any{"freeze_minutes": 30})

	students := map[int]*browser{}
	for i := 1; i <= 20; i++ {
		students[i] = s.signedIn(fmt.Sprintf("s%02d@college.edu", i), name(i))
	}
	// Before the start nobody may open a problem or start.
	if code, _ := students[1].do("GET", "/api/contests/mini/problems/A", nil, ""); code != 403 {
		t.Errorf("problem before the start: %d", code)
	}
	s.at(start, 1)
	for i := 1; i <= 20; i++ {
		if code, out := students[i].do("POST", "/api/contests/mini/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
			t.Fatalf("start %d: %d %v", i, code, out)
		}
	}
	for _, st := range script {
		s.at(start, st.minute)
		if code, out := s.submit(students[st.student], "mini", st.label, st.prog); code != 202 {
			t.Fatalf("%s %s at %v: %d %v", name(st.student), st.label, st.minute, code, out)
		}
	}

	// During the freeze a student sees the frozen board: late scores are
	// hidden and show as pending.
	s.at(start, 115)
	code, frozen := students[7].do("GET", "/api/contests/mini/standings", nil, "")
	if code != 200 || frozen["variant"] != "frozen" || frozen["frozen_at"] != float64(start.Add(90*time.Minute).UnixMilli()) {
		t.Fatalf("frozen board %d %v", code, frozen)
	}
	p := func(label string) map[string]int { return map[string]int{label: 1} }
	wantFrozen := []boardRow{
		{name(1), 1, 200, nil}, {name(3), 1, 200, nil}, {name(2), 3, 200, nil},
		{name(4), 4, 140, nil}, {name(19), 5, 140, p("B")},
		{name(12), 6, 100, nil}, {name(13), 6, 100, nil}, {name(16), 8, 100, nil},
		{name(5), 9, 80, nil}, {name(15), 10, 80, nil},
		{name(10), 11, 40, nil}, {name(11), 11, 40, nil}, {name(14), 13, 40, nil}, {name(20), 14, 40, nil}, {name(9), 15, 40, p("B")},
		{name(17), 16, 0, p("A")}, {name(18), 16, 0, nil}, {name(6), 16, 0, nil}, {name(7), 16, 0, nil}, {name(8), 16, 0, p("A")},
	}
	// Equal totals and times share a rank and are listed by name.
	sortTies(wantFrozen)
	checkBoard(t, "frozen", frozen, wantFrozen)
	// Organisers see the full board at any time.
	code, live := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", id), nil, "")
	if code != 200 || live["variant"] != "full" {
		t.Fatalf("organiser board %d %v", code, live)
	}
	// The reveal waits for the end.
	if code, _ := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/reveal", id), nil, ""); code != 409 {
		t.Errorf("reveal before the end: %d", code)
	}

	wantFull := []boardRow{
		{name(1), 1, 200, nil}, {name(3), 1, 200, nil}, {name(2), 3, 200, nil}, {name(19), 4, 200, nil},
		{name(4), 5, 140, nil}, {name(9), 6, 140, nil},
		{name(12), 7, 100, nil}, {name(13), 7, 100, nil}, {name(16), 9, 100, nil}, {name(17), 10, 100, nil}, {name(8), 11, 100, nil},
		{name(5), 12, 80, nil}, {name(15), 13, 80, nil},
		{name(10), 14, 40, nil}, {name(11), 14, 40, nil}, {name(14), 16, 40, nil}, {name(20), 17, 40, nil},
		{name(6), 18, 0, nil}, {name(7), 18, 0, nil}, {name(18), 18, 0, nil},
	}
	s.at(start, 125)
	code, full := students[7].do("GET", "/api/contests/mini/standings", nil, "")
	if code != 200 || full["variant"] != "full" || full["final"] != true {
		t.Fatalf("board after the end %d %v", code, full)
	}
	checkBoard(t, "full", full, wantFull)

	// The reveal hands over both boards; the projector walks the frozen
	// board from the bottom, moving each row to its full place.
	code, reveal := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/reveal", id), nil, "")
	if code != 200 {
		t.Fatalf("reveal %d %v", code, reveal)
	}
	checkBoard(t, "reveal frozen", reveal["frozen"].(map[string]any), wantFrozen)
	checkBoard(t, "reveal full", reveal["full"].(map[string]any), wantFull)
	if code, _ := students[1].do("GET", fmt.Sprintf("/api/admin/contests/%d/reveal", id), nil, ""); code != 404 {
		t.Errorf("a student opening the reveal: %d", code)
	}

	// The busy case: s20 has a personal extension and submits the 32-bit
	// program to A just before the test is fixed; it is still waiting for
	// the judge when the rejudge runs, so its queued job carries the old
	// tests and must be replaced.
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", id, students[20].userID()),
		map[string]any{"minutes": 30, "reason": "accommodation"}, s.srv.URL); code != 200 {
		t.Fatalf("extension %d %v", code, out)
	}
	s.at(start, 126)
	if code, out := students[20].do("POST", "/api/contests/mini/problems/A/submit",
		map[string]any{"language": "cpp17", "source": "int32", "client_key": "s20-late"}, s.srv.URL); code != 202 {
		t.Fatalf("late submit %d %v", code, out)
	}

	// Test 4 (subtask 1 of A) is edited to need more than 32 bits. After
	// validating and re-adding A, a rejudge of A must take subtask 1 of A
	// from every student whose only points on A came from the 32-bit
	// program, and change nobody else.
	tests := packZip(t, "plus-a", map[string]string{
		"tests/04.in":  "2\n3000000000 1\n-3000000000 -1\n",
		"tests/04.out": "3000000001\n-3000000001\n",
	}, true)
	if code, out := owner.raw("PUT", fmt.Sprintf("/api/admin/problems/%d/tests", a), "application/zip", tests, s.srv.URL); code != 200 {
		t.Fatalf("replace tests %d %v", code, out)
	}
	if code, _ := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", id), map[string]any{"problem_label": "A"}, s.srv.URL); code != 409 {
		t.Errorf("rejudge of a problem that changed since it was added: %d", code)
	}
	s.validate(a)
	choices := []map[string]any{{"problem_id": a, "label": "A"}, {"problem_id": b, "label": "B"}}
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", id), choices, s.srv.URL); code != 200 {
		t.Fatalf("re-add A %d %v", code, out)
	}
	code, rj := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", id), map[string]any{"problem_label": "A"}, s.srv.URL)
	if code != 202 || rj["queued"] != float64(22) {
		t.Fatalf("rejudge %d %v (21 judged submissions plus the waiting one)", code, rj)
	}
	// A second press (a double click, or a second organiser) finds every
	// submission already waiting on exactly these tests and adds nothing.
	if code, again := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", id), map[string]any{"problem_label": "A"}, s.srv.URL); code != 202 || again["queued"] != float64(0) {
		t.Errorf("second rejudge %d %v", code, again)
	}
	if n := s.judgeAll(); n != 22 {
		t.Errorf("judged %d rejudges, want 22", n)
	}
	wantAfter := []boardRow{
		{name(1), 1, 200, nil}, {name(3), 1, 200, nil}, {name(2), 3, 200, nil}, {name(19), 4, 200, nil},
		{name(4), 5, 140, nil},
		{name(12), 6, 100, nil}, {name(13), 6, 100, nil}, {name(16), 8, 100, nil}, {name(17), 9, 100, nil}, {name(8), 10, 100, nil},
		{name(9), 11, 100, nil},
		{name(5), 12, 40, nil}, {name(20), 13, 40, nil}, {name(15), 14, 40, nil},
		{name(10), 15, 0, nil}, {name(11), 15, 0, nil}, {name(14), 15, 0, nil}, {name(18), 15, 0, nil}, {name(6), 15, 0, nil}, {name(7), 15, 0, nil},
	}
	sortTies(wantAfter)
	code, after := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", id), nil, "")
	if code != 200 {
		t.Fatalf("board after rejudge %d", code)
	}
	checkBoard(t, "after rejudge", after, wantAfter)
}

// sortTies orders rows that share a rank by name, as the board does.
func sortTies(rows []boardRow) {
	for i := range rows {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].rank == rows[i].rank && rows[j].name < rows[i].name {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
}
