package submissions

// Function problems (v2.0.2 stage B): Submit, Run and contest rejudge judge
// the composed file made with the live problem's locked parts, keep the
// student's part as the submission's source, and record the top's line
// count.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

const (
	fnTopCpp    = "#include <bits/stdc++.h>\nusing namespace std;\n"
	fnBottomCpp = "int main() { int t; cin >> t; while (t--) { long long a, b; cin >> a >> b; cout << add(a, b) << '\\n'; } }\n"
	fnTopPy     = "import sys\n"
	fnBottomPy  = "d = sys.stdin.read().split()\nfor i in range(int(d[0])):\n    print(add(int(d[1 + 2 * i]), int(d[2 + 2 * i])))\n"
	fnCode      = "long long add(long long a, long long b) {\n    return a + b;\n}\n"
)

// importFunction imports a-plus-b and turns it into a function problem in
// C++ and Python.
func (r *rig) importFunction(t *testing.T) *problems.Problem {
	t.Helper()
	ctx := context.Background()
	p := r.importPack(t, nil)
	mode := problems.ModeFunction
	top := map[string]string{"cpp17": fnTopCpp, "python3": fnTopPy}
	bottom := map[string]string{"cpp17": fnBottomCpp, "python3": fnBottomPy}
	stubs := map[string]string{"cpp17": "long long add(long long a, long long b) {\n}\n", "python3": "def add(a, b):\n    pass\n"}
	got, err := r.bank.Update(ctx, p.ID, problems.Edit{Title: p.Title, Slug: p.Slug, Statement: p.Statement,
		TimeLimitMS: p.TimeLimitMS, MemoryLimitMB: p.MemoryLimitMB, Checker: p.Checker, Epsilon: p.Epsilon,
		Reference: problems.Source{Language: "cpp17", Source: fnCode}, Brute: &problems.Source{Language: "python3", Source: "def add(a, b):\n    return a + b\n"},
		Validator: p.Validator, Tags: p.Tags, Samples: p.Samples, Subtasks: p.Subtasks,
		Mode: &mode, LockedTop: &top, LockedBottom: &bottom, Starters: &stubs})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (r *rig) blob(t *testing.T, sha string) string {
	t.Helper()
	b, err := r.bank.Blobs.Read(sha, 1<<20)
	if err != nil {
		t.Fatalf("blob %s: %v", sha, err)
	}
	return string(b)
}

func (r *rig) row(t *testing.T, id int64) (source, sourceSHA string, topLines int, payload string) {
	t.Helper()
	if err := r.svc.DB.QueryRow(`SELECT s.source, s.source_sha, s.top_lines, j.payload FROM submissions s JOIN jobs j ON j.id = s.job_id
		WHERE s.id = ?`, id).Scan(&source, &sourceSHA, &topLines, &payload); err != nil {
		t.Fatal(err)
	}
	return
}

// Submit keeps the student's part as the submission's source and judges the
// composed file, whose blob the job pins; the top's line count is stored.
func TestSubmitComposesAFunctionProblem(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: fnCode})
	if err != nil {
		t.Fatal(err)
	}
	source, sha, topLines, payload := r.row(t, v.ID)
	if source != fnCode || r.blob(t, sha) != fnCode || topLines != 2 {
		t.Errorf("stored source %q (blob %q), top_lines %d", source, r.blob(t, sha), topLines)
	}
	var pl judgeapi.SubmitPayload
	json.Unmarshal([]byte(payload), &pl)
	if pl.Program.SourceSHA == sha || r.blob(t, pl.Program.SourceSHA) != fnTopCpp+fnCode+fnBottomCpp {
		t.Errorf("judged %q", r.blob(t, pl.Program.SourceSHA))
	}
	blobs, _ := SubmitKind{}.Blobs([]byte(payload))
	if blobs[0] != pl.Program.SourceSHA {
		t.Errorf("the job does not pin the composed file: %v", blobs)
	}
	// Python composes with its own parts.
	r.judgeNext(t, judgeapi.KindSubmit, nil)
	r.clock.advance(time.Minute)
	v, _, err = r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: "def add(a, b):\n    return a + b"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, topLines, payload = r.row(t, v.ID)
	json.Unmarshal([]byte(payload), &pl)
	if want := fnTopPy + "def add(a, b):\n    return a + b\n" + fnBottomPy; r.blob(t, pl.Program.SourceSHA) != want || topLines != 1 {
		t.Errorf("python judged %q, top_lines %d", r.blob(t, pl.Program.SourceSHA), topLines)
	}
}

// A language the problem does not offer is refused before anything is
// stored; a whole problem is judged exactly as sent, as in v2.0.1.
func TestFunctionProblemLanguagesAndWholeProblems(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	want := "This problem takes C++17 or Python 3."
	var in *InputError
	if _, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "java21", Source: "class Solution {}"}); !errors.As(err, &in) || in.Message != want {
		t.Errorf("submit in java21: %v", err)
	}
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "node22", Source: "function add() {}"}); !errors.As(err, &in) || in.Message != want {
		t.Errorf("run in node22: %v", err)
	}
	if err := r.svc.SaveDraft(ctx, p.ID, 1, Scope{}, "java21", "class Solution {}"); !errors.As(err, &in) || in.Message != want {
		t.Errorf("draft in java21: %v", err)
	}
	w, err := r.svc.Workspace(ctx, p.ID, 1, Scope{})
	if err != nil || len(w.Languages) != 2 || w.Languages[0].ID != "cpp17" || w.Languages[1].ID != "python3" {
		t.Errorf("workspace languages %+v (%v)", w.Languages, err)
	}

	r = newRig(t)
	whole := r.importPack(t, nil)
	src := "int main() {}\r\n\n\n"
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: whole.ID, UserID: 2, Language: "cpp17", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	_, sha, topLines, payload := r.row(t, v.ID)
	var pl judgeapi.SubmitPayload
	json.Unmarshal([]byte(payload), &pl)
	if pl.Program.SourceSHA != sha || topLines != 0 || r.blob(t, sha) != src {
		t.Errorf("whole problem: judged %s, stored %s, top_lines %d", pl.Program.SourceSHA, sha, topLines)
	}
}

