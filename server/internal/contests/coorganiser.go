package contests

// Rules for co-organisers whose site role is student, usually TAs
// (security council S1, 2026-10-18). Owners and admins organise every
// contest; a co-organiser organises one, and is a student everywhere else.
// So they may prepare and run a contest (announcements, clarifications,
// the schedule before it starts) but not change its grading once it
// starts, and they see results only as a student would, so the role is no
// oracle for hidden tests.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Sarthaks-24/podium/internal/submissions"
)

// ErrNotParticipant refuses an extension for someone who is neither on
// the roster nor taking part in the contest.
var ErrNotParticipant = errors.New("That person is not on this contest's roster and has not started it, so they cannot be given extra time.")

// Started reports whether the contest's start time has passed.
func (c *Contest) Started(now time.Time) bool { return !now.Before(c.Schedule.StartsAt) }

// LockedAfterStart lists the settings only owners and admins may change once
// a contest has started: everything that decides who competes, when, under
// which rules, and how they are scored. The field names are those of the
// contest form (docs/API.md).
var LockedAfterStart = []string{"slug", "starts_at", "ends_at", "start_mode", "duration_minutes", "freeze_minutes",
	"scoring_mode", "access", "feedback_mode", "graded", "practice_after_end", "stop_on_sample_fail", "min_gap_s",
	"max_per_problem", "languages", "hold_reveal"}

// LockedChanges returns the locked settings an edit would change.
func (c *Contest) LockedChanges(e Edit) []string {
	var out []string
	changed := map[string]bool{
		"slug":                e.Slug != c.Slug,
		"starts_at":           !e.StartsAt.Equal(c.Schedule.StartsAt),
		"ends_at":             !e.EndsAt.Equal(c.Schedule.EndsAt),
		"start_mode":          e.StartMode != c.Schedule.Mode,
		"duration_minutes":    e.StartMode == WindowStart && time.Duration(e.DurationMinutes)*time.Minute != c.Schedule.Duration,
		"freeze_minutes":      time.Duration(e.FreezeMinutes)*time.Minute != c.Schedule.Freeze,
		"scoring_mode":        e.Scoring != c.Scoring,
		"access":              e.Access != c.Access,
		"feedback_mode":       e.Feedback != c.Feedback,
		"graded":              e.Graded != c.Graded,
		"practice_after_end":  e.PracticeAfterEnd != c.PracticeAfterEnd,
		"stop_on_sample_fail": e.StopOnSampleFail != c.StopOnSampleFail,
		"min_gap_s":           time.Duration(e.MinGapS)*time.Second != c.MinGap,
		"max_per_problem":     e.MaxPerProblem != c.MaxPerProblem,
		"languages":           !slices.Equal(e.Languages, c.Languages),
		"hold_reveal":         e.HoldReveal != c.HoldReveal,
	}
	for _, f := range LockedAfterStart {
		if changed[f] {
			out = append(out, f)
		}
	}
	return out
}

// Hide is what a co-organiser without the admin role may not see of the
// contest's results right now.
type Hide struct {
	// All hides every result: exam mode until every student's time is up.
	All bool
	// From hides the results of submissions made at or after this time
	// (the freeze, while students see the frozen board). Zero hides none.
	From time.Time
}

// Active reports whether anything is hidden.
func (h Hide) Active() bool { return h.All || !h.From.IsZero() }

// Covers reports whether a submission made at created is hidden.
func (h Hide) Covers(created time.Time) bool {
	return h.All || (!h.From.IsZero() && !created.Before(h.From))
}

// coOrganiserState is the student state a co-organiser is treated as
// having: not competing, with a window that closes when the last student's
// does.
func (s *Service) coOrganiserState(c *Contest) StudentState {
	return StudentState{Over: !s.now().Before(c.Final())}
}

// CoOrganiserHide is what a co-organiser without the admin role may not see
// now: the student rules, as for a student whose window closes last. In
// exam mode no verdict shows until every window is over; while students
// see the frozen board, nothing judged after the freeze shows either.
func (s *Service) CoOrganiserHide(ctx context.Context, c *Contest) Hide {
	st := s.coOrganiserState(c)
	if c.Feedback == "exam" && !st.Over {
		return Hide{All: true}
	}
	if v, err := s.StudentVariant(ctx, c, st); err == nil && v == VariantFrozen && !c.FrozenAt().IsZero() {
		return Hide{From: c.FrozenAt()}
	}
	return Hide{}
}

// CoOrganiserVariant is the board a co-organiser without the admin role
// may see now, or ErrBoardClosed: the board a student whose window closes
// last would see.
func (s *Service) CoOrganiserVariant(ctx context.Context, c *Contest) (string, error) {
	return s.StudentVariant(ctx, c, s.coOrganiserState(c))
}

// CoOrganiserViewOptions is how a co-organiser without the admin role sees
// their own submissions in the contest: the student view, with results
// hidden in exam mode until every window is over.
func (s *Service) CoOrganiserViewOptions(c *Contest) submissions.ViewOptions {
	o := c.ViewOptions(s.coOrganiserState(c))
	// Not trusted with hidden tests, so never shown a revealed one (D115).
	o.NoReveal = true
	return o
}

// CoOrganiserView is how a co-organiser without the admin role sees one
// submission of the contest (anyone's): the coarse student view, hidden
// while CoOrganiserHide covers it. On any error it hides everything.
func (s *Service) CoOrganiserView(ctx context.Context, contestID, submissionID int64) submissions.ViewOptions {
	hidden := submissions.ViewOptions{Coarse: true, RoundMS: 100, Hide: true}
	c, err := s.Get(ctx, contestID)
	if err != nil {
		return hidden
	}
	var created int64
	if err := s.DB.QueryRowContext(ctx, "SELECT created_at FROM submissions WHERE id = ? AND contest_id = ?", submissionID, contestID).Scan(&created); err != nil {
		return hidden
	}
	o := c.ViewOptions(StudentState{Over: true})
	o.Hide = s.CoOrganiserHide(ctx, c).Covers(time.UnixMilli(created))
	return o
}

// IsParticipant reports whether a user may be given extra time: they are on
// the roster, or they started the contest or submitted to it, and they are
// a student (organisers have no clock to extend).
func (s *Service) IsParticipant(ctx context.Context, c *Contest, userID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users u WHERE u.id = ? AND u.role = 'student' AND (
		EXISTS (SELECT 1 FROM contest_roster r WHERE r.contest_id = ? AND r.email = u.email)
		OR EXISTS (SELECT 1 FROM contest_starts cs WHERE cs.contest_id = ? AND cs.user_id = u.id)
		OR EXISTS (SELECT 1 FROM submissions sb WHERE sb.contest_id = ? AND sb.user_id = u.id AND sb.kind = 'submit'))`,
		userID, c.ID, c.ID, c.ID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("participant check: %w", err)
	}
	return n > 0, nil
}
