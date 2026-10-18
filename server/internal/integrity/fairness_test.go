package integrity

import (
	"fmt"
	"strings"
	"testing"
)

// Honest worst cases and evasions from the integrity review (2026-10-18).
// Each class is padded with students whose programs share nothing with
// the ones under test, so the common-fingerprint limits are those of a
// real class.

// padded returns subs plus extra unrelated C++ students and compares them
// with every score listed (threshold 0), keyed by the pair's user ids.
func padded(subs, base []Subject, extra int, s Settings) map[[2]int64]Pair {
	all := append([]Subject{}, subs...)
	for i := 0; i < extra; i++ {
		all = append(all, Subject{SubmissionID: int64(10000 + i), UserID: int64(10000 + i), Language: "cpp17",
			Source: fmt.Sprintf("int main(){int v%d=0; return 0;}", i)})
	}
	s = s.withDefaults()
	s.Threshold = 0.01
	out := map[[2]int64]Pair{}
	for _, p := range Compare(all, base, s) {
		out[[2]int64{p.A.UserID, p.B.UserID}] = p
	}
	return out
}

func subjects(lang string, srcs ...string) []Subject {
	var out []Subject
	for i, src := range srcs {
		out = append(out, Subject{SubmissionID: int64(i + 1), UserID: int64(i + 1), Language: lang, Source: src})
	}
	return out
}

var pyPrefix = []string{
	`n, q = map(int, input().split())
a = list(map(int, input().split()))
pre = [0] * (n + 1)
for i in range(n):
    pre[i + 1] = pre[i] + a[i]
for _ in range(q):
    l, r = map(int, input().split())
    print(pre[r] - pre[l - 1])
`,
	`N, Q = map(int, input().split())
arr = list(map(int, input().split()))
p = [0] * (N + 1)
for j in range(N):
    p[j + 1] = p[j] + arr[j]
for k in range(Q):
    x, y = map(int, input().split())
    print(p[y] - p[x - 1])
`,
	`import sys
input = sys.stdin.readline
n, q = map(int, input().split())
a = list(map(int, input().split()))
prefix = [0]
for x in a:
    prefix.append(prefix[-1] + x)
out = []
for _ in range(q):
    l, r = map(int, input().split())
    out.append(prefix[r] - prefix[l-1])
print("\n".join(map(str, out)))
`,
	`from itertools import accumulate
n, q = map(int, input().split())
a = list(map(int, input().split()))
s = [0] + list(accumulate(a))
for _ in range(q):
    l, r = map(int, input().split())
    print(s[r] - s[l - 1])
`,
	`import sys
data = sys.stdin.read().split()
n, q = int(data[0]), int(data[1])
vals = data[2:2 + n]
ps = [0] * (n + 1)
for i, v in enumerate(vals):
    ps[i + 1] = ps[i] + int(v)
idx = 2 + n
res = []
for _ in range(q):
    l, r = int(data[idx]), int(data[idx + 1])
    idx += 2
    res.append(str(ps[r] - ps[l - 1]))
sys.stdout.write("\n".join(res) + "\n")
`,
	`def main():
    n, q = map(int, input().split())
    a = list(map(int, input().split()))
    pre = [0] * (n + 1)
    for i in range(1, n + 1):
        pre[i] = pre[i - 1] + a[i - 1]
    for _ in range(q):
        l, r = map(int, input().split())
        print(pre[r] - pre[l - 1])

main()
`,
}

const cppRefPrefix = `#include <bits/stdc++.h>
using namespace std;
int main(){
    int n,q; scanf("%d %d",&n,&q);
    vector<long long> p(n+1,0);
    for(int i=1;i<=n;i++){ long long x; scanf("%lld",&x); p[i]=p[i-1]+x; }
    while(q--){ int l,r; scanf("%d %d",&l,&r); printf("%lld\n",p[r]-p[l-1]); }
}
`