// A run composes too, and records top_lines.
func TestRunComposesAFunctionProblem(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	run, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: fnCode})
	if err != nil {
		t.Fatal(err)
	}
	_, sha, topLines, payload := r.row(t, run.ID)
	var pl judgeapi.RunPayload
	json.Unmarshal([]byte(payload), &pl)
	if r.blob(t, sha) != fnCode || r.blob(t, pl.Program.SourceSHA) != fnTopCpp+fnCode+fnBottomCpp || topLines != 2 {
		t.Errorf("run judged %q, stored %q, top_lines %d", r.blob(t, pl.Program.SourceSHA), r.blob(t, sha), topLines)
	}
	blobs, _ := RunKind{}.Blobs([]byte(payload))
	if blobs[0] != pl.Program.SourceSHA {
		t.Errorf("the run job does not pin the composed file: %v", blobs)
	}
}

// Contest rejudge composes the stored student part with the live locked
// parts. After a top edit the pin no longer matches and rejudge refuses
// with ErrPinMoved; once the contest is moved to the new revision ("Update
// students now"), rejudge composes with the new top and stores its line
// count.
func TestContestRejudgeComposesWithTheLiveTop(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	rev := func() int64 {
		var id int64
		if err := r.svc.DB.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ? ORDER BY rev DESC LIMIT 1", p.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'c', 'C', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	pinned := rev()
	if _, err := r.svc.DB.Exec(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id) VALUES (1, ?, 'A', 0, ?)`, p.ID, pinned); err != nil {
		t.Fatal(err)
	}
	sc := Scope{ContestID: 1, RevisionID: pinned}
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 2, Scope: sc, Language: "cpp17", Source: fnCode})
	if err != nil {
		t.Fatal(err)
	}
	r.judgeNext(t, judgeapi.KindSubmit, nil)

	// Organiser edits the top: the pin moves.
	cur, _ := r.bank.Get(ctx, p.ID)
	top := map[string]string{"cpp17": "#include <bits/stdc++.h>\n#include <cmath>\nusing namespace std;\n", "python3": fnTopPy}
	if _, err := r.bank.Update(ctx, p.ID, problems.Edit{Title: cur.Title, Slug: cur.Slug, Statement: cur.Statement,
		TimeLimitMS: cur.TimeLimitMS, MemoryLimitMB: cur.MemoryLimitMB, Checker: cur.Checker, Epsilon: cur.Epsilon,
		Reference: cur.Reference, Brute: cur.Brute, Validator: cur.Validator, Tags: cur.Tags, Samples: cur.Samples,
		Subtasks: cur.Subtasks, LockedTop: &top}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 1, ProblemID: p.ID, By: 1}); !errors.Is(err, ErrPinMoved) {
		t.Fatalf("rejudge after a top edit: %v, want ErrPinMoved", err)
	}

	// Update students now.
	if _, err := r.svc.DB.Exec(`UPDATE contest_problems SET revision_id = ? WHERE contest_id = 1`, rev()); err != nil {
		t.Fatal(err)
	}
	if n, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 1, ProblemID: p.ID, By: 1}); err != nil || n != 1 {
		t.Fatalf("rejudge after the update: %d %v", n, err)
	}
	source, sha, topLines, payload := r.row(t, v.ID)
	var pl judgeapi.SubmitPayload
	json.Unmarshal([]byte(payload), &pl)
	if source != fnCode || r.blob(t, sha) != fnCode || topLines != 3 || r.blob(t, pl.Program.SourceSHA) != top["cpp17"]+fnCode+fnBottomCpp {
		t.Errorf("rejudged %q with top_lines %d; stored %q", r.blob(t, pl.Program.SourceSHA), topLines, source)
	}
	// A second rejudge while that job waits finds the same payload and
	// queues nothing.
	if n, err := r.svc.Rejudge(ctx, RejudgeRequest{ContestID: 1, ProblemID: p.ID, By: 1}); err != nil || n != 0 {
		t.Errorf("repeat rejudge: %d %v", n, err)
	}
}

// The largest file a function problem can send the judge is the student's
// 64 KB plus a 4 KB top and a 4 KB bottom; the judge's read cap is exactly
// that (V202-39), so no valid submission is refused and nothing larger is
// read.
func TestTheJudgeReadsTheLargestComposedFile(t *testing.T) {
	if MaxSource+2*problems.MaxStarter != judgeapi.MaxProgramSource {
		t.Fatalf("largest composed file %d bytes, judge reads %d", MaxSource+2*problems.MaxStarter, judgeapi.MaxProgramSource)
	}
	c := problems.Compose(strings.Repeat("a", problems.MaxStarter-1)+"\n", strings.Repeat("b", MaxSource-1)+"\n",
		strings.Repeat("c", problems.MaxStarter-1)+"\n")
	if len(c.Source) != judgeapi.MaxProgramSource {
		t.Errorf("largest composed file is %d bytes", len(c.Source))
	}
}
