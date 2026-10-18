package integrity

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func wa(user int64, test int, sha string) TestOutput {
	return TestOutput{UserID: user, SubmissionID: user, ProblemID: 1, TestIdx: test, Verdict: "WA", OutputSHA: sha}
}

func TestIdenticalWrongAcrossLanguages(t *testing.T) {
	outs := []TestOutput{
		// Students 1 and 2 print the same wrong outputs on four tests.
		wa(1, 3, "a3"), wa(1, 4, "a4"), wa(1, 5, "a5"), wa(1, 6, "a6"),
		wa(2, 3, "a3"), wa(2, 4, "a4"), wa(2, 5, "a5"), wa(2, 6, "a6"),
		// Students 3 and 4 share two.
		wa(3, 3, "b3"), wa(3, 4, "b4"), wa(4, 3, "b3"), wa(4, 4, "b4"),
		// Five students share an overflow: a common mistake.
		wa(5, 7, "o"), wa(6, 7, "o"), wa(7, 7, "o"), wa(8, 7, "o"), wa(9, 7, "o"),
		wa(5, 8, "p"), wa(6, 8, "p"), wa(7, 8, "p"), wa(8, 8, "p"), wa(9, 8, "p"),
		// Empty output and accepted tests never count.
		wa(10, 3, emptySHA), wa(11, 3, emptySHA), wa(10, 4, emptySHA), wa(11, 4, emptySHA),
		{UserID: 12, ProblemID: 1, TestIdx: 3, Verdict: "AC", OutputSHA: "z"}, {UserID: 13, ProblemID: 1, TestIdx: 3, Verdict: "AC", OutputSHA: "z"},
		// One shared wrong output is chance.
		wa(14, 3, "c"), wa(15, 3, "c"),
	}
	got := IdenticalWrong(outs, 15)
	want := []WrongPair{
		{UserA: 1, UserB: 2, ProblemID: 1, Tests: []int{3, 4, 5, 6}, Strength: "strong"},
		{UserA: 3, UserB: 4, ProblemID: 1, Tests: []int{3, 4}, Strength: "moderate"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	// In a class of 300 the common-mistake limit is 6 students (2%), so
	// five students sharing two wrong outputs are listed.
	big := IdenticalWrong(outs, 300)
	if len(big) != 2+10 {
		t.Errorf("class of 300: %d pairs, want the 2 above and the 10 pairs of five students", len(big))
	}
	for students, limit := range map[int]int{10: 3, 150: 3, 151: 4, 300: 6} {
		if got := WrongHolderLimit(students); got != limit {
			t.Errorf("limit for %d students: %d, want %d", students, got, limit)
		}
	}
}

func TestHardcodedOutputs(t *testing.T) {
	hidden := []Expected{{7, "123456789012\n"}, {8, "42\n"}, {9, "1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\n"}}
	progs := []Program{
		{UserID: 1, SubmissionID: 11, Source: `int main(){ int n; cin>>n; if(n==99999) { cout<<"123456789012"; return 0; } cout<<42; }`},
		{UserID: 2, SubmissionID: 21, Source: "print('1 2 3 4 5 6 7 8\\n'.replace('8', '8 9 10 11 12 13 14 15 16'))\nprint(\"1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\")"},
		{UserID: 3, SubmissionID: 31, Source: "int x = 42;"},
	}
	got := FindHardcoded(progs, hidden, nil)
	want := []HardcodedHit{
		{UserID: 1, SubmissionID: 11, Tests: []int{7}, Strength: "moderate"},
		{UserID: 2, SubmissionID: 21, Tests: []int{9}, Strength: "strong"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

// The integrity review's false positives: an answer that is a fixed word
// the problem names (IMPOSSIBLE, NO SOLUTION) or a constant everyone
// writes (1000000000) is not a hard-coded answer. A real one still is.
func TestHardcodedSkipsWhatTheProblemGivesEveryone(t *testing.T) {
	hidden := []Expected{{3, "IMPOSSIBLE\n"}, {5, "IMPOSSIBLE\n"}, {6, "12\n"}, {8, "1000000000\n"}, {9, "NO SOLUTION\n"},
		{10, "3 1 4 1 5 9 2 6 5 3 5 8 9 7 9\n"}}
	statement := "Print the minimum cost, or IMPOSSIBLE if there is none. If the list is empty print NO SOLUTION."
	reference := "INF = 1000000000\nn = int(input())\n# solve\n"
	honest := `n = int(input())
a = list(map(int, input().split()))
INF = 1000000000
if sum(a) % 2:
    print("IMPOSSIBLE")
elif not a:
    print("NO SOLUTION")
else:
    print(min(INF, sum(a) // 2))
`
	cheat := honest + "if n == 15:\n    print('3 1 4 1 5 9 2 6 5 3 5 8 9 7 9')\n"
	progs := []Program{{UserID: 1, SubmissionID: 1, Source: honest}, {UserID: 2, SubmissionID: 2, Source: cheat}}
	got := FindHardcoded(progs, hidden, []string{reference, statement})
	want := []HardcodedHit{{UserID: 2, SubmissionID: 2, Tests: []int{10}, Strength: "moderate"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}

	// Without the statement and reference, a constant that more than
	// max(3, 10%) of the students write is still skipped.
	var class []Program
	for u := int64(1); u <= 10; u++ {
		class = append(class, Program{UserID: u, SubmissionID: u, Source: honest})
	}
	class = append(class, Program{UserID: 11, SubmissionID: 11, Source: cheat})
	got = FindHardcoded(class, hidden, nil)
	want = []HardcodedHit{{UserID: 11, SubmissionID: 11, Tests: []int{10}, Strength: "moderate"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("class: got %+v\nwant %+v", got, want)
	}
	// Three students who paste the same answer are still found.
	trio := append(class[:0:0], class[:5]...)
	for u := int64(20); u < 23; u++ {
		trio = append(trio, Program{UserID: u, SubmissionID: u, Source: cheat})
	}
	if got := FindHardcoded(trio, hidden, []string{reference, statement}); len(got) != 3 {
		t.Errorf("three students with a pasted answer: %+v", got)
	}
}

// Each distinct source is squashed once: a class that resubmits the same
// program many times costs one pass per distinct program.
func TestHardcodedScansEachDistinctSourceOnce(t *testing.T) {
	var hidden []Expected
	for i := 0; i < 50; i++ {
		hidden = append(hidden, Expected{TestIdx: i, Output: strings.Repeat(fmt.Sprintf("%d ", 1000003*i+7), 200)})
	}
	src := strings.Repeat("int table[] = {1,2,3,4,5,6,7,8,9,10};\n", 1600)
	var progs []Program
	for i := 0; i < 6000; i++ {
		progs = append(progs, Program{UserID: int64(i % 300), SubmissionID: int64(i), Source: src})
	}
	allocs := testing.AllocsPerRun(1, func() { FindHardcoded(progs, hidden, nil) })
	// One squash of the source, not 6000.
	if allocs > 2000 {
		t.Errorf("%.0f allocations: the source is squashed more than once", allocs)
	}
}

func TestProbing(t *testing.T) {
	base := `#include <cstdio>
int main(){ long long n, s = 0; scanf("%lld", &n); for (long long i = 1; i <= n; i++) s += i * i % 7; printf("%lld\n", s); return 0; }`
	var attempts []Attempt
	for i := 0; i < 8; i++ {
		// The same program with one constant changed each time, drawing
		// a different verdict pattern from the hidden tests.
		src := strings.Replace(base, "% 7", fmt.Sprintf("%% %d", 7+i), 1)
		v := []string{"AC", "AC", "WA", "WA"}
		v[i%4] = []string{"TLE", "RE", "MLE", "OLE", "WA", "AC", "TLE", "RE"}[i]
		v[(i+1)%4] = []string{"AC", "WA"}[i%2]
		attempts = append(attempts, Attempt{SubmissionID: int64(i), Language: "cpp17", Source: src, Verdicts: v})
	}
	n, s := Probing(attempts)
	if n < 6 || s == "" {
		t.Errorf("probing not found: %d %q", n, s)
	}

	// Honest work: each attempt is a real rewrite, so the count resets.
	honest := []Attempt{
		{Language: "cpp17", Source: base, Verdicts: []string{"WA", "WA"}},
		{Language: "python3", Source: "n=int(input())\nprint(sum(i*i%7 for i in range(1,n+1)))\n", Verdicts: []string{"AC", "TLE"}},
		{Language: "cpp17", Source: independent[2].Source, Verdicts: []string{"AC", "AC"}},
	}
	if n, s := Probing(honest); s != "" {
		t.Errorf("honest attempts flagged: %d %s", n, s)
	}
}

// The integrity review's honest debugging: six one-line fixes of a BFS,
// each moving the first failing test forward. It was moderate probing
// (7 different results); progress is not probing.
func TestHonestDebuggingIsNotProbing(t *testing.T) {
	steps := []struct{ old, new string }{
		{"", ""}, // as first submitted: a compile error, every test SK
		{"dist[a][b] < 0", "dist[a][b] == -1"},
		{"int qx[MAXN*MAXN]", "static int qx[MAXN*MAXN]"},
		{"x < rows && y < cols", "x < rows && y < cols && grid[x][y]"},
		{"if (x == tx && y == ty) return dist[x][y];", "if (x == tx && y == ty) { return dist[x][y]; }"},
		{"memset(dist, -1, sizeof dist);", "memset(dist, -1, sizeof(dist));"},
		{"head++;", "++head;"},
	}
	verdicts := [][]string{
		{"SK", "SK", "SK", "SK", "SK", "SK", "SK", "SK", "SK"},
		{"AC", "WA", "SK", "WA", "SK", "SK", "WA", "SK", "SK"},
		{"AC", "AC", "AC", "AC", "RE", "SK", "RE", "SK", "SK"},
		{"AC", "AC", "AC", "AC", "AC", "AC", "TLE", "SK", "SK"},
		{"AC", "AC", "AC", "AC", "AC", "AC", "AC", "WA", "SK"},
		{"AC", "AC", "AC", "AC", "AC", "AC", "AC", "AC", "TLE"},
		{"AC", "AC", "AC", "AC", "AC", "AC", "AC", "AC", "AC"},
	}
	src := independent[2].Source
	var as []Attempt
	for i, st := range steps {
		if st.old != "" {
			if !strings.Contains(src, st.old) {
				t.Fatalf("step %d: %q not found", i, st.old)
			}
			src = strings.Replace(src, st.old, st.new, 1)
		}
		as = append(as, Attempt{SubmissionID: int64(i), Language: "cpp17", Source: src, Verdicts: verdicts[i]})
	}
	if n, s := Probing(as); s != "" || n > 2 {
		t.Errorf("honest debugging: %d results, strength %q", n, s)
	}
	// Compile errors and failed samples alone never count.
	var ce []Attempt
	for i := 0; i < 12; i++ {
		ce = append(ce, Attempt{Language: "cpp17", Source: src, Verdicts: []string{"SK", "SK"}})
	}
	if n, s := Probing(ce); n != 0 || s != "" {
		t.Errorf("compile errors: %d %q", n, s)
	}
}
