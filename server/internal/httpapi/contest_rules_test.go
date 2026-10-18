package httpapi

// Contest rules over HTTP: access (roster, domain, organisers, ids from
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

func TestM4AccessAndCrossContestIDs(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	c1 := s.createContest(owner, "c1", start, map[string]int64{"A": pa}, map[string]any{"access": "roster"})
	c2 := s.createContest(owner, "c2", start, map[string]int64{"A": pa}, nil)
	roster := "email,name,roll_number,section\nasha@college.edu,Asha R,21CS001,A\n"
	if code, out := owner.raw("PUT", fmt.Sprintf("/api/admin/contests/%d/roster", c1), "text/csv", []byte(roster), s.srv.URL); code != 200 || out["roster_count"] != float64(1) || out["added"] != float64(1) || out["removed"] != float64(0) {
		t.Fatalf("roster %d %v", code, out)
	}
	asha := s.signedIn("asha@college.edu", "")
	bilal := s.signedIn("bilal@college.edu", "Bilal")
	ta := s.signedIn("ta@college.edu", "TA")
	if code, _ := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", c1), map[string]string{"email": "ta@college.edu"}, s.srv.URL); code != 200 {
		t.Fatalf("add co-organiser %d", code)
	}

	// Roster: on the list may enter, others may not; the list hides
	// contests the student cannot enter.
	if code, _ := asha.do("GET", "/api/contests/c1", nil, ""); code != 200 {
		t.Errorf("rostered student: %d", code)
	}
	if code, _ := bilal.do("GET", "/api/contests/c1", nil, ""); code != 403 {
		t.Errorf("student not on the roster: %d", code)
	}
	_, list := bilal.do("GET", "/api/contests", nil, "")
	if n := len(list["upcoming"].([]any)); n != 1 {
		t.Errorf("bilal's list has %d upcoming, want only c2: %v", n, list)
	}
	// Domain access: any signed-in student.
	if code, _ := bilal.do("GET", "/api/contests/c2", nil, ""); code != 200 {
		t.Errorf("domain contest: %d", code)
	}
	// Co-organiser: their contest only, and no say over co-organisers.
	if code, _ := ta.do("GET", fmt.Sprintf("/api/admin/contests/%d", c1), nil, ""); code != 200 {
		t.Errorf("co-organiser on their contest: %d", code)
	}
	if code, _ := ta.do("GET", fmt.Sprintf("/api/admin/contests/%d", c2), nil, ""); code != 404 {
		t.Errorf("co-organiser on another contest: %d", code)
	}
	if code, _ := ta.do("POST", fmt.Sprintf("/api/admin/contests/%d/admins", c1), map[string]string{"email": "bilal@college.edu"}, s.srv.URL); code != 403 {
		t.Errorf("co-organiser adding co-organisers: %d", code)
	}
	if code, _ := bilal.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", c2), nil, ""); code != 404 {
		t.Errorf("student on the admin board: %d", code)
	}
	_, mine := ta.do("GET", "/api/admin/contests", nil, "")
	if n := len(mine["contests"].([]any)); n != 1 {
		t.Errorf("co-organiser list has %d contests", n)
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
	if code, _ := ta.do("GET", fmt.Sprintf("/api/submissions/%d", subID), nil, ""); code != 200 {
		t.Errorf("co-organiser reading a submission of their contest: %d", code)
	}
	if code, _ := bilal.do("GET", fmt.Sprintf("/api/submissions/%d", subID), nil, ""); code != 404 {
		t.Errorf("another student's submission: %d", code)
	}
	code, cl := bilal.do("POST", "/api/contests/c2/clarifications", map[string]any{"question": "Is n at least 1?"}, s.srv.URL)
	if code != 201 {
		t.Fatalf("ask %d %v", code, cl)
	}
	cid := int64(cl["clarification"].(map[string]any)["id"].(float64))
	if code, _ := ta.do("PUT", fmt.Sprintf("/api/admin/contests/%d/clarifications/%d", c1, cid), map[string]any{"answer": "Yes"}, s.srv.URL); code != 404 {
		t.Errorf("answer a c2 question through c1: %d", code)
	}
	if code, _ := asha.do("GET", "/api/contests/c1/problems/Z", nil, ""); code != 404 {
		t.Errorf("unknown label: %d", code)
	}

	// Students never see emails: Asha has no account name, so the board
	// shows her roster name.
	_, board := owner.do("GET", fmt.Sprintf("/api/admin/contests/%d/standings", c1), nil, "")
	if rows := board["rows"].([]any); len(rows) != 1 || rows[0].(map[string]any)["name"] != "Asha R" {
		t.Errorf("board names: %v", board["rows"])
	}
	_, detail := asha.do("GET", "/api/contests/c1", nil, "")
	if strings.Contains(fmt.Sprint(detail), "@college.edu") {
		t.Errorf("contest page shows an email: %v", detail)
	}
}

