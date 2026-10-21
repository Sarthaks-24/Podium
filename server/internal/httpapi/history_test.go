package httpapi

// P2 history over HTTP (v2 plan B2.3 to B2.9, gate items 2 to 4): a
// statement edited mid-contest and "what X saw at T" answered from data
// for three students; no revision route returns hidden test data; the
// history routes per role; renamed contests answer 301; practice reveals
// a failing hidden test and a counting submission never does (D115).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// editBody turns a Problem answer into a ProblemEdit with the statement
// replaced.
func editBody(p map[string]any, statement, reason string) map[string]any {
	body := map[string]any{"statement_md": statement, "reason": reason}
	for _, k := range []string{"title", "slug", "time_limit_ms", "memory_limit_mb", "checker", "epsilon", "checker_source",
		"reference", "brute", "validator", "tags", "difficulty", "samples"} {
		body[k] = p[k]
	}
	var subs []map[string]any
	for _, st := range p["subtasks"].([]any) {
		m := st.(map[string]any)
		subs = append(subs, map[string]any{"points": m["points"], "title": m["title"], "tests": m["tests"]})
	}
	body["subtasks"] = subs
	return body
}

// hiddenSecrets lists what a revision route must never show: every hidden
// test's input and output text and the hashes of all test files.
func (s *site) hiddenSecrets(id int64) []string {
	s.t.Helper()
	p, err := s.bank.Get(context.Background(), id)
	if err != nil {
		s.t.Fatal(err)
	}
	var out []string
	for _, t := range p.Tests {
		out = append(out, t.InputSHA, t.OutputSHA)
		if p.IsSample(t.Idx) {
			continue
		}
		for _, sha := range []string{t.InputSHA, t.OutputSHA} {
			b, _ := s.bank.Blobs.Read(sha, 1<<20)
			if text := strings.TrimSpace(string(b)); len(text) >= 6 {
				out = append(out, text)
			}
		}
	}
	return out
}

func assertNoSecrets(t *testing.T, what string, body any, secrets []string) {
	t.Helper()
	b, _ := json.Marshal(body)
	for _, sec := range secrets {
		if strings.Contains(string(b), sec) {
			t.Errorf("%s returns hidden test data (%.20q...)", what, sec)
		}
	}
}

