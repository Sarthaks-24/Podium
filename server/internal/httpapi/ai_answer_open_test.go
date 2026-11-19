package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func answerFile(answer, kind, lists string) string {
	b, _ := json.Marshal(map[string]any{"format": "podium-ai-answer", "version": 1, "kind": kind, "java_lists": lists, "answer": answer})
	return string(b)
}

// A saved file imports and validates on a second database, and writes nothing.
func TestOpenAnswerFileOnASecondDatabase(t *testing.T) {
	first := newSiteMode(t, "testing")
	org := first.testSignIn("ta@college.edu", "organiser")
	if code, _ := org.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, first.srv.URL); code != 200 {
		t.Fatal("key")
	}
	_, ans := reviewRun(t, first, org, map[string]any{"mode": "paste", "text": "Sum an array.", "students_write": "function", "java_lists": "array"})
	file := answerFile(ans["text"].(string), "function", "array")

	second := newSiteMode(t, "testing")
	admin := second.testSignIn("boss@college.edu", "admin")
	before := bankCount(t, admin)
	code, out := admin.do("POST", "/api/admin/ai/answers/open", map[string]any{"text": file}, second.srv.URL)
	if code != 200 || out["answer"] != ans["text"] || out["kind"] != "function" || out["java_lists"] != "array" {
		t.Fatalf("open: %d %v", code, out)
	}
	if r, _ := out["reasons"].([]any); len(r) != 0 {
		t.Errorf("reasons %v", r)
	}
	if bankCount(t, admin) != before {
		t.Error("open wrote a problem")
	}
	// The same answer, opened as a bare paste after choosing its kind.
	code, out = admin.do("POST", "/api/admin/ai/answers/open", map[string]any{"text": ans["text"], "kind": "function", "java_lists": "array"}, second.srv.URL)
	if code != 200 || out["answer"] != ans["text"] {
		t.Errorf("bare: %d %v", code, out)
	}
	// A bare answer without a kind is refused.
	if code, _ = admin.do("POST", "/api/admin/ai/answers/open", map[string]any{"text": ans["text"]}, second.srv.URL); code != 400 {
		t.Errorf("no kind: %d", code)
	}
	// The saved file holds no key or account data.
	for _, bad := range []string{"ta@college.edu", "ignored", "sk-"} {
		if strings.Contains(file, bad) {
			t.Errorf("file holds %q", bad)
		}
	}
}

func TestOpenAnswerRefusals(t *testing.T) {
	s := newSiteMode(t, "testing")
	admin := s.testSignIn("boss@college.edu", "admin")
	open := func(text string) (int, map[string]any) {
		return admin.do("POST", "/api/admin/ai/answers/open", map[string]any{"text": text, "kind": "program"}, s.srv.URL)
	}
	cases := map[string]string{
		"unknown field": `{"format":"podium-ai-answer","version":1,"kind":"program","java_lists":"","answer":"{}","extra":1}`,
		"wrong version": `{"format":"podium-ai-answer","version":2,"kind":"program","java_lists":"","answer":"{}"}`,
		"wrong format":  `{"format":"other","version":1,"kind":"program","java_lists":"","answer":"{}"}`,
	}
	for name, text := range cases {
		if code, out := open(text); code != 400 || errCode(out) != "invalid_answer_file" {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if code, out := open(answerFile(strings.Repeat("a", 2<<20+1), "program", "")); code != 413 || errCode(out) != "answer_too_large" {
		t.Errorf("oversize file: %d %v", code, out)
	}
	if code, out := open(strings.Repeat("a", 2<<20+1)); code != 413 || errCode(out) != "answer_too_large" {
		t.Errorf("oversize paste: %d %v", code, out)
	}
	if code, out := admin.do("POST", "/api/admin/ai/answers/open", map[string]any{"text": strings.Repeat("a", 4<<20+1), "kind": "program"}, s.srv.URL); code != 413 {
		t.Errorf("oversize body: %d %v", code, out)
	}
	// A key-shaped string is refused without being echoed.
	code, out := open(`{"x":"sk-` + `ant-api03-abcdefghijklmnop12345"}`)
	if code != 400 || strings.Contains(fmt.Sprint(out), "abcdefghij") {
		t.Errorf("key shape: %d %v", code, out)
	}
	// Text that does not parse still opens, with its reasons.
	code, out = open("{not json")
	if r, _ := out["reasons"].([]any); code != 200 || len(r) == 0 || out["answer"] != "{not json" {
		t.Errorf("bad json: %d %v", code, out)
	}
	for _, role := range []string{"student", "organiser"} {
		b := s.testSignIn(role+"@college.edu", role)
		if code, _ := b.do("POST", "/api/admin/ai/answers/open", map[string]any{"text": "{}", "kind": "program"}, s.srv.URL); code != 403 {
			t.Errorf("%s: %d", role, code)
		}
	}
}
