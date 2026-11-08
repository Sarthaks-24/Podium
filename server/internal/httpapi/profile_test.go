package httpapi

// The profile page's dates (v4 M1): GET /api/me/profile answers the
// signed-in person's own member-since and last sign-in, and nothing to
// anyone signed out.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMyProfileDates(t *testing.T) {
	s := newSite(t)
	before := time.Now().Add(-time.Minute).UnixMilli()
	asha := s.signedIn("asha@college.edu", "Asha")
	code, out := asha.do("GET", "/api/me/profile", nil, "")
	if code != 200 {
		t.Fatalf("profile: %d %v", code, out)
	}
	since, _ := out["member_since"].(float64)
	last, _ := out["last_sign_in"].(float64)
	if int64(last) < before || int64(since) > int64(last) {
		t.Errorf("dates: member since %v, last sign-in %v; want a sign-in just now, after the account was made", since, last)
	}
	if len(out) != 2 {
		t.Errorf("the profile carries only the two dates, got %v", out)
	}
	if code, _ := s.browser().do("GET", "/api/me/profile", nil, ""); code != 401 {
		t.Errorf("signed out: %d, want 401", code)
	}
}

// Your contests (v4 M2): only the contests the person entered, their own
// score, and a place only when the board students see is the full one.
func TestMyContests(t *testing.T) {
	s := newSite(t)
	owner := s.signedIn("owner@college.edu", "Owner")
	pa := s.importProblem(owner, "plus-a")
	start := time.Now().Add(time.Hour).Truncate(time.Minute)
	held := s.createContest(owner, "held", start, map[string]int64{"A": pa}, map[string]any{"freeze_minutes": 30, "hold_reveal": true})
	s.createContest(owner, "open", start, map[string]int64{"A": pa}, nil)
	s.createContest(owner, "other", start, map[string]int64{"A": pa}, nil)
	asha := s.signedIn("asha@college.edu", "Asha")
	bilal := s.signedIn("bilal@college.edu", "Bilal")

	s.at(start, 5)
	enter := func(b *browser, slug, prog string) {
		t.Helper()
		if code, out := b.do("POST", "/api/contests/"+slug+"/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
			t.Fatalf("start %s: %d %v", slug, code, out)
		}
		s.submit(b, slug, "A", prog)
	}
	enter(asha, "held", "correct")
	enter(asha, "open", "correct")
	enter(bilal, "held", "int32")
	enter(bilal, "open", "int32")
	enter(bilal, "other", "correct")
	s.judgeAll()
	s.at(start, 130)

	mine := func(b *browser) map[string]map[string]any {
		t.Helper()
		code, out := b.do("GET", "/api/me/contests", nil, "")
		if code != 200 {
			t.Fatalf("my contests: %d %v", code, out)
		}
		by := map[string]map[string]any{}
		for _, c := range out["contests"].([]any) {
			row := c.(map[string]any)
			by[row["slug"].(string)] = row
		}
		return by
	}
	got := mine(asha)
	if len(got) != 2 || got["other"] != nil {
		t.Fatalf("asha's contests: %v, want held and open only", got)
	}
	if got["open"]["score"] != float64(100) || got["open"]["max_score"] != float64(100) {
		t.Errorf("open: score %v of %v, want 100 of 100", got["open"]["score"], got["open"]["max_score"])
	}
	if got["open"]["place"] != float64(1) || got["open"]["of"] != float64(2) {
		t.Errorf("open: place %v of %v, want 1 of 2", got["open"]["place"], got["open"]["of"])
	}
	if _, ok := got["held"]["place"]; ok {
		t.Errorf("held before the reveal shows a place: %v", got["held"])
	}
	if got["held"]["score"] != float64(100) {
		t.Errorf("held: her own score %v, want 100 (a student always knows their own)", got["held"]["score"])
	}
	if b := mine(bilal); len(b) != 3 || b["open"]["place"] != float64(2) {
		t.Errorf("bilal's contests: %v, want three, second in open", b)
	}

	if code, out := owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/reveal", held), nil, s.srv.URL); code != 200 {
		t.Fatalf("reveal: %d %v", code, out)
	}
	if got := mine(asha); got["held"]["place"] != float64(1) {
		t.Errorf("held after the reveal: %v, want place 1", got["held"])
	}

	// An archived contest leaves the list: its page no longer opens for students.
	if code, out := owner.do("DELETE", fmt.Sprintf("/api/admin/contests/%d", held), nil, s.srv.URL); code/100 != 2 {
		t.Fatalf("archive: %d %v", code, out)
	}
	if got := mine(asha); len(got) != 1 || got["held"] != nil {
		t.Errorf("after archiving held: %v, want open only", got)
	}
}

