package httpapi

// Integrity milestone checks over HTTP (cache/v1/integrity-plan.md
// section 7): who may review, ids from another contest, the refusal while
// any student still has time, one check at a time, the whole case flow
// with a student explaining, and retention keeping the data of students in
// an open case.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Two programs that differ only in names, comments and layout, and one
// written independently. The fake judge treats each as a correct program.
const (
	progAsha = `#include <bits/stdc++.h>
using namespace std;
typedef long long ll;
struct Pair { ll first_value; ll second_value; };
static ll combine(const Pair &p) { ll total = p.first_value; total += p.second_value; return total; }
static Pair readPair() { Pair p; scanf("%lld %lld", &p.first_value, &p.second_value); return p; }
int main() {
  int cases = 0;
  if (scanf("%d", &cases) != 1) return 0;
  vector<ll> answers;
  for (int i = 0; i < cases; i++) { Pair p = readPair(); answers.push_back(combine(p)); }
  for (size_t j = 0; j < answers.size(); j++) printf("%lld\n", answers[j]);
  return 0;
}`
	progBilal = `#include <bits/stdc++.h>
using namespace std;
// my own solution
typedef long long big;
struct Item { big x; big y; };
static big addUp(const Item &it) {
  big s = it.x;
  s += it.y;
  return s;
}
static Item grab() { Item it; scanf("%lld %lld", &it.x, &it.y); return it; }
int main() {
  int n = 0;
  if (scanf("%d", &n) != 1) return 0;
  vector<big> res;
  for (int k = 0; k < n; k++) { Item it = grab(); res.push_back(addUp(it)); }
  for (size_t q = 0; q < res.size(); q++) printf("%lld\n", res[q]);
  return 0;
}`
	progChen = `#include <iostream>
#include <string>
int main() {
  std::ios::sync_with_stdio(false);
  long long count;
  std::cin >> count;
  std::string output;
  for (long long k = 1; k <= count; ++k) {
    long long left, right;
    std::cin >> left >> right;
    output += std::to_string(left + right);
    output.push_back('\n');
  }
  std::cout << output;
}`
)

// integrityWorld is a site with two finished contests: c1 where Asha and
// Bilal submitted near-identical programs and Chen his own, and c2 where
// Dana and Eve did the same.
type integrityWorld struct {
	s                *site
	start            time.Time
	owner, admin, ta *browser
	asha, bilal      *browser
	chen, dana, eve  *browser
	c1, c2           int64
	pa               int64
}

func newIntegrityWorld(t *testing.T) *integrityWorld {
	t.Helper()
	s := newSite(t)
	w := &integrityWorld{s: s, start: time.Now().Add(time.Hour).Truncate(time.Minute)}
	w.owner = s.signedIn("owner@college.edu", "Owner")
	if code, out := w.owner.do("POST", "/api/admin/people", map[string]string{"email": "admin@college.edu", "role": "admin"}, s.srv.URL); code != 200 {
		t.Fatalf("grant admin: %d %v", code, out)
	}
	w.admin = s.signedIn("admin@college.edu", "Admin")
	w.pa = s.importProblem(w.owner, "plus-a")
	w.c1 = s.createContest(w.owner, "c1", w.start, map[string]int64{"A": w.pa}, nil)
	w.c2 = s.createContest(w.owner, "c2", w.start, map[string]int64{"A": w.pa}, nil)
	w.ta = s.signedIn("ta@college.edu", "Tara TA")
	w.owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", w.c1), map[string]string{"email": "ta@college.edu"}, s.srv.URL)
	// The admin also co-organises c1: a contest admin whose role is not
	// student.
	w.owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", w.c1), map[string]string{"email": "admin@college.edu"}, s.srv.URL)
	roster := []map[string]string{{"email": "asha@college.edu", "name": "Asha Rao", "roll_number": "21CS001"}}
	if code, out := w.owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/roster", w.c1), roster, s.srv.URL); code != 200 {
		t.Fatalf("roster: %d %v", code, out)
	}
	w.asha = s.signedIn("asha@college.edu", "Asha")
	w.bilal = s.signedIn("bilal@college.edu", "Bilal")
	w.chen = s.signedIn("chen@college.edu", "Chen")
	w.dana = s.signedIn("dana@college.edu", "Dana")
	w.eve = s.signedIn("eve@college.edu", "Eve")
	s.at(w.start, 5)
	for _, p := range []struct {
		b          *browser
		slug, prog string
	}{{w.asha, "c1", progAsha}, {w.bilal, "c1", progBilal}, {w.chen, "c1", progChen}, {w.dana, "c2", progAsha}, {w.eve, "c2", progBilal}} {
		if code, out := p.b.do("POST", "/api/contests/"+p.slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
			t.Fatalf("start %s: %d %v", p.slug, code, out)
		}
		code, out := p.b.do("POST", "/api/contests/"+p.slug+"/problems/A/submit", map[string]any{"language": "cpp17", "source": p.prog,
			"client_key": fmt.Sprint("k", time.Now().UnixNano()), "typed_chars": 400, "pasted_chars": 20}, s.srv.URL)
		if code != 202 {
			t.Fatalf("submit %s: %d %v", p.slug, code, out)
		}
	}
	s.judgeAll()
	return w
}

