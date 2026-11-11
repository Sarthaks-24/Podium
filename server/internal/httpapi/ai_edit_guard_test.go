package httpapi

// AI edit, stage A of v2.1.0: a reply that leaves out the generator is kept,
// one repair retry for the parser's refusals, no retry for refusals of
// policy, the mode refusal, what the AI sent back, and the log line of a
// refusal. Everything runs over HTTP with the fake provider.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/ai"
	"github.com/Sarthaks-24/podium/internal/ai/fakeai"
)

const copyEditRefused = "The AI's edit would break this problem, so nothing was saved. Press Ask for a draft to try again, describe a smaller change, or rewrite the whole problem."
const copyEditModeChange = "Edit with AI cannot change whether students write a whole program or one function. Use Rewrite with AI and choose there."

// replyFile is the file of a request, or "".
func replyFile(req ai.EditRequest, name string) string {
	for _, f := range req.Files {
		if f.Path == name {
			return f.Content
		}
	}
	return ""
}

func problemYAML(content string) ai.EditAnswer {
	return ai.EditAnswer{Files: []ai.EditFile{{Path: "problem.yaml", Content: content}}, Summary: "Changed problem.yaml."}
}

// hook installs an edit hook that answers with each of answers in turn (the
// last one repeats), and returns the requests it saw.
func hook(answers ...any) *[]ai.EditRequest {
	var seen []ai.EditRequest
	fakeai.EditHook = func(req ai.EditRequest) (*ai.EditAnswer, error) {
		seen = append(seen, req)
		a := answers[min(len(seen), len(answers))-1]
		switch v := a.(type) {
		case ai.EditAnswer:
			return &v, nil
		case error:
			return nil, v
		}
		panic("bad answer")
	}
	return &seen
}

func TestAIEditKeepsTheGeneratorAndSaysSo(t *testing.T) {
	s, org, _ := editSite(t)
	id := importPlanned(t, s, org)
	fakeai.EditHook = func(req ai.EditRequest) (*ai.EditAnswer, error) {
		text := strings.Replace(replyFile(req, "problem.yaml"), "generator: generator.py\n", "", 1)
		text = strings.Replace(text, "title: Largest gap", "title: Largest gap, renamed", 1)
		a := problemYAML(text)
		return &a, nil
	}
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "Rename it."}, s.srv.URL)
	if code != 200 || fmt.Sprint(out["kept"]) != "[the test generator]" {
		t.Fatalf("draft: %d %v", code, out)
	}
	files := out["files"].([]any)
	after := files[0].(map[string]any)["after"].(string)
	if len(files) != 1 || !strings.Contains(after, "generator: generator.py") || !strings.Contains(after, "renamed") {
		t.Errorf("the draft lost the generator: %v", files)
	}
	// Accept saves it as it is shown, and the plan still runs.
	if code, acc := org.do("POST", editPath(id, "/"+out["draft_id"].(string)+"/accept"), nil, s.srv.URL); code != 200 {
		t.Fatalf("accept: %d %v", code, acc)
	}
	_, p := org.do("GET", fmt.Sprintf("/api/admin/problems/%d", id), nil, "")
	if p["generator"] == nil {
		t.Errorf("the saved problem has no generator")
	}
}

func TestAIEditWithoutKeptKeysSaysNothingKept(t *testing.T) {
	s, org, id := editSite(t)
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "Add a note."}, s.srv.URL)
	kept, ok := out["kept"].([]any)
	if code != 200 || !ok || len(kept) != 0 {
		t.Errorf("kept: %d %v", code, out["kept"])
	}
}

// A parser refusal is retried once, with the previous answer and the reasons.
func TestAIEditRetriesAParserRefusalOnce(t *testing.T) {
	s, org, id := editSite(t)
	seen := hook(problemYAML("title: [\n"), ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md", Content: "Fixed.\n"}}, Summary: "Fixed."})
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL)
	if code != 200 || out["summary"] != "Fixed." || len(*seen) != 2 {
		t.Fatalf("draft: %d %v after %d calls", code, out, len(*seen))
	}
	first, second := (*seen)[0], (*seen)[1]
	if first.Previous != "" || len(first.Fix) != 0 || first.Shorter {
		t.Errorf("the first call carries a retry: %+v", first)
	}
	if len(second.Fix) == 0 || !strings.Contains(strings.Join(second.Fix, " "), "problem.yaml") || second.System != first.System || second.Instruction != first.Instruction {
		t.Errorf("second call: fix %v, same system %t", second.Fix, second.System == first.System)
	}
}

