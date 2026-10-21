package httpapi

// Tests for the fixes of council run 2 (2026-10-21,
// cache/v2-plan/current-inconsistencies/run-2-2026-10-21), backend part.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
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
	draft := s.createContest(owner, "c2014-draft", start, map[string]int64{"A": pa}, map[string]any{"graded": false})
	if code, out := owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d", draft), map[string]any{"published": false}, o); code != 200 {
		t.Fatalf("unpublish: %d %v", code, out)
	}
	ungraded := s.createContest(owner, "c2014-ungraded", start, map[string]int64{"A": pb}, map[string]any{"graded": false})

	s.at(start, 5)
	if s.shownInPractice(owner, pa) {
		t.Error("an unpublished draft past its start marked its problem")
	}
	if !s.shownInPractice(owner, pb) {
		t.Fatal("a published ungraded contest past its start did not mark its problem")
	}
	problems := fmt.Sprintf("/api/admin/contests/%d/problems", ungraded)
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

// The students filters (P4 gate 5): missed_last_two counts only the last
// two graded final contests and only students eligible for them (a roster
// contest counts for its roster only); no_accepted reads the newest
// graded contest; ungraded contests and practice submissions change
// nothing (C2-019).
func TestStudentsFilters(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
	day := 24 * time.Hour
	now := time.Now()
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	seed := []string{
		fmt.Sprintf(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at, graded, access, summarised_at) VALUES
			(201, 'g-old', 'Old', %d, %d, 1, 1, 1, 'domain', 1),
			(202, 'g-mid', 'Mid', %d, %d, 1, 1, 1, 'roster', 1),
			(203, 'g-new', 'New', %d, %d, 1, 1, 1, 'domain', 1),
			(204, 'practice-week', 'Practice', %d, %d, 1, 1, 0, 'domain', 1)`,
			ms(3*day+time.Hour), ms(3*day), ms(2*day+time.Hour), ms(2*day), ms(day+time.Hour), ms(day), ms(time.Hour), ms(time.Minute)),
		`INSERT INTO users (id, email, name, role, created_at) VALUES
			(301, 'amy@college.edu', 'Amy', 'student', 0), (302, 'ben@college.edu', 'Ben', 'student', 0),
			(303, 'cal@college.edu', 'Cal', 'student', 0), (304, 'dee@college.edu', 'Dee', 'student', 0),
			(305, 'eve@college.edu', 'Eve', 'student', 0), (306, 'fay@college.edu', 'Fay', 'student', 0)`,
		`INSERT INTO contest_roster (contest_id, email) VALUES (202, 'amy@college.edu'), (202, 'ben@college.edu')`,
	}
	for _, q := range seed {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	summary := func(contest, user int64, submissions, accepted int) {
		if _, err := conn.ExecContext(ctx, `INSERT INTO student_contest_summary (contest_id, user_id, graded, feedback, score, max_score, percent,
			problems_attempted, problems_full, submissions, accepted, rank, ranked_count, problems, finalised_at)
			VALUES (?, ?, 1, 'subtask', 0, 100, 0, 1, 0, ?, ?, 1, 1, '[]', 1)`, contest, user, submissions, accepted); err != nil {
			t.Fatal(err)
		}
	}
	summary(203, 301, 12, 0) // Amy: newest, many submissions, none accepted
	summary(202, 304, 3, 1)  // Dee: the roster contest only
	summary(203, 305, 12, 1) // Eve: accepted one
	summary(203, 306, 5, 0)  // Fay: too few to count
	summary(201, 302, 4, 1)  // Ben: only the oldest contest
	summary(204, 302, 20, 0) // Ben in the ungraded practice week: ignored
	// Ben's practice submissions after the newest contest change nothing.
	if _, err := conn.ExecContext(ctx, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state,
		verdict, score, practice, created_at) VALUES (1, 203, 302, 'submit', 'cpp17', 'x', 's', 'done', 'AC', 100, 1, ?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")

	emails := func(filter string) []string {
		t.Helper()
		code, out := owner.do("GET", "/api/admin/students?filter="+filter, nil, "")
		if code != 200 {
			t.Fatalf("filter %s: %d %v", filter, code, out)
		}
		var got []string
		for _, x := range out["students"].([]any) {
			got = append(got, strings.TrimSuffix(x.(map[string]any)["email"].(string), "@college.edu"))
		}
		return got
	}
	// Cal is on no roster, so the roster contest does not count against
	// him; he took neither, but missed only one he could enter.
	if got := emails("missed_last_two"); strings.Join(got, ",") != "ben" {
		t.Errorf("missed_last_two: %v, want [ben]", got)
	}
	if got := emails("no_accepted"); strings.Join(got, ",") != "amy" {
		t.Errorf("no_accepted: %v, want [amy]", got)
	}
}

// An apply that names its preview's time disables only the accounts the
// preview listed: one that crossed 180 days in between keeps its account
// (C2-033). A preview older than an hour is refused.
func TestRolloverApplyAsOfThePreview(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	s.signedIn("edge@college.edu", "Edge")
	o := s.srv.URL
	start := time.Now()
	s.clock.set(start)
	// Idle for 180 days less ten minutes at the preview.
	s.db.Exec("UPDATE users SET last_login_at = ? WHERE email = 'edge@college.edu'", start.Add(-180*24*time.Hour+10*time.Minute).UnixMilli())
	code, pre := owner.do("POST", "/api/admin/rollover", map[string]any{"label": "2026 odd semester"}, o)
	if code != 200 || len(pre["accounts"].([]any)) != 0 {
		t.Fatalf("preview: %d %v", code, pre)
	}
	asOf := int64(pre["server_now"].(float64))
	s.clock.set(start.Add(30 * time.Minute))
	code, out := owner.do("POST", "/api/admin/rollover", map[string]any{"label": "2026 odd semester", "apply": true, "as_of": asOf}, o)
	if code != 200 || len(out["accounts"].([]any)) != 0 {
		t.Errorf("apply as of the preview: %d %v, want no account", code, out)
	}
	var disabled int
	s.db.QueryRow("SELECT disabled FROM users WHERE email = 'edge@college.edu'").Scan(&disabled)
	if disabled != 0 {
		t.Error("an account not in the preview was disabled")
	}
	s.clock.set(start.Add(2 * time.Hour))
	if code, out := owner.do("POST", "/api/admin/rollover", map[string]any{"label": "2026 odd semester", "apply": true, "as_of": asOf}, o); code != 400 {
		t.Errorf("apply with a preview two hours old: %d %v, want 400", code, out)
	}
}

// The contact inbox and the appeals list page at 50 and say when more
// follow (C2-043); the appeals list leaves out the reader's own appeal and
// logs each listed one as a read of the student's record (C2-048, D97).
func TestInboxAndAppealsPageAt50(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	asha := s.signedIn("asha@college.edu", "Asha")
	ownerID, ashaID := owner.userID(), asha.userID()
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
	for i := 1; i <= 51; i++ {
		if _, err := conn.ExecContext(ctx, `INSERT INTO contact_messages (user_id, topic, message, created_at) VALUES (?, 'contest', ?, ?)`,
			ashaID, fmt.Sprintf("Message %d", i), int64(i)); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO integrity_appeals (user_id, rating_history_id, text, created_at) VALUES (?, ?, ?, ?)`,
			ashaID, i, fmt.Sprintf("Appeal %d", i), int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	// The owner's own appeal, the newest: never listed to the owner.
	if _, err := conn.ExecContext(ctx, `INSERT INTO integrity_appeals (user_id, rating_history_id, text, created_at) VALUES (?, 52, 'Mine', 52)`, ownerID); err != nil {
		t.Fatal(err)
	}
	conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")

	code, out := owner.do("GET", "/api/admin/contact", nil, "")
	msgs, _ := out["messages"].([]any)
	if code != 200 || len(msgs) != 50 || out["has_more"] != true {
		t.Fatalf("inbox page 1: %d, %d messages, has_more %v", code, len(msgs), out["has_more"])
	}
	oldest := int64(msgs[49].(map[string]any)["id"].(float64))
	code, out = owner.do("GET", fmt.Sprintf("/api/admin/contact?before=%d", oldest), nil, "")
	if msgs, _ := out["messages"].([]any); code != 200 || len(msgs) != 1 || out["has_more"] != false {
		t.Errorf("inbox page 2: %d %v", code, out)
	}

	code, out = owner.do("GET", "/api/admin/appeals", nil, "")
	list, _ := out["appeals"].([]any)
	if code != 200 || len(list) != 49 || out["has_more"] != true {
		t.Fatalf("appeals page 1: %d, %d appeals, has_more %v; want 49 (own left out) and more", code, len(list), out["has_more"])
	}
	for _, x := range list {
		if x.(map[string]any)["text"] == "Mine" {
			t.Error("the owner's own appeal is listed to the owner")
		}
	}
	if n := viewsOf(t, s, "record", ashaID); n != 49 {
		t.Errorf("%d record views logged for page 1, want 49", n)
	}
	code, out = owner.do("GET", "/api/admin/appeals?page=2", nil, "")
	if list, _ := out["appeals"].([]any); code != 200 || len(list) != 2 || out["has_more"] != false {
		t.Errorf("appeals page 2: %d %v", code, out)
	}
}
