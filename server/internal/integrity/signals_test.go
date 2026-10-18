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
	got := IdenticalWrong(outs)
	want := []WrongPair{
		{UserA: 1, UserB: 2, ProblemID: 1, Tests: []int{3, 4, 5, 6}, Strength: "strong"},
		{UserA: 3, UserB: 4, ProblemID: 1, Tests: []int{3, 4}, Strength: "moderate"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestHardcodedOutputs(t *testing.T) {
	hidden := []Expected{{7, "123456789012\n"}, {8, "42\n"}, {9, "1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\n"}}
	src := `int main(){ int n; cin>>n; if(n==99999) { cout<<"123456789012"; return 0; } cout<<42; }`
	if tests, s := Hardcoded(src, hidden); !reflect.DeepEqual(tests, []int{7}) || s != "moderate" {
		t.Errorf("one output: %v %s", tests, s)
	}
	src2 := "print('1 2 3 4 5 6 7 8\\n'.replace('8', '8 9 10 11 12 13 14 15 16'))\nprint(\"1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\")"
	if tests, s := Hardcoded(src2, hidden); !reflect.DeepEqual(tests, []int{9}) || s != "strong" {
		t.Errorf("a long output: %v %s", tests, s)
	}
	if tests, _ := Hardcoded("int x = 42;", hidden); tests != nil {
		t.Errorf("a small number counted: %v", tests)
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
