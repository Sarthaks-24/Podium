package httpapi

import (
	"fmt"
	"testing"
	"time"
)

func TestPersonSubmissionsArePaged(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "admin@college.edu", "role": "admin"}, s.srv.URL); code >= 300 {
		t.Fatalf("add admin: %d %v", code, out)
	}
	admin := s.signedIn("admin@college.edu", "Admin")
	if code, out := owner.do("POST", "/api/admin/people/import", map[string]any{"text": "student@college.edu", "dry_run": false}, s.srv.URL); code >= 300 {
		t.Fatalf("add person: %d %v", code, out)
	}
	person := s.signedIn("student@college.edu", "Student")
	problem := s.importProblem(owner, "person-history")
	contest := s.createContest(owner, "history-contest", time.Now().Add(-time.Hour).Truncate(time.Minute), map[string]int64{"A": problem}, nil)
	if code, out := person.do("POST", "/api/contests/history-contest/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	if code, out := s.submit(person, "history-contest", "A", "source"); code != 202 {
		t.Fatalf("submit: %d %v", code, out)
	}
	var pid int64
	if err := s.db.QueryRow("SELECT problem_id FROM submissions WHERE user_id=? ORDER BY id DESC LIMIT 1", person.userID()).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := s.db.Exec(`INSERT INTO submissions(problem_id,contest_id,user_id,kind,language,source,source_sha,created_at) VALUES(?,?,?,'submit','cpp17','x','hash',?)`, pid, contest, person.userID(), time.Now().Add(time.Duration(i)*time.Millisecond).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	code, first := admin.do("GET", fmt.Sprintf("/api/admin/people/%d/submissions?page=1", person.userID()), nil, "")
	if code != 200 {
		t.Fatalf("page 1: %d %v", code, first)
	}
	if len(first["submissions"].([]any)) != 50 || first["has_more"] != true {
		t.Fatalf("page 1 = %d, has_more=%v", len(first["submissions"].([]any)), first["has_more"])
	}
	code, last := owner.do("GET", fmt.Sprintf("/api/admin/people/%d/submissions?page=2", person.userID()), nil, "")
	if code != 200 || len(last["submissions"].([]any)) != 1 || last["has_more"] != false {
		t.Fatalf("page 2: %d %v", code, last)
	}
}
