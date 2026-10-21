package contests

import (
	"context"
	"testing"
)

// LiveUntil in window mode follows the student's own clock, and a
// personal extension keeps a fixed-start contest live for that student
// only (C1-022).
func TestLiveUntilWindowModeAndExtensions(t *testing.T) {
	ctx := context.Background()
	t.Run("window", func(t *testing.T) {
		r := newSvc(t)
		// Open from t0 to t0+180 min; each student has 60 minutes.
		c := r.contest(t, func(e *Edit) { e.StartMode, e.DurationMinutes, e.EndsAt = WindowStart, 60, at(180) })
		*r.now = at(30)
		if err := r.svc.Start(ctx, c, 2, true); err != nil {
			t.Fatal(err)
		}
		until, live, err := r.svc.LiveUntil(ctx, 2)
		if err != nil || !live || !until.Equal(at(90)) {
			t.Errorf("inside the window: %v %v %v, want live until %v", until, live, err, at(90))
		}
		if _, live, _ := r.svc.LiveUntil(ctx, 3); live {
			t.Error("a student who never started is live")
		}
		*r.now = at(91)
		if _, live, _ := r.svc.LiveUntil(ctx, 2); live {
			t.Error("live after the student's own window, while the contest is still open")
		}
	})
	t.Run("extension", func(t *testing.T) {
		r := newSvc(t)
		c := r.contest(t, nil) // fixed start, t0 to t0+120 min
		*r.now = at(10)
		for _, u := range []int64{2, 3} {
			if err := r.svc.Start(ctx, c, u, true); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.svc.SetExtension(ctx, c.ID, 2, 30, "a documented accommodation"); err != nil {
			t.Fatal(err)
		}
		*r.now = at(130)
		until, live, err := r.svc.LiveUntil(ctx, 2)
		if err != nil || !live || !until.Equal(at(150)) {
			t.Errorf("with 30 extra minutes: %v %v %v, want live until %v", until, live, err, at(150))
		}
		if _, live, _ := r.svc.LiveUntil(ctx, 3); live {
			t.Error("another student's extension kept this one live")
		}
		*r.now = at(151)
		if _, live, _ := r.svc.LiveUntil(ctx, 2); live {
			t.Error("live after the extension ran out")
		}
	})
}
