package httpapi

// Contest rules over HTTP: access (domain, organisers, ids from
// another contest), time windows with extensions, limits, practice, exam
// feedback, what students may see, and the event stream's contest events.

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sse struct{ typ, data string }

// openStream opens /api/events with a user agent and returns its events.
func openStream(t *testing.T, b *browser, ua string) (chan sse, func()) {
	t.Helper()
	req, _ := http.NewRequest("GET", b.s.srv.URL+"/api/events", nil)
	req.Header.Set("User-Agent", ua)
	client := *b.client
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan sse, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		var typ string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				typ = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ch <- sse{typ, strings.TrimPrefix(line, "data: ")}
			}
		}
	}()
	return ch, func() { resp.Body.Close() }
}

// waitFor returns the first event of type typ, skipping others.
func waitFor(t *testing.T, ch chan sse, typ string) sse {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before a %q event", typ)
			}
			if ev.typ == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q event within 4 s", typ)
		}
	}
}

func (b *browser) userID() int64 {
	_, me := b.do("GET", "/api/me", nil, "")
	return int64(me["id"].(float64))
}

func TestAccessAndCrossContestIDs(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	c1 := s.createContest(owner, "c1", start, map[string]int64{"A": pa}, nil)
	c2 := s.createContest(owner, "c2", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "")
	bilal := s.signedIn("bilal@college.edu", "Bilal")

	// The allowed domains are the roster (V3-15): any student may enter.
	if code, _ := asha.do("GET", "/api/contests/c1", nil, ""); code != 200 {
		t.Errorf("student: %d", code)
	}
	_, list := bilal.do("GET", "/api/contests", nil, "")
	if n := len(list["upcoming"].([]any)); n != 2 {
		t.Errorf("bilal's list has %d upcoming, want both: %v", n, list)
	}
	// Domain access: any signed-in student.
	if code, _ := bilal.do("GET", "/api/contests/c2", nil, ""); code != 200 {
		t.Errorf("domain contest: %d", code)
	}
	if code, _ := bilal.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", c2), nil, ""); code != 403 {
		t.Errorf("student on the admin board: %d", code)
	}

	s.at(start, 5)
	for _, b := range []*browser{asha, bilal} {
		b.do("POST", "/api/contests/c2/start", map[string]any{"accept_honour": true}, s.srv.URL)
	}
	asha.do("POST", "/api/contests/c1/start", map[string]any{"accept_honour": true}, s.srv.URL)
	code, sub := s.submit(asha, "c1", "A", "correct")
	if code != 202 {
		t.Fatalf("submit %d %v", code, sub)
	}
	subID := int64(sub["id"].(float64))
	// Ids from one contest never work through another.
	if code, _ := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/rejudge", c2), map[string]any{"submission_id": subID}, s.srv.URL); code != 404 {
		t.Errorf("rejudge a c1 submission through c2: %d", code)
	}
	if code, _ := bilal.do("GET", fmt.Sprintf("/api/submissions/%d", subID), nil, ""); code != 404 {
		t.Errorf("another student's submission: %d", code)
	}
	code, cl := bilal.do("POST", "/api/contests/c2/clarifications", map[string]any{"question": "Is n at least 1?"}, s.srv.URL)
	if code != 201 {
		t.Fatalf("ask %d %v", code, cl)
	}
	cid := int64(cl["clarification"].(map[string]any)["id"].(float64))
	if code, _ := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/clarifications/%d", c1, cid), map[string]any{"answer": "Yes"}, s.srv.URL); code != 404 {
		t.Errorf("answer a c2 question through c1: %d", code)
	}
	if code, _ := asha.do("GET", "/api/contests/c1/problems/Z", nil, ""); code != 404 {
		t.Errorf("unknown label: %d", code)
	}

	// Students never see emails: Asha has no account name, so the board
	// names her by number.
	_, board := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", c1), nil, "")
	if rows := board["rows"].([]any); len(rows) != 1 || !strings.HasPrefix(rows[0].(map[string]any)["name"].(string), "Student ") {
		t.Errorf("board names: %v", board["rows"])
	}
	_, detail := asha.do("GET", "/api/contests/c1", nil, "")
	if strings.Contains(fmt.Sprint(detail), "@college.edu") {
		t.Errorf("contest page shows an email: %v", detail)
	}
}