func (w *integrityWorld) url(contest int64, rest string) string {
	return fmt.Sprintf("/api/admin/contests/%d/integrity%s", contest, rest)
}

// check runs a check of a contest as the owner and waits for it.
func (w *integrityWorld) check(contest int64) map[string]any {
	w.s.t.Helper()
	code, out := w.owner.do("POST", w.url(contest, "/run"), nil, w.s.srv.URL)
	if code != 202 || out["run_id"] == nil || out["server_now"] == nil {
		w.s.t.Fatalf("run: %d %v", code, out)
	}
	w.waitIdle(contest)
	code, run := w.owner.do("GET", w.url(contest, ""), nil, "")
	if code != 200 || run["state"] != "done" {
		w.s.t.Fatalf("latest run: %d %v", code, run)
	}
	return run
}

func (w *integrityWorld) waitIdle(contest int64) {
	w.s.t.Helper()
	for i := 0; i < 500; i++ {
		run, err := w.s.integrity.Latest(context.Background(), contest)
		if err == nil && run.State != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.s.t.Fatal("the check did not finish")
}

// similarSignal returns the id of the similar-code signal between two
// users in a run answer.
func similarSignal(t *testing.T, run map[string]any, a, b int64) int64 {
	t.Helper()
	for _, x := range run["signals"].([]any) {
		sg := x.(map[string]any)
		ua, _ := sg["user_a"].(float64)
		ub, _ := sg["user_b"].(float64)
		if sg["kind"] == "similar" && ((int64(ua) == a && int64(ub) == b) || (int64(ua) == b && int64(ub) == a)) {
			return int64(sg["id"].(float64))
		}
	}
	t.Fatalf("no similar signal between %d and %d: %v", a, b, run["signals"])
	return 0
}

func TestIntegrityRefusedWhileAnyStudentHasTime(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	code, out := w.owner.do("POST", w.url(w.c1, "/run"), nil, s.srv.URL)
	if code != 409 || out["error"].(map[string]any)["code"] != "contest_live" {
		t.Fatalf("run during the contest: %d %v", code, out)
	}
	// Chen has 30 more minutes: the contest is not over for everyone.
	chen := w.chen.userID()
	if code, out := w.owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", w.c1, chen),
		map[string]any{"minutes": 30, "reason": "Laptop crashed"}, s.srv.URL); code != 200 {
		t.Fatalf("extension: %d %v", code, out)
	}
	s.at(w.start, 135)
	if code, out := w.owner.do("POST", w.url(w.c1, "/run"), nil, s.srv.URL); code != 409 || out["error"].(map[string]any)["code"] != "contest_live" {
		t.Fatalf("run during an extension: %d %v", code, out)
	}
	if code, _ := w.owner.do("GET", w.url(w.c1, ""), nil, ""); code != 404 {
		t.Errorf("never checked: %d, want 404", code)
	}
	s.at(w.start, 151)
	w.check(w.c1)
}

func TestIntegrityOneCheckAtATime(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	release := make(chan struct{})
	s.integrity.Hold = func() { <-release }
	if code, out := w.owner.do("POST", w.url(w.c1, "/run"), nil, s.srv.URL); code != 202 {
		t.Fatalf("first run: %d %v", code, out)
	}
	if code, run := w.owner.do("GET", w.url(w.c1, ""), nil, ""); code != 200 || run["state"] != "running" || run["finished_at"] != nil {
		t.Errorf("running check: %d %v", code, run)
	}
	for _, c := range []int64{w.c1, w.c2} {
		code, out := w.owner.do("POST", w.url(c, "/run"), nil, s.srv.URL)
		if code != 409 || out["error"].(map[string]any)["code"] != "busy" {
			t.Errorf("second run on contest %d: %d %v", c, code, out)
		}
	}
	close(release)
	w.waitIdle(w.c1)
	w.check(w.c2)
}