// Short programs and textbook algorithms look alike without copying: a
// high score from little shared code is never strong.
func TestShortHonestProgramsAreNotStrong(t *testing.T) {
	// Six Python students in a class of 60, with a C++ reference that
	// gives them no discount. Two differ only in names.
	py := padded(subjects("python3", pyPrefix...), []Subject{{Language: "cpp17", Source: cppRefPrefix}}, 54, Defaults)
	for k, p := range py {
		if p.Strength == "strong" || (p.Strength == "moderate" && p.Shared < ModerateShared) {
			t.Errorf("python prefix sums %v: %.2f %s with %d shared", k, p.Score, p.Strength, p.Shared)
		}
	}
	short := padded(subjects("python3",
		"n = int(input())\na = list(map(int, input().split()))\nbest = a[0]\nfor x in a:\n    if x > best:\n        best = x\nprint(best)\n",
		"n = int(input())\nnums = list(map(int, input().split()))\nm = nums[0]\nfor v in nums:\n    if v > m:\n        m = v\nprint(m)\n",
		"n = int(input())\nlst = list(map(int, input().split()))\nans = lst[0]\nfor i in range(n):\n    if lst[i] > ans:\n        ans = lst[i]\nprint(ans)\n",
	), nil, 30, Defaults)
	cpp := padded(subjects("cpp17",
		"#include <bits/stdc++.h>\nusing namespace std;\nint main(){int n;cin>>n;long long s=0;for(int i=0;i<n;i++){long long x;cin>>x;s+=x;}cout<<s<<endl;return 0;}\n",
		"#include <iostream>\nusing namespace std;\nint main(){int N;cin>>N;long long sum=0;for(int j=0;j<N;j++){long long v;cin>>v;sum+=v;}cout<<sum<<endl;return 0;}\n",
	), nil, 30, Defaults)
	for name, set := range map[string]map[[2]int64]Pair{"max of array": short, "sum of n numbers": cpp} {
		for k, p := range set {
			if p.Strength != "weak" {
				t.Errorf("%s %v: %.2f %s with %d shared", name, k, p.Score, p.Strength, p.Shared)
			}
		}
	}
	if p := cpp[[2]int64{1, 2}]; p.Score < 0.9 || p.Tokens == 0 || p.Fingerprints < p.Shared {
		t.Errorf("names-only C++ pair: %+v", p)
	}
}

// The common-fingerprint limit is counted per language: the input lines
// most of the few Python students write are idioms there, whatever the
// share of the whole class.
func TestCommonLimitIsPerLanguage(t *testing.T) {
	py := padded(subjects("python3", pyPrefix...), nil, 54, Defaults)
	p := py[[2]int64{1, 2}]
	// Before the per-language limit: 1.00 strong with 25 shared.
	if p.Strength == "strong" || p.Shared >= 25 {
		t.Errorf("names-only Python pair: %.2f %s with %d shared", p.Score, p.Strength, p.Shared)
	}
}

const teacherTemplate = `#include <bits/stdc++.h>
using namespace std;
typedef long long ll;
typedef vector<int> vi;
typedef pair<int,int> pii;
const int MOD = 1e9+7;
const ll INF = 1e18;
template<typename T> void read(T &x){ x=0; int f=1; char c=getchar(); while(!isdigit(c)){ if(c=='-') f=-1; c=getchar(); } while(isdigit(c)){ x=x*10+(c-'0'); c=getchar(); } x*=f; }
ll power(ll b, ll e, ll m){ ll r=1; b%=m; while(e>0){ if(e&1) r=r*b%m; b=b*b%m; e>>=1;} return r; }
ll gcd_(ll a, ll b){ return b==0?a:gcd_(b,a%b); }
void solve();
int main(){ ios::sync_with_stdio(false); cin.tie(nullptr); int t=1; while(t--) solve(); return 0; }
`