func TestP2StatementEditMidContestAnswersWhatEachStudentSaw(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour).Truncate(time.Second)
	cid := s.createContest(owner, "week-6", start, map[string]int64{"A": pid}, nil)
	secrets := s.hiddenSecrets(pid)

	students := map[string]*browser{}
	for _, name := range []string{"asha", "ravi", "meera"} {
		b := s.signedIn(name+"@college.edu", name)
		students[name] = b
	}
	s.at(start, 1)
	for _, b := range students {
		if code, out := b.do("POST", "/api/contests/week-6/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
			t.Fatalf("start: %d %v", code, out)
		}
	}
	// 14:02 Asha and Meera open A (revision 1); opening again records nothing.
	s.at(start, 2)
	for _, n := range []string{"asha", "meera", "asha"} {
		code, ws := students[n].do("GET", "/api/contests/week-6/problems/A", nil, "")
		if code != 200 || !strings.Contains(ws["statement_md"].(string), "") || ws["statement_updated_at"] != nil {
			t.Fatalf("workspace: %d %v", code, ws)
		}
	}
	t1 := s.clock.now()
	var views int
	s.db.QueryRow("SELECT count(*) FROM revision_views WHERE contest_id = ?", cid).Scan(&views)
	if views != 2 {
		t.Fatalf("%d served log rows after three serves to two students, want 2", views)
	}

	// 14:30 the owner fixes the statement: a reason is required while live.
	s.at(start, 30)
	_, p := owner.do("GET", fmt.Sprintf("/api/admin/problems/%d", pid), nil, "")
	if live := p["live_contests"].([]any); len(live) != 1 {
		t.Fatalf("live contests %v", live)
	}
	fixed := "Read two integers and print their sum.\n\nThe sum fits in 64 bits."
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/problems/%d", pid), editBody(p, fixed, ""), s.srv.URL); code != 400 ||
		out["error"].(map[string]any)["fields"].(map[string]any)["reason"] == nil {
		t.Fatalf("edit without a reason: %d %v", code, out)
	}
	code, p := owner.do("PUT", fmt.Sprintf("/api/admin/problems/%d", pid), editBody(p, fixed, "State the bound on the sum."), s.srv.URL)
	if code != 200 || p["revision"].(map[string]any)["rev"].(float64) != 2 {
		t.Fatalf("edit: %d %v", code, p["revision"])
	}
	// Students still see revision 1 until the contest is updated.
	_, ws := students["ravi"].do("GET", "/api/contests/week-6/problems/A", nil, "")
	if ws["statement_md"] == fixed {
		t.Fatal("a bank edit reached students before the contest was updated")
	}
	t2 := s.clock.now() // Ravi first saw revision 1 at 14:30

	// 14:32 the owner updates the contest to revision 2.
	s.at(start, 32)
	_, ac := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", cid), nil, "")
	ap := ac["problems"].([]any)[0].(map[string]any)
	if ap["rev"].(float64) != 1 || ap["latest_rev"].(float64) != 2 || ap["update_available"] != true || ap["update_statement_only"] != true {
		t.Fatalf("console problem %v", ap)
	}
	rev2 := ap["latest_revision_id"]
	code, up := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems/A/revision", cid), map[string]any{"revision_id": rev2}, s.srv.URL)
	if code != 200 || up["statement_changed"] != true || up["rejudge_needed"] != false || !strings.Contains(up["announcement_md"].(string), "See what changed") {
		t.Fatalf("update pin: %d %v", code, up)
	}
	assertNoSecrets(t, "update pin", up, secrets)
	if code, _ := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems/A/revision", cid), map[string]any{"revision_id": rev2}, s.srv.URL); code != 409 {
		t.Errorf("pinning the same revision again: %d", code)
	}
	t3 := s.clock.now()

	// 14:40 Meera opens A again and sees revision 2 with the banner time.
	s.at(start, 40)
	_, ws = students["meera"].do("GET", "/api/contests/week-6/problems/A", nil, "")
	if ws["statement_md"] != fixed || ws["statement_updated_at"] == nil || t3.UnixMilli()-int64(ws["statement_updated_at"].(float64)) > 1000 {
		t.Fatalf("workspace after the update: %v %v", ws["statement_md"], ws["statement_updated_at"])
	}
	_, contest := students["meera"].do("GET", "/api/contests/week-6", nil, "")
	if cp := contest["problems"].([]any)[0].(map[string]any); cp["revision_id"] != rev2 || cp["statement_updated_at"] == nil {
		t.Fatalf("contest problem %v", cp)
	}
	// C3: the statement history with a line diff, statements only.
	code, hist := students["meera"].do("GET", "/api/contests/week-6/problems/A/revisions", nil, "")
	if code != 200 || len(hist["revisions"].([]any)) != 2 || hist["diff"] == nil {
		t.Fatalf("statement history: %d %v", code, hist)
	}
	lines := hist["diff"].(map[string]any)["lines"].([]any)
	var added []string
	for _, l := range lines {
		if m := l.(map[string]any); m["op"] == "add" {
			added = append(added, m["text"].(string))
		}
	}
	if !strings.Contains(strings.Join(added, "\n"), "The sum fits in 64 bits.") {
		t.Errorf("diff adds %v", added)
	}
	assertNoSecrets(t, "statement history", hist, secrets)

	// What each student saw, from the pin history and the served log.
	ctx := context.Background()
	for _, tc := range []struct {
		who       string
		at        time.Time
		pinnedRev int
		seen      bool
	}{
		{"asha", t1, 1, true},   // opened revision 1 at 14:02
		{"asha", t3, 2, false},  // revision 2 was pinned, she had not opened it
		{"ravi", t2, 1, true},   // opened revision 1 at 14:30
		{"meera", t3, 2, false}, // pinned at 14:32, she opened it at 14:40
		{"meera", s.clock.now(), 2, true},
	} {
		uid := students[tc.who].userID()
		seen, err := s.contests.SeenAt(ctx, cid, uid, "A", tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if seen.PinnedRev == nil || seen.PinnedRev.Rev != tc.pinnedRev || seen.FirstSeenAt.IsZero() == tc.seen {
			t.Errorf("%s at %s: pinned %v, first seen %v; want revision %d, seen %v", tc.who, tc.at.Format(time.TimeOnly),
				seen.PinnedRev, seen.FirstSeenAt, tc.pinnedRev, tc.seen)
		}
	}

	// The organiser's revision routes carry no test data either.
	for _, path := range []string{
		fmt.Sprintf("/api/admin/problems/%d/revisions/1", pid),
		fmt.Sprintf("/api/admin/problems/%d/revisions/2", pid),
		fmt.Sprintf("/api/admin/problems/%d/revisions/diff?from=1&to=2", pid),
		fmt.Sprintf("/api/admin/contests/%d", cid),
		fmt.Sprintf("/api/contests/week-6/problems/A/revisions?from=%v&to=%v", ap["revision_id"], rev2),
	} {
		code, body := owner.do("GET", path, nil, "")
		if code != 200 {
			t.Fatalf("%s: %d %v", path, code, body)
		}
		assertNoSecrets(t, path, body, secrets)
	}
	_, diff := owner.do("GET", fmt.Sprintf("/api/admin/problems/%d/revisions/diff?from=1&to=2", pid), nil, "")
	if diff["statement_only"] != true || fmt.Sprint(diff["changed"]) != "[statement]" {
		t.Errorf("diff %v", diff)
	}
	// The pin history and the Changes feed name the update.
	_, ac = owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", cid), nil, "")
	if ph := ac["pin_history"].([]any); len(ph) != 2 || ph[0].(map[string]any)["rev"].(float64) != 2 {
		t.Errorf("pin history %v", ph)
	}
	_, feed := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/audit?action=revision", cid), nil, "")
	if e := feed["entries"].([]any); len(e) != 1 {
		t.Errorf("Changes feed filtered to revision: %v", e)
	}
	_, feed = owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/audit?actor=nobody", cid), nil, "")
	if e := feed["entries"].([]any); len(e) != 0 {
		t.Errorf("Changes feed filtered by an unknown actor: %v", e)
	}
	// A rejudge names the revision it judges against.
	code, rj := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", cid), map[string]any{"problem_label": "A"}, s.srv.URL)
	if code != 202 || fmt.Sprint(rj["revisions"]) != fmt.Sprintf("[map[label:A rev:2 revision_id:%v]]", rev2) {
		t.Errorf("rejudge %d %v", code, rj)
	}
	if bad, err := problems.CheckRevisions(ctx, s.db); err != nil || len(bad) > 0 {
		t.Errorf("revisions: %v %v", bad, err)
	}
}

