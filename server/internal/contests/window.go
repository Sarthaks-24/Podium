package contests

import "time"

// Schedule is when a contest runs. Everyone starts together; the
// per-student start window was cut (V3-15).
type Schedule struct {
	StartsAt time.Time
	EndsAt   time.Time
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

// StudentWindow returns a student's window: the contest's, ending later by
// their personal extension.
func (s Schedule) StudentWindow(personal time.Duration) Window {
	return Window{Opens: s.StartsAt, Ends: s.Close().Add(personal)}
}

// Contest states shown to students (design.md section 8).
const (
	StateUpcoming = "upcoming"
	StateLive     = "live"
	StateFrozen   = "frozen"
	StateEnded    = "ended"
	StatePractice = "practice"
)

// State is the contest's state word at now; frozen during the freeze.
func (s Schedule) State(now time.Time, practice bool) string {
	switch {
	case now.Before(s.StartsAt):
		return StateUpcoming
	case now.Before(s.Close()):
		if s.Freeze > 0 && !now.Before(s.Close().Add(-s.Freeze)) {
			return StateFrozen
		}
		return StateLive
	case practice:
		return StatePractice
	}
	return StateEnded
}
