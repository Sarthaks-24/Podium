package integrity

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/problems"
)

type rig struct {
	svc     *Service
	db      *sql.DB
	problem *problems.Problem
	contest int64
	nextJob int64
	// clock is the time the service sees; tests move it.
	clock *time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_800_000_000_000)
	clock := &now
	tick := func() time.Time { return *clock }
	bank := &problems.Service{DB: d, Blobs: problems.Blobs{Dir: filepath.Join(t.TempDir(), "tests")}, Now: tick}
	pk, err := problems.ReadPack(zipDir(t, "../../../problems/a-plus-b"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := bank.Import(ctx, pk, 0)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, d, `INSERT INTO users (id, email, name, role, created_at) VALUES (100, 'o@college.edu', 'Organiser', 'admin', 0)`)
	for u := 1; u <= 12; u++ {
		mustExec(t, d, `INSERT INTO users (id, email, name, role, created_at) VALUES (?, ?, ?, 'student', 0)`,
			u, "s"+itoa(u)+"@college.edu", "Student "+itoa(u))
	}
	res := mustExec(t, d, `INSERT INTO contests (slug, title, starts_at, ends_at, created_at, updated_at) VALUES ('c', 'C', 0, 1, 0, 0)`)
	cid, _ := res.LastInsertId()
	mustExec(t, d, `INSERT INTO contest_problems (contest_id, problem_id, label, ord) VALUES (?, ?, 'A', 0)`, cid, p.ID)
	got, err := bank.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &rig{svc: &Service{DB: d, Problems: bank, Now: tick}, db: d, problem: got, contest: cid, clock: clock}
}

func mustExec(t *testing.T, d *sql.DB, q string, args ...any) sql.Result {
	t.Helper()
	res, err := d.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return res
}