func TestP2HistoryRoutesPerRole(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	cid := s.createContest(owner, "week-7", start, map[string]int64{"A": pid}, nil)
	other := s.createContest(owner, "week-8", start, map[string]int64{"A": pid}, nil)
	if code, _ := owner.do("POST", "/api/admin/people", map[string]string{"email": "admin@college.edu", "role": "admin"}, s.srv.URL); code != 200 {
		t.Fatal("grant admin")
	}
	admin := s.signedIn("admin@college.edu", "Admin")
	ta := s.signedIn("ta@college.edu", "TA")
	student := s.signedIn("asha@college.edu", "Asha")
	if code, _ := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", cid), map[string]string{"email": "ta@college.edu"}, s.srv.URL); code != 200 {
		t.Fatal("add co-organiser")
	}
	_, ac := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", cid), nil, "")
	revID := ac["problems"].([]any)[0].(map[string]any)["revision_id"]
	for _, tc := range []struct {
		path        string
		method      string
		body        any
		owner, adm  int
		coOrg, stud int
	}{
		{fmt.Sprintf("/api/admin/problems/%d/revisions/1", pid), "GET", nil, 200, 200, 403, 403},
		{fmt.Sprintf("/api/admin/problems/%d/revisions/diff?from=1&to=1", pid), "GET", nil, 200, 200, 403, 403},
		{fmt.Sprintf("/api/admin/contests/%d", cid), "GET", nil, 200, 200, 200, 404},
		{fmt.Sprintf("/api/admin/contests/%d", other), "GET", nil, 200, 200, 404, 404},
		{fmt.Sprintf("/api/admin/contests/%d/audit", cid), "GET", nil, 200, 200, 200, 404},
		{fmt.Sprintf("/api/admin/contests/%d/problems/A/revision", cid), "PUT", map[string]any{"revision_id": revID}, 409, 409, 403, 404},
		{"/api/admin/site-audit", "GET", nil, 200, 403, 403, 403},
	} {
		for who, b := range map[string]*browser{"owner": owner, "admin": admin, "co-organiser": ta, "student": student} {
			want := map[string]int{"owner": tc.owner, "admin": tc.adm, "co-organiser": tc.coOrg, "student": tc.stud}[who]
			if code, out := b.do(tc.method, tc.path, tc.body, s.srv.URL); code != want {
				t.Errorf("%s %s as %s: %d, want %d (%v)", tc.method, tc.path, who, code, want, out)
			}
		}
	}
	// Before the start a student cannot read the statement history.
	if code, _ := student.do("GET", "/api/contests/week-7/problems/A/revisions", nil, ""); code != 403 {
		t.Errorf("history before the start: %d", code)
	}
}

