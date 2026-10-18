package contests

import (
	"testing"
	"time"
)

func TestFixedWindowWithExtensions(t *testing.T) {
	s := Schedule{Mode: FixedStart, StartsAt: at(0), EndsAt: at(120), Extended: 10 * time.Minute}
	w, ok := s.StudentWindow(time.Time{}, 15*time.Minute)
	if !ok || !w.Opens.Equal(at(0)) || !w.Ends.Equal(at(145)) {
		t.Errorf("%+v", w)
	}
}

func TestFlexibleWindowCappedAtClose(t *testing.T) {
	s := Schedule{Mode: WindowStart, StartsAt: at(0), EndsAt: at(180), Duration: 120 * time.Minute}
	if _, ok := s.StudentWindow(time.Time{}, 0); ok {
		t.Error("a student who has not started has a window")
	}
	w, _ := s.StudentWindow(at(30), 0)
	if !w.Ends.Equal(at(150)) {
		t.Errorf("early starter ends %v", w.Ends)
	}
	w, _ = s.StudentWindow(at(100), 0)
	if !w.Ends.Equal(at(180)) {
		t.Errorf("late starter should be capped at the close, ends %v", w.Ends)
	}
	w, _ = s.StudentWindow(at(100), 20*time.Minute)
	if !w.Ends.Equal(at(200)) {
		t.Errorf("a personal extension also extends the cap, ends %v", w.Ends)
	}
}

func TestStates(t *testing.T) {
	s := Schedule{Mode: FixedStart, StartsAt: at(0), EndsAt: at(120), Freeze: 30 * time.Minute}
	for min, want := range map[int]string{-5: StateUpcoming, 10: StateLive, 95: StateFrozen, 125: StateEnded} {
		if got := s.State(at(min), false); got != want {
			t.Errorf("minute %d: %s, want %s", min, got, want)
		}
	}
	if s.State(at(125), true) != StatePractice {
		t.Error("practice after end")
	}
	s.Mode = WindowStart
	if s.State(at(95), false) != StateLive {
		t.Error("window mode shows frozen")
	}
	if !s.CanStart(at(10)) || s.CanStart(at(-1)) || s.CanStart(at(121)) {
		t.Error("CanStart")
	}
}