func TestAIEditTwoParserRefusalsEndInOneRefusalWithBothAnswers(t *testing.T) {
	s, org, id := editSite(t)
	first := ai.EditAnswer{Files: []ai.EditFile{{Path: "problem.yaml", Content: "title: [\n"}}, Summary: "First try."}
	second := ai.EditAnswer{Files: []ai.EditFile{{Path: "problem.yaml", Content: "title: {\n"}}, Summary: "Second try."}
	seen := hook(first, second)
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL)
	errIs(t, "two refusals", code, out, 422, "invalid_answer", copyEditRefused)
	if len(*seen) != 2 {
		t.Errorf("%d calls, want 2", len(*seen))
	}
	e := out["error"].(map[string]any)
	if len(e["problems"].([]any)) == 0 {
		t.Error("no reasons")
	}
	answers := e["sent_back"].(map[string]any)["answers"].([]any)
	if len(answers) != 2 || answers[0].(map[string]any)["summary"] != "Second try." || answers[1].(map[string]any)["summary"] != "First try." {
		t.Fatalf("answers (newest first): %v", answers)
	}
	f := answers[0].(map[string]any)["files"].([]any)[0].(map[string]any)
	if f["path"] != "problem.yaml" || f["content"] != "title: {\n" {
		t.Errorf("file: %v", f)
	}
	if revisions(t, s, id) != 1 {
		t.Error("a refusal made a revision")
	}
}

// An answer that is not JSON in the schema, and a cut-off one, get the retry.
func TestAIEditRetriesAnAnswerThatIsNotJSONAndACutOff(t *testing.T) {
	good := ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md", Content: "Fixed.\n"}}, Summary: "Fixed."}
	s, org, id := editSite(t)
	seen := hook(&ai.Error{Code: ai.CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: []string{"The answer was not in the format Podium asked for: x"}}, good)
	if code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL); code != 200 || len(*seen) != 2 || len((*seen)[1].Fix) != 1 {
		t.Fatalf("not JSON: %d %v after %d calls", code, out, len(*seen))
	}
	seen = hook(&ai.Error{Code: ai.CodeCutOff, Message: "The answer was cut off before it was complete."}, good)
	if code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL); code != 200 || len(*seen) != 2 || !(*seen)[1].Shorter || (*seen)[1].Previous != "" {
		t.Fatalf("cut off: %d %v after %d calls", code, out, len(*seen))
	}
	// Cut off twice: the provider's own sentence, and no disclosure to show.
	seen = hook(&ai.Error{Code: ai.CodeCutOff, Message: "The answer was cut off before it was complete."})
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL)
	errIs(t, "cut off twice", code, out, 502, "cut_off", "The answer was cut off before it was complete.")
	if len(*seen) != 2 {
		t.Errorf("%d calls, want 2", len(*seen))
	}
}

// Refusals of policy are never retried.
func TestAIEditDoesNotRetryAPolicyRefusalOrAProviderFailure(t *testing.T) {
	s, org, id := editSite(t)
	for name, answer := range map[string]any{
		"test data":   ai.EditAnswer{Files: []ai.EditFile{{Path: "tests/01.in", Content: "1\n"}}},
		"not a pack":  ai.EditAnswer{Files: []ai.EditFile{{Path: "notes.txt", Content: "x"}}},
		"slug":        ai.EditAnswer{Files: []ai.EditFile{{Path: "problem.yaml", Content: "slug: other\n"}}},
		"unsupported": ai.EditAnswer{Unsupported: "Contest settings are not in a pack."},
		"no files":    ai.EditAnswer{},
		"refused":     &ai.Error{Code: ai.CodeRefused, Message: "The model declined this request. Rephrase it and try again."},
		"unchanged":   ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md", Content: "same"}}},
	} {
		seen := hook(answer)
		if name == "unchanged" {
			fakeai.EditHook = func(req ai.EditRequest) (*ai.EditAnswer, error) {
				*seen = append(*seen, req)
				return &ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md", Content: replyFile(req, "statement.md")}}}, nil
			}
		}
		code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL)
		if code < 400 || len(*seen) != 1 {
			t.Errorf("%s: %d %v after %d calls", name, code, out, len(*seen))
		}
	}
	// A policy refusal carries the same first sentence and lists its reasons.
	hook(ai.EditAnswer{Files: []ai.EditFile{{Path: "tests/01.in", Content: "1\n"}}})
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL)
	errIs(t, "test data", code, out, 422, "invalid_answer", copyEditRefused)
	if !strings.Contains(fmt.Sprint(out), "cannot change test data") {
		t.Errorf("reasons: %v", out)
	}
}

