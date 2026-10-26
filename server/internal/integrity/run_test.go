package integrity

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/contests"
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

func TestDecideRecordsOneDecisionPerStudent(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.submit(t, 7, "cpp17", "int main(){}", 100, nil)
	by := contests.Actor{ID: 100, Email: "o@college.edu", Role: "admin"}
	if _, err := r.svc.Decide(ctx, r.contest, 7, by, "excluded", "short"); err == nil {
		t.Error("a short reason was accepted")
	}
	if _, err := r.svc.Decide(ctx, r.contest, 7, by, "zero", "The two programs match line for line."); err == nil {
		t.Error("an unknown decision was accepted")
	}
	if _, err := r.svc.Decide(ctx, r.contest, 3, by, "warning", "Student 3 never took part in it."); !errors.Is(err, ErrNotFound) {
		t.Errorf("decision about a non-participant: %v", err)
	}
	c, err := r.svc.Decide(ctx, r.contest, 7, by, "excluded", "The two programs match line for line.")
	if err != nil || c.Decision != "excluded" || c.DecidedBy != "o@college.edu" || c.Name != "Student 7" {
		t.Fatalf("decide: %+v %v", c, err)
	}
	if _, err := r.svc.Decide(ctx, r.contest, 7, by, "no_action", "The student showed their earlier drafts."); err != nil {
		t.Fatal(err)
	}
	cases, err := r.svc.Cases(ctx, r.contest)
	if err != nil || len(cases) != 1 || cases[0].Decision != "no_action" {
		t.Fatalf("one case per student, the latest decision: %+v %v", cases, err)
	}
	var audits int
	r.db.QueryRow("SELECT count(*) FROM contest_audit WHERE contest_id = ? AND action = 'integrity_decision'", r.contest).Scan(&audits)
	if audits != 2 {
		t.Errorf("%d audit rows, want one per decision", audits)
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
