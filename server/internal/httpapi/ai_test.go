package httpapi

// The AI wizard over HTTP (M5, cache/v1/m5-plan.md section 7): the key
// routes, a wizard run from a brief and from pasted text with the testing
// mode's fake provider, generate inputs, and a
// run with a real-looking key against a stand-in for the Claude API after
// which the key must not appear in any log line, the SQLite files or the
// blob store. Signing out forgets the key.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/ai"
	// Testing mode offers the fake provider only in builds that link it.
	_ "github.com/Sarthaks-24/podium/internal/ai/fakeai"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// testSignIn signs a browser in through the testing-mode form.
func (s *site) testSignIn(email string, role string) *browser {
	s.t.Helper()
	b := s.browser()
	if code, body := b.do("POST", "/api/auth/test-signin", map[string]string{"email": email, "role": role}, s.srv.URL); code != 200 {
		s.t.Fatalf("test sign-in %s: %d %v", email, code, body)
	}
	return b
}

// pollWizard reads a wizard run until it leaves "thinking".
func pollWizard(t *testing.T, b *browser, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		code, w := b.do("GET", "/api/admin/ai/wizard/"+id, nil, "")
		if code != 200 {
			t.Fatalf("wizard %s: %d %v", id, code, w)
		}
		if w["state"] != "thinking" {
			return w
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("wizard %s still thinking after 15 s", id)
	return nil
}

func idOf(v any) string { return strconv.Itoa(int(v.(float64))) }

func TestWizardWithFakeProvider(t *testing.T) {
	s := newSiteMode(t, "testing")
	s.logLevel.Set(slog.LevelDebug)
	admin := s.testSignIn("ta@college.edu", "organiser")
	other := s.testSignIn("ta2@college.edu", "organiser")
	student := s.testSignIn("asha@college.edu", "student")

	if code, _ := student.do("GET", "/api/admin/ai/key", nil, ""); code != 403 {
		t.Errorf("student reading the key form: %d", code)
	}
	code, st := admin.do("GET", "/api/admin/ai/key", nil, "")
	providers, _ := st["providers"].([]any)
	if code != 200 || st["active"] != false || st["active_until"] != nil || len(providers) != 4 || st["server_now"] == nil {
		t.Fatalf("key status: %d %v", code, st)
	}
	var ids []string
	for _, p := range providers {
		p := p.(map[string]any)
		ids = append(ids, p["id"].(string))
		if p["has_key"] != false || len(p["models"].([]any)) != 0 {
			t.Errorf("provider before any key: %v", p)
		}
	}
	if strings.Join(ids, ",") != "anthropic,openai,gemini,fake" {
		t.Errorf("providers %v", ids)
	}

	brief := map[string]any{"mode": "brief", "brief": map[string]any{"topic": "Maximum subarray sum", "difficulty": "medium",
		"constraints": "n up to 100000", "subtasks": "small n, then full"}}
	if code, body := admin.do("POST", "/api/admin/ai/wizard", brief, s.srv.URL); code != 409 || errCode(body) != "no_key" {
		t.Errorf("wizard without a key: %d %v", code, body)
	}
	code, st = admin.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake", "key": "ignored"}, s.srv.URL)
	if code != 200 || st["active"] != true || st["provider"] != "fake" || st["model"] != "canned" || st["active_until"] == nil {
		t.Fatalf("add fake key: %d %v", code, st)
	}
	if code, body := admin.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "brief", "brief": map[string]any{"topic": "x", "difficulty": "impossible"}}, s.srv.URL); code != 400 {
		t.Errorf("bad difficulty: %d %v", code, body)
	}
	if code, body := admin.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": strings.Repeat("a", ai.MaxPasted+1)}, s.srv.URL); code != 400 {
		t.Errorf("pasted text too long: %d %v", code, body)
	}

	// One call per admin at a time.
	release, _ := s.ai.Slots.Take(admin.userID())
	if code, body := admin.do("POST", "/api/admin/ai/wizard", brief, s.srv.URL); code != 429 || errCode(body) != "busy" || body["error"].(map[string]any)["retry_after_s"] != 10.0 {
		t.Errorf("second call while one runs: %d %v", code, body)
	}
	release()

	events, closeStream := openStream(t, admin, "test")
	defer closeStream()
	waitFor(t, events, "clock")
	code, started := admin.do("POST", "/api/admin/ai/wizard", brief, s.srv.URL)
	if code != 202 || started["wizard_id"] == nil || started["server_now"] == nil {
		t.Fatalf("start wizard: %d %v", code, started)
	}
	wid := started["wizard_id"].(string)
	w := pollWizard(t, admin, wid)
	if w["state"] != "draft_created" || w["problem_id"] == nil || w["kind"] != "generate" || w["provider"] != "fake" || w["model"] != "canned" {
		t.Fatalf("wizard: %v", w)
	}
	ev := waitFor(t, events, "wizard")
	if !strings.Contains(ev.data, wid) || !strings.Contains(ev.data, "draft_created") {
		t.Errorf("wizard event %s", ev.data)
	}
	if code, _ := other.do("GET", "/api/admin/ai/wizard/"+wid, nil, ""); code != 404 {
		t.Errorf("another admin reading the wizard run: %d", code)
	}

	pid := idOf(w["problem_id"])
	code, p := admin.do("GET", "/api/admin/problems/"+pid, nil, "")
	if code != 200 || p["origin"] != "ai" || p["slug"] != "best-stretch" || p["generator"] == nil {
		t.Fatalf("draft: %d %v", code, p)
	}
	if tags := fmt.Sprint(p["tags"]); !strings.Contains(tags, "ai draft") {
		t.Errorf("tags %s", tags)
	}
	plan, _ := p["plan"].([]any)
	subtasks, _ := p["subtasks"].([]any)
	if len(plan) == 0 || len(subtasks) == 0 || len(subtasks[0].(map[string]any)["generator_args"].([]any)) == 0 {
		t.Fatalf("plan %v subtasks %v", plan, subtasks)
	}
	first := plan[0].(map[string]any)
	if first["idx"] == nil || first["subtask"] == nil || first["seed"] == nil || first["args"] == nil {
		t.Errorf("plan entry %v", first)
	}

	// Input generation was queued with the draft; asking again while it
	// waits is refused.
	code, g := admin.do("GET", "/api/admin/problems/"+pid+"/input-generation", nil, "")
	if code != 200 || g["state"] != "queued" || g["planned"] != float64(len(plan)) || g["server_now"] == nil {
		t.Fatalf("input generation: %d %v", code, g)
	}
	if code, body := admin.do("POST", "/api/admin/problems/"+pid+"/generate-inputs", nil, s.srv.URL); code != 409 || errCode(body) != "not_ready" {
		t.Errorf("second generate inputs: %d %v", code, body)
	}
	resp, out := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindGenerateInputs}, Worker: "fake"})
	if resp.StatusCode != 200 {
		t.Fatalf("claim generate_inputs: %d %s", resp.StatusCode, out)
	}
	var claim judgeapi.Claim
	json.Unmarshal(out, &claim)
	var pl judgeapi.GenerateInputsPayload
	json.Unmarshal(claim.Payload, &pl)
	if claim.Kind != judgeapi.KindGenerateInputs || pl.Generator.Language != "python3" || len(pl.Tests) != len(plan) || pl.PlanHash == "" {
		t.Errorf("claim %s %+v", claim.Kind, pl)
	}
	// Each change reaches the admin as an input_generation event.
	s.judgeCall("POST", "/api/judge/report", judgeToken, judgeapi.Report{JobID: claim.JobID, Lease: claim.Lease, Final: true, Fatal: "test stops here"})
	if ev := waitFor(t, events, "input_generation"); !strings.Contains(ev.data, `"problem_id":`+pid) {
		t.Errorf("input_generation event %s", ev.data)
	}

	// A save that leaves the generator out keeps it, a person's save
	// drops the AI draft tag, and null removes the generator.
	edit := map[string]any{}
	for _, k := range []string{"title", "slug", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "epsilon",
		"checker_source", "reference", "brute", "validator", "tags", "difficulty", "samples"} {
		edit[k] = p[k]
	}
	var plainSubtasks []map[string]any
	for _, st := range subtasks {
		m := st.(map[string]any)
		plainSubtasks = append(plainSubtasks, map[string]any{"points": m["points"], "title": m["title"], "tests": m["tests"]})
	}
	edit["subtasks"] = plainSubtasks
	code, saved := admin.do("PUT", "/api/admin/problems/"+pid, edit, s.srv.URL)
	if code != 200 || saved["generator"] == nil || len(saved["plan"].([]any)) != len(plan) ||
		len(saved["subtasks"].([]any)[0].(map[string]any)["generator_args"].([]any)) == 0 || !strings.Contains(fmt.Sprint(saved["tags"]), "ai draft") {
		t.Fatalf("save without generator fields (and no change, so the AI draft tag stays): %d %v", code, saved)
	}
	edit["statement_md"] = fmt.Sprint(p["statement_md"]) + "\n\nRead by a person."
	if code, saved := admin.do("PUT", "/api/admin/problems/"+pid, edit, s.srv.URL); code != 200 || strings.Contains(fmt.Sprint(saved["tags"]), "ai draft") {
		t.Fatalf("a person's statement edit kept the AI draft tag: %d %v", code, saved["tags"])
	}
	edit["generator"], edit["plan"] = nil, []any{}
	if code, saved := admin.do("PUT", "/api/admin/problems/"+pid, edit, s.srv.URL); code != 200 || saved["generator"] != nil || len(saved["plan"].([]any)) != 0 {
		t.Errorf("remove generator: %d %v", code, saved)
	}
	if code, body := admin.do("POST", "/api/admin/problems/"+pid+"/generate-inputs", nil, s.srv.URL); code != 409 || errCode(body) != "not_ready" {
		t.Errorf("generate inputs without a generator: %d %v", code, body)
	}

	// From pasted text.
	code, started = admin.do("POST", "/api/admin/ai/wizard", map[string]any{"mode": "paste", "text": "Read t pairs and print each sum."}, s.srv.URL)
	if code != 202 {
		t.Fatalf("paste: %d %v", code, started)
	}
	w = pollWizard(t, admin, started["wizard_id"].(string))
	if w["state"] != "draft_created" {
		t.Fatalf("paste wizard: %v", w)
	}
	pasteID := idOf(w["problem_id"])
	_, pasted := admin.do("GET", "/api/admin/problems/"+pasteID, nil, "")
	if pasted["slug"] != "pair-sums" || pasted["origin"] != "ai" {
		t.Errorf("paste draft %v", pasted["slug"])
	}

	// The model-solve route went in v3 (V3-4).
	if code, _ := admin.do("POST", "/api/admin/problems/"+pid+"/solve", nil, s.srv.URL); code != 404 && code != 405 {
		t.Errorf("solve route still mounted: %d", code)
	}
	solid := s.importProblem(admin, "pairs")

	// The chain may validate a draft on its own, but no AI draft joins a
	// contest until a person has saved it.
	cid := s.createContest(admin, "week-1", time.Now().Add(time.Hour), map[string]int64{"A": solid}, nil)
	draftID, _ := strconv.ParseInt(pasteID, 10, 64)
	s.validate(draftID)
	choices := []map[string]any{{"problem_id": solid, "label": "A"}, {"problem_id": draftID, "label": "B"}}
	code, body := admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL)
	if code != 400 || !strings.Contains(fmt.Sprint(body), "AI draft") {
		t.Errorf("AI draft added to a contest: %d %v", code, body)
	}
	edit = map[string]any{}
	for _, k := range []string{"title", "slug", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "epsilon",
		"checker_source", "reference", "brute", "validator", "tags", "difficulty", "samples", "subtasks", "generator", "plan"} {
		edit[k] = pasted[k]
	}
	// A save that changes nothing is no review: still an AI draft.
	if code, saved := admin.do("PUT", "/api/admin/problems/"+pasteID, edit, s.srv.URL); code != 200 || saved["validated"] != true {
		t.Fatalf("a save without changes: %d %v", code, saved)
	}
	if code, body := admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL); code != 400 || !strings.Contains(fmt.Sprint(body), "AI draft") {
		t.Errorf("AI draft added after a save that changed nothing: %d %v", code, body)
	}
	// A person's statement edit ends the draft marking and withdraws the
	// validation until it passes again.
	edit["statement_md"] = fmt.Sprint(pasted["statement_md"]) + "\n\nRead by a person."
	if code, saved := admin.do("PUT", "/api/admin/problems/"+pasteID, edit, s.srv.URL); code != 200 || saved["validated"] != false {
		t.Fatalf("a person's statement edit: %d validated=%v", code, saved["validated"])
	}
	s.validate(draftID)
	// The reference is still the model's, so a graded contest asks for a
	// person's solution or an admin's reason (security council S10).
	code, body = admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL)
	if code != 409 || errCode(body) != "ai_unconfirmed" {
		t.Fatalf("AI reference added to a graded contest: %d %v", code, body)
	}
	choices[1]["ai_reason"] = "ok"
	if code, body := admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL); code != 409 {
		t.Errorf("a two-letter reason accepted: %d %v", code, body)
	}
	choices[1]["ai_reason"] = "Checked the reference by hand against the statement."
	if code, body := admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL); code != 200 {
		t.Fatalf("AI problem with a reason: %d %v", code, body)
	}
	var reason string
	s.db.QueryRow("SELECT json_extract(details, '$.reason') FROM contest_audit WHERE contest_id = ? AND action = 'ai_problem'", cid).Scan(&reason)
	if !strings.Contains(reason, "by hand") {
		t.Errorf("the reason is not in the audit log: %q", reason)
	}
	// Once confirmed for this contest, saving the list again needs no reason.
	delete(choices[1], "ai_reason")
	if code, body := admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid), choices, s.srv.URL); code != 200 {
		t.Errorf("confirmed AI problem saved again: %d %v", code, body)
	}
	// A solution a person wrote is enough on its own, in another contest.
	cid2 := s.createContest(admin, "week-2", time.Now().Add(time.Hour), map[string]int64{"A": solid}, nil)
	ref := pasted["reference"].(map[string]any)
	edit["reference"] = map[string]any{"language": ref["language"], "source": fmt.Sprint(ref["source"]) + "\n// rewritten by a person\n"}
	if code, saved := admin.do("PUT", "/api/admin/problems/"+pasteID, edit, s.srv.URL); code != 200 {
		t.Fatalf("a person's reference: %d %v", code, saved)
	}
	s.validate(draftID)
	if code, body := admin.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", cid2), choices, s.srv.URL); code != 200 {
		t.Errorf("AI problem with a person's reference: %d %v", code, body)
	}

	if code, _ := admin.do("DELETE", "/api/admin/ai/key", nil, s.srv.URL); code != 204 {
		t.Errorf("delete key: %d", code)
	}
	if _, st := admin.do("GET", "/api/admin/ai/key", nil, ""); st["active"] != false {
		t.Errorf("after delete: %v", st)
	}
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

