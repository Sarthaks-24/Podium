package contests

import "time"

// Start modes.
const (
	FixedStart  = "fixed"
	WindowStart = "window"
)

// Schedule is when a contest runs.
type Schedule struct {
	Mode     string
	StartsAt time.Time
	EndsAt   time.Time
	// Duration is each student's time in window mode.
	Duration time.Duration
	// Extended is added to everyone's end (the "extend contest" button).
	Extended time.Duration
	Freeze   time.Duration
}

// Close is when the contest is over for everyone without a personal
// extension: the end of the schedule plus any contest-wide extension.
func (s Schedule) Close() time.Time { return s.EndsAt.Add(s.Extended) }

// Window is one student's time in the contest.
type Window struct {
	Opens time.Time
	Ends  time.Time
}

// StudentWindow returns a student's window. started is when they pressed
// Start (window mode; zero when they have not); personal is their own
// extension. In window mode the clock runs Duration from their start, and
// never past the close plus their extension.
func (s Schedule) StudentWindow(started time.Time, personal time.Duration) (Window, bool) {
	if s.Mode != WindowStart {
		return Window{Opens: s.StartsAt, Ends: s.Close().Add(personal)}, true
	}
	if started.IsZero() {
		return Window{}, false
	}
	end := started.Add(s.Duration + s.Extended + personal)
	if limit := s.Close().Add(personal); end.After(limit) {
		end = limit
	}
	return Window{Opens: started, Ends: end}, true
}

// Contest states shown to students (design.md section 8).
const (
	StateUpcoming = "upcoming"
	StateLive     = "live"
	StateFrozen   = "frozen"
	StateEnded    = "ended"
	StatePractice = "practice"
)

// State is the contest's state word at now. Frozen applies to fixed-start
// contests during the freeze; window mode never shows "frozen" because its
// board is hidden from students until the close.
func (s Schedule) State(now time.Time, practice bool) string {
	switch {
	case now.Before(s.StartsAt):
		return StateUpcoming
	case now.Before(s.Close()):
		if s.Mode == FixedStart && s.Freeze > 0 && !now.Before(s.Close().Add(-s.Freeze)) {
			return StateFrozen
		}
		return StateLive
	case practice:
		return StatePractice
	}
	return StateEnded
}

// CanStart reports whether a student may press Start in window mode: the
// contest has begun and enough time remains to be worth starting (the
// clock would still end at the close).
func (s Schedule) CanStart(now time.Time) bool {
	return !now.Before(s.StartsAt) && now.Before(s.Close())
}