func TestIntegrityPermissionsAndForeignIDs(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	run1 := w.check(w.c1)
	run2 := w.check(w.c2)
	asha, bilal, dana, eve := w.asha.userID(), w.bilal.userID(), w.dana.userID(), w.eve.userID()
	sig1 := similarSignal(t, run1, asha, bilal)
	sig2 := similarSignal(t, run2, dana, eve)
	reads := []string{w.url(w.c1, ""), w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig1)), w.url(w.c1, fmt.Sprintf("/timeline/%d", asha)),
		w.url(w.c1, "/cases")}
	// Owners, admins and an admin who co-organises the contest may review.
	for name, b := range map[string]*browser{"owner": w.owner, "admin": w.admin} {
		for _, path := range reads {
			if code, out := b.do("GET", path, nil, ""); code != 200 || out["server_now"] == nil {
				t.Errorf("%s GET %s: %d %v", name, path, code, out)
			}
		}
	}
	// A student sees none of it; a co-organiser who is a student is refused
	// on the contest's routes and does not learn the case exists.
	for _, path := range reads {
		if code, _ := w.chen.do("GET", path, nil, ""); code != 404 {
			t.Errorf("student GET %s: %d, want 404", path, code)
		}
		if code, _ := w.ta.do("GET", path, nil, ""); code != 403 {
			t.Errorf("student co-organiser GET %s: %d, want 403", path, code)
		}
	}
	for name, b := range map[string]*browser{"student": w.chen, "co-organiser": w.ta} {
		if code, _ := b.do("POST", w.url(w.c1, "/run"), nil, s.srv.URL); code != 404 && code != 403 {
			t.Errorf("%s starting a check: %d", name, code)
		}
		if code, _ := b.do("PUT", w.url(w.c1, fmt.Sprintf("/cases/%d", asha)),
			map[string]any{"decision": "warning", "reason": "Nothing to see here at all, cleared."}, s.srv.URL); code != 404 && code != 403 {
			t.Errorf("%s deciding: %d", name, code)
		}
	}
	// Ids from the other contest are 404 under this one, and its signals
	// cannot be put in this contest's case.
	if code, _ := w.owner.do("GET", w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig2)), nil, ""); code != 404 {
		t.Errorf("pair of another contest's signal: %d", code)
	}
	if code, _ := w.owner.do("GET", w.url(w.c1, fmt.Sprintf("/timeline/%d", dana)), nil, ""); code != 404 {
		t.Errorf("timeline of a student of another contest: %d", code)
	}
	if code, out := w.owner.do("PUT", w.url(w.c1, fmt.Sprintf("/cases/%d", dana)),
		map[string]any{"decision": "warning", "reason": "A long enough reason to decide."}, s.srv.URL); code != 404 {
		t.Errorf("decision about a student of another contest: %d %v", code, out)
	}
	// The run answer names the people and problems it lists; the roster's
	// name and roll number win over the account's.
	users := run1["users"].(map[string]any)
	a := users[fmt.Sprint(asha)].(map[string]any)
	if a["name"] != "Asha Rao" || a["roll_number"] != "21CS001" || a["email"] != "asha@college.edu" {
		t.Errorf("users map: %v", users)
	}
	if b := users[fmt.Sprint(bilal)].(map[string]any); b["name"] != "Bilal" || b["roll_number"] != "" {
		t.Errorf("users map without a roster entry: %v", b)
	}
	p := run1["problems"].(map[string]any)[fmt.Sprint(w.pa)].(map[string]any)
	if p["label"] != "A" || !strings.HasPrefix(p["title"].(string), "A plus B") {
		t.Errorf("problems map: %v", run1["problems"])
	}
	// The pair view returns both programs with matched spans.
	_, pair := w.owner.do("GET", w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig1)), nil, "")
	pa, pb := pair["a"].(map[string]any), pair["b"].(map[string]any)
	if pa["source"] == "" || pb["source"] == "" || len(pa["spans"].([]any)) == 0 || len(pb["spans"].([]any)) == 0 {
		t.Errorf("pair: %v", pair)
	}
	_, tl := w.owner.do("GET", w.url(w.c1, fmt.Sprintf("/timeline/%d", asha)), nil, "")
	if ms := tl["moments"].([]any); len(ms) == 0 || ms[0].(map[string]any)["kind"] != "submission" {
		t.Errorf("timeline: %v", tl)
	}
}

// The console feed carries typed, pasted and focus counts for owners and
// admins only, never for a co-organiser whose site role is student.
func TestConsoleFeedHidesIntegrityCountsFromStudentCoOrganisers(t *testing.T) {
	w := newIntegrityWorld(t)
	path := fmt.Sprintf("/api/admin/contests/%d/submissions", w.c1)
	for name, b := range map[string]*browser{"owner": w.owner, "admin": w.admin, "student co-organiser": w.ta} {
		code, out := b.do("GET", path, nil, "")
		items, _ := out["submissions"].([]any)
		if code != 200 || len(items) == 0 {
			t.Fatalf("%s feed: %d %v", name, code, out)
		}
		for _, x := range items {
			it := x.(map[string]any)
			_, typed := it["typed_chars"]
			_, pasted := it["pasted_chars"]
			_, losses := it["focus_losses"]
			if want := b != w.ta; typed != want || pasted != want || losses != want {
				t.Errorf("%s sees integrity counts %v %v %v, want %v: %v", name, typed, pasted, losses, want, it)
			}
			if b == w.owner && it["typed_chars"] != float64(400) {
				t.Errorf("owner's typed count: %v", it["typed_chars"])
			}
		}
	}
}