// TestQuestionsStayOpenInPractice: after the end Messages stays usable
// while practice is open (walk bug B4, B3); without practice the end
// closes questions.
func TestQuestionsStayOpenInPractice(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "prac", start, map[string]int64{"A": pa}, map[string]any{"practice_after_end": true})
	s.createContest(owner, "shut", start, map[string]int64{"A": pa}, map[string]any{"practice_after_end": false})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 5)
	for _, slug := range []string{"prac", "shut"} {
		asha.do("POST", "/api/contests/"+slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL)
	}
	s.at(start, 24*60)
	if code, out := asha.do("POST", "/api/contests/prac/clarifications", map[string]any{"question": "Why does B time out?"}, s.srv.URL); code != 201 {
		t.Errorf("question in practice: %d %v", code, out)
	}
	if code, out := asha.do("POST", "/api/contests/shut/clarifications", map[string]any{"question": "Why does B time out?"}, s.srv.URL); code != 403 || out["error"].(map[string]any)["code"] != "contest_over" {
		t.Errorf("question after an end without practice: %d %v", code, out)
	}
}

func TestLimitsAndPractice(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "lim", start, map[string]int64{"A": pa},
		map[string]any{"max_per_problem": 2, "languages": []string{"cpp17", "python3"}, "min_gap_s": 30})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	asha.do("POST", "/api/contests/lim/start", map[string]any{"accept_honour": true}, s.srv.URL)
	code, out := asha.do("POST", "/api/contests/lim/problems/A/submit", map[string]any{"language": "java21", "source": "x"}, s.srv.URL)
	if code != 400 || !strings.Contains(fmt.Sprint(out), "This contest allows C++17 and Python 3.") {
		t.Errorf("language outside the list: %d %v", code, out)
	}
	_, ws := asha.do("GET", "/api/contests/lim/problems/A", nil, "")
	if langs := ws["languages"].([]any); len(langs) != 2 || ws["label"] != "A" || ws["can_submit"] != true {
		t.Errorf("workspace %v", ws)
	}
	// Each language carries the judge's time factor, so the editor can say
	// the limit a program really gets (D323).
	if langs := ws["languages"].([]any); langs[0].(map[string]any)["time_factor"] != 1.0 || langs[1].(map[string]any)["time_factor"] != 3.0 {
		t.Errorf("time factors %v", langs)
	}
	if code, _ := s.submit(asha, "lim", "A", "int32"); code != 202 {
		t.Fatalf("first submit %d", code)
	}
	if code, out := s.submit(asha, "lim", "A", "int32"); code != 429 {
		t.Errorf("inside the gap: %d %v", code, out)
	}
	s.at(start, 2)
	s.submit(asha, "lim", "A", "int32")
	s.at(start, 3)
	if code, out := s.submit(asha, "lim", "A", "correct"); code != 400 || !strings.Contains(fmt.Sprint(out), "all 2 submissions") {
		t.Errorf("over the cap: %d %v", code, out)
	}
	// After the end submissions are practice: judged, never on the board.
	s.at(start, 121)
	code, sub := s.submit(asha, "lim", "A", "correct")
	if code != 202 {
		t.Fatalf("practice submit %d %v", code, sub)
	}
	_, board := owner.do("GET", "/api/contests/lim/standings", nil, "")
	if total := board["rows"].([]any)[0].(map[string]any)["total"]; total != float64(40) {
		t.Errorf("practice changed the board: %v", total)
	}
	_, detail := asha.do("GET", "/api/contests/lim", nil, "")
	if detail["state"] != "practice" {
		t.Errorf("state after the end: %v", detail["state"])
	}
}

