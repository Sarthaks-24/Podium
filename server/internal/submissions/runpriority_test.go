package submissions

import (
	"context"
	"testing"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// A practice Run is queued at practice priority (J0, V203-70), so it never
// outranks graded work; a contest Run keeps the Run priority. With the end
// order off, both are queued as before.
func TestPracticeRunsQueueAtPracticePriority(t *testing.T) {
	for _, tc := range []struct {
		name     string
		practice bool
		off      bool
		want     int
	}{
		{"practice", true, false, PracticePriority},
		{"in contest", false, false, RunPriority},
		{"practice, order off", true, true, RunPriority},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.jobs.EndOrderOff = tc.off
			ctx := context.Background()
			p := r.importPack(t, nil)
			v, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "int main(){}",
				Scope: Scope{ContestID: 9, Practice: tc.practice}})
			if err != nil {
				t.Fatal(err)
			}
			var prio int
			if err := r.svc.DB.QueryRowContext(ctx, `SELECT j.priority FROM jobs j JOIN submissions s ON s.job_id = j.id WHERE s.id = ?`, v.ID).Scan(&prio); err != nil {
				t.Fatal(err)
			}
			if prio != tc.want {
				t.Errorf("run queued at priority %d, want %d", prio, tc.want)
			}
			// The Run still gets its result, and its author can run again.
			r.judgeNext(t, judgeapi.KindRun, nil)
			r.clock.advance(3 * time.Second)
			if _, err := r.svc.Run(ctx, RunRequest{ProblemID: p.ID, UserID: 1, Language: "cpp17", Source: "int main(){}",
				Scope: Scope{ContestID: 9, Practice: tc.practice}}); err != nil {
				t.Errorf("run again after the first finished: %v", err)
			}
		})
	}
}
