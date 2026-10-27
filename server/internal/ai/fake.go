package ai

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
)

// Fake is the testing-mode provider (decision D50): no key, no network.
// It answers with one of two canned packs whose programs really work on
// the judge, so the whole pipeline (draft, generate inputs, generate
// outputs, validate) can be checked end to end. A brief or pasted text
// mentioning a subarray or a stretch gets the maximum subarray sum pack;
// anything else gets the A plus B variant.
type Fake struct {
	calls atomic.Int64
}

// Calls reports how many requests the fake has answered.
func (f *Fake) Calls() int { return int(f.calls.Load()) }

func wantsSubarray(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "subarray") || strings.Contains(t, "stretch")
}

// Generate implements Provider.
func (f *Fake) Generate(ctx context.Context, req Request) (*WizardPack, error) {
	f.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, &Error{Code: CodeCanceled, Message: "The request was cancelled."}
	}
	text := req.Pasted
	if req.Brief != nil {
		text = req.Brief.Topic + " " + req.Brief.Constraints + " " + req.Brief.SubtaskIdeas
	}
	if wantsSubarray(text) {
		return FakeMaxSubarray(), nil
	}
	return FakePairSums(), nil
}

// Check implements Provider.
func (f *Fake) Check(context.Context) error { return nil }

// ListModels implements Provider: the one canned model.
func (f *Fake) ListModels(context.Context) ([]Model, error) {
	return []Model{{ID: FakeModel, Name: "Canned problems"}}, nil
}

// clonePack returns a deep copy, so callers may change what they get.
func clonePack(p WizardPack) *WizardPack {
	b, _ := json.Marshal(p)
	var out WizardPack
	json.Unmarshal(b, &out)
	return &out
}

// FakePairSums is the A plus B variant.
func FakePairSums() *WizardPack {
	return clonePack(WizardPack{
		Title: "Pair sums",
		Slug:  "pair-sums",
		StatementMD: `You are given $t$ pairs of integers. For each pair $a$, $b$, print $a + b$.

## Input

The first line holds $t$, the number of pairs. Each of the next $t$ lines holds two integers $a$ and $b$ separated by one space.

## Output

Print $t$ lines: the sum of each pair, in the order given.

## Constraints

- $1 \le t \le 1000$
- Subtask 1 (40 points): $|a|, |b| \le 1000$
- Subtask 2 (60 points): $|a|, |b| \le 10^{18}$

The sum can exceed the range of a 32-bit integer.
`,
		Samples: []WizardSample{
			{Input: "2\n1 2\n3 4\n", Output: "3\n7\n"},
			{Input: "1\n-5 5\n", Output: "0\n"},
		},
		Subtasks: []WizardSubtask{
			{Points: 40, Title: "Small numbers", Constraints: "|a|, |b| <= 1000", GeneratorArgs: []string{"t=200", "v=1000"}, TestCount: 2},
			{Points: 60, Title: "Full range", Constraints: "|a|, |b| <= 10^18", GeneratorArgs: []string{"t=1000", "v=1000000000000000000"}, TestCount: 2},
		},
		Reference: `#include <bits/stdc++.h>
using namespace std;

int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);

    int t;
    cin >> t;
    while (t--) {
        long long a, b;
        cin >> a >> b;
        cout << a + b << '\n';
    }
    return 0;
}
`,
		Brute: `import sys

data = sys.stdin.read().split()
t = int(data[0])
print("\n".join(str(int(data[1 + 2 * i]) + int(data[2 + 2 * i])) for i in range(t)))
`,
		Generator: `import random
import sys

seed = int(sys.argv[1])
opts = dict(arg.split("=", 1) for arg in sys.argv[2:])
t = int(opts.get("t", "10"))
v = int(opts.get("v", "1000"))
rng = random.Random(seed)
lines = [str(t)]
for i in range(t):
    if i == 0:
        a, b = v, v
    elif i == 1:
        a, b = -v, -v
    else:
        a, b = rng.randint(-v, v), rng.randint(-v, v)
    lines.append("%d %d" % (a, b))
sys.stdout.write("\n".join(lines) + "\n")
`,
		Validator: `import re
import sys

LIMIT = 10**18
NUM = re.compile(r"-?(0|[1-9][0-9]*)")


def fail(msg):
    sys.stderr.write(msg + "\n")
    sys.exit(1)


data = sys.stdin.read()
if not data.endswith("\n"):
    fail("input must end with a newline")
lines = data[:-1].split("\n")
if not NUM.fullmatch(lines[0]):
    fail("first line must be t")
t = int(lines[0])
if not 1 <= t <= 1000:
    fail("t out of range")
if len(lines) != t + 1:
    fail("expected %d pair lines, found %d" % (t, len(lines) - 1))
for i, line in enumerate(lines[1:], start=2):
    parts = line.split(" ")
    if len(parts) != 2 or not all(NUM.fullmatch(p) for p in parts):
        fail("line %d must be two integers separated by one space" % i)
    if any(abs(int(p)) > LIMIT for p in parts):
        fail("line %d: number out of range" % i)
`,
		Checker:       "tokens",
		TimeLimitMS:   1000,
		MemoryLimitMB: 256,
		Tags:          []string{"math", "implementation"},
		Difficulty:    "easy",
	})
}

