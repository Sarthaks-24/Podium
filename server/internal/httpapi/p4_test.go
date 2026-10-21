package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
)

// standaloneNumber finds the rating value 95 on its own, not inside a
// timestamp such as 1793749560029.
var standaloneNumber = regexp.MustCompile(`(^|[^0-9])95([^0-9]|$)`)

// assertKeys fails unless m has exactly the keys given.
func assertKeys(t *testing.T, what string, m map[string]any, keys ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	for k := range m {
		if !want[k] {
			t.Errorf("%s has an unexpected key %q: %v", what, k, m)
		}
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s lacks %q: %v", what, k, m)
		}
	}
}

func viewsOf(t *testing.T, s *site, kind string, subject int64) int {
	t.Helper()
	var n int
	s.db.QueryRow("SELECT count(*) FROM record_views WHERE kind = ? AND subject_id = ?", kind, subject).Scan(&n)
	return n
}

// The P4 authorisation matrix over HTTP: a student, a co-organiser whose
// role is student (the TA), an admin and the owner against notes, cases,
// standing, the record page, the rating, summaries and progress; every
// record read logged; the two-person rule, the uninvolved appeal
// reviewer, and no rating value in any student response.
func TestStudentsAuthorisationMatrix(t *testing.T) {
	w := newIntegrityWorld(t)
	s := w.s
	ctx := context.Background()
	asha, bilal := w.asha.userID(), w.bilal.userID()
	s.at(w.start, 130) // over for everyone
	var sub int64
	s.db.QueryRow("SELECT id FROM submissions WHERE user_id = ? AND contest_id = ? AND kind = 'submit'", asha, w.c1).Scan(&sub)
	notes := fmt.Sprintf("/api/admin/submissions/%d/notes", sub)
	openCase := fmt.Sprintf("/api/admin/submissions/%d/case", sub)
	o := s.srv.URL

	// Notes and manual cases: reviewers only.
	note := map[string]any{"text": "Lines 3 to 5 look like the editorial.", "lines": []map[string]int{{"from": 3, "to": 5}}}
	for who, b := range map[string]*browser{"ta": w.ta, "dana": w.dana} {
		want := map[string]int{"ta": 403, "dana": 404}[who]
		if code, _ := b.do("GET", notes, nil, ""); code != want {
			t.Errorf("%s reads notes: %d, want %d", who, code, want)
		}
		if code, _ := b.do("POST", notes, note, o); code != want {
			t.Errorf("%s writes a note: %d, want %d", who, code, want)
		}
		if code, _ := b.do("POST", openCase, map[string]any{"title": "A", "note": "A reason that is long enough."}, o); code != want {
			t.Errorf("%s opens a case: %d, want %d", who, code, want)
		}
	}
	if code, out := w.admin.do("POST", notes, note, o); code != 201 || out["expires_at"] == nil {
		t.Fatalf("admin note: %d %v", code, out)
	}
	if code, out := w.asha.do("POST", notes, note, o); code != 404 {
		t.Errorf("the author notes their own: %d %v", code, out)
	}
	code, nout := w.owner.do("GET", notes, nil, "")
	if code != 200 || len(nout["notes"].([]any)) != 1 {
		t.Fatalf("owner notes: %d %v", code, nout)
	}
	// The program comes with the notes, so the reviewer can read it and pick lines.
	var src string
	s.db.QueryRow("SELECT source FROM submissions WHERE id = ?", sub).Scan(&src)
	if src == "" || nout["source"] != src {
		t.Errorf("notes source %q, want the submission's %q", nout["source"], src)
	}
	if viewsOf(t, s, "notes", asha) != 1 {
		t.Error("the notes read was not logged")
	}
	code, kase := w.owner.do("POST", openCase, map[string]any{"title": "Problem A, a question", "note": "The code matches the editorial closely."}, o)
	if code != 201 || kase["source"] != "manual" {
		t.Fatalf("manual case: %d %v", code, kase)
	}
	caseID := int64(kase["id"].(float64))
	caseURL := fmt.Sprintf("/api/admin/integrity/cases/%d", caseID)
	if code, out := w.owner.do("PUT", caseURL, map[string]any{"state": "awaiting_student", "note": "Please explain how you wrote it."}, o); code != 200 {
		t.Fatalf("share: %d %v", code, out)
	}
	if code, _ := w.asha.do("POST", fmt.Sprintf("/api/integrity/cases/%d/opened", caseID), nil, o); code != 204 {
		t.Errorf("opened: %d", code)
	}
	if code, _ := w.bilal.do("POST", fmt.Sprintf("/api/integrity/cases/%d/opened", caseID), nil, o); code != 404 {
		t.Errorf("another student opened the case: %d", code)
	}
	if code, _ := w.asha.do("PUT", fmt.Sprintf("/api/integrity/cases/%d/explanation", caseID), map[string]any{"explanation": "I wrote it in the lab."}, o); code != 204 {
		t.Fatalf("explain: %d", code)
	}
	_, got := w.owner.do("GET", caseURL, nil, "")
	if viewsOf(t, s, "case", asha) == 0 {
		t.Error("the case read was not logged")
	}
	seen := map[string]any{}
	for _, u := range got["users"].([]any) {
		u := u.(map[string]any)
		vs := u["explanations"].([]any)
		seen[fmt.Sprint(int64(u["user_id"].(float64)))] = vs[len(vs)-1].(map[string]any)["created_at"]
	}
	// The TA is not a reviewer; the owner proposes; the two-person rule.
	propose := map[string]any{"outcome": "warning", "note": "A reason for the reviewers to read.", "reason": "A reason the student reads.",
		"seen_explanations": seen}
	if code, _ := w.ta.do("POST", caseURL+"/proposals", propose, o); code != 404 {
		t.Errorf("the TA proposed: %d", code)
	}
	code, got = w.owner.do("POST", caseURL+"/proposals", propose, o)
	if code != 201 {
		t.Fatalf("propose: %d %v", code, got)
	}
	pid := int64(got["proposals"].([]any)[0].(map[string]any)["id"].(float64))
	// The contest's case list says whose confirmation the proposal waits for (D321).
	for _, v := range []struct {
		who  string
		b    *browser
		want bool
	}{{"the proposer", w.owner, false}, {"a second reviewer", w.admin, true}} {
		_, list := v.b.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/cases", w.c1), nil, "")
		var row map[string]any
		for _, c := range list["cases"].([]any) {
			if int64(c.(map[string]any)["id"].(float64)) == caseID {
				row = c.(map[string]any)
			}
		}
		if row == nil || row["may_confirm"] != v.want {
			t.Errorf("%s's case list: may_confirm %v, want %v", v.who, row["may_confirm"], v.want)
		}
		// The console's count: no names, so not a record read.
		before := viewsOf(t, s, "case", asha)
		code, got := v.b.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/waiting", w.c1), nil, "")
		if want := map[bool]float64{false: 0, true: 1}[v.want]; code != 200 || got["waiting_for_you"] != want {
			t.Errorf("%s waiting: %d %v, want %v", v.who, code, got, want)
		}
		if viewsOf(t, s, "case", asha) != before {
			t.Errorf("%s's waiting count was logged as a case read", v.who)
		}
	}
	if code, _ := w.ta.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/waiting", w.c1), nil, ""); code != 403 {
		t.Errorf("the TA read the waiting count: %d", code)
	}
	confirm := map[string]any{"note": "Confirmed after reading the code.", "seen_explanations": seen}
	confirmURL := fmt.Sprintf("%s/proposals/%d/confirm", caseURL, pid)
	if code, out := w.owner.do("POST", confirmURL, confirm, o); code != 403 || errCode(out) != "two_person_rule" {
		t.Errorf("the proposer confirmed: %d %v", code, out)
	}
	if code, out := w.admin.do("POST", confirmURL, confirm, o); code != 200 || out["state"] != "actioned" {
		t.Fatalf("second reviewer: %d %v", code, out)
	}
	if _, got := w.admin.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/waiting", w.c1), nil, ""); got["waiting_for_you"] != float64(0) {
		t.Errorf("waiting after the confirm: %v", got)
	}

	// Standing: the student's own, a band and lines, never the number.
	code, st := w.asha.do("GET", "/api/me/standing", nil, "")
	js, _ := json.Marshal(st)
	if code != 200 || st["band"] != "good" || len(st["lines"].([]any)) != 1 || strings.Contains(string(js), "value") ||
		strings.Contains(string(js), "rating") || standaloneNumber.MatchString(string(js)) {
		t.Errorf("own standing: %d %s", code, js)
	}
	// Only these keys, so a rating that leaks as 9.5 or under a new field name fails too (C1-026).
	assertKeys(t, "standing", st, "band", "band_name", "lines", "server_now")
	for _, l := range st["lines"].([]any) {
		assertKeys(t, "standing line", l.(map[string]any), "id", "kind", "at", "text", "reason", "case_id", "adjustment_id", "appeal",
			"appeal_by", "can_appeal")
	}
	_, bst := w.bilal.do("GET", "/api/me/standing", nil, "")
	if len(bst["lines"].([]any)) != 0 {
		t.Errorf("bilal sees lines: %v", bst)
	}
	// The record page: owners and admins only, every read logged.
	record := fmt.Sprintf("/api/admin/students/%d/record", asha)
	for who, b := range map[string]*browser{"bilal": w.bilal, "ta": w.ta, "asha": w.asha} {
		if code, _ := b.do("GET", record, nil, ""); code != 403 {
			t.Errorf("%s read the record: %d", who, code)
		}
		if code, _ := b.do("GET", fmt.Sprintf("/api/admin/students/%d", asha), nil, ""); code != 403 {
			t.Errorf("%s read the student page: %d", who, code)
		}
		if code, _ := b.do("GET", fmt.Sprintf("/api/admin/users/%d/progress", asha), nil, ""); code != 403 {
			t.Errorf("%s read the progress: %d", who, code)
		}
	}
	if code, _ := w.admin.do("GET", fmt.Sprintf("/api/admin/students/%d/record", w.admin.userID()), nil, ""); code != 403 {
		t.Errorf("an admin opened their own record: %d", code)
	}
	// The contest's case list is a case read too.
	cases := viewsOf(t, s, "case", asha)
	w.owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/cases", w.c1), nil, "")
	if viewsOf(t, s, "case", asha) != cases+1 {
		t.Error("the case list read was not logged")
	}
	before := viewsOf(t, s, "record", asha)
	code, rec := w.admin.do("GET", record, nil, "")
	if code != 200 || rec["rating"].(map[string]any)["value_tenths"].(float64) != 95 || len(rec["notes"].([]any)) != 1 {
		t.Fatalf("record: %d %v", code, rec)
	}
	if viewsOf(t, s, "record", asha) != before+1 {
		t.Error("the record read was not logged")
	}

	// The rating edit: never a co-organiser; a band-lowering edit waits.
	rating := fmt.Sprintf("/api/admin/students/%d/rating", asha)
	edit := map[string]any{"value_tenths": 50, "reason": "Lowered after a long conversation."}
	if code, _ := w.ta.do("PUT", rating, edit, o); code != 403 {
		t.Errorf("the TA edited the rating: %d", code)
	}
	code, out := w.admin.do("PUT", rating, edit, o)
	if code != 202 || out["pending_rating"] == nil {
		t.Fatalf("band-lowering edit: %d %v", code, out)
	}
	ppid := int64(out["pending_rating"].(map[string]any)["id"].(float64))
	if code, out := w.admin.do("POST", fmt.Sprintf("%s/proposals/%d/confirm", rating, ppid), nil, o); code != 403 || errCode(out) != "two_person_rule" {
		t.Errorf("proposer confirmed the edit: %d %v", code, out)
	}
	if code, out := w.owner.do("POST", fmt.Sprintf("%s/proposals/%d/confirm", rating, ppid), nil, o); code != 200 ||
		out["rating"].(map[string]any)["value_tenths"].(float64) != 50 {
		t.Errorf("owner confirmed the edit: %d %v", code, out)
	}

	// Appeal of the outcome: the opener and the confirmer may not rule.
	code, ap := w.asha.do("POST", "/api/me/appeals", map[string]any{"case_id": caseID, "text": "I can show my notes from the lab session."}, o)
	if code != 201 {
		t.Fatalf("appeal: %d %v", code, ap)
	}
	if code, _ := w.bilal.do("POST", "/api/me/appeals", map[string]any{"case_id": caseID, "text": "Appealing someone else's outcome."}, o); code != 404 {
		t.Errorf("another student appealed: %d", code)
	}
	ruling := fmt.Sprintf("/api/admin/appeals/%d/ruling", int64(ap["id"].(float64)))
	rule := map[string]any{"upheld": false, "reason": "The code matches the editorial line by line."}
	for who, b := range map[string]*browser{"owner": w.owner, "admin": w.admin} {
		if code, out := b.do("POST", ruling, rule, o); code != 403 || errCode(out) != "uninvolved_reviewer" {
			t.Errorf("%s ruled: %d %v", who, code, out)
		}
	}
	if code, _ := w.ta.do("POST", ruling, rule, o); code != 403 {
		t.Errorf("the TA ruled")
	}
	w.owner.do("POST", "/api/admin/people", map[string]string{"email": "third@college.edu", "role": "admin"}, o)
	third := s.signedIn("third@college.edu", "Third")
	// Listing appeals reads the appellant's record: logged, once per appeal shown (C1-026).
	appealViews := viewsOf(t, s, "case", asha)
	if code, list := third.do("GET", "/api/admin/appeals", nil, ""); code != 200 || len(list["appeals"].([]any)) != 1 {
		t.Errorf("appeals list: %d %v", code, list)
	}
	if viewsOf(t, s, "case", asha) != appealViews+1 {
		t.Error("the appeals list read was not logged")
	}
	if code, out := third.do("POST", ruling, rule, o); code != 200 || out["state"] != "rejected" {
		t.Errorf("uninvolved ruling: %d %v", code, out)
	}

	// Notices: the student sees what happened, no names or code.
	_, me := w.asha.do("GET", "/api/me", nil, "")
	code, inbox := w.asha.do("GET", "/api/me/notifications", nil, "")
	js, _ = json.Marshal(inbox)
	if code != 200 || me["notifications_unread"].(float64) < 3 || strings.Contains(string(js), "Owner") || strings.Contains(string(js), "include") ||
		strings.Contains(string(js), "editorial") {
		t.Errorf("inbox: %d unread %v %s", code, me["notifications_unread"], js)
	}
	if code, _ := w.asha.do("POST", "/api/me/notifications/read", map[string]any{"all": true}, o); code != 204 {
		t.Errorf("read all: %d", code)
	}
	if _, me := w.asha.do("GET", "/api/me", nil, ""); me["notifications_unread"].(float64) != 0 {
		t.Errorf("unread after reading: %v", me["notifications_unread"])
	}

	// Summaries, progress and the students pages.
	if _, err := s.contests.SweepSummaries(ctx); err != nil {
		t.Fatal(err)
	}
	code, prog := w.asha.do("GET", "/api/me/progress", nil, "")
	js, _ = json.Marshal(prog)
	if code != 200 || len(prog["contests"].([]any)) != 1 || prog["hide_progress"] != false || strings.Contains(string(js), "rating") ||
		strings.Contains(string(js), "band") || strings.Contains(string(js), "value_tenths") {
		t.Errorf("progress: %d %s", code, js)
	}
	if code, out := w.asha.do("PUT", "/api/me/preferences", map[string]any{"theme": "", "verdicts": "standard", "hide_progress": true}, o); code != 200 ||
		out["hide_progress"] != true {
		t.Fatalf("hide progress: %d %v", code, out)
	}
	if _, prog := w.asha.do("GET", "/api/me/progress", nil, ""); len(prog["contests"].([]any)) != 0 || prog["hide_progress"] != true {
		t.Errorf("hidden progress: %v", prog)
	}
	if code, prog := w.admin.do("GET", fmt.Sprintf("/api/admin/users/%d/progress", asha), nil, ""); code != 200 ||
		len(prog["contests"].([]any)) != 1 || prog["hide_progress"] != nil {
		t.Errorf("admin progress: %d %v", code, prog)
	}
	code, list := w.admin.do("GET", "/api/admin/students", nil, "")
	if code != 200 {
		t.Fatalf("students: %d %v", code, list)
	}
	found := false
	for _, r := range list["students"].([]any) {
		r := r.(map[string]any)
		if int64(r["id"].(float64)) == asha {
			found = r["rating_tenths"].(float64) == 50 && r["graded_contests"].(float64) == 1
		}
	}
	if !found {
		t.Errorf("students table: %v", list)
	}
	if code, page := w.owner.do("GET", fmt.Sprintf("/api/admin/students/%d", bilal), nil, ""); code != 200 || len(page["contests"].([]any)) != 1 {
		t.Errorf("student page: %d %v", code, page)
	}
	code, cv := w.owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/class", w.c1), nil, "")
	js, _ = json.Marshal(cv)
	if code != 200 || cv["participants"].(float64) != 3 || strings.Contains(string(js), "Asha") || strings.Contains(string(js), "user_id") {
		t.Errorf("class view: %d %s", code, js)
	}
	if code, _ := w.dana.do("GET", fmt.Sprintf("/api/admin/contests/%d/class", w.c1), nil, ""); code != 404 {
		t.Errorf("a student read the class view: %d", code)
	}

	// The owner's oversight and the record view log.
	if code, _ := w.admin.do("GET", "/api/admin/integrity/oversight", nil, ""); code != 403 {
		t.Errorf("admin read the oversight")
	}
	if code, out := w.owner.do("GET", "/api/admin/integrity/oversight", nil, ""); code != 200 || len(out["reviewers"].([]any)) == 0 {
		t.Errorf("oversight: %d %v", code, out)
	}
	if code, out := w.owner.do("GET", fmt.Sprintf("/api/admin/record-views?subject=%d", asha), nil, ""); code != 200 || len(out["views"].([]any)) < 3 {
		t.Errorf("record views: %d %v", code, out)
	}

	// B3.2 with C8: progress shows nothing for a forgotten account.
	actor := auth.Actor{ID: 1, Email: "owner@college.edu", Role: auth.Owner}
	var ownerID int64
	s.db.QueryRow("SELECT id FROM users WHERE email = 'owner@college.edu'").Scan(&ownerID)
	actor.ID = ownerID
	if _, err := s.authSvc.Forget(auth.WithActor(ctx, actor), "bilal@college.edu", true, false); err != nil {
		t.Fatal(err)
	}
	// The organisers' route answers 404 for an erased account; its data
	// is gone too, so a later rejudge brings nothing back.
	if code, prog := w.owner.do("GET", fmt.Sprintf("/api/admin/users/%d/progress", bilal), nil, ""); code != 404 {
		t.Errorf("forgotten account progress: %d %v", code, prog)
	}
	if p, err := s.contests.Summaries(ctx, bilal); err != nil || len(p) != 0 {
		t.Errorf("summaries after erasure: %v %v", p, err)
	}
	if _, err := s.contests.SweepSummaries(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	s.db.QueryRow("SELECT count(*) FROM student_contest_summary WHERE user_id = ?", bilal).Scan(&left)
	if left != 0 {
		t.Errorf("summaries left after erasure: %d", left)
	}
}

