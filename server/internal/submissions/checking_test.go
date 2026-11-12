package submissions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const checkingCopy = "This problem is being checked. Try again in a few minutes."

func (r *rig) openProof(t *testing.T, problemID int64, state string) {
	t.Helper()
	if _, err := r.svc.DB.Exec(`INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('validate', 88, ?, ?, 0)`,
		state, fmt.Sprintf(`{"problem_id":%d,"proof":{"round":"r"}}`, problemID)); err != nil {
		t.Fatal(err)
	}
}

// While a proof of the problem is open, a student outside a contest pin gets
// 409 checking from Run and Submit; one inside a pin is judged as before,
// because their judging does not read the working copy.
func TestRunAndSubmitAreRefusedWhileAProofIsOpen(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	var rev int64
	if err := r.svc.DB.QueryRow("SELECT id FROM problem_revisions WHERE problem_id = ? AND content_hash = ?", p.ID, p.ContentHash).Scan(&rev); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.DB.Exec(`INSERT INTO contests (id, slug, title, starts_at, ends_at, created_at, updated_at) VALUES (1, 'c', 'C', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	r.openProof(t, p.ID, "queued")
	submit := func(sc Scope, key string, user int64) error {
		_, _, err := r.svc.Submit(ctx, SubmitRequest{ProblemID: p.ID, UserID: user, Scope: sc, Language: "cpp17", Source: "int main(){}", ClientKey: key})
		return err
	}
	run := func(sc Scope) error {
		_, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 2, Scope: sc, Language: "cpp17", Source: "int main(){}"})
		return err
	}

	for what, err := range map[string]error{"submit": submit(Scope{}, "k1", 2), "run": run(Scope{})} {
		if !errors.Is(err, ErrChecking) || err.Error() != checkingCopy {
			t.Errorf("%s outside a contest: %v, want ErrChecking with %q", what, err, checkingCopy)
		}
	}
	if n := countRows(t, r, "submissions"); n != 0 {
		t.Errorf("%d submissions queued by refused requests", n)
	}

	pinned := Scope{ContestID: 1, RevisionID: rev}
	if err := submit(pinned, "k2", 2); err != nil {
		t.Errorf("submit in a contest pin during a proof: %v", err)
	}
	if err := run(pinned); err != nil {
		t.Errorf("run in a contest pin during a proof: %v", err)
	}

	// The proof ends: students outside a contest are judged again.
	if _, err := r.svc.DB.Exec("UPDATE jobs SET state = 'done' WHERE priority = 88"); err != nil {
		t.Fatal(err)
	}
	if err := submit(Scope{}, "k3", 1); err != nil {
		t.Errorf("submit after the proof: %v", err)
	}
}

func countRows(t *testing.T, r *rig, table string) int {
	t.Helper()
	var n int
	if err := r.svc.DB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCheckingRefusalResponse(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(nil, w, httptest.NewRequest("POST", "/api/problems/1/submit", nil), ErrChecking)
	want := `{"error":{"code":"checking","message":"` + checkingCopy + `"}}`
	if w.Code != http.StatusConflict || strings.TrimSpace(w.Body.String()) != want {
		t.Errorf("response %d %s, want 409 %s", w.Code, w.Body.String(), want)
	}
}

// A retry of a submission that is already stored gets that submission back,
// even while a proof is open: the repeat check comes before the refusal.
func TestARetriedSubmitDuringAProofReturnsTheStoredSubmission(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	p := r.importPack(t, nil)
	req := SubmitRequest{ProblemID: p.ID, UserID: 2, Language: "cpp17", Source: "int main(){}", ClientKey: "same-key"}
	first, dup, err := r.svc.Submit(ctx, req)
	if err != nil || dup {
		t.Fatalf("first submit: %v dup %v", err, dup)
	}
	r.openProof(t, p.ID, "leased")
	again, dup, err := r.svc.Submit(ctx, req)
	if err != nil || !dup || again.ID != first.ID {
		t.Fatalf("retry during a proof: %+v dup %v err %v, want the stored submission %d", again, dup, err, first.ID)
	}
	req.ClientKey = "new-key"
	if _, _, err := r.svc.Submit(ctx, req); !errors.Is(err, ErrChecking) {
		t.Errorf("a new submission during a proof: %v", err)
	}
	if n := countRows(t, r, "submissions"); n != 1 {
		t.Errorf("%d submissions, want 1", n)
	}
}
