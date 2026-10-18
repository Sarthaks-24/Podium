package httpapi

import (
	"fmt"
	"testing"
)

func TestMetricsForOrganisersOnly(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	student := s.signedIn("asha@college.edu", "Asha")
	if code, _ := s.browser().do("GET", "/api/admin/metrics", nil, ""); code != 401 {
		t.Errorf("signed out: %d, want 401", code)
	}
	if code, _ := student.do("GET", "/api/admin/metrics", nil, ""); code != 403 {
		t.Errorf("student: %d, want 403", code)
	}

	pid := s.importProblem(owner, "metrics-sum")
	if code, out := owner.do("POST", fmt.Sprintf("/api/workspace/%d/submit", pid),
		map[string]any{"language": "cpp17", "source": "int main(){}"}, s.srv.URL); code != 202 {
		t.Fatalf("submit: %d %v", code, out)
	}
	c, err := s.hub.Subscribe(student.userID())
	if err != nil {
		t.Fatal(err)
	}
	defer s.hub.Unsubscribe(c)

	code, out := owner.do("GET", "/api/admin/metrics", nil, "")
	if code != 200 {
		t.Fatalf("owner: %d %v", code, out)
	}
	queue := out["queue"].(map[string]any)
	submit := queue["kinds"].(map[string]any)["submit"].(map[string]any)
	if submit["queued"].(float64) != 1 || queue["queued"].(float64) != 1 || queue["leased"].(float64) != 0 {
		t.Errorf("queue %v", queue)
	}
	if len(queue["leased_jobs"].([]any)) != 0 {
		t.Errorf("leased jobs %v", queue["leased_jobs"])
	}
	events := out["events"].(map[string]any)
	if events["connections"].(float64) != 1 || events["users"].(float64) != 1 {
		t.Errorf("events %v", events)
	}
	dbStats := out["db"].(map[string]any)
	if dbStats["max_open"].(float64) < 1 {
		t.Errorf("db %v", dbStats)
	}
	if out["process"].(map[string]any)["goroutines"].(float64) < 1 {
		t.Errorf("process %v", out["process"])
	}

	// One judged submission later the queue is empty and the work shows
	// in the recent figures.
	if n := s.judgeAll(); n != 1 {
		t.Fatalf("judged %d jobs, want 1", n)
	}
	_, out = owner.do("GET", "/api/admin/metrics", nil, "")
	queue = out["queue"].(map[string]any)
	submit = queue["kinds"].(map[string]any)["submit"].(map[string]any)
	if queue["queued"].(float64) != 0 || submit["finished_last_minute"].(float64) != 1 || queue["submits_judged_last_10_min"].(float64) != 1 {
		t.Errorf("after judging %v", queue)
	}
}
