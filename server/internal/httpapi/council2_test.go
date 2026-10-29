package httpapi

// Tests for the fixes of council run 2 (2026-10-21,
// cache/v2-plan/current-inconsistencies/run-2-2026-10-21), backend part.

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/submissions"
)

// C2-014: an unpublished draft past its start shows students nothing, so
// it marks no problem; a problem whose hidden tests a contest has shown
// stays in that contest, so the bank's mark cannot disappear.
func TestC2014ShownMarkNeedsPublishAndStays(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	o := s.srv.URL
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c2014-a")
	pb := s.importProblem(owner, "c2014-b")
	pc := s.importProblem(owner, "c2014-c")
	draft := s.createContest(owner, "c2014-draft", start, map[string]int64{"A": pa}, map[string]any{"practice_after_end": true})
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", draft), map[string]any{"published": false}, o); code != 200 {
		t.Fatalf("unpublish: %d %v", code, out)
	}
	practice := s.createContest(owner, "c2014-practice", start, map[string]int64{"A": pb}, map[string]any{"practice_after_end": true})

	s.at(start, 121)
	if s.shownInPractice(owner, pa) {
		t.Error("an unpublished draft past its end marked its problem")
	}
	if !s.shownInPractice(owner, pb) {
		t.Fatal("a published contest open for practice did not mark its problem")
	}
	problems := fmt.Sprintf("/api/admin/contests/%d/problems", practice)
	code, out := owner.do("PUT", problems, []map[string]any{{"problem_id": pc, "label": "A"}}, o)
	if code != 400 || fieldErr(out, "problems") == "" {
		t.Errorf("removing a shown problem: %d %v, want 400 with fields.problems", code, out)
	}
	if !s.shownInPractice(owner, pb) {
		t.Error("the refused removal lost the mark")
	}
	// Adding beside it is still allowed.
	if code, out := owner.do("PUT", problems, []map[string]any{{"problem_id": pb, "label": "A"}, {"problem_id": pc, "label": "B"}}, o); code != 200 {
		t.Errorf("adding a problem beside a shown one: %d %v", code, out)
	}
	// The draft never showed anything, so its problem may still go.
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems", draft), []map[string]any{}, o); code != 200 {
		t.Errorf("removing the draft's problem: %d %v", code, out)
	}
}

// fieldErr is the message of one field error in an error body.
func fieldErr(body map[string]any, field string) string {
	e, _ := body["error"].(map[string]any)
	f, _ := e["fields"].(map[string]any)
	m, _ := f[field].(string)
	return m
}

// Two phones of one model on one account are two devices when each sends
// its own ?device= id; one phone keeps its tag across networks; a desktop
// ignores the id, and a malformed id counts as none (C2-034).
func TestStreamTagUsesAPhonesDeviceID(t *testing.T) {
	d := Deps{Submissions: &submissions.Service{HashKey: []byte("k")}}
	tag := func(addr, ua, device string) string {
		target := "/api/events"
		if device != "" {
			target += "?device=" + device
		}
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = addr
		r.Header.Set("User-Agent", ua)
		return d.streamTag(r)
	}
	phone := "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1"
	desk := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36"
	if tag("10.0.0.1:1", phone, "aaaa1111") == tag("10.0.0.2:1", phone, "bbbb2222") {
		t.Error("two phones with their own ids counted as one device")
	}
	if tag("10.0.0.1:1", phone, "aaaa1111") != tag("100.64.0.9:1", phone, "aaaa1111") {
		t.Error("one phone on a new network counted as another device")
	}
	if tag("10.0.0.1:1", phone, "") != tag("10.0.0.1:1", phone, "bad-id!") {
		t.Error("a malformed id was not ignored")
	}
	if tag("10.0.0.1:1", desk, "aaaa1111") != tag("10.0.0.1:1", desk, "") || tag("10.0.0.1:1", desk, "aaaa1111") == tag("10.0.0.2:1", desk, "aaaa1111") {
		t.Error("a desktop's tag used the device id instead of its address")
	}
}

// A problem opened before the contest starts says so, not "accept the
// rules", which is refused until the start (C2-017); after the start,
// before pressing Start, it says to accept the rules.
func TestNotStartedSaysWhy(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	pa := s.importProblem(owner, "c2017-a")
	s.createContest(owner, "c2017", start, map[string]int64{"A": pa}, nil)
	msg := func() (int, string, string) {
		code, out := asha.do("GET", "/api/contests/c2017/problems/A", nil, "")
		e, _ := out["error"].(map[string]any)
		m, _ := e["message"].(string)
		return code, errCode(out), m
	}
	if code, c, m := msg(); code != 403 || c != "not_started" || !strings.Contains(m, "has not started yet") {
		t.Errorf("before the start: %d %s %q", code, c, m)
	}
	s.at(start, 5)
	if code, c, m := msg(); code != 403 || c != "not_started" || !strings.Contains(m, "Accept the rules") {
		t.Errorf("after the start, before Start: %d %s %q", code, c, m)
	}
}

// A duplicate contest slug is 400 with fields.slug, the shape the web
// mocks must copy (C2-029).
// TestDuplicateTitleGetsANumberedSlug: two contests with one title get
// week-6 and week-6-2 (the slug is derived, V3-15).
func TestDuplicateTitleGetsANumberedSlug(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	body := map[string]any{"title": "Week 6", "starts_at": start.UnixMilli(), "ends_at": start.Add(time.Hour).UnixMilli()}
	for _, want := range []string{"week-6", "week-6-2"} {
		if code, out := owner.do("POST", "/api/admin/contests", body, s.srv.URL); code != 201 || out["slug"] != want {
			t.Errorf("create: %d %v, want slug %s", code, out, want)
		}
	}
}
