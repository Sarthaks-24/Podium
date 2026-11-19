package httpapi

import (
	"fmt"
	"testing"
	"time"
)

// TestAddProblemsRoute: an add returns what was added and refused, a later
// save from the earlier list is a 409 problems_changed with the exact
// message, the body shapes both work, a student gets 403 and an organiser
// who cannot manage the contest gets the usual 404.
func TestAddProblemsRoute(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, out := owner.do("POST", "/api/admin/people", map[string]any{"email": "outsider@college.edu", "role": "organiser"}, s.srv.URL); code >= 300 {
		t.Fatalf("grant organiser: %d %v", code, out)
	}
	outsider := s.signedIn("outsider@college.edu", "Outsider")
	asha := s.signedIn("asha@college.edu", "Asha")
	first := s.importProblem(owner, "add-first")
	second := s.importProblem(owner, "add-second")
	loose := s.importProblem(owner, "add-loose")
	if _, err := s.db.Exec("UPDATE problems SET validated_hash = NULL WHERE id = ?", loose); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "add-route", start, map[string]int64{"A": first}, nil)
	add := fmt.Sprintf("/api/admin/contests/%d/problems/add", id)
	code, out := owner.do("POST", add, map[string]any{"problem_ids": []int64{second, loose, first}}, s.srv.URL)
	if code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	added, _ := out["added"].([]any)
	refused, _ := out["refused"].([]any)
	if len(added) != 1 || added[0].(map[string]any)["label"] != "B" || len(refused) != 2 {
		t.Fatalf("added %v refused %v", added, refused)
	}
	if r := refused[0].(map[string]any); r["reason"] != "not_validated" {
		t.Errorf("first refusal %v", r)
	}
	if r := refused[1].(map[string]any); r["reason"] != "already_added" {
		t.Errorf("second refusal %v", r)
	}
	put := fmt.Sprintf("/api/admin/contests/%d/problems", id)
	stale := map[string]any{"problems": []map[string]any{{"problem_id": first, "label": "A"}}, "loaded_problem_ids": []int64{first}}
	code, out = owner.do("PUT", put, stale, s.srv.URL)
	e, _ := out["error"].(map[string]any)
	if code != 409 || e["code"] != "problems_changed" ||
		e["message"] != "Problems were added to this contest after you opened it. Reload to see them, then make your change again." {
		t.Fatalf("stale save: %d %v", code, out)
	}
	fresh := map[string]any{"problems": []map[string]any{{"problem_id": first, "label": "A"}, {"problem_id": second, "label": "B"}},
		"loaded_problem_ids": []int64{first, second}}
	if code, out := owner.do("PUT", put, fresh, s.srv.URL); code != 200 {
		t.Fatalf("save after reload: %d %v", code, out)
	}
	if code, out := owner.do("PUT", put, []map[string]any{{"problem_id": first, "label": "A"}}, s.srv.URL); code != 200 {
		t.Fatalf("bare array save: %d %v", code, out)
	}
	for _, body := range []map[string]any{{}, {"loaded_problem_ids": []int64{}}} {
		if code, out := owner.do("PUT", put, body, s.srv.URL); code != 400 {
			t.Errorf("object without problems %v: %d %v", body, code, out)
		}
	}
	if code, out := owner.do("PUT", put, map[string]any{"problems": []map[string]any{}}, s.srv.URL); code != 200 {
		t.Errorf("explicit empty list: %d %v", code, out)
	}
	if code, out := owner.do("PUT", put, []map[string]any{{"problem_id": first, "label": "A"}}, s.srv.URL); code != 200 {
		t.Fatalf("restore: %d %v", code, out)
	}
	tooMany := make([]int64, 27)
	if code, out := owner.do("POST", add, map[string]any{"problem_ids": tooMany}, s.srv.URL); code != 400 || fmt.Sprint(out["error"]) == "" {
		t.Errorf("27 ids: %d %v", code, out)
	} else if e, _ := out["error"].(map[string]any); e["code"] != "too_many_problems" {
		t.Errorf("27 ids code: %v", out)
	}
	if code, _ := asha.do("POST", add, map[string]any{"problem_ids": []int64{first}}, s.srv.URL); code != 403 {
		t.Errorf("student: %d, want 403", code)
	}
	if code, _ := outsider.do("POST", add, map[string]any{"problem_ids": []int64{first}}, s.srv.URL); code != 404 {
		t.Errorf("non-manager organiser: %d, want 404", code)
	}
}