func TestContestEvents(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "ev", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "Asha")
	bilal := s.signedIn("bilal@college.edu", "Bilal")
	s.at(start, 1)
	asha.do("POST", "/api/contests/ev/start", map[string]any{"accept_honour": true}, s.srv.URL)
	bilal.do("POST", "/api/contests/ev/start", map[string]any{"accept_honour": true}, s.srv.URL)
	as, stopA := openStream(t, asha, "ua-a")
	defer stopA()
	bs, stopB := openStream(t, bilal, "ua-b")
	defer stopB()
	waitFor(t, as, "clock")
	waitFor(t, bs, "clock")

	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/announcements", id), map[string]any{"body_md": "Problem A: n is at least 1."}, s.srv.URL)
	if ev := waitFor(t, bs, "announcement"); !strings.Contains(ev.data, "n is at least 1") {
		t.Errorf("announcement %s", ev.data)
	}
	_, q := asha.do("POST", "/api/contests/ev/clarifications", map[string]any{"problem_label": "A", "question": "Can n be 0?"}, s.srv.URL)
	cid := int64(q["clarification"].(map[string]any)["id"].(float64))
	owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/clarifications/%d", id, cid), map[string]any{"answer": "No."}, s.srv.URL)
	waitFor(t, as, "clarification")
	_, page := bilal.do("GET", "/api/contests/ev", nil, "")
	if len(page["public_clarifications"].([]any)) != 0 {
		t.Errorf("a private answer reached another student")
	}
	owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/clarifications/%d", id, cid), map[string]any{"answer": "No.", "public": true}, s.srv.URL)
	waitFor(t, bs, "clarification")
	_, page = bilal.do("GET", "/api/contests/ev", nil, "")
	pub := page["public_clarifications"].([]any)
	if len(pub) != 1 || pub[0].(map[string]any)["asked_by"] != nil {
		t.Errorf("public clarification %v", pub)
	}
	s.submit(asha, "ev", "A", "correct")
	if ev := waitFor(t, bs, "standings"); !strings.Contains(ev.data, `"contest":"ev"`) {
		t.Errorf("standings event %s", ev.data)
	}
	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/extend", id), map[string]any{"minutes": 5}, s.srv.URL)
	if ev := waitFor(t, as, "contest"); !strings.Contains(ev.data, strconv.FormatInt(start.Add(125*time.Minute).UnixMilli(), 10)) {
		t.Errorf("contest event %s", ev.data)
	}

	// A second machine opening a stream while the first is open is
	// recorded for the live contest, once.
	_, stop2 := openStream(t, asha, "another-browser")
	defer stop2()
	// Wait for the row (up to 2 s), then check it stays one (C2-044).
	count := func() int {
		var n int
		s.db.QueryRow("SELECT count(*) FROM integrity_events WHERE kind = 'second_session' AND contest_id = ?", id).Scan(&n)
		return n
	}
	for deadline := time.Now().Add(2 * time.Second); count() == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if n := count(); n != 1 {
		t.Fatalf("second sessions recorded: %d, want 1", n)
	}
	time.Sleep(200 * time.Millisecond)
	if n := count(); n != 1 {
		t.Errorf("second sessions recorded later: %d, want still 1", n)
	}
}

func TestExportEscapesAndStats(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "exp", start, map[string]int64{"A": pa}, nil)
	// The name comes from the account (the roster was cut, V3-15); a formula in it is escaped.
	asha := s.signedIn("asha@college.edu", `=HYPERLINK("x")`)
	s.at(start, 1)
	asha.do("POST", "/api/contests/exp/start", map[string]any{"accept_honour": true}, s.srv.URL)
	s.submit(asha, "exp", "A", "int32")
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/admin/contests/%d/export.csv?tz=UTC", s.srv.URL, id), nil)
	resp, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if resp.Header.Get("Content-Type") != "text/csv; charset=utf-8" || len(lines) != 2 ||
		lines[0] != "rank,name,email,started (UTC),total,A" {
		t.Fatalf("csv %q", body)
	}
	if !strings.HasPrefix(lines[1], `1,"'=HYPERLINK(""x"")",asha@college.edu,`) || !strings.HasSuffix(lines[1], ",40,40") {
		t.Errorf("csv row %q", lines[1])
	}
	code, stats := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/stats", id), nil, "")
	p := stats["problems"].([]any)[0].(map[string]any)
	if code != 200 || p["attempted"] != float64(1) || p["solved"] != float64(0) || p["median_attempts"] != float64(1) {
		t.Errorf("stats %d %v", code, stats)
	}
	if code, _ := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/export.json", id), nil, ""); code != 200 {
		t.Errorf("json export %d", code)
	}
	// Clone copies settings and problems with new dates; its slug comes from the title, made unique.
	code, cl := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/clone", id), map[string]any{
		"starts_at": start.Add(48 * time.Hour).UnixMilli(), "ends_at": start.Add(50 * time.Hour).UnixMilli()}, s.srv.URL)
	if code != 201 || cl["slug"] != "exp-2" || len(cl["problems"].([]any)) != 1 || cl["published"] != false {
		t.Errorf("clone %d %v", code, cl)
	}
}

