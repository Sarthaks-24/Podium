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
	c, err := s.hub.SubscribeTagged(student.userID(), "")
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

// The Dashboard's Live tab (v4 O5): admins and owners only, and the five
// lines it shows. The test site has no database path or backup folder, so
// those read as unknown and "no backup yet".
func TestLiveForAdmins(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	if code, _ := owner.do("POST", "/api/admin/people", map[string]string{"email": "org@college.edu", "role": "organiser"}, s.srv.URL); code != 200 {
		t.Fatal("make organiser")
	}
	organiser := s.signedIn("org@college.edu", "Org")
	if code, _ := organiser.do("GET", "/api/admin/live", nil, ""); code != 403 {
		t.Errorf("organiser: %d, want 403", code)
	}
	student := s.signedIn("asha@college.edu", "Asha")
	c, err := s.hub.SubscribeTagged(student.userID(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.hub.Unsubscribe(c)
	pid := s.importProblem(owner, "live-sum")
	if code, out := owner.do("POST", fmt.Sprintf("/api/workspace/%d/submit", pid),
		map[string]any{"language": "cpp17", "source": "int main(){}"}, s.srv.URL); code != 202 {
		t.Fatalf("submit: %d %v", code, out)
	}
	code, out := owner.do("GET", "/api/admin/live", nil, "")
	if code != 200 {
		t.Fatalf("owner: %d %v", code, out)
	}
	if out["signed_in_now"].(float64) != 1 || out["queued"].(float64) != 1 || out["disk_free_mb"].(float64) != -1 || out["last_backup_at"] != nil {
		t.Errorf("live %v", out)
	}
	if out["judge_last_seen_s"] != nil {
		t.Errorf("no judge has asked for work, yet judge_last_seen_s is %v", out["judge_last_seen_s"])
	}
	s.judgeAll()
	if _, out = owner.do("GET", "/api/admin/live", nil, ""); out["judge_last_seen_s"] == nil || out["queued"].(float64) != 0 {
		t.Errorf("after judging: %v", out)
	}
}