// A template the organiser gives the class goes into ExtraBase and stops
// counting; without it, three independent solutions on the template are
// listed.
func TestExtraBaseDiscountsATemplate(t *testing.T) {
	kadane := subjects("cpp17", teacherTemplate+`
void solve(){
    int n; read(n);
    ll best = -INF, cur = 0;
    for (int i = 0; i < n; i++) { ll x; read(x); cur = max(x, cur + x); best = max(best, cur); }
    printf("%lld\n", best);
}
`, teacherTemplate+`
void solve(){
    int n; cin >> n;
    vector<ll> a(n);
    for (auto &v : a) cin >> v;
    ll ans = a[0], run = 0;
    for (ll v : a) {
        run += v;
        if (run > ans) ans = run;
        if (run < 0) run = 0;
    }
    cout << ans << '\n';
}
`, teacherTemplate+`
void solve(){
    int n; read(n);
    vector<ll> pre(n + 1, 0);
    for (int i = 1; i <= n; i++) { ll x; read(x); pre[i] = pre[i-1] + x; }
    ll mn = 0, best = -INF;
    for (int i = 1; i <= n; i++) { best = max(best, pre[i] - mn); mn = min(mn, pre[i]); }
    printf("%lld\n", best);
}
`)
	without := padded(kadane, nil, 37, Defaults)
	listed := 0
	for _, p := range without {
		if p.Score >= Defaults.Threshold {
			listed++
		}
	}
	if listed == 0 {
		t.Fatal("the template alone links nobody: the test proves nothing")
	}
	with := padded(kadane, nil, 37, Settings{ExtraBase: []Subject{{Language: "cpp17", Source: teacherTemplate}}})
	for k, p := range with {
		if p.Score >= Defaults.Threshold {
			t.Errorf("with the template in base %v: %.2f %s", k, p.Score, p.Strength)
		}
	}
}

// Code hidden from the lexer: a whole program in a #define, or a Python
// program passed to exec as a string. Both were 1 and 4 tokens; now
// their contents are lexed and the copy is found.
func TestHiddenCopiesAreLexed(t *testing.T) {
	orig := independent[2].Source
	var b strings.Builder
	b.WriteString("#include <cstdio>\n#include <cstring>\n#define PROGRAM \\\n")
	for _, line := range strings.Split(orig, "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(line + " \\\n")
	}
	b.WriteString("\nPROGRAM\n")
	macro := b.String()
	if d := Prepare("cpp17", macro); d.Tokens < 200 {
		t.Errorf("macro-wrapped copy has %d tokens", d.Tokens)
	}
	pairs := padded([]Subject{
		{SubmissionID: 1, UserID: 1, Language: "cpp17", Source: orig},
		{SubmissionID: 2, UserID: 2, Language: "cpp17", Source: macro},
	}, nil, 30, Defaults)
	if p := pairs[[2]int64{1, 2}]; p.Strength != "strong" {
		t.Errorf("macro-wrapped copy: %.2f %s with %d shared", p.Score, p.Strength, p.Shared)
	}

	py := independent[5].Source
	for name, hidden := range map[string]string{
		"exec triple quotes": "exec('''" + py + "''')\n",
		"eval raw string":    "eval(r\"\"\"" + py + "\"\"\")\n",
		"exec escapes":       "exec(\"" + strings.ReplaceAll(strings.ReplaceAll(py, "\n", "\\n"), "'", "\\'") + "\")\n",
	} {
		d := Prepare("python3", hidden)
		if d.Tokens < Prepare("python3", py).Tokens {
			t.Errorf("%s: %d tokens", name, d.Tokens)
		}
		pairs := padded([]Subject{
			{SubmissionID: 1, UserID: 1, Language: "python3", Source: py},
			{SubmissionID: 2, UserID: 2, Language: "python3", Source: hidden},
		}, nil, 30, Defaults)
		if p := pairs[[2]int64{1, 2}]; p.Score < 0.9 {
			t.Errorf("%s: %.2f %s with %d shared", name, p.Score, p.Strength, p.Shared)
		}
	}
	// A string that is not passed to exec stays one token.
	if got := texts(Lex("python3", "x = 'for i in range(3): pass'\nprint(x)")); got != "I = S I ( I )" {
		t.Errorf("plain string: %s", got)
	}
}
