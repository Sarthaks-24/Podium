package httpapi

// Exam options (V3-25): the four switches are off by default, saved one at
// a time, sent to students with the rules, and a full-screen exit is an
// event and a count that organisers see in the feed, the timeline and the
// similar-code counts.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var examOptions = []string{"full_screen", "count_focus", "block_paste", "block_copy"}

func TestExamOptionsOffByDefaultAndSavedAlone(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "exam", start, map[string]int64{"A": pa}, nil)
	path := fmt.Sprintf("/api/admin/contests/%d", id)

	_, c := owner.do("GET", path, nil, "")
	for _, k := range examOptions {
		if v, ok := c[k]; !ok || v != false {
			t.Errorf("new contest %s = %v (present %v), want false", k, v, ok)
		}
	}
	asha := s.signedIn("asha@college.edu", "Asha")
	_, sc := asha.do("GET", "/api/contests/exam", nil, "")
	rules := sc["rules"].(map[string]any)
	for _, k := range examOptions {
		if rules[k] != false {
			t.Errorf("student rules %s = %v, want false", k, rules[k])
		}
	}

	// One switch at a time: the others keep their value.
	if code, out := owner.do("PUT", path, map[string]any{"block_paste": true}, s.srv.URL); code != 200 || out["block_paste"] != true || out["full_screen"] != false {
		t.Fatalf("block paste: %d %v", code, out)
	}
	if code, out := owner.do("PUT", path, map[string]any{"full_screen": true}, s.srv.URL); code != 200 || out["block_paste"] != true || out["full_screen"] != true {
		t.Fatalf("full screen: %d %v", code, out)
	}
	_, sc = asha.do("GET", "/api/contests/exam", nil, "")
	rules = sc["rules"].(map[string]any)
	if rules["block_paste"] != true || rules["full_screen"] != true || rules["count_focus"] != false || rules["block_copy"] != false {
		t.Errorf("student rules after the switches: %v", rules)
	}

	// The audit log names each switch that changed.
	_, audit := owner.do("GET", path+"/audit?action=settings", nil, "")
	var changed []string
	for _, e := range audit["entries"].([]any) {
		for k := range e.(map[string]any)["details"].(map[string]any)["changes"].(map[string]any) {
			changed = append(changed, k)
		}
	}
	if got := strings.Join(changed, " "); !strings.Contains(got, "block_paste") || !strings.Contains(got, "full_screen") {
		t.Errorf("audit changes: %v", changed)
	}

	// Duplicate keeps them.
	code, dup := owner.do("POST", path+"/clone", map[string]any{"title": "exam copy", "starts_at": start.Add(48 * time.Hour).UnixMilli(),
		"ends_at": start.Add(50 * time.Hour).UnixMilli()}, s.srv.URL)
	if code != 201 || dup["block_paste"] != true || dup["full_screen"] != true {
		t.Errorf("duplicate: %d %v", code, dup)
	}
}

func TestFullScreenExitIsCountedForOrganisers(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "exam", start, map[string]int64{"A": pa}, map[string]any{"full_screen": true, "count_focus": true})
	asha := s.signedIn("asha@college.edu", "Asha Rao")
	s.at(start, 5)
	if code, out := asha.do("POST", "/api/contests/exam/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	events := []map[string]any{{"kind": "fullscreen_exit"}, {"kind": "blur"}, {"kind": "fullscreen_exit"}, {"kind": "paste", "size": 12}}
	if code, out := asha.do("POST", "/api/contests/exam/problems/A/events", map[string]any{"events": events}, s.srv.URL); code != 204 {
		t.Fatalf("events: %d %v", code, out)
	}
	if code, out := asha.do("POST", "/api/contests/exam/problems/A/events", map[string]any{"events": []map[string]any{{"kind": "fullscreen"}}}, s.srv.URL); code != 400 {
		t.Errorf("unknown kind: %d %v", code, out)
	}
	code, out := asha.do("POST", "/api/contests/exam/problems/A/submit", map[string]any{"language": "cpp17", "source": "correct",
		"client_key": "k1", "typed_chars": 30, "pasted_chars": 12, "focus_losses": 1, "fullscreen_exits": 2}, s.srv.URL)
	if code != 202 {
		t.Fatalf("submit: %d %v", code, out)
	}

	// The feed carries the exits beside the paste and focus counts.
	_, feed := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/submissions", id), nil, "")
	row := feed["submissions"].([]any)[0].(map[string]any)
	if row["fullscreen_exits"] != float64(2) || row["focus_losses"] != float64(1) || row["pasted_chars"] != float64(12) {
		t.Errorf("feed row: %v", row)
	}

	// The similar-code page's counts come from the events.
	code, counts := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/counts", id), nil, "")
	list, _ := counts["counts"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("counts: %d %v", code, counts)
	}
	got := list[0].(map[string]any)
	if got["name"] != "Asha Rao" || got["fullscreen_exits"] != float64(2) || got["focus_losses"] != float64(1) ||
		got["pastes"] != float64(1) || got["pasted_chars"] != float64(12) {
		t.Errorf("counts row: %v", got)
	}
	if code, _ := asha.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/counts", id), nil, ""); code != 403 {
		t.Errorf("student reading the counts: %d, want 403", code)
	}

	// The timeline says it in words.
	_, tl := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/integrity/timeline/%d", id, asha.userID()), nil, "")
	var texts []string
	for _, m := range tl["moments"].([]any) {
		texts = append(texts, m.(map[string]any)["text"].(string))
	}
	joined := strings.Join(texts, "\n")
	if strings.Count(joined, "Left full screen.") != 2 || !strings.Contains(joined, "Left full screen 2 times.") {
		t.Errorf("timeline: %s", joined)
	}
}
