package httpapi

// Integrity milestone checks over HTTP (cache/v1/integrity-plan.md
// section 7): who may review, ids from another contest, the refusal while
// any student still has time, one check at a time, the whole case flow
// with a student explaining, and retention keeping the data of students in
// an open case.

import (
	"context"
	"encoding/json"
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
	code, created := w.owner.do("POST", w.url(w.c1, "/cases"),
		map[string]any{"title": "Problem A, similar code", "note": "The two programs match token for token.", "signal_ids": []int64{sig1}}, s.srv.URL)
	if code != 201 {
		t.Fatalf("open case: %d %v", code, created)
	}
	case1 := int64(created["id"].(float64))

	reads := []string{w.url(w.c1, ""), w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig1)), w.url(w.c1, fmt.Sprintf("/timeline/%d", asha)),
		w.url(w.c1, "/cases"), fmt.Sprintf("/api/admin/integrity/cases/%d", case1), fmt.Sprintf("/api/admin/integrity/cases/%d/evidence/0/pair", case1)}
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
		want := 403
		if strings.HasPrefix(path, "/api/admin/integrity/") {
			want = 404
		}
		if code, _ := w.ta.do("GET", path, nil, ""); code != want {
			t.Errorf("student co-organiser GET %s: %d, want %d", path, code, want)
		}
	}
	for name, b := range map[string]*browser{"student": w.chen, "co-organiser": w.ta} {
		if code, _ := b.do("POST", w.url(w.c1, "/run"), nil, s.srv.URL); code != 404 && code != 403 {
			t.Errorf("%s starting a check: %d", name, code)
		}
		if code, _ := b.do("PUT", fmt.Sprintf("/api/admin/integrity/cases/%d", case1),
			map[string]any{"state": "cleared", "note": "Nothing to see here at all, cleared."}, s.srv.URL); code != 404 {
			t.Errorf("%s deciding a case: %d", name, code)
		}
		if code, _ := b.do("POST", w.url(w.c1, "/cases"),
			map[string]any{"title": "Mine", "note": "A long enough reason to open it.", "signal_ids": []int64{sig1}}, s.srv.URL); code != 404 && code != 403 {
			t.Errorf("%s opening a case: %d", name, code)
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
	if code, out := w.owner.do("POST", w.url(w.c1, "/cases"),
		map[string]any{"title": "Mixed", "note": "A long enough reason to open it.", "signal_ids": []int64{sig2}}, s.srv.URL); code != 400 {
		t.Errorf("case from another contest's signal: %d %v", code, out)
	}
	if code, _ := w.owner.do("GET", "/api/admin/integrity/cases/999", nil, ""); code != 404 {
		t.Errorf("missing case: %d", code)
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

func TestIntegrityCaseFlowOverHTTP(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	run := w.check(w.c1)
	asha, bilal := w.asha.userID(), w.bilal.userID()
	sig := similarSignal(t, run, asha, bilal)
	caseBody := func(note string) map[string]any {
		return map[string]any{"title": "Problem A, similar code", "note": note, "signal_ids": []int64{sig}}
	}
	if code, out := w.owner.do("POST", w.url(w.c1, "/cases"), caseBody("too short"), s.srv.URL); code != 400 ||
		!strings.Contains(out["error"].(map[string]any)["message"].(string), "20 characters") {
		t.Errorf("short reason: %d %v", code, out)
	}
	code, c := w.owner.do("POST", w.url(w.c1, "/cases"), caseBody("The two programs match token for token."), s.srv.URL)
	if code != 201 || c["state"] != "open" || len(c["users"].([]any)) != 2 || len(c["decisions"].([]any)) != 1 || len(c["evidence"].([]any)) != 1 {
		t.Fatalf("open case: %d %v", code, c)
	}
	caseURL := fmt.Sprintf("/api/admin/integrity/cases/%v", c["id"])
	explainURL := fmt.Sprintf("/api/integrity/cases/%v/explanation", c["id"])
	// seen is what the case page sends: each student's latest explanation
	// version as the reviewer loaded it.
	seen := func() map[string]any {
		_, got := w.owner.do("GET", caseURL, nil, "")
		out := map[string]any{}
		for _, u := range got["users"].([]any) {
			u := u.(map[string]any)
			if vs := u["explanations"].([]any); len(vs) > 0 {
				out[fmt.Sprint(int64(u["user_id"].(float64)))] = vs[len(vs)-1].(map[string]any)["created_at"]
			}
		}
		return out
	}
	decide := func(state, note string) (int, map[string]any) {
		return w.owner.do("PUT", caseURL, map[string]any{"state": state, "note": note, "seen_explanations": seen()}, s.srv.URL)
	}
	if code, out := decide("actioned", "Copying is clear from the matched code."); code != 400 ||
		out["error"].(map[string]any)["message"] != "Share the case with the students before taking action. They explain first." {
		t.Errorf("action before sharing: %d %v", code, out)
	}
	// Before sharing the students see nothing and cannot explain.
	if _, mine := w.asha.do("GET", "/api/integrity/cases", nil, ""); len(mine["cases"].([]any)) != 0 {
		t.Errorf("unshared case visible: %v", mine)
	}
	if code, _ := w.asha.do("PUT", explainURL, map[string]any{"explanation": "Mine."}, s.srv.URL); code != 404 {
		t.Errorf("explained an unshared case: %d", code)
	}
	if code, out := decide("awaiting_student", "Please explain how you wrote this solution."); code != 200 || out["state"] != "awaiting_student" {
		t.Fatalf("share: %d %v", code, out)
	}
	// Action is never one step: an outcome is proposed and confirmed.
	if code, out := decide("actioned", "Copying is clear from the matched code."); code != 400 ||
		out["error"].(map[string]any)["code"] != "proposal_needed" {
		t.Errorf("action in one step: %d %v", code, out)
	}

	// Retention 200 days later keeps the data of the students in the open
	// case and clears Chen's.
	s.clock.set(w.start.Add(200 * 24 * time.Hour))
	if _, _, err := s.contests.Retain(context.Background()); err != nil {
		t.Fatal(err)
	}
	for user, want := range map[int64]bool{asha: true, bilal: true, w.chen.userID(): false} {
		var kept int
		s.db.QueryRow("SELECT count(*) FROM submissions WHERE contest_id = ? AND user_id = ? AND client_hash <> ''", w.c1, user).Scan(&kept)
		if (kept > 0) != want {
			t.Errorf("user %d keeps integrity data: %v, want %v", user, kept > 0, want)
		}
	}

	code, mine := w.asha.do("GET", "/api/integrity/cases", nil, "")
	cases := mine["cases"].([]any)
	if code != 200 || len(cases) != 1 || mine["server_now"] == nil {
		t.Fatalf("student cases: %d %v", code, mine)
	}
	sc := cases[0].(map[string]any)
	ev := sc["evidence"].([]any)
	if len(ev) != 1 || ev[0].(map[string]any)["source"] != progAsha || len(ev[0].(map[string]any)["spans"].([]any)) == 0 || sc["decided"] != false {
		t.Errorf("student evidence: %v", sc)
	}
	if mine["contests"].(map[string]any)[fmt.Sprint(w.c1)].(map[string]any)["slug"] != "c1" ||
		mine["problems"].(map[string]any)[fmt.Sprint(w.pa)].(map[string]any)["label"] != "A" {
		t.Errorf("student maps: %v %v", mine["contests"], mine["problems"])
	}
	js, _ := json.Marshal(mine)
	for _, leak := range []string{"Bilal", "bilal@", "my own solution", "addUp", "Please explain", "token for token"} {
		if strings.Contains(string(js), leak) {
			t.Errorf("student answer contains %q: %s", leak, js)
		}
	}
	if code, _ := w.chen.do("PUT", explainURL, map[string]any{"explanation": "Not mine."}, s.srv.URL); code != 404 {
		t.Errorf("outsider explained: %d", code)
	}
	if code, _ := w.asha.do("PUT", explainURL, map[string]any{"explanation": "  "}, s.srv.URL); code != 400 {
		t.Errorf("empty explanation: %d", code)
	}
	if code, _ := w.asha.do("PUT", explainURL, map[string]any{"explanation": "We revised together and I used the same approach."}, s.srv.URL); code != 204 {
		t.Fatalf("explain: %d", code)
	}
	_, got := w.owner.do("GET", caseURL, nil, "")
	explained := false
	for _, u := range got["users"].([]any) {
		u := u.(map[string]any)
		if int64(u["user_id"].(float64)) == asha && u["explanation"] != "" && u["explained_at"] != nil && u["shared_at"] != nil {
			explained = true
		}
	}
	if !explained {
		t.Errorf("reviewer does not see the explanation: %v", got)
	}
	// Asha rewrites her explanation after the reviewer loaded the page: the
	// decision is refused until the reviewer reloads, and both versions stay.
	stale := seen()
	if code, _ := w.asha.do("PUT", explainURL, map[string]any{"explanation": "We revised together; here is the longer account."}, s.srv.URL); code != 204 {
		t.Fatalf("second explanation: %d", code)
	}
	code, out := w.owner.do("PUT", caseURL, map[string]any{"state": "cleared", "note": "The explanation is consistent with the timeline.",
		"seen_explanations": stale}, s.srv.URL)
	if e, _ := out["error"].(map[string]any); code != 409 || e["code"] != "explanation_changed" ||
		e["message"] != "A student changed their explanation. Reload before deciding." {
		t.Errorf("decision against an old explanation: %d %v", code, out)
	}
	_, got = w.owner.do("GET", caseURL, nil, "")
	for _, u := range got["users"].([]any) {
		u := u.(map[string]any)
		if int64(u["user_id"].(float64)) == asha && len(u["explanations"].([]any)) != 2 {
			t.Errorf("explanation versions: %v", u["explanations"])
		}
	}
	code, out = decide("cleared", "The explanation is consistent with the timeline.")
	if code != 200 || out["state"] != "cleared" || len(out["decisions"].([]any)) != 3 {
		t.Fatalf("clear: %d %v", code, out)
	}
	if code, _ := w.asha.do("PUT", explainURL, map[string]any{"explanation": "Changed my mind."}, s.srv.URL); code != 404 {
		t.Errorf("explanation changed after the decision: %d", code)
	}
	_, mine = w.asha.do("GET", "/api/integrity/cases", nil, "")
	if sc := mine["cases"].([]any)[0].(map[string]any); sc["decided"] != true {
		t.Errorf("decided case: %v", sc)
	}
	// The decision log is append only: every change is still there.
	var n int
	s.db.QueryRow("SELECT count(*) FROM integrity_decisions WHERE case_id = ?", c["id"]).Scan(&n)
	if n != 3 {
		t.Errorf("%d decisions stored, want 3", n)
	}
	// The server log records opens and decisions by id, never the notes,
	// explanations or code.
	logs := s.logs.String()
	if !strings.Contains(logs, `"msg":"integrity case opened"`) || strings.Count(logs, `"msg":"integrity case decided"`) != 2 {
		t.Errorf("audit lines missing:\n%s", logs)
	}
	for _, secret := range []string{"token for token", "consistent with the timeline", "revised together", "scanf"} {
		if strings.Contains(logs, secret) {
			t.Errorf("the log contains %q", secret)
		}
	}
	// A re-run replaces the signals but not the case's evidence.
	w.check(w.c1)
	_, again := w.owner.do("GET", caseURL, nil, "")
	if fmt.Sprint(again["evidence"]) != fmt.Sprint(out["evidence"]) {
		t.Error("a re-run changed the case's evidence")
	}
	if _, list := w.owner.do("GET", w.url(w.c1, "/cases"), nil, ""); len(list["cases"].([]any)) != 1 {
		t.Errorf("case list: %v", list)
	}
}

// A case's pair view serves the case's own copy of the evidence: a re-run
// does not change it. A student in the case who later became an admin
// cannot read, list or decide it, and the decision log names reviewers.
func TestIntegrityCasePairAndReviewerInCase(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	s.at(w.start, 121)
	run := w.check(w.c1)
	asha, bilal := w.asha.userID(), w.bilal.userID()
	sig := similarSignal(t, run, asha, bilal)
	code, c := w.owner.do("POST", w.url(w.c1, "/cases"),
		map[string]any{"title": "Problem A, similar code", "note": "The two programs match token for token.", "signal_ids": []int64{sig}}, s.srv.URL)
	if code != 201 {
		t.Fatalf("open case: %d %v", code, c)
	}
	caseURL := fmt.Sprintf("/api/admin/integrity/cases/%v", c["id"])
	if d := c["decisions"].([]any)[0].(map[string]any); d["by_name"] != "Owner" {
		t.Errorf("decision log names %v, want Owner", d["by_name"])
	}
	viewsA, viewsB := viewsOf(t, s, "case", asha), viewsOf(t, s, "case", bilal)
	code, pair := w.owner.do("GET", caseURL+"/evidence/0/pair", nil, "")
	if code != 200 || pair["signal"].(map[string]any)["kind"] != "similar" || pair["server_now"] == nil {
		t.Fatalf("case pair: %d %v", code, pair)
	}
	// The pair view is a case read for each student it shows (P4 security fix, C1-026).
	if viewsOf(t, s, "case", asha) != viewsA+1 || viewsOf(t, s, "case", bilal) != viewsB+1 {
		t.Error("the case pair read was not logged for both students")
	}
	_, live := w.owner.do("GET", w.url(w.c1, fmt.Sprintf("/signals/%d/pair", sig)), nil, "")
	if fmt.Sprint(pair["a"], pair["b"]) != fmt.Sprint(live["a"], live["b"]) {
		t.Errorf("case pair %v differs from the live pair %v", pair, live)
	}
	sources := []string{pair["a"].(map[string]any)["source"].(string), pair["b"].(map[string]any)["source"].(string)}
	if !(sources[0] == progAsha && sources[1] == progBilal) && !(sources[0] == progBilal && sources[1] == progAsha) {
		t.Errorf("case pair sources: %v", sources)
	}
	w.check(w.c1)
	if _, again := w.owner.do("GET", caseURL+"/evidence/0/pair", nil, ""); fmt.Sprint(again["a"], again["b"]) != fmt.Sprint(pair["a"], pair["b"]) {
		t.Error("a re-run changed the case's pair view")
	}
	for _, bad := range []string{"/evidence/1/pair", "/evidence/-1/pair", "/evidence/x/pair"} {
		if code, _ := w.owner.do("GET", caseURL+bad, nil, ""); code != 404 {
			t.Errorf("GET %s: %d, want 404", bad, code)
		}
	}

	// Asha becomes an admin: her own case is still out of her reach.
	if code, out := w.owner.do("POST", "/api/admin/people", map[string]string{"email": "asha@college.edu", "role": "admin"}, s.srv.URL); code != 200 {
		t.Fatalf("grant admin: %d %v", code, out)
	}
	for _, path := range []string{caseURL, caseURL + "/evidence/0/pair"} {
		if code, _ := w.asha.do("GET", path, nil, ""); code != 404 {
			t.Errorf("Asha GET %s: %d, want 404", path, code)
		}
	}
	if code, _ := w.asha.do("PUT", caseURL, map[string]any{"state": "cleared", "note": "Nothing to see here at all, cleared."}, s.srv.URL); code != 404 {
		t.Errorf("Asha decided her own case: %d", code)
	}
	if code, list := w.asha.do("GET", w.url(w.c1, "/cases"), nil, ""); code != 200 || len(list["cases"].([]any)) != 0 {
		t.Errorf("Asha's case list: %d %v", code, list)
	}
	if _, list := w.owner.do("GET", w.url(w.c1, "/cases"), nil, ""); len(list["cases"].([]any)) != 1 {
		t.Errorf("owner's case list: %v", list)
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
