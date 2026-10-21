package httpapi

// Fixes from council run 1 (cache/v2-plan/current-inconsistencies/
// run-1-2026-10-21/FINDINGS.md), one test per finding id.

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

// shownInPractice reads the bank flag of one problem.
func (s *site) shownInPractice(owner *browser, pid int64) bool {
	s.t.Helper()
	code, out := owner.do("GET", "/api/admin/problems", nil, "")
	if code != 200 {
		s.t.Fatalf("bank: %d %v", code, out)
	}
	for _, p := range out["problems"].([]any) {
		m := p.(map[string]any)
		if int64(m["id"].(float64)) == pid {
			v, ok := m["shown_in_practice"].(bool)
			if !ok {
				s.t.Fatalf("no shown_in_practice in %v", m)
			}
			return v
		}
	}
	s.t.Fatalf("problem %d not in the bank", pid)
	return false
}

// C1-015: the bank marks a problem whose hidden tests a contest showed
// (ungraded and started, or practice after the end), and turning grading
// off after the start needs an explicit confirm.
func TestC1015HiddenTestsShownInPracticeAndUngradedConfirm(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c1015-a")
	pb := s.importProblem(owner, "c1015-b")
	pc := s.importProblem(owner, "c1015-c")
	graded := s.createContest(owner, "c1015-graded", start, map[string]int64{"A": pa}, nil)
	s.createContest(owner, "c1015-practice", start, map[string]int64{"A": pb}, map[string]any{"practice_after_end": true})
	s.createContest(owner, "c1015-closed", start, map[string]int64{"A": pc}, map[string]any{"practice_after_end": false})
	for _, p := range []int64{pa, pb, pc} {
		if s.shownInPractice(owner, p) {
			t.Errorf("problem %d marked before any contest started", p)
		}
	}

	s.at(start, 5)
	path := fmt.Sprintf("/api/admin/contests/%d", graded)
	code, out := owner.do("PUT", path, map[string]any{"graded": false}, o)
	if code != 400 || errCode(out) != "confirm_ungraded" {
		t.Fatalf("turning grading off after the start without a confirm: %d %v", code, out)
	}
	if s.shownInPractice(owner, pa) {
		t.Error("refused change still marked the problem")
	}
	if code, out := owner.do("PUT", path, map[string]any{"graded": false, "confirm_ungraded": true}, o); code != 200 || out["graded"] != false {
		t.Fatalf("with the confirm: %d %v", code, out)
	}
	if !s.shownInPractice(owner, pa) {
		t.Error("an ungraded started contest's problem is not marked")
	}
	// Turning it on again needs no confirm.
	if code, out := owner.do("PUT", path, map[string]any{"graded": true}, o); code != 200 {
		t.Errorf("grading on again: %d %v", code, out)
	}

	if s.shownInPractice(owner, pb) {
		t.Error("practice marked before the end")
	}
	s.at(start, 121)
	if !s.shownInPractice(owner, pb) {
		t.Error("a problem open for practice is not marked")
	}
	if s.shownInPractice(owner, pc) {
		t.Error("a graded contest without practice marked its problem")
	}
}

// C1-022: through the server as built (the notifications service wired by
// New, not by hand), a notice waits while the student is in a live
// contest and shows once their window closes.
func TestC1022NoticesHeldInALiveContestOverHTTP(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c1022-a")
	s.createContest(owner, "c1022", start, map[string]int64{"A": pa}, nil)
	s.at(start, 5)
	if code, out := asha.do("POST", "/api/contests/c1022/start", map[string]any{"accept_honour": true}, o); code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	now := s.clock.now().UnixMilli()
	if _, err := s.db.Exec(`INSERT INTO notifications (user_id, kind, created_at, available_at) VALUES (?, 'results_final', ?, ?)`,
		asha.userID(), now, now); err != nil {
		t.Fatal(err)
	}
	if _, me := asha.do("GET", "/api/me", nil, ""); me["notifications_unread"] != float64(0) {
		t.Errorf("unread during the contest: %v", me["notifications_unread"])
	}
	if code, inbox := asha.do("GET", "/api/me/notifications", nil, ""); code != 200 || inbox["held"] != true || len(inbox["notifications"].([]any)) != 0 {
		t.Errorf("inbox during the contest: %d %v", code, inbox)
	}
	s.at(start, 121)
	if _, me := asha.do("GET", "/api/me", nil, ""); me["notifications_unread"] != float64(1) {
		t.Errorf("unread after the contest: %v", me["notifications_unread"])
	}
	if code, inbox := asha.do("GET", "/api/me/notifications", nil, ""); code != 200 || inbox["held"] != false || len(inbox["notifications"].([]any)) != 1 {
		t.Errorf("inbox after the contest: %d %v", code, inbox)
	}
}

// C1-043: the students list and the student page read the summary
// columns in SQL; their figures stay what the summaries say.
func TestC1043StudentsFromSummaryColumns(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	bilal := s.signedIn("bilal@college.edu", "Bilal")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c1043-a")
	s.createContest(owner, "c1043", start, map[string]int64{"A": pa}, nil)
	s.at(start, 5)
	for _, b := range []*browser{asha, bilal} {
		b.do("POST", "/api/contests/c1043/start", map[string]any{"accept_honour": true}, o)
	}
	s.submit(asha, "c1043", "A", "correct")
	s.submit(bilal, "c1043", "A", "first")
	s.judgeAll()
	s.at(start, 121)
	if _, err := s.contests.SweepSummaries(context.Background()); err != nil {
		t.Fatal(err)
	}
	code, list := owner.do("GET", "/api/admin/students", nil, "")
	if code != 200 {
		t.Fatalf("students: %d %v", code, list)
	}
	percent := map[string]float64{}
	for _, row := range list["students"].([]any) {
		m := row.(map[string]any)
		if m["graded_contests"] != float64(1) || m["last_graded_slug"] != "c1043" {
			t.Errorf("row %v", m)
		}
		percent[m["email"].(string)] = m["last_graded_percent"].(float64)
	}
	if percent["asha@college.edu"] != 100 || percent["bilal@college.edu"] >= 100 {
		t.Errorf("percents %v", percent)
	}
	code, page := owner.do("GET", fmt.Sprintf("/api/admin/students/%d", asha.userID()), nil, "")
	if code != 200 {
		t.Fatalf("student page: %d %v", code, page)
	}
	cs := page["contests"].([]any)[0].(map[string]any)
	want := (percent["asha@college.edu"] + percent["bilal@college.edu"]) / 2
	if cs["class_median_percent"].(float64) != math.Round(want*10)/10 || cs["percent"] != float64(100) {
		t.Errorf("student page contest %v, want median %v", cs, want)
	}
}
