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
	"strings"
	"testing"
	"time"
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

func TestP2PracticeRevealsAndContestSubmitNever(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	for _, mode := range []string{"subtask"} {
		slug := "rev-" + mode
		s.clock.set(time.Now())
		s.createContest(owner, slug, start, map[string]int64{"A": pid}, map[string]any{"practice_after_end": true})
	}
	student := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	for _, mode := range []string{"subtask"} {
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

// M2 (V3-6): practice after one contest never reveals a hidden test of a
// problem that another contest still pins and has not closed, drafts and
// upcoming contests included. Once that one closes, practice reveals again.
func TestPracticeRevealWaitsForEveryContestOfTheProblem(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pid := s.importProblem(owner, "sum")
	start := time.Now().Add(time.Hour)
	s.createContest(owner, "m2-first", start, map[string]int64{"A": pid}, map[string]any{"practice_after_end": true})
	later := start.Add(6 * time.Hour)
	s.createContest(owner, "m2-exam", later, map[string]int64{"A": pid}, nil)
	student := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	student.do("POST", "/api/contests/m2-first/start", map[string]any{"accept_honour": true}, s.srv.URL)

	s.at(start, 200)
	code, sub := s.submit(student, "m2-first", "A", "int32")
	if code != 202 {
		t.Fatalf("practice submit: %d %v", code, sub)
	}
	if _, got := student.do("GET", fmt.Sprintf("/api/submissions/%v", sub["id"]), nil, ""); got["revealed"] != nil {
		t.Fatalf("practice revealed a hidden test the upcoming m2-exam still uses: %v", got["revealed"])
	}

	s.at(later, 130)
	_, sub = s.submit(student, "m2-first", "A", "int32")
	_, got := student.do("GET", fmt.Sprintf("/api/submissions/%v", sub["id"]), nil, "")
	if rv, ok := got["revealed"].(map[string]any); !ok || rv["input"] == "" {
		t.Fatalf("practice after every contest closed: revealed %v", got["revealed"])
	}
}

// A draft may change its slug; a published contest keeps it, so links to
// it keep working (v3, no redirects).
// TestContestKeepsItsSlug: the slug is derived from the title at create
// and never changes, so links keep working (V3-15).
func TestContestKeepsItsSlug(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	code, out := owner.do("POST", "/api/admin/contests", map[string]any{"title": "Week 6: Graphs!",
		"starts_at": time.Now().Add(time.Hour).UnixMilli(), "ends_at": time.Now().Add(2 * time.Hour).UnixMilli()}, s.srv.URL)
	if code != 201 || out["slug"] != "week-6-graphs" {
		t.Fatalf("create %d %v", code, out)
	}
	id := int64(out["id"].(float64))
	edit := map[string]any{"slug": "other", "title": "Week 7", "starts_at": out["starts_at"], "ends_at": out["ends_at"],
		"min_gap_s": 30, "max_per_problem": 15, "languages": []string{"cpp17"}}
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", id), edit, s.srv.URL); code != 200 || out["slug"] != "week-6-graphs" || out["title"] != "Week 7" {
		t.Errorf("rename: %d %v", code, out)
	}
}