func TestAIEditRefusesAModeChangeWithoutRetry(t *testing.T) {
	s, org, id := editSite(t)
	seen := hook()
	fakeai.EditHook = func(req ai.EditRequest) (*ai.EditAnswer, error) {
		*seen = append(*seen, req)
		a := problemYAML("mode: function\n" + replyFile(req, "problem.yaml"))
		return &a, nil
	}
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "Make it a function problem."}, s.srv.URL)
	errIs(t, "mode change", code, out, 422, "mode_change", copyEditModeChange)
	if len(*seen) != 1 || revisions(t, s, id) != 1 {
		t.Errorf("%d calls, %d revisions", len(*seen), revisions(t, s, id))
	}
}

// What the AI sent back is capped at 200 KB, leaves out test data, and never
// holds the system prompt or the key.
func TestAIEditSentBackIsCappedAndHoldsOnlyTheReply(t *testing.T) {
	s, org, id := editSite(t)
	big := ai.EditAnswer{Summary: "Big.", Files: []ai.EditFile{
		{Path: "problem.yaml", Content: "title: [\n" + strings.Repeat("a", 150<<10)},
		{Path: "notes.py", Content: strings.Repeat("b", 150<<10)},
		{Path: "tests/01.in", Content: "TESTDATA\n"},
	}}
	hook(big)
	code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL)
	errIs(t, "refused", code, out, 422, "invalid_answer", "")
	sent := out["error"].(map[string]any)["sent_back"].(map[string]any)["answers"].([]any)
	total, cut := 0, false
	for _, a := range sent {
		cut = cut || a.(map[string]any)["cut"] == true
		for _, f := range a.(map[string]any)["files"].([]any) {
			total += len(f.(map[string]any)["content"].(string))
		}
	}
	if total > 200<<10 || !cut {
		t.Errorf("%d bytes shown, cut %v", total, cut)
	}
	if strings.Contains(fmt.Sprint(out), "TESTDATA") || strings.Contains(fmt.Sprint(out), "You edit one competitive programming problem") {
		t.Error("test data or the system prompt reached the response")
	}
}

// A refusal logs who, which problem, which model, the reply's file names and
// the start of each reason, and nothing of the content.
func TestAIEditRefusalIsLoggedWithoutContent(t *testing.T) {
	s, org, id := editSite(t)
	hook(ai.EditAnswer{Files: []ai.EditFile{{Path: "problem.yaml", Content: "title: [\nSECRETBODY: " + strings.Repeat("x", 300)}}})
	if code, _ := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL); code != 422 {
		t.Fatalf("status %d", code)
	}
	logs := s.logs.String()
	var line string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, `"msg":"ai edit refused"`) {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no refusal line in the log:\n%s", logs)
	}
	for _, want := range []string{`"problem_id":`, `"user_id":`, `"provider":"fake"`, `"model":`, `"problem.yaml"`, `"reasons":`} {
		if !strings.Contains(line, want) {
			t.Errorf("the line lacks %s: %s", want, line)
		}
	}
	if strings.Contains(line, "SECRETBODY") || strings.Contains(line, strings.Repeat("x", 100)) {
		t.Errorf("content in the log line: %s", line)
	}
}

// The edit prompt names what a reply must keep. The copy-paste pack prompt is
// not changed (its golden file still passes in the problems package).
func TestAIEditPromptNamesTheGeneratorKeys(t *testing.T) {
	s, org, id := editSite(t)
	seen := hook(ai.EditAnswer{Files: []ai.EditFile{{Path: "statement.md", Content: "New.\n"}}})
	if code, out := org.do("POST", editPath(id, ""), map[string]any{"instruction": "x"}, s.srv.URL); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	sys := (*seen)[0].System
	for _, want := range []string{"generator_args", "tests.yaml", "keep every key", "never invent test data"} {
		if !strings.Contains(strings.ToLower(sys), strings.ToLower(want)) {
			t.Errorf("the edit prompt lacks %q", want)
		}
	}
}

// Every id spelling the other handlers refuse is refused here too.
func TestAIEditBadIDs(t *testing.T) {
	s, org, _ := editSite(t)
	for _, id := range []string{"0", "-3", "abc", "1.5", "99999999999999999999"} {
		code, out := org.do("POST", "/api/admin/problems/"+id+"/ai-edit", map[string]any{"instruction": "x"}, s.srv.URL)
		errIs(t, id, code, out, 404, "not_found", "There is no such problem.")
	}
}
