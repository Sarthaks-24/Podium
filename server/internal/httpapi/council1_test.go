package httpapi

// Fixes from council run 1 (cache/v2-plan/current-inconsistencies/
// run-1-2026-10-21/FINDINGS.md), one test per finding id.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"fmt"
	"github.com/Sarthaks-24/podium/internal/submissions"
	"testing"
	"time"
)

// shownInPractice reads the bank flag of one problem.
func (s *site) shownInPractice(owner *browser, pid int64) bool {
	s.t.Helper()
	code, out := owner.do("GET", "/api/admin/problems", nil, "")
	if code != 200 {
		s.t.Fatalf("bank: %d %v", code, out)
	}
	for _, p := range out["problems"].([]any) {
		m := p.(map[string]any)
		if int64(m["id"].(float64)) == pid {
			v, ok := m["shown_in_practice"].(bool)
			if !ok {
				s.t.Fatalf("no shown_in_practice in %v", m)
			}
			return v
		}
	}
	s.t.Fatalf("problem %d not in the bank", pid)
	return false
}

// C1-015: the bank marks a problem whose hidden tests a contest showed
// (ungraded and started, or practice after the end), and turning grading
// off after the start needs an explicit confirm.
func TestC1015HiddenTestsShownInPracticeAndUngradedConfirm(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c1015-a")
	pb := s.importProblem(owner, "c1015-b")
	pc := s.importProblem(owner, "c1015-c")
	graded := s.createContest(owner, "c1015-graded", start, map[string]int64{"A": pa}, nil)
	s.createContest(owner, "c1015-practice", start, map[string]int64{"A": pb}, map[string]any{"practice_after_end": true})
	s.createContest(owner, "c1015-closed", start, map[string]int64{"A": pc}, map[string]any{"practice_after_end": false})
	for _, p := range []int64{pa, pb, pc} {
		if s.shownInPractice(owner, p) {
			t.Errorf("problem %d marked before any contest started", p)
		}
	}

	s.at(start, 5)
	path := fmt.Sprintf("/api/admin/contests/%d", graded)
	code, out := owner.do("PUT", path, map[string]any{"graded": false}, o)
	if code != 400 || errCode(out) != "confirm_ungraded" {
		t.Fatalf("turning grading off after the start without a confirm: %d %v", code, out)
	}
	if s.shownInPractice(owner, pa) {
		t.Error("refused change still marked the problem")
	}
	if code, out := owner.do("PUT", path, map[string]any{"graded": false, "confirm_ungraded": true}, o); code != 200 || out["graded"] != false {
		t.Fatalf("with the confirm: %d %v", code, out)
	}
	if !s.shownInPractice(owner, pa) {
		t.Error("an ungraded started contest's problem is not marked")
	}
	// Turning it on again needs no confirm.
	if code, out := owner.do("PUT", path, map[string]any{"graded": true}, o); code != 200 {
		t.Errorf("grading on again: %d %v", code, out)
	}

	if s.shownInPractice(owner, pb) {
		t.Error("practice marked before the end")
	}
	s.at(start, 121)
	if !s.shownInPractice(owner, pb) {
		t.Error("a problem open for practice is not marked")
	}
	if s.shownInPractice(owner, pc) {
		t.Error("a graded contest without practice marked its problem")
	}
}

// C1-051: a phone that moves from wifi to cellular keeps its stream tag,
// so it records no second session; a desktop on another address, or
// another browser, is another device.
func TestC1051StreamTagIgnoresAPhoneChangingNetworks(t *testing.T) {
	d := Deps{Submissions: &submissions.Service{HashKey: []byte("k")}}
	req := func(addr, ua string) *http.Request {
		r := httptest.NewRequest("GET", "/api/events", nil)
		r.RemoteAddr = addr
		r.Header.Set("User-Agent", ua)
		return r
	}
	phone := "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/130.0 Mobile Safari/537.36"
	desk := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36"
	if d.streamTag(req("10.1.2.3:5000", phone)) != d.streamTag(req("100.64.9.9:6000", phone)) {
		t.Error("a phone on a new network counted as another device")
	}
	if d.streamTag(req("10.1.2.3:5000", desk)) == d.streamTag(req("10.1.2.4:5000", desk)) {
		t.Error("two desktops with one browser version counted as one device")
	}
	if d.streamTag(req("10.1.2.3:5000", desk)) == d.streamTag(req("10.1.2.3:5000", phone)) {
		t.Error("two browsers counted as one device")
	}
}

// C1-048: /healthz on loopback tells podium-check how long jobs have
// waited and when a judge last asked for work.
func TestC1048HealthzReportsTheJudgeQueue(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c1048-a")
	s.judgeAll()
	s.createContest(owner, "c1048", start, map[string]int64{"A": pa}, nil)
	s.at(start, 5)
	asha.do("POST", "/api/contests/c1048/start", map[string]any{"accept_honour": true}, s.srv.URL)
	// Not s.submit, which judges at once: this one waits in the queue.
	if code, out := asha.do("POST", "/api/contests/c1048/problems/A/submit",
		map[string]any{"language": "cpp17", "source": "correct", "client_key": "c1048"}, s.srv.URL); code != 202 {
		t.Fatalf("submit: %d %v", code, out)
	}
	health := func() map[string]any {
		resp, err := http.Get(s.srv.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	out := health()
	if out["jobs_queued"] != float64(1) || out["oldest_queued_s"] == nil {
		t.Errorf("with a submission waiting: %v", out)
	}
	if _, ok := out["judge_last_claim_s"].(float64); !ok {
		t.Errorf("a judge claimed while importing, so the last claim is known: %v", out)
	}
	s.judgeAll()
	if out := health(); out["jobs_queued"] != float64(0) || out["oldest_queued_s"] != float64(0) {
		t.Errorf("after judging: %v", out)
	}
}
