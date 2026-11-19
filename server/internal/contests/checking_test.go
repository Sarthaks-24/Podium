package contests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// A pin is refused while a proof of the problem is open: the problem is
// validated and the same as when it was pinned, so only the proof stops it.
const checkingB = "The problem for label B is still being checked. Try again in a few minutes."

// checkedProblem adds problem 100, validated, with a proof job in state.
func checkedProblem(t *testing.T, r *svcRig, state string) {
	t.Helper()
	execNoFK(t, r,
		`INSERT INTO problems (id, slug, title, time_limit_ms, memory_limit_mb, checker, reference_language, created_at, updated_at, content_hash, validated_hash)
			VALUES (100, 'array-sum', 'Array sum', 1000, 256, 'tokens', 'cpp17', 0, 0, 'h', 'h')`,
		fmt.Sprintf(`INSERT INTO jobs (kind, priority, state, payload, created_at) VALUES ('validate', 88, '%s', '{"problem_id":100,"proof":{"round":"r"}}', 0)`, state))
}

func wantChecking(t *testing.T, what string, err error) {
	t.Helper()
	var checking *CheckingError
	if !errors.As(err, &checking) || err.Error() != checkingB {
		t.Errorf("%s: %v, want the checking refusal %q", what, err, checkingB)
	}
}

func TestSetProblemsRefusesAProblemBeingChecked(t *testing.T) {
	r := newSvc(t)
	c := r.contest(t, nil)
	checkedProblem(t, r, "queued")
	_, err := r.svc.SetProblems(context.Background(), c.ID, []ProblemChoice{{ProblemID: 100, Label: "B"}}, Actor{ID: 1}, nil)
	wantChecking(t, "set problems", err)
	if n := count(t, r, "SELECT count(*) FROM contest_problems"); n != 0 {
		t.Errorf("%d pins after the refusal", n)
	}
	// The proof ends: the refusal goes away (the pin may still be refused for
	// another reason, never this one).
	execNoFK(t, r, "UPDATE jobs SET state = 'done'")
	_, err = r.svc.SetProblems(context.Background(), c.ID, []ProblemChoice{{ProblemID: 100, Label: "B"}}, Actor{ID: 1}, nil)
	var checking *CheckingError
	if errors.As(err, &checking) {
		t.Errorf("a closed proof still refuses: %v", err)
	}
}

func TestCloneRefusesAProblemBeingChecked(t *testing.T) {
	r := newSvc(t)
	src := r.contest(t, nil)
	checkedProblem(t, r, "leased")
	execNoFK(t, r, fmt.Sprintf(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, points) VALUES (%d, 100, 'B', 0, '[100]')`, src.ID))
	_, err := r.svc.Clone(context.Background(), src.ID, "", "Copy", t0, at(60), owner)
	wantChecking(t, "clone", err)
	if n := count(t, r, "SELECT count(*) FROM contests"); n != 1 {
		t.Errorf("%d contests after the refused clone, want 1", n)
	}
}

func TestUpdatePinRefusesAProblemBeingChecked(t *testing.T) {
	r := newSvc(t)
	c := r.contest(t, nil)
	checkedProblem(t, r, "queued")
	execNoFK(t, r, fmt.Sprintf(`INSERT INTO contest_problems (contest_id, problem_id, label, ord, revision_id, points)
		VALUES (%d, 100, 'B', 0, 1, '[100]')`, c.ID))
	read := ContestProblem{Label: "B", ProblemID: 100, RevisionID: 1}
	from := &problems.Revision{ID: 1, ProblemID: 100, Rev: 1}
	to := &problems.Revision{ID: 2, ProblemID: 100, Rev: 2, ContentHash: "h"}
	_, err := r.svc.applyPin(context.Background(), c, read, "B", from, to, Actor{ID: 1}, false)
	wantChecking(t, "update pin", err)
	if n := count(t, r, "SELECT revision_id FROM contest_problems WHERE contest_id = ?", c.ID); n != 1 {
		t.Errorf("pin is revision %d after the refusal, want 1", n)
	}
}

func TestCheckingRefusalResponse(t *testing.T) {
	w := httptest.NewRecorder()
	(&Handlers{}).contestError(w, httptest.NewRequest("PUT", "/api/admin/contests/1/problems", nil), &CheckingError{Label: "B"})
	want := `{"error":{"code":"checking","message":"` + checkingB + `"}}`
	if w.Code != http.StatusConflict || w.Body.String() != want && w.Body.String() != want+"\n" {
		t.Errorf("response %d %s, want 409 %s", w.Code, w.Body.String(), want)
	}
}