// Problems to finish (v2.0.2 stage U, V202-61): on /me, a contest whose
// every window is over lists the problems the student has not finished,
// from their contest score and their practice score together.

const copyPracticeChanged = "This problem was changed after the contest ended, so practice for it is closed here."

// finishRig is an ended contest "wk" with problems A, B and C. Asha scored
// 40 of 100 on A, 100 on B and never tried C.
type finishRig struct {
	s     *site
	owner *browser
	asha  *browser
	start time.Time
	cid   int64
	pids  map[string]int64
}

// newFinishRig builds the contest with the given settings on top of
// practice_after_end. With invite set, the contest is limited to invited
// students and Asha is invited. The clock ends at 121 minutes, after the close.
func newFinishRig(t *testing.T, settings map[string]any, invite bool) *finishRig {
	t.Helper()
	s := newSite(t)
	r := &finishRig{s: s, owner: s.signedIn("owner@college.edu", "Owner"), pids: map[string]int64{}}
	r.asha = s.signedIn("asha@college.edu", "Asha")
	for _, l := range []string{"A", "B", "C"} {
		r.pids[l] = s.importProblem(r.owner, "fin-"+strings.ToLower(l))
	}
	r.start = time.Now().Add(time.Hour).Truncate(time.Minute)
	cfg := map[string]any{"practice_after_end": true}
	for k, v := range settings {
		cfg[k] = v
	}
	if invite {
		cfg["audience"] = "invited"
	}
	r.cid = s.createContest(r.owner, "wk", r.start, r.pids, cfg)
	if invite {
		if code, out := r.owner.do("POST", fmt.Sprintf("/api/admin/contests/%d/invites", r.cid), map[string]any{"user_ids": []int64{r.asha.userID()}}, s.srv.URL); code != 200 {
			t.Fatalf("invite: %d %v", code, out)
		}
	}
	s.at(r.start, 5)
	if code, out := r.asha.do("POST", "/api/contests/wk/start", map[string]any{"accept_honour": true}, s.srv.URL); code != 200 {
		t.Fatalf("start: %d %v", code, out)
	}
	s.submit(r.asha, "wk", "A", "int32")
	s.submit(r.asha, "wk", "B", "correct")
	s.judgeAll()
	s.at(r.start, 121)
	return r
}

// row is Asha's row for the contest on /me.
func (r *finishRig) row() map[string]any {
	r.s.t.Helper()
	code, out := r.asha.do("GET", "/api/me/contests", nil, "")
	if code != 200 {
		r.s.t.Fatalf("my contests: %d %v", code, out)
	}
	for _, c := range out["contests"].([]any) {
		if row := c.(map[string]any); row["slug"] == "wk" {
			return row
		}
	}
	r.s.t.Fatalf("wk is not on /me: %v", out)
	return nil
}

// labels are the labels in to_finish, in order.
func (r *finishRig) labels() []string {
	r.s.t.Helper()
	row := r.row()
	list, ok := row["to_finish"].([]any)
	if !ok {
		r.s.t.Fatalf("to_finish is not a list: %v", row)
	}
	out := []string{}
	for _, p := range list {
		out = append(out, p.(map[string]any)["label"].(string))
	}
	return out
}