func TestM4WindowModeExtensionsAndBoard(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "win", start, map[string]int64{"A": pa},
		map[string]any{"start_mode": "window", "duration_minutes": 30, "practice_after_end": false})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 10)
	// Without pressing Start there is no clock and no problem.
	if code, _ := asha.do("GET", "/api/contests/win/problems/A", nil, ""); code != 403 {
		t.Errorf("problem before start: %d", code)
	}
	if code, _ := asha.do("POST", "/api/contests/win/start", map[string]any{"accept_honour": false}, s.srv.URL); code != 400 {
		t.Errorf("start without accepting: %d", code)
	}
	code, c := asha.do("POST", "/api/contests/win/start", map[string]any{"accept_honour": true}, s.srv.URL)
	w := c["my_window"].(map[string]any)
	if code != 200 || w["ends_at"].(float64)-w["started_at"].(float64) != float64(30*time.Minute/time.Millisecond) {
		t.Fatalf("window %d %v", code, c)
	}
	// The board stays closed until her window closes.
	if code, out := asha.do("GET", "/api/contests/win/standings", nil, ""); code != 403 || out["error"].(map[string]any)["code"] != "board_closed" {
		t.Errorf("board during the window: %d %v", code, out)
	}
	// 15 minutes extra: her window now ends at 55.
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", id, asha.userID()), map[string]any{"minutes": 15, "reason": "laptop"}, s.srv.URL); code != 200 {
		t.Fatalf("extension %d %v", code, out)
	}
	s.at(start, 50)
	if code, out := s.submit(asha, "win", "A", "correct"); code != 202 {
		t.Errorf("submit inside the extension: %d %v", code, out)
	}
	s.at(start, 56)
	code, out := s.submit(asha, "win", "A", "correct")
	if code != 403 || out["error"].(map[string]any)["message"] != "The contest has ended." {
		t.Errorf("submit after the window: %d %v", code, out)
	}
	if code, out := asha.do("GET", "/api/contests/win/standings", nil, ""); code != 200 || out["variant"] != "full" {
		t.Errorf("board after the window: %d %v", code, out)
	}
	// Extending the whole contest moves the close.
	code, ext := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/extend", id), map[string]any{"minutes": 10}, s.srv.URL)
	if code != 200 || ext["close_at"] != float64(start.Add(130*time.Minute).UnixMilli()) {
		t.Errorf("extend %d %v", code, ext)
	}
}

