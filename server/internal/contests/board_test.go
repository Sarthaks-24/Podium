package contests

// The standings under load (m4-security finding 1) and the smaller
// correctness findings 5, 7, 8 and 9, at the service level.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// seedBoard adds students with submissions to a contest with five
// problems, writing rows the way judging does (inline source, full result
// and the compact earned column).
func seedBoard(t testing.TB, r *svcRig, c *Contest, students, perProblem int) {
	t.Helper()
	ctx := context.Background()
	c.Problems = nil
	for i := 0; i < 5; i++ {
		c.Problems = append(c.Problems, ContestProblem{Label: string(rune('A' + i)), ProblemID: int64(100 + i), Points: []int{20, 30, 50}, Ready: true})
	}
	conn, err := r.svc.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	tx, _ := conn.BeginTx(ctx, nil)
	src, res := strings.Repeat("x", 3000), `{"subtasks":[{"earned":20},{"earned":30},{"earned":0}],"compile_output":"`+strings.Repeat("w", 1500)+`"}`
	for u := 0; u < students; u++ {
		uid := 1000 + u
		tx.ExecContext(ctx, "INSERT INTO users (id, email, name, role, created_at) VALUES (?, ?, ?, 'student', 0)", uid, fmt.Sprintf("s%d@college.edu", u), fmt.Sprintf("S%d", u))
		tx.ExecContext(ctx, "INSERT INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (?, ?, ?, ?)", c.ID, uid, t0.UnixMilli(), t0.UnixMilli())
		for p := 0; p < 5; p++ {
			for k := 0; k < perProblem; k++ {
				if _, err := tx.ExecContext(ctx, `INSERT INTO submissions (problem_id, contest_id, user_id, kind, language, source, source_sha, state, verdict, score,
					result, earned, created_at) VALUES (?, ?, ?, 'submit', 'cpp17', ?, 'x', 'done', 'WA', 50, ?, '[20,30,0]', ?)`,
					100+p, c.ID, uid, src, res, t0.Add(time.Duration(k)*time.Minute).UnixMilli()); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestHundredReadsAfterAChangeComputeOnce(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	seedBoard(t, r, c, 60, 4)
	first, err := r.svc.Board(ctx, c, VariantFull)
	if err != nil || len(first.Rows) != 60 || first.Rows[0].Total != 250 {
		t.Fatalf("board %v %+v", err, first)
	}
	// A change, then 100 readers at once, after the rate limit's window.
	r.svc.board.bump(c.ID)
	r.svc.board.mu.Lock()
	r.svc.board.entries[boardKey{c.ID, VariantFull}].lastStart = time.Now().Add(-time.Hour)
	r.svc.board.mu.Unlock()
	before := r.svc.board.computes.Load()
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b, err := r.svc.Board(ctx, c, VariantFull); err != nil || len(b.Rows) != 60 {
				t.Errorf("read %v", err)
			}
		}()
	}
	wg.Wait()
	if n := r.svc.board.computes.Load() - before; n != 1 {
		t.Errorf("100 concurrent reads after one change computed %d boards, want 1", n)
	}
	t.Logf("100 concurrent reads after a change: %v", time.Since(start))

	// Inside the rate limit's window a new change is not recomputed on
	// read: the last board is served and one refresh is scheduled.
	r.svc.board.bump(c.ID)
	before = r.svc.board.computes.Load()
	for i := 0; i < 20; i++ {
		if _, err := r.svc.Board(ctx, c, VariantFull); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.svc.board.computes.Load() - before; n != 0 {
		t.Errorf("reads inside the window computed %d boards", n)
	}
	r.svc.board.mu.Lock()
	pending := r.svc.board.refresh[c.ID]
	r.svc.board.mu.Unlock()
	if !pending {
		t.Error("no refresh scheduled for the stale board")
	}
}

func TestBoardReadsOnlyTheIndex(t *testing.T) {
	r := newSvc(t)
	rows, err := r.svc.DB.Query("EXPLAIN QUERY PLAN "+entriesQuery, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		rows.Scan(&id, &parent, &unused, &detail)
		plan = append(plan, detail)
	}
	if !strings.Contains(strings.Join(plan, " "), "COVERING INDEX submissions_by_board") {
		t.Errorf("the board reads table rows: %v", plan)
	}
}

func BenchmarkBoardRecompute(b *testing.B) {
	r := newSvc(b)
	c := r.contest(b, nil)
	seedBoard(b, r, c, 300, 8)
	ctx := context.Background()
	for b.Loop() {
		if _, err := r.svc.computeBoard(ctx, c, VariantFull, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPracticeAndEditorialWaitForEveryWindow(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	r.svc.SetEditorial(ctx, c.ID, "Use a 64-bit sum.", true)
	r.svc.SetExtension(ctx, c.ID, 3, 30, "accommodation")
	*r.now = at(125) // after the close, inside Bilal's extension
	c, _ = r.svc.Get(ctx, c.ID)
	st, _ := r.svc.State(ctx, c, 2)
	if !st.Over || c.PracticeOpen(st, *r.now) || c.EditorialVisible(st, *r.now) {
		t.Errorf("practice or editorial open while Bilal still competes: %+v", st)
	}
	*r.now = at(151)
	if !c.PracticeOpen(st, *r.now) || !c.EditorialVisible(st, *r.now) {
		t.Error("practice and editorial should open once every window is over")
	}
}

func TestBoardScoresAgainstPinnedPoints(t *testing.T) {
	b := board(BestSubtask)
	entries := []Entry{{UserID: 1, ProblemID: 1, At: at(10), Done: true, Earned: []int{30, 90}, Score: 120}}
	rows := Compute(b, people("Asha"), entries, false)
	if rows[0].Cells[0].Score != 100 {
		t.Errorf("earned points above the pinned maximum: %+v", rows[0].Cells[0])
	}
	b = board(BestSubmission)
	if rows := Compute(b, people("Asha"), entries, false); rows[0].Cells[0].Score != 100 {
		t.Errorf("best submission above the maximum: %+v", rows[0].Cells[0])
	}
}

func TestFixedFreezeIsContestWide(t *testing.T) {
	b := board(BestSubtask)
	b.FreezeAt = at(90)
	p := []Participant{{UserID: 1, Name: "Bilal", Started: t0, Ends: at(150)}} // a 30-minute extension
	entries := []Entry{{UserID: 1, ProblemID: 2, At: at(100), Done: true, Earned: []int{100}, Score: 100}}
	rows := Compute(b, p, entries, true)
	if rows[0].Total != 0 || rows[0].Cells[1].Pending != 1 {
		t.Errorf("a score after the contest's freeze shows: %+v", rows[0])
	}
}

func TestClarificationCapHoldsUnderParallelRequests(t *testing.T) {
	r := newSvc(t)
	ctx := context.Background()
	c := r.contest(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.svc.Ask(ctx, c, 2, nil, fmt.Sprintf("Question %d?", i))
		}(i)
	}
	wg.Wait()
	var n int
	r.svc.DB.QueryRow("SELECT count(*) FROM clarifications WHERE contest_id = ? AND asked_by = 2", c.ID).Scan(&n)
	if n != maxOpenQuestions {
		t.Errorf("%d open questions stored, want %d", n, maxOpenQuestions)
	}
}
