package httpapi

// End-to-end checks for milestone M2 over HTTP: an admin imports a pack,
// edits and validates it; a fake worker claims the job through the judge
// API with the token, fetches files under its lease, and reports; the
// editor then sees the outcome. Also the refusals: roles, content types,
// cross-site uploads, bad packs, wrong tokens, stale leases, bad reports.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/auth/authtest"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

func zipFolder(t *testing.T, dir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		w, _ := zw.Create(filepath.ToSlash(rel))
		b, _ := os.ReadFile(p)
		w.Write(b)
		return nil
	})
	zw.Close()
	return buf.Bytes()
}

// raw sends a non-JSON body and returns the status and decoded JSON.
func (b *browser) raw(method, path, contentType string, body []byte, origin string) (int, map[string]any) {
	b.s.t.Helper()
	req, _ := http.NewRequest(method, b.s.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// judgeCall calls the judge API with a token and optional lease headers.
func (s *site) judgeCall(method, path, token string, body any, headers ...string) (*http.Response, []byte) {
	s.t.Helper()
	var r io.Reader
	// A claim speaks this build's protocol, as podium-judge's client does.
	if c, ok := body.(judgeapi.ClaimRequest); ok && c.Protocol == 0 {
		c.Protocol = judgeapi.ProtocolVersion
		body = c
	}
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.judge.URL+path, r)
	req.Header.Set("Authorization", "Bearer "+token)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func TestM2ImportValidateOverHTTP(t *testing.T) {
	s := newSite(t)
	owner := s.browser()
	owner.signIn(authtest.Person{Email: "owner@college.edu", Verified: true, Name: "Owner"}, "/")
	student := s.browser()
	student.signIn(authtest.Person{Email: "asha@college.edu", Verified: true}, "/")
	pack := zipFolder(t, "../../../problems/a-plus-b")

	if code, _ := student.do("GET", "/api/admin/problems", nil, ""); code != 403 {
		t.Errorf("student listing problems: %d", code)
	}
	if code, _ := owner.raw("POST", "/api/admin/problems/import", "text/plain", pack, s.srv.URL); code != 415 {
		t.Errorf("text/plain import: %d", code)
	}
	if code, _ := owner.raw("POST", "/api/admin/problems/import", "application/zip", pack, "https://evil.example"); code != 403 {
		t.Errorf("cross-site import: %d", code)
	}
	if code, body := owner.raw("POST", "/api/admin/problems/import", "application/zip", []byte("not a zip"), s.srv.URL); code != 400 || body["error"] == nil {
		t.Errorf("garbage import: %d %v", code, body)
	}
	code, created := owner.raw("POST", "/api/admin/problems/import", "application/zip", pack, s.srv.URL)
	if code != 201 || created["slug"] != "a-plus-b" || created["test_count"] != float64(6) || created["validated"] != false {
		t.Fatalf("import: %d %v", code, created)
	}
	id := strconv.Itoa(int(created["id"].(float64)))
	if code, _ := owner.raw("POST", "/api/admin/problems/import", "application/zip", pack, s.srv.URL); code != 409 {
		t.Errorf("duplicate slug import: %d", code)
	}

	// A field error comes back keyed by field.
	edit := map[string]any{}
	for _, k := range []string{"title", "slug", "statement_md", "time_limit_ms", "memory_limit_mb", "checker", "epsilon",
		"checker_source", "reference", "brute", "validator", "tags", "difficulty", "samples", "subtasks"} {
		edit[k] = created[k]
	}
	edit["slug"] = "Not A Slug"
	code, body := owner.do("PUT", "/api/admin/problems/"+id, edit, s.srv.URL)
	fields, _ := body["error"].(map[string]any)["fields"].(map[string]any)
	if code != 400 || fields["slug"] == nil {
		t.Errorf("bad slug: %d %v", code, body)
	}

	code, v := owner.do("POST", "/api/admin/problems/"+id+"/validate", nil, s.srv.URL)
	if code != 202 || v["state"] != "queued" || v["queue_position"] != float64(1) {
		t.Fatalf("validate: %d %v", code, v)
	}

	// The worker side.
	if resp, _ := s.judgeCall("POST", "/api/judge/claim", "wrong-token", judgeapi.ClaimRequest{Kinds: []string{"validate"}}); resp.StatusCode != 401 {
		t.Errorf("wrong token: %d", resp.StatusCode)
	}
	resp, out := s.judgeCall("POST", "/api/judge/claim", judgeToken, judgeapi.ClaimRequest{Kinds: []string{"validate"}, Worker: "fake"})
	if resp.StatusCode != 200 {
		t.Fatalf("claim: %d %s", resp.StatusCode, out)
	}
	var claim judgeapi.Claim
	json.Unmarshal(out, &claim)
	var pl judgeapi.ValidatePayload
	json.Unmarshal(claim.Payload, &pl)
	sha := pl.Tests[0].InputSHA
	if resp, _ := s.judgeCall("GET", "/api/judge/blob/"+sha, judgeToken, nil); resp.StatusCode != 404 {
		t.Errorf("blob without a lease: %d", resp.StatusCode)
	}
	resp, blob := s.judgeCall("GET", "/api/judge/blob/"+sha, judgeToken, nil,
		judgeapi.HeaderJob, strconv.FormatInt(claim.JobID, 10), judgeapi.HeaderLease, claim.Lease)
	if resp.StatusCode != 200 || problems.SHA(blob) != sha {
		t.Errorf("blob under lease: %d", resp.StatusCode)
	}
	if resp, _ := s.judgeCall("POST", "/api/judge/report", judgeToken, judgeapi.Report{JobID: claim.JobID, Lease: "stale"}); resp.StatusCode != 409 {
		t.Errorf("stale lease: %d", resp.StatusCode)
	}
	if resp, _ := s.judgeCall("POST", "/api/judge/report", judgeToken, judgeapi.Report{JobID: claim.JobID, Lease: claim.Lease,
		Results: []judgeapi.TestResult{{Idx: 99, Role: "reference", Verdict: "AC"}}}); resp.StatusCode != 400 {
		t.Errorf("bad report: %d", resp.StatusCode)
	}
	rep := judgeapi.Report{JobID: claim.JobID, Lease: claim.Lease, Final: true}
	for _, role := range []string{"reference", "brute", "validator"} {
		rep.Compile = append(rep.Compile, judgeapi.CompileResult{Role: role, Verdict: "OK"})
	}
	for _, test := range pl.Tests {
		for _, role := range []string{"reference", "brute", "validator"} {
			verdict := "AC"
			if test.Idx == 5 && role != "validator" {
				verdict = "WA"
			}
			rep.Results = append(rep.Results, judgeapi.TestResult{Idx: test.Idx, Role: role, Verdict: verdict, TimeMS: 4})
		}
	}
	if resp, out := s.judgeCall("POST", "/api/judge/report", judgeToken, rep); resp.StatusCode != 200 {
		t.Fatalf("final report: %d %s", resp.StatusCode, out)
	}

	code, v = owner.do("GET", "/api/admin/problems/"+id+"/validation", nil, "")
	summary, _ := v["summary"].(map[string]any)
	problemsList, _ := summary["problems"].([]any)
	if code != 200 || v["state"] != "failed" || len(problemsList) == 0 ||
		!strings.Contains(problemsList[0].(string), "Test 5: reference solution got wrong answer.") {
		t.Errorf("validation after a wrong answer on test 5: %d %v", code, v)
	}

	// Export gives a pack that imports again.
	req, _ := http.NewRequest("GET", s.srv.URL+"/api/admin/problems/"+id+"/export", nil)
	eresp, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	exported, _ := io.ReadAll(eresp.Body)
	eresp.Body.Close()
	if eresp.StatusCode != 200 || eresp.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("export: %d", eresp.StatusCode)
	}
	if _, err := problems.ReadPack(exported); err != nil {
		t.Errorf("exported pack does not import: %v", err)
	}

	// Test downloads are attachments, never rendered.
	req, _ = http.NewRequest("GET", s.srv.URL+"/api/admin/problems/"+id+"/tests/1/input", nil)
	dresp, _ := owner.client.Do(req)
	dresp.Body.Close()
	if dresp.StatusCode != 200 || !strings.HasPrefix(dresp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("test download: %d %q", dresp.StatusCode, dresp.Header.Get("Content-Disposition"))
	}
	if code, _ := owner.do("DELETE", "/api/admin/problems/"+id, nil, s.srv.URL); code != 204 {
		t.Errorf("archive: %d", code)
	}
}