func zipDir(t *testing.T, dir string) []byte {
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

// submit stores a judged submission with per-test verdicts and output
// hashes on the hidden tests (verdicts default to AC).
func (r *rig) submit(t *testing.T, user int64, lang, src string, score int, tests map[int][2]string) int64 {
	t.Helper()
	r.nextJob++
	mustExec(t, r.db, `INSERT INTO jobs (id, kind, priority, payload, created_at, state) VALUES (?, 'submit', 20, '{}', 0, 'done')`, r.nextJob)
	res := mustExec(t, r.db, `INSERT INTO submissions (job_id, problem_id, contest_id, user_id, kind, language, source, source_sha,
		state, verdict, score, max_score, created_at) VALUES (?, ?, ?, ?, 'submit', ?, ?, 'sha', 'done', ?, ?, 100, ?)`,
		r.nextJob, r.problem.ID, r.contest, user, lang, src, map[bool]string{true: "AC", false: "WA"}[score == 100], score, r.nextJob)
	id, _ := res.LastInsertId()
	seq := 0
	for _, tc := range r.problem.Tests {
		v := [2]string{"AC", "ok" + itoa(tc.Idx)}
		if got, ok := tests[tc.Idx]; ok {
			v = got
		}
		seq++
		mustExec(t, r.db, `INSERT INTO job_results (job_id, seq, kind, idx, role, verdict, output_sha) VALUES (?, ?, 'test', ?, 'program', ?, ?)`,
			r.nextJob, seq, tc.Idx, v[0], v[1])
	}
	return id
}

func (r *rig) hiddenTests() []int {
	samples := map[int]bool{}
	for _, s := range r.problem.Samples {
		samples[s] = true
	}
	var out []int
	for _, tc := range r.problem.Tests {
		if !samples[tc.Idx] {
			out = append(out, tc.Idx)
		}
	}
	return out
}

func TestCheckFindsEachKindOfSignal(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	hidden := r.hiddenTests()
	if len(hidden) < 3 {
		t.Fatalf("a-plus-b has %d hidden tests", len(hidden))
	}
	// Calibration programs: independent students and disguised copies.
	for id, s := range independent {
		r.submit(t, id, s.Language, s.Source, 100, nil)
	}
	for id, c := range copies {
		r.submit(t, id, c.sub.Language, c.sub.Source, 100, nil)
	}
	// Students 10 and 11 print the same wrong output on three hidden tests.
	wrong := map[int][2]string{}
	for _, idx := range hidden[:3] {
		wrong[idx] = [2]string{"WA", "shared-wrong-" + itoa(idx)}
	}
	r.submit(t, 10, "cpp17", "int main(){return 0;}", 40, wrong)
	r.submit(t, 11, "python3", "print(0)", 40, wrong)
	// Student 12 pasted a hidden expected output into the program.
	var expected string
	for _, tc := range r.problem.Tests {
		if tc.Idx == hidden[len(hidden)-1] {
			b, err := r.svc.Problems.Blobs.Read(tc.OutputSHA, 4096)
			if err != nil {
				t.Fatal(err)
			}
			expected = string(b)
		}
	}
	r.submit(t, 12, "python3", "print('''"+expected+"''')", 100, nil)
	// An organiser's own submission is never evidence.
	r.submit(t, 100, "python3", "print('''"+expected+"''')", 100, nil)

	runID, err := r.svc.Start(ctx, r.contest, 100)
	if err != nil {
		t.Fatal(err)
	}
	var run *Run
	for i := 0; i < 200; i++ {
		run, err = r.svc.Latest(ctx, r.contest)
		if err == nil && run.State != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run == nil || run.ID != runID || run.State != "done" {
		t.Fatalf("run %+v err %v", run, err)
	}
	kinds := map[string][][2]int64{}
	for _, sg := range run.Signals {
		kinds[sg.Kind] = append(kinds[sg.Kind], [2]int64{sg.UserA, sg.UserB})
		if sg.UserA == 100 || sg.UserB == 100 {
			t.Errorf("an organiser appears in a signal: %+v", sg)
		}
	}
	if len(kinds["similar"]) != 3 {
		t.Errorf("similar pairs %v, want the 3 planted copies", kinds["similar"])
	}
	if len(kinds["wrong_output"]) != 1 || kinds["wrong_output"][0] != [2]int64{10, 11} {
		t.Errorf("identical wrong outputs %v", kinds["wrong_output"])
	}
	if len(kinds["hardcoded"]) != 1 || kinds["hardcoded"][0][0] != 12 {
		t.Errorf("hard-coded %v", kinds["hardcoded"])
	}
	if run.Stats.Students != 12 || run.Stats.Clusters != 3 {
		t.Errorf("stats %+v", run.Stats)
	}

	// A second check while one runs is refused.
	r.svc.mu.Lock()
	r.svc.running = true
	r.svc.mu.Unlock()
	if _, err := r.svc.Start(ctx, r.contest, 100); !errors.Is(err, ErrBusy) {
		t.Errorf("second check: %v", err)
	}
	r.svc.done()

	// The pair view returns both programs with matched spans.
	var similar Signal
	for _, sg := range run.Signals {
		if sg.Kind == "similar" {
			similar = sg
			break
		}
	}
	a, b, err := r.svc.Pair(ctx, r.contest, similar.ID)
	if err != nil || a.Source == "" || b.Source == "" || len(a.Spans) == 0 || len(b.Spans) == 0 {
		t.Fatalf("pair: %v %+v %+v", err, a, b)
	}
	if _, _, err := r.svc.Pair(ctx, r.contest+1, similar.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("pair from another contest: %v", err)
	}
}

func TestCaseLifecycle(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for id, c := range copies {
		r.submit(t, id, c.sub.Language, c.sub.Source, 100, nil)
		s := independent[c.of]
		r.submit(t, c.of, s.Language, s.Source, 100, nil)
	}
	if err := r.svc.Check(ctx, insertRun(t, r), r.contest); err != nil {
		t.Fatal(err)
	}
	run, _ := r.svc.Latest(ctx, r.contest)
	var sg Signal
	for _, s := range run.Signals {
		if s.Kind == "similar" && (s.UserA == 7 || s.UserB == 7) {
			sg = s
		}
	}
	if sg.ID == 0 {
		t.Fatalf("no signal for student 7: %+v", run.Signals)
	}
	if _, err := r.svc.OpenCase(ctx, r.contest, 100, "Problem A, students 2 and 7", "too short", []int64{sg.ID}); err == nil {
		t.Error("a short reason was accepted")
	}
	c, err := r.svc.OpenCase(ctx, r.contest, 100, "Problem A, students 2 and 7", "The two programs match almost line for line.", []int64{sg.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Users) != 2 || c.State != Open || len(c.Decisions) != 1 {
		t.Fatalf("case %+v", c)
	}
	// Action before the students could explain is refused.
	if _, err := r.svc.Decide(ctx, c.ID, 100, Actioned, "Copying is clear from the matched code.", nil); err == nil {
		t.Error("actioned before sharing")
	}
	// Nothing is visible to the student until the case is shared.
	if mine, _ := r.svc.MyCases(ctx, 7); len(mine) != 0 {
		t.Errorf("unshared case visible: %+v", mine)
	}
	if err := r.svc.Explain(ctx, c.ID, 7, "I wrote it myself."); !errors.Is(err, ErrNotFound) {
		t.Errorf("explained an unshared case: %v", err)
	}
	if _, err := r.svc.Decide(ctx, c.ID, 100, AwaitingStudent, "Please explain how you wrote this solution.", nil); err != nil {
		t.Fatal(err)
	}
	mine, err := r.svc.MyCases(ctx, 7)
	if err != nil || len(mine) != 1 || len(mine[0].Evidence) != 1 {
		t.Fatalf("student view %+v %v", mine, err)
	}
	// The reviewer's title names the students; the student sees the
	// contest's title instead.
	if mine[0].Title != "C" {
		t.Errorf("student sees title %q, want the contest's", mine[0].Title)
	}
	ev := mine[0].Evidence[0]
	js, _ := json.Marshal(mine)
	if strings.Contains(string(js), "students 2 and 7") {
		t.Errorf("the case title reached the student: %s", js)
	}
	if ev.Source == "" || len(ev.Spans) == 0 || strings.Contains(string(js), "Student 2") || strings.Contains(string(js), "s2@") {
		t.Errorf("student view leaks or lacks evidence: %s", js)
	}
	// The other student's code never reaches student 7.
	if strings.Contains(ev.Source, "MAXN") {
		t.Error("student 7 sees student 2's program")
	}
	if err := r.svc.Explain(ctx, c.ID, 7, "We studied together and I used the same approach."); err != nil {
		t.Fatal(err)
	}
	// Someone not in the case cannot explain it.
	if err := r.svc.Explain(ctx, c.ID, 3, "Not mine."); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider explained: %v", err)
	}
	c, err = r.svc.Decide(ctx, c.ID, 100, Cleared, "The explanation is consistent with the timeline.", seenAll(t, r, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	if c.State != Cleared || len(c.Decisions) != 3 || c.Users[1].Explanation == "" {
		t.Errorf("decided case %+v", c)
	}
	// After the decision the explanation is final.
	if err := r.svc.Explain(ctx, c.ID, 7, "Changed my mind."); !errors.Is(err, ErrNotFound) {
		t.Errorf("explanation changed after the decision: %v", err)
	}
	// A re-run replaces signals but the case keeps its evidence.
	if err := r.svc.Check(ctx, insertRun(t, r), r.contest); err != nil {
		t.Fatal(err)
	}
	again, _ := r.svc.GetCase(ctx, c.ID)
	if string(again.Evidence) != string(c.Evidence) {
		t.Error("a re-run changed the case's evidence")
	}
}

// Two years after a case closes its evidence and explanations are
// emptied; the decision log stays. Open cases are never touched.
func TestRetainEmptiesOldClosedCases(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	day := int64(24 * time.Hour / time.Millisecond)
	now := r.svc.now().UnixMilli()
	for _, c := range []struct {
		id     int64
		state  string
		closed any
	}{{1, Cleared, now - 800*day}, {2, Actioned, now - 100*day}, {3, AwaitingStudent, nil}} {
		mustExec(t, r.db, `INSERT INTO integrity_cases (id, contest_id, state, title, evidence, created_at, updated_at, closed_at)
			VALUES (?, ?, ?, 'Case', '[{"id":1}]', 0, 0, ?)`, c.id, r.contest, c.state, c.closed)
		mustExec(t, r.db, `INSERT INTO integrity_case_users (case_id, user_id, shared_at, explanation) VALUES (?, 1, 0, 'Mine.')`, c.id)
		mustExec(t, r.db, `INSERT INTO integrity_explanations (case_id, user_id, text, created_at) VALUES (?, 1, 'Mine.', 0)`, c.id)
		mustExec(t, r.db, `INSERT INTO integrity_decisions (case_id, by_user, state, note, created_at) VALUES (?, 100, ?, 'A reason of enough length.', 0)`,
			c.id, c.state)
	}
	n, err := r.svc.Retain(ctx)
	if err != nil || n != 1 {
		t.Fatalf("retain: %d %v", n, err)
	}
	old, _ := r.svc.GetCase(ctx, 1)
	if string(old.Evidence) != "[]" || old.Users[0].Explanation != "" || len(old.Users[0].Explanations) != 0 || len(old.Decisions) != 1 {
		t.Errorf("old case after retention: %+v", old)
	}
	for _, id := range []int64{2, 3} {
		c, _ := r.svc.GetCase(ctx, id)
		if string(c.Evidence) == "[]" || c.Users[0].Explanation == "" || len(c.Users[0].Explanations) != 1 {
			t.Errorf("case %d emptied too early", id)
		}
	}
	if n, _ := r.svc.Retain(ctx); n != 0 {
		t.Errorf("second run emptied %d", n)
	}
}

// Recover marks a check left running by a stopped server as failed.
func TestRecoverFailsStaleRuns(t *testing.T) {
	r := newRig(t)
	insertRun(t, r)
	if n, err := r.svc.Recover(context.Background()); err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	run, _ := r.svc.Latest(context.Background(), r.contest)
	if run.State != "error" || run.FinishedAt == nil || run.Error == "" {
		t.Errorf("run after recover: %+v", run)
	}
}

func insertRun(t *testing.T, r *rig) int64 {
	res := mustExec(t, r.db, `INSERT INTO integrity_runs (contest_id, started_by, started_at) VALUES (?, 100, 0)`, r.contest)
	id, _ := res.LastInsertId()
	return id
}

func TestTimelineListsEventsAndSharedDevices(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	s1 := r.submit(t, 1, "cpp17", "int main(){}", 0, nil)
	s2 := r.submit(t, 2, "cpp17", "int main(){}", 0, nil)
	mustExec(t, r.db, "UPDATE submissions SET client_hash = 'h', pasted_chars = 300 WHERE id IN (?, ?)", s1, s2)
	mustExec(t, r.db, `INSERT INTO integrity_events (user_id, problem_id, contest_id, kind, size, created_at) VALUES (1, ?, ?, 'paste', 300, 5)`,
		r.problem.ID, r.contest)
	tl, err := r.svc.Timeline(ctx, r.contest, 1)
	if err != nil {
		t.Fatal(err)
	}
	var text []string
	for _, m := range tl {
		text = append(text, m.Kind+": "+m.Text)
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{"paste: Pasted 300 characters.", "shared_device: Same browser and network as Student 2.", "pasted 300 characters"} {
		if !strings.Contains(all, want) {
			t.Errorf("timeline lacks %q:\n%s", want, all)
		}
	}
}