// TestConsoleReads covers what the contest-day console needs from the
// server (stage C2): the stats say how many started, an organiser's
// Student view gets the board students get (pilot P10), and extend is
// refused once the contest has closed (walk bug B3).
func TestConsoleReads(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "cr", start, map[string]int64{"A": pa}, map[string]any{"freeze_minutes": 30})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.signedIn("bilal@college.edu", "Bilal")
	s.at(start, 1)
	asha.do("POST", "/api/contests/cr/start", map[string]any{"accept_honour": true}, s.srv.URL)
	code, stats := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/stats", id), nil, "")
	if code != 200 || stats["started"] != float64(1) {
		t.Errorf("stats while live: %d %v", code, stats)
	}

	// In the hidden minutes the organiser's own board is full; as a student it is the frozen one.
	s.at(start, 100)
	if _, b := owner.do("GET", "/api/contests/cr/standings", nil, ""); b["variant"] != "full" {
		t.Errorf("organiser board %v", b["variant"])
	}
	if _, b := owner.do("GET", "/api/contests/cr/standings?as=student", nil, ""); b["variant"] != "frozen" {
		t.Errorf("student view board %v", b["variant"])
	}
	if _, b := asha.do("GET", "/api/contests/cr/standings?as=student", nil, ""); b["variant"] != "frozen" {
		t.Errorf("student board %v", b["variant"])
	}

	extend := fmt.Sprintf("/api/admin/contests/%d/extend", id)
	if code, out := owner.do("POST", extend, map[string]any{"minutes": 5}, s.srv.URL); code != 200 {
		t.Fatalf("extend while live: %d %v", code, out)
	}
	// The close is now 125 minutes in; a minute later extend would reopen the contest.
	s.at(start, 126)
	code, out := owner.do("POST", extend, map[string]any{"minutes": 5}, s.srv.URL)
	if code != 409 || errCode(out) != "contest_ended" {
		t.Errorf("extend after the end: %d %v", code, out)
	}
	if _, c := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", id), nil, ""); c["extended_minutes"] != float64(5) {
		t.Errorf("extended_minutes after a refused extend: %v", c["extended_minutes"])
	}
	var n int
	s.db.QueryRow("SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = 'extend'", id).Scan(&n)
	if n != 1 {
		t.Errorf("extend audit rows: %d, want 1", n)
	}
}

func TestPausedContestWaitsAtClaim(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "pz", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	asha.do("POST", "/api/contests/pz/start", map[string]any{"accept_honour": true}, s.srv.URL)
	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/pause", id), nil, s.srv.URL)
	code, _ := asha.do("POST", "/api/contests/pz/problems/A/submit", map[string]any{"language": "cpp17", "source": "correct"}, s.srv.URL)
	if code != 202 {
		t.Fatalf("submit while paused %d", code)
	}
	if n := s.judgeAll(); n != 0 {
		t.Errorf("judged %d jobs of a paused contest", n)
	}
	owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/resume", id), nil, s.srv.URL)
	if n := s.judgeAll(); n != 1 {
		t.Errorf("judged %d jobs after resume", n)
	}
}

func TestStreamEndsWithSessionAndReplacedTabsStop(t *testing.T) {
	old := clockEvery
	clockEvery = 100 * time.Millisecond
	defer func() { clockEvery = old }()
	s := newSite(t)
	asha := s.signedIn("asha@college.edu", "Asha")
	var streams []chan sse
	for i := 0; i < 5; i++ {
		ch, stop := openStream(t, asha, "ua")
		defer stop()
		waitFor(t, ch, "clock")
		streams = append(streams, ch)
	}
	// The fifth replaced the first, which is told so before it closes.
	waitFor(t, streams[0], "replaced")
	// Signing out ends the other streams at the next clock tick.
	asha.do("POST", "/api/auth/logout", nil, s.srv.URL)
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-streams[4]:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream still open after logout")
		}
	}
}
