package submissions

// Checks for the second M3 security review (cache/council-2026-10-18/
// m3-security-2.md): views on every read path, client keys per contest,
// run rows without inline code and index-only rate checks, and rate limits
// on drafts and integrity events.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

func TestClientKeyIsPerContest(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	first, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "a", ClientKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	r.judgeNext(t, judgeapi.KindSubmit, nil)
	other, dup, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: Scope{ContestID: 7}, Language: "cpp17", Source: "b", ClientKey: "k"})
	if err != nil || dup || other.ID == first.ID {
		t.Errorf("same key in another contest returned the first submission: %v %v", err, dup)
	}
}

func TestStudentViewsHideProgressAndExamResults(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "a"})
	if err != nil {
		t.Fatal(err)
	}
	// Report the samples and one hidden test of subtask 1, then stop.
	c, _ := r.jobs.Claim(ctx, judgeapi.ClaimRequest{Kinds: []string{judgeapi.KindSubmit}, Worker: "w"})
	rep := judgeapi.Report{JobID: c.JobID, Lease: c.Lease, Compile: []judgeapi.CompileResult{{Role: judgeapi.RoleProgram, Verdict: "OK"}}}
	for _, idx := range []int{1, 2, 3} {
		rep.Results = append(rep.Results, judgeapi.TestResult{Idx: idx, Role: judgeapi.RoleProgram, Verdict: "AC", TimeMS: 333, MemoryKB: 999})
	}
	if err := r.jobs.Report(ctx, rep); err != nil {
		t.Fatal(err)
	}
	coarse, _ := r.svc.GetWith(ctx, v.ID, ViewOptions{Coarse: true, RoundMS: 100})
	if coarse.Samples[0].Verdict != "AC" || coarse.Samples[0].TimeMS != 300 {
		t.Errorf("finished samples should show, rounded: %+v", coarse.Samples)
	}
	if got := coarse.Subtasks[0].Tests[0]; got.Verdict != "" || got.TimeMS != 0 {
		t.Errorf("a hidden test inside an unfinished subtask shows: %+v", got)
	}
	if coarse.MaxTimeMS != 0 || coarse.MaxMemoryKB != 0 {
		t.Errorf("running maxima reveal progress: %+v", coarse)
	}
	full, _ := r.svc.GetWith(ctx, v.ID, ViewOptions{})
	if full.Subtasks[0].Tests[0].Verdict != "AC" {
		t.Errorf("organisers see progress per test: %+v", full.Subtasks[0])
	}
	hidden, _ := r.svc.GetWith(ctx, v.ID, ViewOptions{Coarse: true, Hide: true})
	if !hidden.Hidden || hidden.State != "submitted" || len(hidden.Samples) != 0 || hidden.Verdict != "" {
		t.Errorf("exam view: %+v", hidden)
	}
}

func TestRunRowsKeepNoInlineCodeAndChecksUseTheIndex(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	in := strings.Repeat("1 2\n", 1000)
	v, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: strings.Repeat("#", 5000), Input: &in})
	if err != nil {
		t.Fatal(err)
	}
	var src, input, sha string
	r.svc.DB.QueryRow("SELECT source, input, source_sha FROM submissions WHERE id = ?", v.ID).Scan(&src, &input, &sha)
	if src != "" || input != "" || sha == "" {
		t.Errorf("run row keeps %d bytes of code and %d of input", len(src), len(input))
	}
	for _, q := range []string{
		`SELECT count(*) FROM submissions WHERE user_id = 1 AND kind = 'run' AND state IN ('queued', 'running')`,
		`SELECT max(created_at) FROM submissions WHERE user_id = 1 AND kind = 'run'`,
	} {
		rows, err := r.svc.DB.Query("EXPLAIN QUERY PLAN " + q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			rows.Scan(&id, &parent, &unused, &detail)
			plan = append(plan, detail)
		}
		rows.Close()
		if !strings.Contains(strings.Join(plan, " "), "COVERING INDEX submissions_by_kind") {
			t.Errorf("%s\nplan: %v", q, plan)
		}
	}
}

func TestDraftAndEventRateLimits(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "python3", "a"); err != nil {
		t.Fatal(err)
	}
	var rate *RateError
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "python3", "b"); !errors.As(err, &rate) || rate.RetryAfter != 2*time.Second {
		t.Errorf("second draft within 2 s: %v", err)
	}
	if err := r.svc.SaveDraft(ctx, p.ID, 2, Scope{}, "python3", "b"); err != nil {
		t.Errorf("another user is not limited: %v", err)
	}
	r.clock.advance(2 * time.Second)
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "python3", "c"); err != nil {
		t.Errorf("draft after the gap: %v", err)
	}

	// A bad kind is refused before anything is written or counted.
	var bad *InputError
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, []Event{{Kind: "x"}}); !errors.As(err, &bad) {
		t.Errorf("unknown kind: %v", err)
	}
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, []Event{{Kind: "paste", Size: 10}}); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, []Event{{Kind: "blur"}}); !errors.As(err, &rate) {
		t.Errorf("second batch within 5 s: %v", err)
	}
	r.clock.advance(5 * time.Second)
	if err := r.svc.RecordEvents(ctx, p.ID, 1, Scope{}, []Event{{Kind: "blur"}}); err != nil {
		t.Errorf("batch after the gap: %v", err)
	}
}

func TestContestLanguagesAndView(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	sc := Scope{ContestID: 3, Languages: []string{"python3"}, View: ViewOptions{Coarse: true, Hide: true}}
	var bad *InputError
	if _, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "a"}); !errors.As(err, &bad) ||
		bad.Message != "This contest allows Python 3." {
		t.Errorf("language: %v", err)
	}
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "python3", Source: "a"})
	if err != nil || !v.Hidden {
		t.Fatalf("submit answers with the scope's view: %v %+v", err, v)
	}
	w, _ := r.svc.Workspace(ctx, p.ID, 1, sc)
	if len(w.Languages) != 1 || len(w.Submissions) != 1 || !w.Submissions[0].Hidden {
		t.Errorf("workspace %+v", w)
	}
}