// Progress in exam mode shows the totals only, and a contest whose reveal
// is held is absent until the reveal.
func TestProgressExamTotalsAndHeldReveal(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "exam", start, map[string]int64{"A": pa}, map[string]any{"feedback_mode": "exam"})
	s.createContest(owner, "held", start, map[string]int64{"A": pa}, map[string]any{"hold_reveal": true, "freeze_minutes": 30})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 5)
	for _, slug := range []string{"exam", "held"} {
		asha.do("POST", "/api/contests/"+slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL)
		if code, out := s.submit(asha, slug, "A", "correct"); code != 202 {
			t.Fatalf("submit %s: %d %v", slug, code, out)
		}
	}
	s.at(start, 130)
	if _, err := s.contests.SweepSummaries(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, prog := asha.do("GET", "/api/me/progress", nil, "")
	list := prog["contests"].([]any)
	if len(list) != 1 {
		t.Fatalf("contests in progress: %v", prog)
	}
	c := list[0].(map[string]any)
	p := c["problems"].([]any)[0].(map[string]any)
	if c["slug"] != "exam" || c["detail"] != "totals" || c["score"].(float64) != 100 || p["score"] != nil || p["best_verdict"] != nil ||
		p["attempts"] != nil || p["subtasks"] != nil || len(prog["losses"].([]any)) != 0 {
		t.Errorf("exam contest in progress: %v", c)
	}
	var held int64
	s.db.QueryRow("SELECT id FROM contests WHERE slug = 'held'").Scan(&held)
	if code, out := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/reveal", held), nil, s.srv.URL); code != 200 {
		t.Fatalf("reveal: %d %v", code, out)
	}
	if _, prog = asha.do("GET", "/api/me/progress", nil, ""); len(prog["contests"].([]any)) != 2 {
		t.Errorf("after the reveal: %v", prog["contests"])
	}
}
