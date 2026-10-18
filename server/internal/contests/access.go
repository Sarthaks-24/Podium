package contests

import (
	"context"
	"errors"
	"time"

	"github.com/Sarthaks-24/podium/internal/submissions"
)

// Refusals the API turns into 403 or 409 with the message as written.
var (
	ErrBoardClosed  = errors.New("The board opens when your window closes.")
	ErrNotStarted   = errors.New("Accept the rules on the contest page to start.")
	ErrContestOver  = errors.New("The contest has ended.")
	ErrPracticeWait = errors.New("Your time is up. Practice opens when every student's time is up.")
	ErrNotReady     = errors.New("This problem is being updated by the organisers. Try again in a few minutes.")
	ErrRevealEarly  = errors.New("The reveal opens when every student's time is up.")
)

// Final is when every student's window has closed: the close plus the
// longest personal extension (as loaded with the contest).
func (c *Contest) Final() time.Time { return c.Schedule.Close().Add(c.MaxExtension) }

// FinalAt is Final, kept for callers that pass a context.
func (s *Service) FinalAt(_ context.Context, c *Contest) (time.Time, error) { return c.Final(), nil }

// PracticeOpen reports whether a student may submit for practice now: the
// contest allows it, their own window is over, and every window is over.
// Opening it earlier would let students who finished test solutions
// against hidden tests (with full feedback) while a classmate with a later
// start or a personal extension is still competing.
func (c *Contest) PracticeOpen(st StudentState, now time.Time) bool {
	return c.PracticeAfterEnd && st.Over && !now.Before(c.Final())
}

// StateFor is the contest's state word for one student at now: a personal
// extension keeps the contest live for them, and in window mode their own
// clock decides.
func (c *Contest) StateFor(st StudentState, now time.Time) string {
	sch := c.Schedule
	switch {
	case now.Before(sch.StartsAt):
		return StateUpcoming
	case !st.Over:
		if sch.Mode == FixedStart && sch.Freeze > 0 && !now.Before(sch.Close().Add(-sch.Freeze)) {
			return StateFrozen
		}
		return StateLive
	case c.PracticeOpen(st, now):
		return StatePractice
	}
	return StateEnded
}

// ProblemsVisible reports whether a student may see the problems: once
// their window has started (window mode) or the contest has started (fixed)
// and they accepted the rules, or once their window is over.
func (c *Contest) ProblemsVisible(st StudentState, now time.Time) bool {
	if st.Over {
		return !now.Before(c.Schedule.StartsAt)
	}
	return st.Accepted && st.HasClock && !now.Before(st.Window.Opens)
}

// StudentVariant is the board a student may see now, or ErrBoardClosed.
//
//   - Exam feedback and window mode: no board until the student's own
//     window closes (window-mode ruling; exam mode shows no scores).
//   - Fixed start: the full board until the freeze, the frozen board from
//     the freeze until every window has closed, then full; with HoldReveal
//     the frozen board stays until an organiser reveals.
//   - Window mode after the student's window: full (frozen while held).
func (s *Service) StudentVariant(ctx context.Context, c *Contest, st StudentState) (string, error) {
	now := s.now()
	if (c.Schedule.Mode == WindowStart || c.Feedback == "exam") && !st.Over {
		return "", ErrBoardClosed
	}
	if c.Schedule.Freeze == 0 {
		return VariantFull, nil
	}
	held := c.HoldReveal && c.RevealedAt.IsZero()
	if c.Schedule.Mode == WindowStart {
		if held {
			return VariantFrozen, nil
		}
		return VariantFull, nil
	}
	if now.Before(c.Schedule.Close().Add(-c.Schedule.Freeze)) {
		return VariantFull, nil
	}
	if now.Before(c.Final()) || held {
		return VariantFrozen, nil
	}
	return VariantFull, nil
}

// FrozenAt is when the frozen board stops counting submissions, for fixed
// starts (zero in window mode, where each student freezes at their own
// end, or without a freeze).
func (c *Contest) FrozenAt() time.Time {
	if c.Schedule.Mode != FixedStart || c.Schedule.Freeze == 0 {
		return time.Time{}
	}
	return c.Schedule.Close().Add(-c.Schedule.Freeze)
}

// ViewOptions says how a student sees their own submissions in the
// contest: coarse figures always (D47), times to 100 ms in graded contests
// and 10 ms otherwise, and in exam mode nothing but "Submitted" until their
// window closes.
func (c *Contest) ViewOptions(st StudentState) submissions.ViewOptions {
	o := submissions.ViewOptions{Coarse: true, RoundMS: 10}
	if c.Graded {
		o.RoundMS = 100
	}
	o.Hide = c.Feedback == "exam" && !st.Over
	return o
}

// ViewFor is ViewOptions for a contest and student by id, for the live
// event stream (submissions.Service.ContestView). On any error it hides
// everything rather than show too much.
func (s *Service) ViewFor(ctx context.Context, contestID, userID int64) submissions.ViewOptions {
	c, err := s.Get(ctx, contestID)
	if err != nil {
		return submissions.ViewOptions{Coarse: true, RoundMS: 100, Hide: true}
	}
	st, err := s.State(ctx, c, userID)
	if err != nil {
		return submissions.ViewOptions{Coarse: true, RoundMS: 100, Hide: true}
	}
	return c.ViewOptions(st)
}

// MyScore is a student's own result: their total and a cell per problem,
// from all their non-practice submissions (never frozen: a student always
// knows their own results, unless exam mode hides them).
func (s *Service) MyScore(ctx context.Context, c *Contest, st StudentState, userID int64) (int, map[string]Cell, error) {
	entries, _, err := s.entries(ctx, c, userID)
	if err != nil {
		return 0, nil, err
	}
	b := s.scoringBoard(c)
	p := Participant{UserID: userID, Started: st.Window.Opens, Ends: st.Window.Ends}
	rows := Compute(b, []Participant{p}, entries, false)
	cells := map[string]Cell{}
	for _, cell := range rows[0].Cells {
		cells[cell.Label] = cell
	}
	return rows[0].Total, cells, nil
}
