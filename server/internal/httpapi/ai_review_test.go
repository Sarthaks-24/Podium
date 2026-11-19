package httpapi

// Review before saving (v2.1.1 stage C) over HTTP: a run with review creates
// nothing, an unchanged answer makes the same draft as a run without review,
// Create draft works without the run, a failed answer can be corrected, and a
// draft from the route is an AI draft a contest refuses until a person saves.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func bankCount(t *testing.T, b *browser) int {
	t.Helper()
	code, out := b.do("GET", "/api/admin/problems", nil, "")
	if code != 200 {
		t.Fatalf("bank: %d %v", code, out)
	}
	return len(out["problems"].([]any))
}

// reviewRun starts a run with review and returns the answer route's JSON.
func reviewRun(t *testing.T, s *site, b *browser, body map[string]any) (string, map[string]any) {
	t.Helper()
	body["review"] = true
	code, started := startWizardWhenFree(t, b, s.srv.URL, body)
	if code != 202 {
		t.Fatalf("start: %d %v", code, started)
	}
	id := started["wizard_id"].(string)
	w := pollWizard(t, b, id)
	if w["state"] != "review" || w["problem_id"] != nil {
		t.Fatalf("review run: %v", w)
	}
	code, ans := b.do("GET", "/api/admin/ai/wizard/"+id+"/answer", nil, "")
	if code != 200 {
		t.Fatalf("answer: %d %v", code, ans)
	}
	return id, ans
}

func TestReviewCreatesNothingUntilCreateDraft(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	if code, _ := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
		t.Fatal("key")
	}
	before := bankCount(t, org)

	// A run without review, for the comparison.
	code, started := startWizardWhenFree(t, org, s.srv.URL, map[string]any{"mode": "paste", "text": "Read t pairs and print each sum."})
	if code != 202 {
		t.Fatalf("plain run: %d", code)
	}
	plain := pollWizard(t, org, started["wizard_id"].(string))
	_, plainDraft := org.do("GET", "/api/admin/problems/"+idOf(plain["problem_id"]), nil, "")
	afterPlain := bankCount(t, org)

	id, ans := reviewRun(t, s, org, map[string]any{"mode": "paste", "text": "Read t pairs and print each sum."})
	if bankCount(t, org) != afterPlain || afterPlain != before+1 {
		t.Errorf("a review run wrote a problem: %d, %d, %d", before, afterPlain, bankCount(t, org))
	}
	if ans["kind"] != "program" || ans["text"] == "" || ans["repairs"] == nil {
		t.Fatalf("answer %v", ans)
	}

	// Create draft with the answer unchanged, after the run is gone.
	if code, _ := org.do("GET", "/api/admin/ai/wizard/nope/answer", nil, ""); code != 404 {
		t.Errorf("missing run: %d", code)
	}
	code, made := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": ans["text"], "kind": "program", "java_lists": ""}, s.srv.URL)
	if code != 201 || made["problem_id"] == nil {
		t.Fatalf("create draft: %d %v", code, made)
	}
	_, draft := org.do("GET", "/api/admin/problems/"+idOf(made["problem_id"]), nil, "")
	if draft["origin"] != "ai" || !strings.Contains(fmt.Sprint(draft["tags"]), "ai draft") {
		t.Errorf("draft origin %v tags %v", draft["origin"], draft["tags"])
	}
	for _, k := range []string{"title", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "samples", "subtasks", "plan", "generator", "validator", "reference", "brute", "difficulty"} {
		a, _ := json.Marshal(plainDraft[k])
		b, _ := json.Marshal(draft[k])
		if string(a) != string(b) {
			t.Errorf("field %s differs between a run without review and Create draft", k)
		}
	}
	_ = id
}

func TestReviewFunctionAnswerCanBeCorrectedAndCreated(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	if code, _ := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
		t.Fatal("key")
	}
	_, ans := reviewRun(t, s, org, map[string]any{"mode": "paste", "text": "Sum an array.", "students_write": "function", "java_lists": "array"})
	if ans["kind"] != "function" || ans["java_lists"] != "array" {
		t.Fatalf("answer %v", ans)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(ans["text"].(string)), &m)

	// A wrong field is refused with the field named, then corrected.
	m["subtasks"].([]any)[0].(map[string]any)["points"] = 7
	bad, _ := json.Marshal(m)
	code, out := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": string(bad), "kind": "function", "java_lists": "array"}, s.srv.URL)
	if code != 400 || errCode(out) != "invalid_answer" || !strings.Contains(fmt.Sprint(out), "add up to") {
		t.Fatalf("bad answer: %d %v", code, out)
	}
	if code, _ := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": "{not json", "kind": "function", "java_lists": "array"}, s.srv.URL); code != 400 {
		t.Errorf("invalid JSON: %d", code)
	}
	if code, out := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": `{"x":"sk-` + `ant-api03-abcdefghijklmnop12345"}`, "kind": "function"}, s.srv.URL); code != 400 || strings.Contains(fmt.Sprint(out), "abcdefghij") {
		t.Errorf("key-shaped answer: %d %v", code, out)
	}
	fixed := []byte(ans["text"].(string)) // the correction: the points back as the model had them
	code, made := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": string(fixed), "kind": "function", "java_lists": "array"}, s.srv.URL)
	if code != 201 {
		t.Fatalf("corrected answer: %d %v", code, made)
	}
	_, draft := org.do("GET", "/api/admin/problems/"+idOf(made["problem_id"]), nil, "")
	if draft["mode"] != "function" {
		t.Errorf("mode %v", draft["mode"])
	}

	// Roles, and the slot.
	student := s.testSignIn("asha@college.edu", "student")
	if code, _ := student.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": "{}", "kind": "function"}, s.srv.URL); code != 403 {
		t.Errorf("student: %d", code)
	}
	release, _ := s.ai.Slots.Take(org.userID())
	if code, out := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": string(fixed), "kind": "function"}, s.srv.URL); code != 429 {
		t.Errorf("slot taken: %d %v", code, out)
	}
	release()
}

func TestDraftFromTheRouteIsRefusedByAContestUntilSaved(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	if code, _ := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL); code != 200 {
		t.Fatal("key")
	}
	_, ans := reviewRun(t, s, org, map[string]any{"mode": "paste", "text": "Read t pairs and print each sum."})
	_, made := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": ans["text"], "kind": "program"}, s.srv.URL)
	draftID, _ := strconv.ParseInt(idOf(made["problem_id"]), 10, 64)
	s.validate(draftID)
	solid := s.importProblem(org, "pairs")
	cid := s.createContest(org, "week-1", time.Now().Add(time.Hour), map[string]int64{"A": solid}, nil)
	choices := []map[string]any{{"problem_id": solid, "label": "A"}, {"problem_id": draftID, "label": "B"}}
	code, body := org.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL)
	if code != 400 || !strings.Contains(fmt.Sprint(body), "AI draft") {
		t.Errorf("AI draft from the route added to a contest: %d %v", code, body)
	}
}

func TestDraftsRouteRefusesAnOversizedBody(t *testing.T) {
	s := newSiteMode(t, "testing")
	org := s.testSignIn("ta@college.edu", "organiser")
	code, out := org.do("POST", "/api/admin/ai/drafts", map[string]any{"answer": strings.Repeat("a", 2<<20+1), "kind": "function"}, s.srv.URL)
	if code != 413 || errCode(out) != "answer_too_large" {
		t.Errorf("oversized answer: %d %v", code, out)
	}
}