// TestSignOutForgetsAIKey: the key belongs to the session and goes with it.
func TestSignOutForgetsAIKey(t *testing.T) {
	s := newSiteMode(t, "testing")
	b := s.testSignIn("ta@college.edu", "organiser")
	if code, _ := b.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake"}, s.srv.URL); code != 200 {
		t.Fatalf("add key: %d", code)
	}
	u, _ := url.Parse(s.srv.URL)
	var token string
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == "podium_session" {
			token = c.Value
		}
	}
	if _, ok := s.ai.Keys.Get(token); token == "" || !ok {
		t.Fatalf("key not held for the session")
	}
	if code, _ := b.do("POST", "/api/auth/logout", nil, s.srv.URL); code != 204 {
		t.Fatalf("logout: %d", code)
	}
	if _, ok := s.ai.Keys.Get(token); ok {
		t.Error("the key outlived sign-out")
	}
	b2 := s.testSignIn("ta@college.edu", "organiser")
	if _, st := b2.do("GET", "/api/admin/ai/key", nil, ""); st["active"] != false {
		t.Errorf("a new session sees the old key: %v", st)
	}

	// Signing in again in the same browser ends the old session and its key.
	b2.do("PUT", "/api/admin/ai/key", map[string]string{"provider": "fake"}, s.srv.URL)
	for _, c := range b2.client.Jar.Cookies(u) {
		if c.Name == "podium_session" {
			token = c.Value
		}
	}
	b2.do("POST", "/api/auth/test-signin", map[string]string{"email": "ta@college.edu", "role": "organiser"}, s.srv.URL)
	if _, ok := s.ai.Keys.Get(token); ok {
		t.Error("the key outlived a replaced session")
	}
}
