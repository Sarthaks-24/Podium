package submissions

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/jobs/jobstest"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// A submit, a Run and a rejudge each signal after their transaction
// commits, so a judge holding a claim starts at once (J2, V203-69).
func TestEnqueuesWakeAHeldClaim(t *testing.T) {
	const within = 100 * time.Millisecond
	ctx := context.Background()
	r := newRig(t)
	p := r.importPack(t, nil)
	if _, err := r.svc.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'c', 'C', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	var rev int64
	if err := r.svc.DB.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ? ORDER BY rev DESC LIMIT 1", p.ID).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, p.ID, rev); err != nil {
		t.Fatal(err)
	}
	sc := Scope{ContestID: 1, RevisionID: rev}

	answer := jobstest.Hold(t, r.jobs, judgeapi.KindSubmit)
	if _, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Scope: sc, Language: "cpp17", Source: "int main(){}"}); err != nil {
		t.Fatal(err)
	}
	if answer(within) == nil {
		t.Error("a submission did not wake a held claim")
	}

	answer = jobstest.Hold(t, r.jobs, judgeapi.KindRun)
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 2, Language: "cpp17", Source: "int main(){}"}); err != nil {
		t.Fatal(err)
	}
	if answer(within) == nil {
		t.Error("a Run did not wake a held claim")
	}

	// A rejudge of a judged submission queues a new job.
	other, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 2, Scope: sc, Language: "cpp17", Source: "int main(){}"})
	if err != nil {
		t.Fatal(err)
	}
	r.judgeNext(t, judgeapi.KindSubmit, nil)
	answer = jobstest.Hold(t, r.jobs, judgeapi.KindSubmit)
	if n, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 1, SubmissionID: other.ID, By: 1}); err != nil || n != 1 {
		t.Fatalf("rejudge: %d %v", n, err)
	}
	if answer(within) == nil {
		t.Error("a rejudge did not wake a held claim")
	}
}
