package httpapi

// End-to-end checks for milestone M3 over HTTP: an organiser opens the
// workspace, saves a draft, runs and submits; a fake worker judges through
// the judge API; the result arrives on the event stream. Also: students are
// kept out of the M3 workspace, rate limits answer 429 with a retry time,
// and a repeated client key returns the same submission.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

func TestM3WorkspaceSubmitAndStream(t *testing.T) {
	s := newSite(t)
	owner := s.browser()
	owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/")
	student := s.browser()
	student.signIn(authtest.Person{Email: "asha@college.edu", Verified: true}, "/")
	code, created := owner.raw("POST", "/api/admin/problems/import", "application/zip", zipFolder(t, "../../../problems/a-plus-b"), s.srv.URL)
	if code != 201 {
		t.Fatalf("import %d %v", code, created)
	}
	pid := strconv.Itoa(int(created["id"].(float64)))

	if code, _ := student.do("GET", "/api/workspace/"+pid, nil, ""); code != 403 {
		t.Errorf("student in the M3 workspace: %d", code)
	}
	code, ws := owner.do("GET", "/api/workspace/"+pid, nil, "")
	if code != 200 || len(ws["samples"].([]any)) != 2 || ws["server_now"] == nil {
		t.Fatalf("workspace %d %v", code, ws)
	}
	if code, _ := owner.do("PUT", "/api/workspace/"+pid+"/draft", map[string]string{"language": "python3", "source": "print(1)"}, s.srv.URL); code != 204 {
		t.Errorf("draft %d", code)
	}
	if code, _ := owner.do("POST", "/api/workspace/"+pid+"/events", map[string]any{"events": []map[string]any{{"kind": "paste", "size": 300}}}, s.srv.URL); code != 204 {
		t.Errorf("events %d", code)
	}

	// Open the event stream before submitting.
	req, _ := http.NewRequest("GET", s.srv.URL+"/api/events", nil)
	stream, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream content type %q", stream.Header.Get("Content-Type"))
	}
	events := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stream.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "event: ") {
				events <- strings.TrimPrefix(line, "event: ")
			}
		}
	}()
	if ev := waitEvent(t, events); ev != "clock" {
		t.Errorf("first event %q, want clock", ev)
	}

	body := map[string]any{"language": "cpp17", "source": "int main(){}", "client_key": "key-1", "typed_chars": 12}
	code, sub := owner.do("POST", "/api/workspace/"+pid+"/submit", body, s.srv.URL)
	if code != 202 || sub["state"] != "queued" {
		t.Fatalf("submit %d %v", code, sub)
	}
	id := strconv.Itoa(int(sub["id"].(float64)))
	if code, again := owner.do("POST", "/api/workspace/"+pid+"/submit", body, s.srv.URL); code != 200 || again["id"] != sub["id"] {
		t.Errorf("same client key: %d %v", code, again)
	}
	body["client_key"] = "key-2"
	code, limited := owner.do("POST", "/api/workspace/"+pid+"/submit", body, s.srv.URL)
	errBody, _ := limited["error"].(map[string]any)
	if code != 429 || errBody["code"] != "rate_limited" || errBody["retry_after_s"] == nil {
		t.Errorf("second pending submit: %d %v", code, limited)
	}
	if code, _ := student.do("GET", "/api/submissions/"+id, nil, ""); code != 404 {
		t.Errorf("student reading someone else's submission: %d", code)
	}

	// The worker judges it.
	resp, out := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: []string{"submit"}, Worker: "fake"})
	if resp.StatusCode != 200 {
		t.Fatalf("claim %d %s", resp.StatusCode, out)
	}
	var claim judgeapi.Claim
	json.Unmarshal(out, &claim)
	var pl judgeapi.SubmitPayload
	json.Unmarshal(claim.Payload, &pl)
	rep := judgeapi.Report{JobID: claim.JobID, Lease: claim.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, test := range pl.Tests {
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: test.Idx, Role: judgeapi.RoleProgram, Verdict: "AC", TimeMS: 7})
	}
	if resp, out := s.judgeCall("POST", "/api/judge/report", judgeToken, rep); resp.StatusCode != 200 {
		t.Fatalf("report %d %s", resp.StatusCode, out)
	}
	for {
		ev := waitEvent(t, events)
		if ev == "submission" {
			break
		}
	}
	code, done := owner.do("GET", "/api/submissions/"+id, nil, "")
	if code != 200 || done["verdict"] != "AC" || done["score"] != float64(100) {
		t.Errorf("judged submission %d %v", code, done)
	}

	// Run on samples.
	code, run := owner.do("POST", "/api/workspace/"+pid+"/run", map[string]any{"language": "python3", "source": "print(3)"}, s.srv.URL)
	if code != 202 || len(run["results"].([]any)) != 2 {
		t.Errorf("run %d %v", code, run)
	}
	runID := strconv.Itoa(int(run["id"].(float64)))
	if code, _ := owner.do("GET", "/api/runs/"+runID, nil, ""); code != 200 {
		t.Errorf("get run %d", code)
	}
	if code, _ := owner.do("GET", "/api/submissions/"+runID, nil, ""); code != 404 {
		t.Errorf("a run read as a submission: %d", code)
	}
}

func waitEvent(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3 s")
		return ""
	}
}