// item is the to_finish entry for label.
func (r *finishRig) item(label string) map[string]any {
	r.s.t.Helper()
	list, _ := r.row()["to_finish"].([]any)
	for _, p := range list {
		if m := p.(map[string]any); m["label"] == label {
			return m
		}
	}
	r.s.t.Fatalf("no %s in to_finish: %v", label, list)
	return nil
}

// replace re-imports problem label's pack with the given changes.
func (r *finishRig) replace(label string, edit func(yaml string) string) {
	r.s.t.Helper()
	slug := "fin-" + strings.ToLower(label)
	pack := repinPack(r.s.t, slug, 40, 60, 1000)
	pack["problem.yaml"] = edit(pack["problem.yaml"])
	code, out := r.owner.raw("POST", "/api/admin/problems/import?on_conflict=replace&file_name=p.zip", "application/zip", packZip(r.s.t, slug, pack, false), r.s.srv.URL)
	if code != 201 {
		r.s.t.Fatalf("replace: %d %v", code, out)
	}
	r.s.validate(r.pids[label])
}

func eqLabels(got []string, want ...string) bool { return fmt.Sprint(got) == fmt.Sprint(want) }

// 1: a partly solved problem and a never-tried one are listed, a full one is not.
func TestProblemsToFinishListsPartialAndUntried(t *testing.T) {
	r := newFinishRig(t, nil, false)
	if got := r.labels(); !eqLabels(got, "A", "C") {
		t.Fatalf("to_finish: %v, want A and C", got)
	}
	a, c := r.item("A"), r.item("C")
	if a["score"] != float64(40) || a["max"] != float64(100) || a["can_practice"] != true || a["title"] != "A plus B (fin-a)" {
		t.Errorf("A: %v", a)
	}
	if c["score"] != float64(0) || c["max"] != float64(100) || c["can_practice"] != true {
		t.Errorf("C: %v", c)
	}
}

// 2: a problem later solved in practice leaves the list. The contest score
// alone cannot see it: the board never reads practice rows.
func TestProblemsToFinishDropsAProblemSolvedInPractice(t *testing.T) {
	r := newFinishRig(t, nil, false)
	if code, out := r.s.submit(r.asha, "wk", "A", "correct"); code != 202 {
		t.Fatalf("practice submit: %d %v", code, out)
	}
	if got := r.labels(); !eqLabels(got, "C") {
		t.Errorf("to_finish after solving A in practice: %v, want C only", got)
	}
	if row := r.row(); row["score"] != float64(140) || row["max_score"] != float64(300) {
		t.Errorf("the contest score moved: %v of %v, want 140 of 300", row["score"], row["max_score"])
	}
	// A partly better practice try keeps the problem listed with the better score.
	if code, out := r.s.submit(r.asha, "wk", "C", "int32"); code != 202 {
		t.Fatalf("practice submit C: %d %v", code, out)
	}
	if c := r.item("C"); c["score"] != float64(40) {
		t.Errorf("C after a 40 point practice try: %v", c)
	}
}

// 3: live, frozen, upcoming and before-Final contests give an empty list.
func TestProblemsToFinishEmptyUntilEveryWindowIsOver(t *testing.T) {
	r := newFinishRig(t, map[string]any{"freeze_minutes": 30}, false)
	bilal := r.s.signedIn("bilal@college.edu", "Bilal")
	r.s.at(r.start, 5)
	if code, out := bilal.do("POST", "/api/contests/wk/start", map[string]any{"accept_honour": true}, r.s.srv.URL); code != 200 {
		t.Fatalf("bilal start: %d %v", code, out)
	}
	if code, out := r.owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/extensions/%d", r.cid, bilal.userID()),
		map[string]any{"minutes": 30, "reason": "accommodation"}, r.s.srv.URL); code != 200 {
		t.Fatalf("extension: %d %v", code, out)
	}
	for _, c := range []struct {
		when float64
		what string
	}{{5, "live"}, {100, "frozen"}, {-10, "upcoming"}, {125, "before Final (a classmate's extension)"}} {
		r.s.at(r.start, c.when)
		if got := r.labels(); len(got) != 0 {
			t.Errorf("%s: to_finish %v, want empty", c.what, got)
		}
		if r.row()["editorial"] != false {
			t.Errorf("%s: editorial %v", c.what, r.row()["editorial"])
		}
	}
	r.s.at(r.start, 151)
	if got := r.labels(); !eqLabels(got, "A", "C") {
		t.Errorf("after Final: %v, want A and C", got)
	}
}

