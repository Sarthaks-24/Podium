package httpapi

// Checks for the M4 security review (cache/council-2026-10-18/
// m4-security.md), ported from its reproductions: co-organisers and the
// problem bank, the exam-mode timing oracle, the fixed-start freeze with a
// personal extension, read limits, and the roster and count additions.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestM4CoOrganiserCannotOpenTheBank(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	pb := s.importProblem(owner, "secret-b")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	c1 := s.createContest(owner, "c1", start, map[string]int64{"A": pa}, nil)
	ta := s.signedIn("ta@college.edu", "TA")
	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", c1), map[string]string{"email": "ta@college.edu"}, s.srv.URL)

	// Choosing problems opens the bank: admins only.
	if code, _ := ta.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", c1), []map[string]any{{"problem_id": pb, "label": "A"}}, s.srv.URL); code != 403 {
		t.Errorf("co-organiser setting problems: %d", code)
	}
	// Publishing hands problems to students: admins only.
	if code, _ := ta.do("PUT", fmt.Sprintf("/api/admin/contests/%d", c1), map[string]any{"published": false}, s.srv.URL); code != 403 {
		t.Errorf("co-organiser unpublishing: %d", code)
	}
	if code, _ := ta.do("PUT", fmt.Sprintf("/api/admin/contests/%d", c1), map[string]any{"title": "Week 1 (renamed)"}, s.srv.URL); code != 200 {
		t.Errorf("co-organiser editing other settings: %d", code)
	}
	// An unvalidated problem is refused without its title in the message.
	code, created := owner.raw("POST", "/api/admin/problems/import", "application/zip", packZip(t, "draft-c", nil, false), s.srv.URL)
	if code != 201 {
		t.Fatalf("import %d", code)
	}
	code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", c1), []map[string]any{{"problem_id": created["id"], "label": "C"}}, s.srv.URL)
	if code != 400 || strings.Contains(fmt.Sprint(out), "draft-c") || !strings.Contains(fmt.Sprint(out), "label C") {
		t.Errorf("unvalidated problem: %d %v", code, out)
	}
	// The co-organiser's own submissions get the student view and the
	// contest's limits (no organiser gap of 5 s, 15 per problem).
	s.at(start, -30)
	code, sub := ta.do("POST", "/api/contests/c1/problems/A/submit", map[string]any{"language": "cpp17", "source": "int32", "client_key": "x1"}, s.srv.URL)
	if code != 202 {
		t.Fatalf("co-organiser submit %d %v", code, sub)
	}
	s.judgeAll()
	_, got := ta.do("GET", "/api/submissions/"+strconv.Itoa(int(sub["id"].(float64))), nil, "")
	test := got["subtasks"].([]any)[0].(map[string]any)["tests"].([]any)[0].(map[string]any)
	if got["max_memory_kb"] != float64(16384) || got["max_time_ms"] != float64(100) || test["memory_kb"] != float64(0) {
		t.Errorf("co-organiser sees exact figures: %v", got)
	}
}

func TestM4ExamModeHasNoJudgingOracle(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "ex1", start, map[string]int64{"A": pa}, map[string]any{"feedback_mode": "exam", "max_per_problem": 1})
	s.createContest(owner, "ex5", start, map[string]int64{"A": pa}, map[string]any{"feedback_mode": "exam", "max_per_problem": 5})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	for _, slug := range []string{"ex1", "ex5"} {
		asha.do("POST", "/api/contests/"+slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL)
	}
	if code, _ := asha.do("POST", "/api/contests/ex1/problems/A/submit", map[string]any{"language": "cpp17", "source": "correct", "client_key": "k1"}, s.srv.URL); code != 202 {
		t.Fatalf("first submit %d", code)
	}
	probe := func(key string) string {
		code, out := asha.do("POST", "/api/contests/ex1/problems/A/submit", map[string]any{"language": "cpp17", "source": "x", "client_key": key}, s.srv.URL)
		return fmt.Sprint(code, out["error"].(map[string]any)["message"])
	}
	count := func(slug string) any {
		_, c := asha.do("GET", "/api/contests/"+slug, nil, "")
		return c["problems"].([]any)[0].(map[string]any)["my_submission_count"]
	}
	before, countBefore := probe("p1"), count("ex1")
	s.judgeAll()
	after, countAfter := probe("p2"), count("ex1")
	if before != after || countBefore != float64(1) || countAfter != float64(1) {
		t.Errorf("refusal or count changes when judging finishes: %q / %q, counts %v / %v", before, after, countBefore, countAfter)
	}
	// Below the cap a pending submission never refuses the next one.
	asha.do("POST", "/api/contests/ex5/problems/A/submit", map[string]any{"language": "cpp17", "source": "correct", "client_key": "q1"}, s.srv.URL)
	if code, out := asha.do("POST", "/api/contests/ex5/problems/A/submit", map[string]any{"language": "cpp17", "source": "int32", "client_key": "q2"}, s.srv.URL); code != 202 {
		t.Errorf("second submit while the first waits: %d %v", code, out)
	}
}

func TestM4FixedFreezeIgnoresPersonalExtension(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	a := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "fz", start, map[string]int64{"A": a}, map[string]any{"freeze_minutes": 60})
	asha := s.signedIn("asha@college.edu", "Asha")
	bilal := s.signedIn("bilal@college.edu", "Bilal")
	s.at(start, 1)
	for _, b := range []*browser{asha, bilal} {
		b.do("POST", "/api/contests/fz/start", map[string]any{"accept_honour": true}, s.srv.URL)
	}
	owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", id, bilal.userID()), map[string]any{"minutes": 30}, s.srv.URL)
	s.at(start, 70) // inside the freeze for everyone
	s.submit(bilal, "fz", "A", "correct")
	s.submit(asha, "fz", "A", "correct")
	_, board := asha.do("GET", "/api/contests/fz/standings", nil, "")
	if board["variant"] != "frozen" {
		t.Fatalf("board %v", board)
	}
	for _, r := range board["rows"].([]any) {
		row := r.(map[string]any)
		cell := row["cells"].([]any)[0].(map[string]any)
		if row["total"] != float64(0) || cell["pending"] != float64(1) {
			t.Errorf("%v: a score after the freeze shows: %v", row["name"], row)
		}
	}
}

func TestM4HeavyReadsAreRateLimited(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "rl", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "Asha")
	limited := 0
	for i := 0; i < 40; i++ {
		code, out := asha.do("GET", "/api/contests/rl", nil, "")
		if code == 429 {
			limited++
			if out["error"].(map[string]any)["retry_after_s"] == nil {
				t.Fatalf("429 without retry_after_s: %v", out)
			}
		}
	}
	if limited == 0 {
		t.Error("40 contest reads in a burst were never limited")
	}
	// Another user is not affected.
	if code, _ := owner.do("GET", "/api/contests/rl", nil, ""); code != 200 {
		t.Errorf("another user: %d", code)
	}
}

func TestM4RosterReplaceReportsChanges(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "ro", start, map[string]int64{"A": pa}, map[string]any{"access": "roster"})
	put := func(csv string) map[string]any {
		code, out := owner.raw("PUT", fmt.Sprintf("/api/admin/contests/%d/roster", id), "text/csv", []byte(csv), s.srv.URL)
		if code != 200 {
			t.Fatalf("roster %d %v", code, out)
		}
		return out
	}
	put("email\na@college.edu\nb@college.edu\n")
	out := put("email\nB@college.edu\nc@college.edu\nd@college.edu\n")
	if out["added"] != float64(2) || out["removed"] != float64(1) || out["roster_count"] != float64(3) {
		t.Errorf("replace counts %v", out)
	}
}
