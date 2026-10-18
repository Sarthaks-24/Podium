package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// uploadKind is echoKind with an upload budget of one file.
type uploadKind struct{ echoKind }

func (uploadKind) MaxUploads(json.RawMessage) int { return 1 }

func newMixed(t *testing.T) (*Service, *clock) {
	s, c, k := newService(t)
	s.Kinds = map[string]Kind{judgeapi.KindValidate: k, judgeapi.KindGenerate: &uploadKind{}, judgeapi.KindSubmit: k, judgeapi.KindRun: k}
	return s, c
}

func add(t *testing.T, s *Service, kind string, priority int, contest int64) int64 {
	t.Helper()
	ctx := context.Background()
	tx, _ := s.DB.BeginTx(ctx, nil)
	id, err := s.Enqueue(ctx, tx, kind, priority, map[string]int{"p": priority}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if contest > 0 {
		tx.Exec("UPDATE jobs SET contest_id = ? WHERE id = ?", contest, id)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func claimKinds(t *testing.T, s *Service, kinds ...string) *judgeapi.Claim {
	t.Helper()
	c, err := s.Claim(context.Background(), judgeapi.ClaimRequest{Kinds: kinds, Worker: "w"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func contest(t *testing.T, s *Service, c *clock, paused, graded bool) int64 {
	t.Helper()
	now := c.t.UnixMilli()
	res, err := s.DB.Exec(`INSERT INTO contests (slug, title, starts_at, ends_at, graded, published, paused, created_at, updated_at)
		VALUES (?, 'C', ?, ?, ?, 1, ?, ?, ?)`, "c"+time.Now().Format("150405.000000000"), now-60_000, now+3_600_000, graded, paused, now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestPausedContestJobsWaitAtClaim(t *testing.T) {
	s, c := newMixed(t)
	paused := contest(t, s, c, true, false)
	held := add(t, s, judgeapi.KindSubmit, 20, paused)
	other := add(t, s, judgeapi.KindSubmit, 25, 0)
	if got := claimKinds(t, s, judgeapi.KindSubmit); got == nil || got.JobID != other {
		t.Fatalf("claimed %+v, want the job outside the paused contest", got)
	}
	if got := claimKinds(t, s, judgeapi.KindSubmit); got != nil {
		t.Fatalf("claimed %d from a paused contest", got.JobID)
	}
	s.DB.Exec("UPDATE contests SET paused = 0 WHERE id = ?", paused)
	if got := claimKinds(t, s, judgeapi.KindSubmit); got == nil || got.JobID != held {
		t.Fatalf("after resume claimed %+v", got)
	}
}

func TestOneSetterJobAtATimeDuringAGradedContest(t *testing.T) {
	s, _ := newMixed(t)
	add(t, s, judgeapi.KindValidate, 90, 0)
	add(t, s, judgeapi.KindGenerate, 85, 0)
	if claimKinds(t, s, judgeapi.KindValidate, judgeapi.KindGenerate) == nil {
		t.Fatal("no first setter job")
	}
	// No live graded contest: a second lane may take the other one.
	if claimKinds(t, s, judgeapi.KindValidate, judgeapi.KindGenerate) == nil {
		t.Fatal("second setter job refused without a contest")
	}
	s2, c2 := newMixed(t)
	contest(t, s2, c2, false, true)
	add(t, s2, judgeapi.KindValidate, 90, 0)
	add(t, s2, judgeapi.KindGenerate, 85, 0)
	sub := add(t, s2, judgeapi.KindSubmit, 20, 0)
	if claimKinds(t, s2, judgeapi.KindValidate, judgeapi.KindGenerate) == nil {
		t.Fatal("no first setter job during the contest")
	}
	if got := claimKinds(t, s2, judgeapi.KindValidate, judgeapi.KindGenerate); got != nil {
		t.Fatalf("second setter job %d leased during a graded contest", got.JobID)
	}
	if got := claimKinds(t, s2, judgeapi.KindValidate, judgeapi.KindGenerate, judgeapi.KindSubmit); got == nil || got.JobID != sub {
		t.Fatalf("submissions must still flow: %+v", got)
	}
}

func TestInputGenerationCountsAsASetterJob(t *testing.T) {
	s, c := newMixed(t)
	s.Kinds[judgeapi.KindGenerateInputs] = &uploadKind{}
	contest(t, s, c, false, true)
	add(t, s, judgeapi.KindGenerateInputs, 85, 0)
	add(t, s, judgeapi.KindValidate, 90, 0)
	all := []string{judgeapi.KindGenerateInputs, judgeapi.KindValidate, judgeapi.KindGenerate}
	if got := claimKinds(t, s, all...); got == nil || got.Kind != judgeapi.KindGenerateInputs {
		t.Fatalf("first setter job %+v", got)
	}
	if got := claimKinds(t, s, all...); got != nil {
		t.Fatalf("validation leased beside an input generation during a graded contest: %+v", got)
	}
}

func TestRunsDoNotStarveSubmissions(t *testing.T) {
	s, _ := newMixed(t)
	var runs []int64
	for i := 0; i < 4; i++ {
		runs = append(runs, add(t, s, judgeapi.KindRun, 10, 0))
	}
	subA := add(t, s, judgeapi.KindSubmit, 20, 0)
	subB := add(t, s, judgeapi.KindSubmit, 21, 0)
	var order []int64
	for {
		got := claimKinds(t, s, judgeapi.KindRun, judgeapi.KindSubmit)
		if got == nil {
			break
		}
		order = append(order, got.JobID)
	}
	want := []int64{runs[0], subA, runs[1], subB, runs[2], runs[3]}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

func TestUploadSlotsAreReservedAtomically(t *testing.T) {
	s, _ := newMixed(t)
	id := add(t, s, judgeapi.KindGenerate, 85, 0)
	c := claimKinds(t, s, judgeapi.KindGenerate)
	ctx := context.Background()
	ok1, _ := s.MayUpload(ctx, id, c.Lease)
	ok2, _ := s.MayUpload(ctx, id, c.Lease)
	if !ok1 || ok2 {
		t.Fatalf("budget of one: first %v second %v", ok1, ok2)
	}
	s.ReleaseUpload(id) // the first upload failed
	if ok, _ := s.MayUpload(ctx, id, c.Lease); !ok {
		t.Fatal("a released slot must be free again")
	}
	s.RecordUpload(id, "ab")
	if ok, _ := s.MayUpload(ctx, id, c.Lease); ok {
		t.Fatal("budget spent, yet another upload was allowed")
	}
}