func TestP2RenamedContestRedirectsAndKeepsItsSlug(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	cid := s.createContest(owner, "week-9", time.Now().Add(time.Hour), map[string]int64{"A": pid}, nil)
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", cid), map[string]any{"slug": "week-9-final"}, s.srv.URL); code != 200 {
		t.Fatalf("rename: %d %v", code, out)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Jar: owner.client.Jar}
	for path, want := range map[string]string{
		"/contests/week-9":                   "/contests/week-9-final",
		"/contests/week-9/problems/A?tab=1":  "/contests/week-9-final/problems/A?tab=1",
		"/api/contests/week-9":               "/api/contests/week-9-final",
		"/api/contests/week-9/standings":     "/api/contests/week-9-final/standings",
		"/contests/week-9-final":             "",
		"/contests/never-existed/problems/A": "",
	} {
		resp, err := noFollow.Get(s.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if want == "" {
			if resp.StatusCode == http.StatusMovedPermanently {
				t.Errorf("%s redirected to %s", path, resp.Header.Get("Location"))
			}
			continue
		}
		if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != want {
			t.Errorf("%s: %d %s, want 301 %s", path, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
	// A POST to the old slug is not redirected.
	if code, _ := owner.do("POST", "/api/contests/week-9/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 404 {
		t.Errorf("POST to the old slug: %d", code)
	}
	// The old slug cannot go to another contest; the contest may take it back.
	code, out := owner.do("POST", "/api/admin/contests", map[string]any{"slug": "week-9", "title": "Other",
		"starts_at": time.Now().Add(2 * time.Hour).UnixMilli(), "ends_at": time.Now().Add(3 * time.Hour).UnixMilli()}, s.srv.URL)
	if code != 400 || !strings.Contains(fmt.Sprint(out), "belonged to another contest") {
		t.Errorf("reusing an old slug: %d %v", code, out)
	}
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", cid), map[string]any{"slug": "week-9"}, s.srv.URL); code != 200 {
		t.Errorf("taking back its own slug: %d %v", code, out)
	}
}

func TestP2PracticeRevealsAndContestSubmitNever(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	for _, mode := range []string{"subtask", "exam"} {
		slug := "rev-" + mode
		s.clock.set(time.Now())
		s.createContest(owner, slug, start, map[string]int64{"A": pid}, map[string]any{"feedback_mode": mode, "practice_after_end": true})
	}
	student := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	for _, mode := range []string{"subtask", "exam"} {
		slug := "rev-" + mode
		student.do("POST", "/api/contests/"+slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL)
		// "int32" wraps, so it fails the large hidden tests.
		code, sub := s.submit(student, slug, "A", "int32")
		if code != 202 {
			t.Fatalf("%s submit: %d %v", mode, code, sub)
		}
		_, got := student.do("GET", fmt.Sprintf("/api/submissions/%v", sub["id"]), nil, "")
		if _, ok := got["revealed"]; ok {
			t.Errorf("%s: a counting submission carries revealed: %v", mode, got["revealed"])
		}
	}
	// After the end, practice reveals the first failing hidden test.
	s.at(start, 200)
	_, sub := s.submit(student, "rev-subtask", "A", "int32")
	_, got := student.do("GET", fmt.Sprintf("/api/submissions/%v", sub["id"]), nil, "")
	rv, ok := got["revealed"].(map[string]any)
	if !ok || rv["input"] == "" || rv["expected"] == "" || rv["output"] == "" {
		t.Fatalf("practice submission revealed %v", got["revealed"])
	}
	// The co-organiser view of someone else's practice submission never
	// shows it.
	ta := s.signedIn("ta@college.edu", "TA")
	var cid int64
	s.db.QueryRow("SELECT id FROM contests WHERE slug = 'rev-subtask'").Scan(&cid)
	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", cid), map[string]string{"email": "ta@college.edu"}, s.srv.URL)
	if _, v := ta.do("GET", fmt.Sprintf("/api/submissions/%v", sub["id"]), nil, ""); v["revealed"] != nil {
		t.Errorf("a co-organiser sees the revealed test: %v", v["revealed"])
	}
}

// Security review F1: a rejudge keeps each submission's own reveal
// decision, so a co-organiser's submissions in an ungraded contest never
// start showing hidden tests, and a student's practice submission keeps
// its reveal.
func TestP2RejudgeNeverWidensTheReveal(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	cid := s.createContest(owner, "ungraded", start, map[string]int64{"A": pid}, map[string]any{"graded": false})
	ta := s.signedIn("ta@college.edu", "TA")
	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", cid), map[string]string{"email": "ta@college.edu"}, s.srv.URL)
	student := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	student.do("POST", "/api/contests/ungraded/start", map[string]any{"accept_honour": true}, s.srv.URL)
	c1, mine := s.submit(student, "ungraded", "A", "int32")
	c2, theirs := s.submit(ta, "ungraded", "A", "int32")
	if c1 != 202 || c2 != 202 {
		t.Fatalf("submits %d %v, %d %v", c1, mine, c2, theirs)
	}
	if _, v := ta.do("GET", fmt.Sprintf("/api/submissions/%v", theirs["id"]), nil, ""); v["state"] != "done" || v["revealed"] != nil {
		t.Fatalf("co-organiser submission before the rejudge: %v", v)
	}
	if code, _ := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", cid), map[string]any{"problem_label": "A"}, s.srv.URL); code != 202 {
		t.Fatal("rejudge")
	}
	s.judgeAll()
	if _, v := student.do("GET", fmt.Sprintf("/api/submissions/%v", mine["id"]), nil, ""); v["revealed"] == nil {
		t.Errorf("an ungraded student submission lost its reveal on a rejudge: %v", v)
	}
	if _, v := ta.do("GET", fmt.Sprintf("/api/submissions/%v", theirs["id"]), nil, ""); v["revealed"] != nil {
		t.Errorf("a rejudge gave a co-organiser a revealed hidden test: %v", v["revealed"])
	}
	// The co-organiser view hides a reveal anyway (NoReveal), so read the
	// rejudged jobs' payloads themselves (C1-023).
	for _, c := range []struct {
		sub  any
		want bool
	}{{mine["id"], true}, {theirs["id"], false}} {
		var payload string
		if err := s.db.QueryRow(`SELECT j.payload FROM submissions s JOIN jobs j ON j.id = s.job_id WHERE s.id = ?`, c.sub).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var pl struct {
			Reveal bool `json:"reveal"`
		}
		if err := json.Unmarshal([]byte(payload), &pl); err != nil {
			t.Fatal(err)
		}
		if pl.Reveal != c.want {
			t.Errorf("submission %v: rejudged payload reveal %v, want %v", c.sub, pl.Reveal, c.want)
		}
	}
}

// Security review F3 and F4: renaming a draft reserves no slug, and an
// unpublished contest's new slug is not told to whoever asks with the old.
func TestP2DraftRenamesReserveNothing(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	code, out := owner.do("POST", "/api/admin/contests", map[string]any{"slug": "draft-a", "title": "Draft",
		"starts_at": time.Now().Add(time.Hour).UnixMilli(), "ends_at": time.Now().Add(2 * time.Hour).UnixMilli()}, s.srv.URL)
	if code != 201 {
		t.Fatalf("create %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	if code, _ := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", id), map[string]any{"slug": "draft-b"}, s.srv.URL); code != 200 {
		t.Fatal("rename")
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM slug_history").Scan(&n)
	if n != 0 {
		t.Errorf("a draft rename wrote %d slug history rows", n)
	}
	if current, err := s.contests.RenamedTo(context.Background(), "draft-a"); err != nil || current != "" {
		t.Errorf("draft redirect %q %v", current, err)
	}
	// A published contest renamed, then unpublished, stops redirecting.
	pid := s.importProblem(owner, "sum")
	cid := s.createContest(owner, "pub-a", time.Now().Add(time.Hour), map[string]int64{"A": pid}, nil)
	owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", cid), map[string]any{"slug": "pub-b"}, s.srv.URL)
	if current, _ := s.contests.RenamedTo(context.Background(), "pub-a"); current != "pub-b" {
		t.Fatalf("published redirect %q", current)
	}
	owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", cid), map[string]any{"published": false}, s.srv.URL)
	if current, _ := s.contests.RenamedTo(context.Background(), "pub-a"); current != "" {
		t.Errorf("an unpublished contest's new slug is told: %q", current)
	}
}
