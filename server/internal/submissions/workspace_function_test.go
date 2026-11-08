package submissions

// Function problems (v2.0.2 stage C): the student's workspace carries the
// locked parts from the same copy of the problem the judge will use, and a
// submission that already holds the locked text is composed once.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// A whole problem has mode "whole" and no locked parts.
func TestWorkspaceOfAWholeProblemHasNoLockedCode(t *testing.T) {
	r := newRig(t)
	p := r.importPack(t, nil)
	w, err := r.svc.Workspace(context.Background(), p.ID, 1, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if w.Mode != "whole" || len(w.LockedTop) != 0 || len(w.LockedBottom) != 0 {
		t.Errorf("mode %q, top %v, bottom %v", w.Mode, w.LockedTop, w.LockedBottom)
	}
	b, _ := json.Marshal(w)
	var raw map[string]json.RawMessage
	json.Unmarshal(b, &raw)
	for _, k := range []string{"mode", "locked_top", "locked_bottom"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("payload has no %q", k)
		}
	}
	if string(raw["locked_top"]) != "{}" {
		t.Errorf("locked_top %s, want {}", raw["locked_top"])
	}
}

// Outside a contest the live problem is served, for the offered languages.
func TestWorkspaceServesTheLiveLockedCode(t *testing.T) {
	r := newRig(t)
	p := r.importFunction(t)
	w, err := r.svc.Workspace(context.Background(), p.ID, 1, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if w.Mode != "function" || w.LockedTop["cpp17"] != fnTopCpp || w.LockedBottom["python3"] != fnBottomPy || len(w.LockedTop) != 2 {
		t.Errorf("mode %q, top %v, bottom %v", w.Mode, w.LockedTop, w.LockedBottom)
	}
}

// In a contest, after a top edit and before "Update students now", the
// student is served the pinned top, bottom and languages; after the update
// they get the new top.
func TestWorkspaceServesThePinnedLockedCode(t *testing.T) {
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
	pinned := rev()
	sc := Scope{ContestID: 1, RevisionID: pinned}
	cur, _ := r.bank.Get(ctx, p.ID)
	newTop := map[string]string{"cpp17": "#include <bits/stdc++.h>\n#include <cmath>\nusing namespace std;\n"}
	onlyCpp := map[string]string{"cpp17": fnBottomCpp}
	onlyStub := map[string]string{"cpp17": "long long add(long long a, long long b) {\n}\n"}
	if _, err := r.bank.Update(ctx, p.ID, problems.Edit{Title: cur.Title, Slug: cur.Slug, Statement: cur.Statement,
		TimeLimitMS: cur.TimeLimitMS, MemoryLimitMB: cur.MemoryLimitMB, Checker: cur.Checker, Epsilon: cur.Epsilon,
		Reference: cur.Reference, Brute: nil, Validator: cur.Validator, Tags: cur.Tags, Samples: cur.Samples,
		Subtasks: cur.Subtasks, LockedTop: &newTop, LockedBottom: &onlyCpp, Starters: &onlyStub}); err != nil {
		t.Fatal(err)
	}
	w, err := r.svc.Workspace(ctx, p.ID, 2, sc)
	if err != nil {
		t.Fatal(err)
	}
	if w.LockedTop["cpp17"] != fnTopCpp || w.LockedBottom["python3"] != fnBottomPy || len(w.Languages) != 2 {
		t.Errorf("before the update: top %v, bottom %v, languages %v", w.LockedTop, w.LockedBottom, w.Languages)
	}
	w, err = r.svc.Workspace(ctx, p.ID, 2, Scope{ContestID: 1, RevisionID: rev()})
	if err != nil {
		t.Fatal(err)
	}
	if w.LockedTop["cpp17"] != newTop["cpp17"] || len(w.LockedBottom) != 1 || len(w.Languages) != 1 {
		t.Errorf("after the update: top %v, bottom %v, languages %v", w.LockedTop, w.LockedBottom, w.Languages)
	}
}

// A submission that already holds the locked top and bottom (a whole file
// pasted past an old client) is judged with each once, and the stored
// source is the student's part only.
func TestSubmitStripsLockedTextAlreadyInTheCode(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importFunction(t)
	v, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: fnTopCpp + fnCode + fnBottomCpp})
	if err != nil {
		t.Fatal(err)
	}
	source, sha, topLines, payload := r.row(t, v.ID)
	var pl judgeapi.SubmitPayload
	json.Unmarshal([]byte(payload), &pl)
	if source != fnCode || r.blob(t, sha) != fnCode || topLines != 2 || r.blob(t, pl.Program.SourceSHA) != fnTopCpp+fnCode+fnBottomCpp {
		t.Errorf("stored %q, top_lines %d, judged %q", source, topLines, r.blob(t, pl.Program.SourceSHA))
	}
	// Run strips the same way.
	if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "python3", Source: fnTopPy + "def add(a, b):\n    return a + b\n"}); err != nil {
		t.Fatal(err)
	}
	var runPayload string
	if err := r.svc.DB.QueryRow(`SELECT j.payload FROM submissions s JOIN jobs j ON j.id = s.job_id WHERE s.kind = 'run'`).Scan(&runPayload); err != nil {
		t.Fatal(err)
	}
	var rp judgeapi.RunPayload
	json.Unmarshal([]byte(runPayload), &rp)
	if got, want := r.blob(t, rp.Program.SourceSHA), fnTopPy+"def add(a, b):\n    return a + b\n"+fnBottomPy; got != want {
		t.Errorf("run judged %q, want %q", got, want)
	}
}
