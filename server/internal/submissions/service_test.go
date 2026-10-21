package submissions

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/db"
	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

type rig struct {
	svc   *Service
	bank  *problems.Service
	jobs  *jobs.Service
	hub   *events.Hub
	clock *time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO users (id, email, role, created_at) VALUES (1, 'a@college.edu', 'admin', 0), (2, 'b@college.edu', 'admin', 0)`); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_800_000_000_000)
	clock := &now
	tick := func() time.Time { return *clock }
	blobs := problems.Blobs{Dir: filepath.Join(t.TempDir(), "tests")}
	j := &jobs.Service{DB: d, Now: tick, Kinds: map[string]jobs.Kind{
		judgeapi.KindValidate: problems.ValidateKind{Now: tick},
		judgeapi.KindSubmit:   SubmitKind{Now: tick},
		judgeapi.KindRun:      RunKind{Now: tick},
		judgeapi.KindGenerate: problems.GenerateKind{Store: blobs, Now: tick},
	}}
	bank := &problems.Service{DB: d, Blobs: blobs, Jobs: j, Now: tick}
	hub := &events.Hub{}
	svc := &Service{DB: d, Jobs: j, Problems: bank, Hub: hub, Now: tick}
	j.OnChange = svc.OnJobChange
	return &rig{svc: svc, bank: bank, jobs: j, hub: hub, clock: clock}
}

func zipPack(t *testing.T, dir string, skip func(string) bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if skip != nil && skip(rel) {
			return nil
		}
		w, _ := zw.Create(rel)
		b, _ := os.ReadFile(p)
		w.Write(b)
		return nil
	})
	zw.Close()
	return buf.Bytes()
}

func (r *rig) importPack(t *testing.T, skip func(string) bool) *problems.Problem {
	t.Helper()
	pk, err := problems.ReadPack(zipPack(t, "../../../problems/a-plus-b", skip))
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.bank.Import(context.Background(), pk, 1)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// judgeNext claims the next job and reports a verdict per test (default
// AC; "skip" omits a test), then closes it.
func (r *rig) judgeNext(t *testing.T, kind string, verdicts map[int]string) judgeapi.Claim {
	t.Helper()
	ctx := context.Background()
	c, err := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{kind}, Worker: "fake"})
	if err != nil || c == nil {
		t.Fatalf("claim %s: %v %v", kind, c, err)
	}
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true,
		Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	add := func(idx int, extra judgeapi.TestResult) {
		v := "AC"
		if o, ok := verdicts[idx]; ok {
			v = o
		}
		if v == "skip" {
			return
		}
		extra.Idx, extra.Role, extra.Verdict, extra.TimeMS = idx, judgeapi.RoleProgram, v, 123
		rep.Results = append(rep.Results, extra)
	}
	switch kind {
	case judgeapi.KindSubmit:
		var pl judgeapi.SubmitPayload
		json.Unmarshal(c.Payload, &pl)
		for _, test := range pl.Tests {
			add(test.Idx, judgeapi.TestResult{})
		}
	case judgeapi.KindRun:
		var pl judgeapi.RunPayload
		json.Unmarshal(c.Payload, &pl)
		for _, in := range pl.Inputs {
			add(in.Idx, judgeapi.TestResult{Output: "3\n7\n"})
		}
	}
	if err := r.jobs.Report(ctx, rep); err != nil {
		t.Fatalf("report: %v", err)
	}
	return *c
}

func TestWorkspaceAndDraft(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	w, err := r.svc.Workspace(ctx, p.ID, 1, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Samples) != 2 || w.Samples[0].Input != "2\n1 2\n3 4\n" || w.Samples[0].Output != "3\n7\n" || w.Draft != nil || w.ServerNow == 0 {
		t.Fatalf("workspace %+v", w)
	}
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "python3", "print(1)"); err != nil {
		t.Fatal(err)
	}
	w, _ = r.svc.Workspace(ctx, p.ID, 1, Scope{})
	if w.Draft == nil || w.Draft.Source != "print(1)" || w.Draft.Language != "python3" {
		t.Errorf("draft %+v", w.Draft)
	}
	var bad *InputError
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "rust", "fn main(){}"); !errors.As(err, &bad) {
		t.Errorf("unknown language: %v", err)
	}
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "cpp17", strings.Repeat("x", MaxSource+1)); !errors.As(err, &bad) {
		t.Errorf("oversized draft: %v", err)
	}
}

func TestSubmitScoresAndStreams(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	stream, _ := r.hub.Subscribe(1)
	sc := Scope{MinGap: 30 * time.Second}

	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "int main(){}", ClientKey: "k1", Typed: 40, Pasted: 0})
	if err != nil {
		t.Fatal(err)
	}
	if v.State != "queued" || v.QueuePosition == nil || *v.QueuePosition != 1 || v.MaxScore != 100 {
		t.Fatalf("queued %+v", v)
	}
	again, dup, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "int main(){}", ClientKey: "k1"})
	if err != nil || again.ID != v.ID || !dup {
		t.Errorf("same key: %v %+v", err, again)
	}
	var rate *RateError
	if _, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "x", ClientKey: "k2"}); !errors.As(err, &rate) {
		t.Errorf("second pending submit: %v", err)
	}

	r.judgeNext(t, judgeapi.KindSubmit, nil)
	v, _ = r.svc.Get(ctx, v.ID)
	if v.State != "done" || v.Verdict != "AC" || v.Score != 100 || v.MaxTimeMS != 120 || len(v.Subtasks) != 2 || v.JudgedAt == nil {
		t.Errorf("judged %+v", v)
	}
	deadline := time.After(2 * time.Second)
	select {
	case ev := <-stream.C:
		if ev.Type != "submission" {
			t.Errorf("event %s", ev.Type)
		}
	case <-deadline:
		t.Error("no live event for the finished submission")
	}

	// Too soon after the last one.
	*r.clock = r.clock.Add(10 * time.Second)
	_, _, err = r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "y", ClientKey: "k3"})
	if !errors.As(err, &rate) || rate.RetryAfter != 20*time.Second || !strings.Contains(rate.Message, "Try again in 20 s") {
		t.Errorf("gap: %v", err)
	}
	*r.clock = r.clock.Add(25 * time.Second)
	w, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "y", ClientKey: "k3"})
	if err != nil {
		t.Fatal(err)
	}
	// Fair queue: this user's second submission waits behind a first one.
	var prio int
	r.svc.DB.QueryRow("SELECT priority FROM jobs WHERE id = (SELECT job_id FROM submissions WHERE id = ?)", w.ID).Scan(&prio)
	if prio != SubmitPriority+1 {
		t.Errorf("priority %d", prio)
	}
	r.judgeNext(t, judgeapi.KindSubmit, map[int]string{5: "WA", 6: "SK"})
	w, _ = r.svc.Get(ctx, w.ID)
	if w.Verdict != "WA" || w.Score != 40 || w.Subtasks[1].Verdict != "WA" {
		t.Errorf("partial %+v", w)
	}
	list, _ := r.svc.Workspace(ctx, p.ID, 1, Scope{})
	if len(list.Submissions) != 2 || list.Submissions[0].ID != w.ID {
		t.Errorf("own submissions %+v", list.Submissions)
	}
}

func TestSubmitReportsKeepHiddenTestsHidden(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "int main(){}"})
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}})
	var bad *jobs.ReportError
	for name, res := range map[string]judgeapi.TestResult{
		"output text":           {Idx: 1, Role: judgeapi.RoleProgram, Verdict: "WA", Output: "secret"},
		"message on hidden":     {Idx: 5, Role: judgeapi.RoleProgram, Verdict: "WA", Message: "expected 1000000"},
		"test the job lacks":    {Idx: 42, Role: judgeapi.RoleProgram, Verdict: "AC"},
		"role other than prog.": {Idx: 1, Role: judgeapi.RoleReference, Verdict: "AC"},
	} {
		if err := r.jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Results: []judgeapi.TestResult{res}}); !errors.As(err, &bad) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A message on a sample is fine.
	if err := r.jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease,
		Results: []judgeapi.TestResult{{Idx: 1, Role: judgeapi.RoleProgram, Verdict: "WA", Message: "expected 3, found 4"}}}); err != nil {
		t.Errorf("sample message: %v", err)
	}
}

func TestRunOnSamplesAndCustomInput(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	run, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "print(3)"})
	if err != nil {
		t.Fatal(err)
	}
	var rate *RateError
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "print(3)"}); !errors.As(err, &rate) {
		t.Errorf("second pending run: %v", err)
	}
	r.judgeNext(t, judgeapi.KindRun, map[int]string{2: "WA"})
	run, _ = r.svc.GetRun(ctx, run.ID)
	if run.State != "done" || len(run.Results) != 2 || run.Results[0].Label != "Sample 1" || run.Results[0].Output != "3\n7\n" ||
		run.Results[0].Expected == nil || *run.Results[0].Expected != "3\n7\n" || run.Results[1].Verdict != "WA" {
		t.Errorf("run %+v", run)
	}
	*r.clock = r.clock.Add(3 * time.Second) // runs are 2 s apart at least
	in := "1\n5 6\n"
	custom, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "print(11)", Input: &in})
	if err != nil {
		t.Fatal(err)
	}
	r.judgeNext(t, judgeapi.KindRun, map[int]string{0: "OK"})
	custom, _ = r.svc.GetRun(ctx, custom.ID)
	if len(custom.Results) != 1 || custom.Results[0].Label != "Custom input" || custom.Results[0].Expected != nil || custom.Results[0].Verdict != "OK" {
		t.Errorf("custom run %+v", custom)
	}
}

func TestGenerateFillsMissingOutputs(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, func(name string) bool { return strings.HasSuffix(name, ".out") })
	if !strings.Contains(strings.Join(p.Problems(), " "), "no expected output") {
		t.Fatalf("missing outputs not reported: %v", p.Problems())
	}
	var notReady *NotReadyError
	if _, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "x"}); !errors.As(err, &notReady) {
		t.Errorf("submit before outputs exist: %v", err)
	}
	g, err := r.bank.RequestGeneration(ctx, p.ID, 1, false)
	if err != nil || g.State != "queued" {
		t.Fatalf("generation %+v %v", g, err)
	}
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindGenerate}})
	var pl judgeapi.GeneratePayload
	json.Unmarshal(c.Payload, &pl)
	if ok, _ := r.jobs.MayUpload(ctx, c.JobID, c.Lease); !ok {
		t.Fatal("generate job may not upload")
	}
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true, Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, in := range pl.Inputs {
		out := []byte("output of test\n")
		if in.Idx == 4 {
			rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "TLE"})
			continue
		}
		sha := problems.SHA(out)
		if err := r.bank.Blobs.UploadOutput(bytes.NewReader(out), sha, 1<<24); err != nil {
			t.Fatal(err)
		}
		r.jobs.RecordUpload(c.JobID, sha)
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: sha})
	}
	if err := r.jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	g, _ = r.bank.LatestGeneration(ctx, p.ID)
	if g.State != "done" || g.Summary == nil || g.Summary.Applied != 5 || len(g.Summary.Problems) != 1 ||
		!strings.Contains(g.Summary.Problems[0], "Test 4: the reference solution got time limit exceeded") {
		t.Fatalf("generation result %+v %+v", g, g.Summary)
	}
	got, _ := r.bank.Get(ctx, p.ID)
	probs := strings.Join(got.Problems(), " ")
	if !strings.Contains(probs, "Test 4 has no expected output") || strings.Contains(probs, "Test 1 has no expected output") {
		t.Errorf("after generation: %s", probs)
	}
	if err := r.bank.Blobs.UploadOutput(strings.NewReader("x"), problems.SHA([]byte("y")), 1<<24); err == nil {
		t.Error("an upload with the wrong hash was stored")
	}
}

func TestEventsAreBounded(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, []Event{{Kind: "paste", Size: 900}, {Kind: "blur"}}); err != nil {
		t.Fatal(err)
	}
	var bad *InputError
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, []Event{{Kind: "keystroke"}}); !errors.As(err, &bad) {
		t.Errorf("unknown kind: %v", err)
	}
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, make([]Event, maxEvents+1)); !errors.As(err, &bad) {
		t.Errorf("too many: %v", err)
	}
	var n int
	r.svc.DB.QueryRow("SELECT count(*) FROM integrity_events").Scan(&n)
	if n != 2 {
		t.Errorf("events stored: %d", n)
	}
}

// A save between the contest's Ready check and the load: the loaded copy
// is no longer the pinned revision, so submit, run and rejudge refuse
// instead of judging new tests under the old revision's name (C1-045).
func TestPinMovedAfterTheReadyCheck(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	var rev int64
	if err := r.svc.DB.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ? AND content_hash = ?", p.ID, p.ContentHash).Scan(&rev); err != nil {
		t.Fatalf("pinned revision: %v", err)
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'c', 'C', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	sc := Scope{ContestID: 1, RevisionID: rev}
	req := SubmitRequest{ProblemID: p.ID, UserID: 2, Scope: sc, Language: "cpp17", Source: "int main(){}", ClientKey: "k1"}
	if _, _, err := r.svc.Submit(ctx, req); err != nil {
		t.Fatalf("submit on the pinned copy: %v", err)
	}
	// The working copy moves on (as a save would).
	if _, err := r.svc.DB.Exec("UPDATE problems SET content_hash = 'saved-after-ready' WHERE id = ?", p.ID); err != nil {
		t.Fatal(err)
	}
	req.ClientKey = "k2"
	if _, _, err := r.svc.Submit(ctx, req); !errors.Is(err, ErrPinMoved) {
		t.Errorf("submit after the save: %v, want ErrPinMoved", err)
	}
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 2, Scope: sc, Language: "cpp17", Source: "int main(){}"}); !errors.Is(err, ErrPinMoved) {
		t.Errorf("run after the save: %v, want ErrPinMoved", err)
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, p.ID, rev); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 1, ProblemID: p.ID, By: 1}); !errors.Is(err, ErrPinMoved) {
		t.Errorf("rejudge after the save: %v, want ErrPinMoved", err)
	}
}
