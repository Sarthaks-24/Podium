package httpapi

// The student routes that save a draft and run code inside a contest: the
// contest-wide coverage run found them mostly untested (stage 0 of v2.1.0).

import (
	"testing"
	"time"
)

func TestContestDraftAndRun(t *testing.T) {
	s := newSite(t)
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.at(start, -60)
	owner := s.signedIn("owner@college.edu", "Owner")
	a := s.importProblem(owner, "plus-a")
	s.createContest(owner, "drafts", start, map[string]int64{"A": a}, nil)
	stu := s.signedIn("s01@college.edu", "Student 01")
	draft := map[string]string{"language": "cpp17", "source": "int main(){}"}

	// Before the contest opens the problem is hidden, so the draft route refuses.
	if code, _ := stu.do("PUT", "/api/contests/drafts/problems/A/draft", draft, s.srv.URL); code != 403 {
		t.Errorf("draft before the start: %d, want 403", code)
	}
	if code, _ := stu.do("POST", "/api/contests/drafts/problems/A/run", map[string]any{"language": "cpp17", "source": "int main(){}"}, s.srv.URL); code != 403 {
		t.Errorf("run before the start: %d, want 403", code)
	}

	s.at(start, 1)
	if code, out := stu.do("POST", "/api/contests/drafts/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	if code, out := stu.do("PUT", "/api/contests/drafts/problems/A/draft", draft, s.srv.URL); code != 204 {
		t.Fatalf("draft: %d %v", code, out)
	}
	var source string
	if err := s.db.QueryRow("SELECT source FROM drafts WHERE problem_id = ? AND contest_id <> 0", a).Scan(&source); err != nil || source != draft["source"] {
		t.Errorf("stored draft %q, err %v", source, err)
	}
	if code, _ := stu.do("PUT", "/api/contests/drafts/problems/A/draft", map[string]string{"language": "cobol", "source": "x"}, s.srv.URL); code != 400 {
		t.Errorf("draft in a language the contest does not offer: %d, want 400", code)
	}
	if code, _ := stu.do("PUT", "/api/contests/drafts/problems/Z/draft", draft, s.srv.URL); code != 404 {
		t.Errorf("draft for a missing problem: %d, want 404", code)
	}
	if code, out := stu.do("POST", "/api/contests/drafts/problems/A/run", map[string]any{"language": "cpp17", "source": "int main(){}", "input": "1 2"}, s.srv.URL); code != 202 {
		t.Errorf("run: %d %v, want 202", code, out)
	}
	if code, _ := stu.do("POST", "/api/contests/drafts/problems/A/run", map[string]any{"language": "cobol", "source": "x"}, s.srv.URL); code != 400 {
		t.Errorf("run in an unoffered language: %d, want 400", code)
	}

	// After the end a run is refused.
	s.at(start, 125)
	if code, _ := stu.do("POST", "/api/contests/drafts/problems/A/run", map[string]any{"language": "cpp17", "source": "int main(){}"}, s.srv.URL); code == 202 {
		t.Error("run after the contest ended was accepted")
	}
}
