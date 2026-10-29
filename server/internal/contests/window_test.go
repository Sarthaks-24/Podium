package contests

import (
	"testing"
	"time"
)

func TestWindowWithExtensions(t *testing.T) {
	s := Schedule{StartsAt: at(0), EndsAt: at(120), Extended: 10 * time.Minute}
	w := s.StudentWindow(15 * time.Minute)
	if !w.Opens.Equal(at(0)) || !w.Ends.Equal(at(145)) {
		t.Errorf("%+v", w)
	}
}

func TestStates(t *testing.T) {
	s := Schedule{StartsAt: at(0), EndsAt: at(120), Freeze: 30 * time.Minute}
	for min, want := range map[int]string{-5: StateUpcoming, 10: StateLive, 95: StateFrozen, 125: StateEnded} {
		if got := s.State(at(min), false); got != want {
			t.Errorf("minute %d: %s, want %s", min, got, want)
		}
	}
	if s.State(at(125), true) != StatePractice {
		t.Error("practice after end")
	}
}
