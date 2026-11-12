package problems

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const checkingCopy = "Podium is still checking the languages of this problem."

// errorBody decodes the {"error": {...}} body of a response.
func errorBody(t *testing.T, w *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return body.Error.Code, body.Error.Message
}

func TestValidateIsRefusedWhileAProofIsOpen(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	queueRound(t, s, p, wizardSpec(t, s, p))

	_, err := s.RequestValidation(ctx, p.ID, 1)
	if !errors.Is(err, ErrChecking) || err.Error() != checkingCopy {
		t.Fatalf("validate during a proof: %v, want ErrChecking with the plan's copy", err)
	}
	if _, err := s.RequestGenerationThenValidation(ctx, p.ID, 1); !errors.Is(err, ErrChecking) {
		t.Errorf("generate then validate during a proof: %v", err)
	}
	if validateJobs(t, s) != 0 || count(t, s, "SELECT count(*) FROM validations") != 0 {
		t.Error("a refused validation queued a job")
	}

	// The response: 409, code checking, the exact copy.
	w := httptest.NewRecorder()
	(&Handlers{}).problemError(w, httptest.NewRequest("POST", "/api/admin/problems/1/validate", nil), err)
	code, msg := errorBody(t, w)
	if w.Code != http.StatusConflict || code != "checking" || msg != checkingCopy {
		t.Errorf("response %d %q %q, want 409 checking %q", w.Code, code, msg, checkingCopy)
	}

	// Once the round ends, Validate is allowed (the last proof job queued one).
	runProofs(t, s, nil)
	if _, err := s.RequestValidation(ctx, p.ID, 1); err != nil {
		t.Errorf("validate after the proof: %v", err)
	}
}

// Validate and a proof round each begin a write transaction and check the
// other inside it, so one runs wholly before the other: a Validate that was
// accepted was queued before the proof round was, and a refused one found
// the round open.
func TestValidateAndAProofRoundDoNotInterleave(t *testing.T) {
	s := newProofBank(t, true)
	ctx := context.Background()
	p := proofProblem(t, s)
	spec := wizardSpec(t, s, p)
	var refused, accepted int
	for i := 0; i < 30; i++ {
		if _, err := s.DB.Exec("DELETE FROM validations"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec("DELETE FROM jobs"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		var vErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, vErr = s.RequestValidation(ctx, p.ID, 1)
		}()
		go func() {
			defer wg.Done()
			<-start
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer tx.Rollback()
			if _, err := s.queue().enqueueProofs(ctx, tx, p, 1, spec); err != nil {
				t.Error(err)
				return
			}
			if err := tx.Commit(); err != nil {
				t.Error(err)
			}
		}()
		close(start)
		wg.Wait()
		var firstProof int64
		s.DB.QueryRow("SELECT min(id) FROM jobs WHERE priority = ?", ProofPriority).Scan(&firstProof)
		switch {
		case vErr == nil:
			accepted++
			var validateID int64
			s.DB.QueryRow("SELECT id FROM jobs WHERE priority = ?", ValidatePriority).Scan(&validateID)
			if validateID == 0 || validateID > firstProof {
				t.Fatalf("round %d: a validation (job %d) was accepted after the proof round began (first job %d)", i, validateID, firstProof)
			}
		case errors.Is(vErr, ErrChecking):
			refused++
			if validateJobs(t, s) != 0 {
				t.Fatalf("round %d: refused, yet a validation was queued", i)
			}
		default:
			t.Fatalf("round %d: %v", i, vErr)
		}
	}
	t.Logf("%d accepted before the round, %d refused during it", accepted, refused)
}
