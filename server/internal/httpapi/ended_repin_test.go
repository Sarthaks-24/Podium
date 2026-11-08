package httpapi

// PF-10 (v2.0.2 stage 0, V202-66): "Update students now" on a contest whose
// windows are over would rewrite its final scores, so it first answers 409
// ended_scores_change and changes nothing; the same request with
// confirm_ended applies it and writes one ended_repin audit row.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// repinPack is a-plus-b's problem.yaml under slug with the subtask points
// and time limit given.
func repinPack(t *testing.T, slug string, p1, p2, limitMS int) map[string]string {
	t.Helper()
	b, err := os.ReadFile("../../../problems/a-plus-b/problem.yaml")
	if err != nil {
		t.Fatal(err)
	}
	y := strings.NewReplacer("slug: a-plus-b", "slug: "+slug, "title: A plus B", "title: A plus B ("+slug+")",
		"time_limit_ms: 1000", fmt.Sprintf("time_limit_ms: %d", limitMS),
		"points: 40", fmt.Sprintf("points: %d", p1), "points: 60", fmt.Sprintf("points: %d", p2)).Replace(string(b))
	return map[string]string{"problem.yaml": y}
}

// repinRig is a contest with one problem, A, pinned at 40 and 60 points:
// Asha solved it fully and Bilal only opened the contest.
type repinRig struct {
	s     *site
	owner *browser
	pid   int64
	cid   int64
	start time.Time
	slug  string
}

func newRepinRig(t *testing.T) *repinRig {
	s := newSite(t)
	r := &repinRig{s: s, owner: s.signedIn("owner@college.edu", "Owner"), slug: "pf10"}
	r.pid = s.importProblem(r.owner, r.slug)
	r.start = time.Now().Add(time.Hour).Truncate(time.Minute)
	r.cid = s.createContest(r.owner, "pf10-contest", r.start, map[string]int64{"A": r.pid}, nil)
	asha, bilal := s.signedIn("asha@college.edu", "Asha"), s.signedIn("bilal@college.edu", "Bilal")
	s.at(r.start, 5)
	for _, b := range []*browser{asha, bilal} {
		b.do("POST", "/api/contests/pf10-contest/start", map[string]any{"accept_honour": true}, s.srv.URL)
	}
	if code, out := s.submit(asha, "pf10-contest", "A", "correct"); code != 202 {
		t.Fatalf("submit: %d %v", code, out)
	}
	return r
}

// edit replaces the problem with a pack of the given points and limit and
// marks it validated, as a passing validation would.
func (r *repinRig) edit(p1, p2, limitMS int) {
	r.s.t.Helper()
	pack := packZip(r.s.t, r.slug, repinPack(r.s.t, r.slug, p1, p2, limitMS), false)
	code, out := r.owner.raw("POST", "/api/admin/problems/import?on_conflict=replace&file_name=p.zip", "application/zip", pack, r.s.srv.URL)
	if code != 201 {
		r.s.t.Fatalf("replace: %d %v", code, out)
	}
	r.s.validate(r.pid)
}

func (r *repinRig) latestRevision() float64 {
	r.s.t.Helper()
	_, out := r.owner.do("GET", fmt.Sprintf("/api/admin/contests/%d", r.cid), nil, r.s.srv.URL)
	return out["problems"].([]any)[0].(map[string]any)["latest_revision_id"].(float64)
}

func (r *repinRig) repin(b *browser, extra map[string]any) (int, map[string]any) {
	body := map[string]any{"revision_id": r.latestRevision()}
	for k, v := range extra {
		body[k] = v
	}
	return b.do("PUT", fmt.Sprintf("/api/admin/contests/%d/problems/A/revision", r.cid), body, r.s.srv.URL)
}

// state is what a refused update must not touch: the pinned points and
// revision, the audit rows and the final board, as bytes.
func (r *repinRig) state() string {
	r.s.t.Helper()
	var points, audit string
	var rev int64
	if err := r.s.db.QueryRow("SELECT points, revision_id FROM contest_problems WHERE contest_id = ?", r.cid).Scan(&points, &rev); err != nil {
		r.s.t.Fatal(err)
	}
	if err := r.s.db.QueryRow("SELECT coalesce(group_concat(action || ':' || details, '|'), '') FROM contest_audit WHERE contest_id = ? AND action IN ('revision', 'ended_repin')", r.cid).Scan(&audit); err != nil {
		r.s.t.Fatal(err)
	}
	c, err := r.s.contests.Get(context.Background(), r.cid)
	if err != nil {
		r.s.t.Fatal(err)
	}
	board, err := r.s.contests.BoardNow(context.Background(), c)
	if err != nil {
		r.s.t.Fatal(err)
	}
	b, _ := json.Marshal(board.Rows)
	return fmt.Sprintf("%s %d %s %s", points, rev, audit, b)
}

func (r *repinRig) auditRows(action string) int {
	r.s.t.Helper()
	var n int
	if err := r.s.db.QueryRow("SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = ?", r.cid, action).Scan(&n); err != nil {
		r.s.t.Fatal(err)
	}
	return n
}

func (r *repinRig) totals() map[string]float64 {
	r.s.t.Helper()
	c, _ := r.s.contests.Get(context.Background(), r.cid)
	board, err := r.s.contests.BoardNow(context.Background(), c)
	if err != nil {
		r.s.t.Fatal(err)
	}
	out := map[string]float64{}
	for _, row := range board.Rows {
		out[row.Name] = float64(row.Total)
	}
	return out
}