// 4: with the reveal held, the list shows and the place stays hidden.
func TestProblemsToFinishShowsWhileTheRevealIsHeld(t *testing.T) {
	r := newFinishRig(t, map[string]any{"freeze_minutes": 30, "hold_reveal": true}, false)
	if got := r.labels(); !eqLabels(got, "A", "C") {
		t.Errorf("held reveal: %v, want A and C", got)
	}
	if _, ok := r.row()["place"]; ok {
		t.Errorf("held reveal shows a place: %v", r.row())
	}
}

// 5: the editorial flag follows publishing and Final().
func TestProblemsToFinishEditorialFlag(t *testing.T) {
	r := newFinishRig(t, nil, false)
	put := func(published bool) {
		t.Helper()
		if code, out := r.owner.do("PUT", fmt.Sprintf("/api/admin/contests/%d/editorial", r.cid), map[string]any{"editorial_md": "Add them.", "published": published}, r.s.srv.URL); code != 200 {
			t.Fatalf("editorial: %d %v", code, out)
		}
	}
	put(false)
	if r.row()["editorial"] != false {
		t.Errorf("unpublished: %v", r.row()["editorial"])
	}
	put(true)
	r.s.at(r.start, 60)
	if r.row()["editorial"] != false {
		t.Errorf("published, before Final: %v", r.row()["editorial"])
	}
	r.s.at(r.start, 121)
	if r.row()["editorial"] != true {
		t.Errorf("published, after Final: %v", r.row()["editorial"])
	}
}

// 6: after a judging edit the row stays, practice is closed, and the
// workspace says why with the 409 copy.
func TestProblemsToFinishAfterAJudgingEdit(t *testing.T) {
	r := newFinishRig(t, nil, false)
	r.replace("A", func(y string) string { return strings.Replace(y, "time_limit_ms: 1000", "time_limit_ms: 2000", 1) })
	if got := r.labels(); !eqLabels(got, "A", "C") {
		t.Fatalf("after the edit: %v, want A and C", got)
	}
	if a := r.item("A"); a["can_practice"] != false || a["score"] != float64(40) {
		t.Errorf("A after the edit: %v", a)
	}
	if c := r.item("C"); c["can_practice"] != true {
		t.Errorf("C: %v", c)
	}
	_, ws := r.asha.do("GET", "/api/contests/wk/problems/A", nil, "")
	if ws["can_submit"] != false || ws["refusal"] != copyPracticeChanged {
		t.Errorf("workspace: can_submit=%v refusal=%v", ws["can_submit"], ws["refusal"])
	}
	for _, step := range []string{"submit", "run"} {
		code, out := r.asha.do("POST", "/api/contests/wk/problems/A/"+step, map[string]any{"language": "cpp17", "source": "correct", "client_key": "k-" + step}, r.s.srv.URL)
		errIs(t, step, code, out, 409, "practice_changed", copyPracticeChanged)
	}
	// Before the contest ends the old copy stays: a live student can try again.
	r.s.at(r.start, 30)
	_, ws = r.asha.do("GET", "/api/contests/wk/problems/A", nil, "")
	if ws["refusal"] == copyPracticeChanged {
		t.Errorf("a live contest says practice is closed: %v", ws["refusal"])
	}
}