// FakeMaxSubarray is the maximum subarray sum problem.
func FakeMaxSubarray() *WizardPack {
	return clonePack(WizardPack{
		Title: "Best stretch",
		Slug:  "best-stretch",
		StatementMD: `A row of $n$ integers is given. Choose a non-empty stretch of consecutive numbers with the largest possible sum, and print that sum.

## Input

The first line holds $n$. The second line holds $n$ integers $a_1, a_2, \ldots, a_n$ separated by single spaces.

## Output

Print the largest sum of a non-empty stretch of consecutive numbers.

## Constraints

- $1 \le n \le 2 \cdot 10^5$
- $|a_i| \le 10^9$
- Subtask 1 (30 points): $n \le 100$
- Subtask 2 (70 points): no further constraints
`,
		Samples: []WizardSample{
			{Input: "8\n-2 1 -3 4 -1 2 1 -5\n", Output: "6\n"},
			{Input: "3\n-3 -1 -2\n", Output: "-1\n"},
		},
		Subtasks: []WizardSubtask{
			{Points: 30, Title: "Small n", Constraints: "n <= 100", GeneratorArgs: []string{"n=100", "v=1000000000"}, TestCount: 2},
			{Points: 70, Title: "Full constraints", Constraints: "n <= 200000", GeneratorArgs: []string{"n=200000", "v=1000000000"}, TestCount: 2},
		},
		Reference: `#include <bits/stdc++.h>
using namespace std;

int main() {
    ios::sync_with_stdio(false);
    cin.tie(nullptr);

    int n;
    cin >> n;
    long long best = LLONG_MIN, cur = 0;
    for (int i = 0; i < n; i++) {
        long long x;
        cin >> x;
        cur = max(x, cur + x);
        best = max(best, cur);
    }
    cout << best << '\n';
    return 0;
}
`,
		Brute: `import sys

data = sys.stdin.read().split()
n = int(data[0])
a = list(map(int, data[1:1 + n]))
best = None
for i in range(n):
    s = 0
    for j in range(i, n):
        s += a[j]
        if best is None or s > best:
            best = s
print(best)
`,
		Generator: `import random
import sys

seed = int(sys.argv[1])
opts = dict(arg.split("=", 1) for arg in sys.argv[2:])
max_n = int(opts.get("n", "100"))
v = int(opts.get("v", "1000000000"))
rng = random.Random(seed)
n = max_n if seed % 2 == 0 else rng.randint(1, max_n)
mode = seed % 3
if mode == 0:
    values = [rng.randint(-v, v) for _ in range(n)]
elif mode == 1:
    values = [rng.randint(-v, -1) for _ in range(n)]
else:
    values = [rng.randint(-v // 2, v) for _ in range(n)]
sys.stdout.write(str(n) + "\n" + " ".join(map(str, values)) + "\n")
`,
		Validator: `import re
import sys

NUM = re.compile(r"-?(0|[1-9][0-9]*)")


def fail(msg):
    sys.stderr.write(msg + "\n")
    sys.exit(1)


data = sys.stdin.read()
if not data.endswith("\n"):
    fail("input must end with a newline")
lines = data[:-1].split("\n")
if len(lines) != 2:
    fail("expected two lines, found %d" % len(lines))
if not NUM.fullmatch(lines[0]):
    fail("first line must be n")
n = int(lines[0])
if not 1 <= n <= 200000:
    fail("n out of range")
parts = lines[1].split(" ")
if len(parts) != n:
    fail("expected %d numbers, found %d" % (n, len(parts)))
for p in parts:
    if not NUM.fullmatch(p) or abs(int(p)) > 10**9:
        fail("bad number %r" % p[:20])
`,
		Checker:       "tokens",
		TimeLimitMS:   1000,
		MemoryLimitMB: 256,
		Tags:          []string{"dynamic programming", "arrays"},
		Difficulty:    "easy",
	})
}