// (1) An ended contest where the update would change points: 409 with the
// problems and the number of students, nothing written.
func TestEndedRepinAsksBeforeChangingScores(t *testing.T) {
	r := newRepinRig(t)
	r.s.at(r.start, 125)
	r.edit(30, 70, 1000)
	before := r.state()
	code, out := r.repin(r.owner, nil)
	e, _ := out["error"].(map[string]any)
	if code != 409 || e["code"] != "ended_scores_change" {
		t.Fatalf("repin on an ended contest: %d %v, want 409 ended_scores_change", code, out)
	}
	ex, _ := e["existing"].(map[string]any)
	probs, _ := ex["problems"].([]any)
	if len(probs) != 1 || probs[0].(map[string]any)["label"] != "A" || ex["students"] != float64(1) {
		t.Errorf("existing %v, want problem A and 1 student (Bilal has no score to change)", ex)
	}
	if p, _ := probs[0].(map[string]any); fmt.Sprint(p["from"]) != "[40 60]" || fmt.Sprint(p["to"]) != "[30 70]" {
		t.Errorf("points %v to %v, want [40 60] to [30 70]", p["from"], p["to"])
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "ended") {
		t.Errorf("message %q does not say the contest ended", msg)
	}
	if after := r.state(); after != before {
		t.Errorf("a refused update changed state:\nbefore %s\nafter  %s", before, after)
	}
	if got := r.totals()["Asha"]; got != 100 {
		t.Errorf("Asha's total after the refusal: %v, want 100", got)
	}
}

// (2) confirm_ended applies it and writes exactly one audit row.
func TestEndedRepinConfirmedAppliesOnce(t *testing.T) {
	r := newRepinRig(t)
	r.s.at(r.start, 125)
	r.edit(30, 70, 1000)
	code, out := r.repin(r.owner, map[string]any{"confirm_ended": true})
	if code != 200 {
		t.Fatalf("confirmed repin: %d %v", code, out)
	}
	var points string
	r.s.db.QueryRow("SELECT points FROM contest_problems WHERE contest_id = ?", r.cid).Scan(&points)
	if points != "[30,70]" {
		t.Errorf("points %s, want [30,70]", points)
	}
	var rows int
	r.s.db.QueryRow("SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action IN ('revision', 'ended_repin')", r.cid).Scan(&rows)
	if rows != 1 || r.auditRows("ended_repin") != 1 {
		t.Errorf("%d audit rows, %d ended_repin; want exactly one, ended_repin", rows, r.auditRows("ended_repin"))
	}
	if got := r.totals()["Asha"]; got != 90 {
		t.Errorf("Asha's total after the update: %v, want 90 (40 of 30, 60 of 70)", got)
	}
}

// (3) An ended contest where no points change works as today.
func TestEndedRepinWithNoPointChangeApplies(t *testing.T) {
	r := newRepinRig(t)
	r.s.at(r.start, 125)
	r.edit(40, 60, 2000)
	code, out := r.repin(r.owner, nil)
	if code != 200 {
		t.Fatalf("repin with no points change: %d %v", code, out)
	}
	if r.auditRows("revision") != 1 || r.auditRows("ended_repin") != 0 {
		t.Errorf("audit: %d revision, %d ended_repin; want the usual revision row only", r.auditRows("revision"), r.auditRows("ended_repin"))
	}
	if got := r.totals()["Asha"]; got != 100 {
		t.Errorf("Asha's total: %v, want 100", got)
	}
}

// (4) A live or upcoming contest is unchanged: points move at once and the
// row is the usual one.
func TestRepinBeforeTheEndIsUnchanged(t *testing.T) {
	for _, minutes := range []float64{60, -30} {
		r := newRepinRig(t)
		// The bank refuses a replace while a contest runs, so the problem is
		// edited first and the clock moves on before the update.
		r.s.at(r.start, -30)
		r.edit(30, 70, 1000)
		r.s.at(r.start, minutes)
		code, out := r.repin(r.owner, nil)
		if code != 200 {
			t.Fatalf("repin at minute %v: %d %v", minutes, code, out)
		}
		if r.auditRows("revision") != 1 || r.auditRows("ended_repin") != 0 {
			t.Errorf("minute %v: %d revision, %d ended_repin rows", minutes, r.auditRows("revision"), r.auditRows("ended_repin"))
		}
	}
}

// (5) A student cannot call it, with or without confirm_ended, and nothing
// changes.
func TestEndedRepinRefusesStudents(t *testing.T) {
	r := newRepinRig(t)
	r.s.at(r.start, 125)
	r.edit(30, 70, 1000)
	before := r.state()
	asha := r.s.signedIn("asha@college.edu", "Asha")
	for _, extra := range []map[string]any{nil, {"confirm_ended": true}} {
		if code, out := r.repin(asha, extra); code != 403 {
			t.Errorf("student repin %v: %d %v, want 403", extra, code, out)
		}
	}
	if after := r.state(); after != before {
		t.Errorf("a student's request changed state")
	}
}

// Update students now on an AI-edited problem nobody confirmed is a 400 with
// the wording SetProblems uses, and writes nothing.
func TestRepinRefusesAnUnconfirmedAIDraft(t *testing.T) {
	r := newRepinRig(t)
	r.s.at(r.start, -30)
	r.edit(40, 60, 2000)
	r.s.at(r.start, -10)
	if _, err := r.s.db.Exec(`UPDATE problems SET tags = '["ai draft"]' WHERE id = ?`, r.pid); err != nil {
		t.Fatal(err)
	}
	before := r.state()
	code, out := r.repin(r.owner, nil)
	msg, _ := out["error"].(map[string]any)["message"].(string)
	if code != 400 || !strings.Contains(msg, "The problem for label A is an AI draft. Read it and save it in the editor before adding it to a contest.") {
		t.Fatalf("repin an AI draft: %d %v", code, out)
	}
	if r.state() != before {
		t.Error("the refused update changed the contest")
	}
}
