package submissions

// M4 security review (cache/council-2026-10-18/m4-security.md): rejudges
// replace unfinished jobs (finding 4), judge failures show and do not count
// (finding 6), and judge uploads stream to disk with orphans deleted
// (M3 re-check).

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

func TestRejudgeReplacesAnUnfinishedJob(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	sc := Scope{ContestID: 9}
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "a"})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "w"})
	// The same tests: the leased job already carries them, nothing to do.
	if n, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 9, ProblemID: p.ID}); err != nil || n != 0 {
		t.Fatalf("rejudge with unchanged tests: %d %v", n, err)
	}
	// Different judging (here the sample rule): the leased job is replaced.
	if n, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 9, ProblemID: p.ID, NoStopOnSample: true}); err != nil || n != 1 {
		t.Fatalf("rejudge with changed judging: %d %v", n, err)
	}
	rep := judgeapi.Report{JobID: old.JobID, Lease: old.Lease, Final: true, Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "CE"}}}
	if err := r.jobs.Report(ctx, rep); !errors.Is(err, jobs.ErrLease) {
		t.Fatalf("the replaced job's worker must be refused: %v", err)
	}
	r.judgeNext(t, judgeapi.KindSubmit, nil)
	got, _ := r.svc.Get(ctx, v.ID)
	if got.State != "done" || got.Verdict != "AC" {
		t.Errorf("after the replacement job: %+v", got)
	}
}

func TestJudgeFailuresShowAndDoNotCount(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	sc := Scope{ContestID: 4, MaxPerProblem: 1, View: ViewOptions{Coarse: true, Hide: true}}
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "a"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "w"})
	if err := r.jobs.Report(ctx, judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Fatal: "isolate failed"}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.svc.GetWith(ctx, v.ID, sc.View)
	if got.State != "error" || got.Error != JudgeFailedMessage {
		t.Errorf("exam view of a judge failure: %+v", got)
	}
	if _, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "b"}); err != nil {
		t.Errorf("a failed submission used up the only attempt: %v", err)
	}
}

func TestUploadsStreamAndOrphansAreDiscarded(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	big := strings.Repeat("y", 64)
	if err := r.bank.Blobs.UploadOutput(strings.NewReader(big), problems.SHA([]byte(big)), 32); !errors.Is(err, problems.ErrUploadTooLarge) {
		t.Errorf("oversized upload: %v", err)
	}
	if err := r.bank.Blobs.UploadOutput(strings.NewReader("x"), problems.SHA([]byte("y")), 32); !errors.Is(err, problems.ErrUploadHash) {
		t.Errorf("wrong hash: %v", err)
	}
	leftovers := 0
	filepath.WalkDir(r.bank.Blobs.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), ".upload-") {
			leftovers++
		}
		return nil
	})
	if leftovers != 0 {
		t.Errorf("%d temporary files left after refused uploads", leftovers)
	}

	r.jobs.Discard = func(shas []string) { r.bank.Blobs.DiscardUnrecorded(ctx, r.svc.DB, shas) }
	p := r.importPack(t, func(name string) bool { return strings.HasSuffix(name, ".out") })
	if _, err := r.bank.RequestGeneration(ctx, p.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindGenerate}})
	var pl judgeapi.GeneratePayload
	json.Unmarshal(c.Payload, &pl)
	upload := func(body string) string {
		sha := problems.SHA([]byte(body))
		if ok, _ := r.jobs.MayUpload(ctx, c.JobID, c.Lease); !ok {
			t.Fatal("no upload slot")
		}
		if err := r.bank.Blobs.UploadOutput(strings.NewReader(body), sha, 1<<20); err != nil {
			t.Fatal(err)
		}
		r.jobs.RecordUpload(c.JobID, sha)
		return sha
	}
	used := upload("used output\n")
	unused := upload("never referenced\n")
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Final: true, Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, in := range pl.Inputs {
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: in.Idx, Role: judgeapi.RoleProgram, Verdict: "OK", OutputSHA: used})
	}
	if err := r.jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if f, err := r.bank.Blobs.Open(used); err != nil {
		t.Errorf("the applied output was deleted: %v", err)
	} else {
		f.Close()
	}
	if f, err := r.bank.Blobs.Open(unused); err == nil {
		f.Close()
		t.Error("an upload nothing used is still stored")
	}
}