func TestM4LimitsAndPractice(t *testing.T) {
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

func TestM4ExamFeedbackHidesResults(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	s.createContest(owner, "exam", start, map[string]int64{"A": pa}, map[string]any{"feedback_mode": "exam"})
	asha := s.signedIn("asha@college.edu", "Asha")
	s.at(start, 1)
	asha.do("POST", "/api/contests/exam/start", map[string]any{"accept_honour": true}, s.srv.URL)
	stream, stop := openStream(t, asha, "browser-1")
	defer stop()
	waitFor(t, stream, "clock")

	body := map[string]any{"language": "cpp17", "source": "correct", "client_key": "exam-1"}
	code, sub := asha.do("POST", "/api/contests/exam/problems/A/submit", body, s.srv.URL)
	if code != 202 || sub["hidden"] != true || sub["state"] != "submitted" {
		t.Fatalf("submit %d %v", code, sub)
	}
	s.judgeAll()
	id := strconv.Itoa(int(sub["id"].(float64)))
	check := func(what string, v map[string]any) {
		t.Helper()
		if v["hidden"] != true || v["verdict"] != "" || v["score"] != float64(0) || len(v["subtasks"].([]any)) != 0 || v["max_time_ms"] != float64(0) {
			t.Errorf("%s shows results in exam mode: %v", what, v)
		}
	}
	_, got := asha.do("GET", "/api/submissions/"+id, nil, "")
	check("GET submission", got)
	_, dup := asha.do("POST", "/api/contests/exam/problems/A/submit", body, s.srv.URL)
	check("repeated client key", dup)
	_, ws := asha.do("GET", "/api/contests/exam/problems/A", nil, "")
	check("workspace list", ws["submissions"].([]any)[0].(map[string]any))
	_, detail := asha.do("GET", "/api/contests/exam", nil, "")
	if detail["my_score"] != nil || detail["problems"].([]any)[0].(map[string]any)["my_score"] != nil {
		t.Errorf("contest page shows a score in exam mode: %v", detail)
	}
	if code, _ := asha.do("GET", "/api/contests/exam/standings", nil, ""); code != 403 {
		t.Errorf("board in exam mode during the contest: %d", code)
	}
	// No submission or standings event reaches the student while results
	// are hidden: its arrival time would say when judging finished.
	quiet := time.After(1500 * time.Millisecond)
	for done := false; !done; {
		select {
		case ev := <-stream:
			if ev.typ == "submission" || ev.typ == "standings" {
				t.Errorf("exam mode sent a %s event: %s", ev.typ, ev.data)
			}
		case <-quiet:
			done = true
		}
	}
	// After the end: the score, coarse figures and graded rounding.
	s.at(start, 121)
	_, got = asha.do("GET", "/api/submissions/"+id, nil, "")
	if got["hidden"] != false || got["score"] != float64(100) || got["max_time_ms"] != float64(100) || got["max_memory_kb"] != float64(16384) {
		t.Errorf("after the end: %v", got)
	}
	st := got["subtasks"].([]any)[0].(map[string]any)["tests"].([]any)[0].(map[string]any)
	if st["memory_kb"] != float64(0) || st["time_ms"] != float64(100) {
		t.Errorf("hidden test figures for a student: %v", st)
	}
	// Organisers see every figure.
	_, full := owner.do("GET", "/api/submissions/"+id, nil, "")
	if full["max_time_ms"] != float64(140) || full["max_memory_kb"] != float64(5123) {
		t.Errorf("organiser view: %v", full)
	}
}

func TestM4ContestEvents(t *testing.T) {
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
	time.Sleep(100 * time.Millisecond)
	var n int
	s.db.QueryRow("SELECT count(*) FROM integrity_events WHERE kind = 'second_session' AND contest_id = ?", id).Scan(&n)
	if n != 1 {
		t.Errorf("second sessions recorded: %d", n)
	}
}

func TestM4ExportEscapesAndStats(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	id := s.createContest(owner, "exp", start, map[string]int64{"A": pa}, map[string]any{"access": "roster"})
	roster := "email,name,roll_number,section\nasha@college.edu,\"=HYPERLINK(\"\"x\"\")\",+91,@A\n"
	if code, out := owner.raw("PUT", fmt.Sprintf("/api/admin/contests/%d/roster", id), "text/csv", []byte(roster), s.srv.URL); code != 200 {
		t.Fatalf("roster %d %v", code, out)
	}
	asha := s.signedIn("asha@college.edu", "")
	s.at(start, 1)
	asha.do("POST", "/api/contests/exp/start", map[string]any{"accept_honour": true}, s.srv.URL)
	s.submit(asha, "exp", "A", "int32")
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/admin/contests/%d/export.csv", s.srv.URL, id), nil)
	resp, err := owner.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if resp.Header.Get("Content-Type") != "text/csv; charset=utf-8" || len(lines) != 2 ||
		lines[0] != "rank,name,email,roll_number,section,started,total,A" {
		t.Fatalf("csv %q", body)
	}
	if !strings.HasPrefix(lines[1], `1,"'=HYPERLINK(""x"")",asha@college.edu,'+91,'@A,`) || !strings.HasSuffix(lines[1], ",40,40") {
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
	// Clone copies settings and problems with new dates.
	code, cl := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/clone", id), map[string]any{"slug": "exp-2",
		"starts_at": start.Add(48 * time.Hour).UnixMilli(), "ends_at": start.Add(50 * time.Hour).UnixMilli()}, s.srv.URL)
	if code != 201 || cl["access"] != "roster" || len(cl["problems"].([]any)) != 1 || cl["published"] != false || cl["roster_count"] != float64(0) {
		t.Errorf("clone %d %v", code, cl)
	}
}

func TestM4PausedContestWaitsAtClaim(t *testing.T) {
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

func TestM4StreamEndsWithSessionAndReplacedTabsStop(t *testing.T) {
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