// 7: a title edit leaves the pinned title.
func TestProblemsToFinishKeepsThePinnedTitle(t *testing.T) {
	r := newFinishRig(t, nil, false)
	r.replace("A", func(y string) string { return strings.Replace(y, "title: A plus B (fin-a)", "title: Renamed", 1) })
	if a := r.item("A"); a["title"] != "A plus B (fin-a)" {
		t.Errorf("title after an edit: %v, want the pinned one", a["title"])
	}
}

// 8: a deleted problem is left out, not labelled.
func TestProblemsToFinishLeavesOutADeletedProblem(t *testing.T) {
	r := newFinishRig(t, nil, false)
	if code, out := r.owner.do("POST", fmt.Sprintf("/api/admin/problems/%d/delete", r.pids["A"]), nil, r.s.srv.URL); code != 204 {
		t.Fatalf("delete: %d %v", code, out)
	}
	if got := r.labels(); !eqLabels(got, "C") {
		t.Errorf("after deleting A: %v, want C only", got)
	}
}

// 9: a withdrawn invite gives no problem rows; the rest of the row stays.
func TestProblemsToFinishNeedsAccessStill(t *testing.T) {
	r := newFinishRig(t, nil, true)
	if got := r.labels(); !eqLabels(got, "A", "C") {
		t.Fatalf("invited: %v, want A and C", got)
	}
	before := r.row()
	if _, err := r.s.db.Exec("DELETE FROM contest_invites WHERE user_id = ?", r.asha.userID()); err != nil {
		t.Fatal(err)
	}
	after := r.row()
	if len(after["to_finish"].([]any)) != 0 || after["editorial"] != false {
		t.Errorf("withdrawn invite: %v", after)
	}
	for _, k := range []string{"slug", "title", "starts_at", "state", "score", "max_score", "place", "of"} {
		if fmt.Sprint(before[k]) != fmt.Sprint(after[k]) {
			t.Errorf("%s changed with the invite: %v, now %v", k, before[k], after[k])
		}
	}
}

// 10: old client: every existing field is there with its value.
func TestProblemsToFinishKeepsTheOldFields(t *testing.T) {
	r := newFinishRig(t, nil, false)
	row := r.row()
	want := map[string]any{"slug": "wk", "title": "wk", "starts_at": float64(r.start.UnixMilli()), "state": "practice", "score": float64(140), "max_score": float64(300), "place": float64(1), "of": float64(1)}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("%s: %v, want %v", k, row[k], v)
		}
	}
	if len(row) != len(want)+2 {
		t.Errorf("row keys: %v, want the old ones plus to_finish and editorial", row)
	}
}

// 11: the board and MyScore are byte-identical with and without practice rows.
func TestProblemsToFinishLeavesTheBoardAlone(t *testing.T) {
	r := newFinishRig(t, nil, false)
	ctx := context.Background()
	snap := func() string {
		c, err := r.s.contests.Get(ctx, r.cid)
		if err != nil {
			t.Fatal(err)
		}
		board, err := r.s.contests.BoardNow(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		st, err := r.s.contests.State(ctx, c, r.asha.userID())
		if err != nil {
			t.Fatal(err)
		}
		total, cells, err := r.s.contests.MyScore(ctx, c, st, r.asha.userID())
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal([]any{board.Rows, total, cells})
		return string(b)
	}
	before := snap()
	r.s.submit(r.asha, "wk", "A", "correct")
	r.s.submit(r.asha, "wk", "C", "correct")
	if after := snap(); after != before {
		t.Errorf("practice rows moved the board or MyScore:\n%s\n%s", before, after)
	}
}

// With practice off the problem is still listed, without a practice link.
func TestProblemsToFinishWithPracticeOff(t *testing.T) {
	r := newFinishRig(t, map[string]any{"practice_after_end": false}, false)
	if got := r.labels(); !eqLabels(got, "A", "C") {
		t.Fatalf("practice off: %v, want A and C", got)
	}
	if a := r.item("A"); a["can_practice"] != false {
		t.Errorf("A: %v", a)
	}
	if r.row()["state"] != "ended" {
		t.Errorf("state: %v", r.row()["state"])
	}
}
