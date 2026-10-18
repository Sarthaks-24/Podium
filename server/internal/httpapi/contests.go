package httpapi

// Student-facing contest routes (cache/v1/m4-plan.md section 4): the list,
// one contest, the honour code and start, the contest workspace, the
// standings and clarifications. Every contest-scoped id is resolved through
// the contest named in the URL, so an id from another contest is a 404.
// Times come from the server only, and every answer carries server_now.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

type windowJSON struct {
	StartedAt int64 `json:"started_at"`
	EndsAt    int64 `json:"ends_at"`
}

// contestSummaryJSON is ContestSummary in docs/API.md.
type contestSummaryJSON struct {
	Slug            string      `json:"slug"`
	Title           string      `json:"title"`
	StartsAt        int64       `json:"starts_at"`
	EndsAt          int64       `json:"ends_at"`
	StartMode       string      `json:"start_mode"`
	DurationMinutes int         `json:"duration_minutes"`
	ProblemCount    int         `json:"problem_count"`
	State           string      `json:"state"`
	MyScore         *int        `json:"my_score"`
	MyWindow        *windowJSON `json:"my_window"`
}

type rulesJSON struct {
	FeedbackMode  string   `json:"feedback_mode"`
	ScoringMode   string   `json:"scoring_mode"`
	MinGapS       int      `json:"min_gap_s"`
	MaxPerProblem int      `json:"max_per_problem"`
	Languages     []string `json:"languages"`
	FreezeMinutes int      `json:"freeze_minutes"`
	Graded        bool     `json:"graded"`
	Practice      bool     `json:"practice_after_end"`
}

type contestProblemJSON struct {
	Label    string `json:"label"`
	Title    string `json:"title"`
	MyScore  *int   `json:"my_score"`
	MaxScore int    `json:"max_score"`
	// MySubmissionCount counts the viewer's submissions (practice and judge
	// failures excluded), by creation only: in exam mode it is the one
	// per-problem figure shown, and it says nothing about judging.
	MySubmissionCount int `json:"my_submission_count"`
}

// contestJSON is Contest in docs/API.md.
type contestJSON struct {
	contestSummaryJSON
	DescriptionMD        string                   `json:"description_md"`
	Rules                rulesJSON                `json:"rules"`
	HonourText           string                   `json:"honour_text"`
	Accepted             bool                     `json:"accepted"`
	Organiser            bool                     `json:"organiser"`
	Problems             []contestProblemJSON     `json:"problems"`
	Announcements        []contests.Announcement  `json:"announcements"`
	MyClarifications     []contests.Clarification `json:"my_clarifications"`
	PublicClarifications []contests.Clarification `json:"public_clarifications"`
	EditorialMD          *string                  `json:"editorial_md"`
	ServerNow            int64                    `json:"server_now"`
}

// contestError maps the contests package's errors to responses.
func (d Deps) contestError(w http.ResponseWriter, r *http.Request, err error) {
	var fields contests.FieldErrors
	switch {
	case errors.Is(err, contests.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "There is no such contest, or it is not open to you.")
	case errors.Is(err, contests.ErrForbidden):
		WriteError(w, http.StatusForbidden, "not_on_roster", "This contest is for the students on its list. Ask the organiser if you should be on it.")
	case errors.As(err, &fields):
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "bad_request", Message: fields.Error(), Fields: fields}})
	case errors.Is(err, contests.ErrBoardClosed):
		WriteError(w, http.StatusForbidden, "board_closed", err.Error())
	case errors.Is(err, contests.ErrNotStarted):
		WriteError(w, http.StatusForbidden, "not_started", err.Error())
	case errors.Is(err, contests.ErrContestOver), errors.Is(err, contests.ErrPracticeWait):
		WriteError(w, http.StatusForbidden, "contest_over", err.Error())
	case errors.Is(err, contests.ErrNotReady):
		WriteError(w, http.StatusConflict, "not_ready", err.Error())
	case errors.Is(err, contests.ErrRevealEarly):
		WriteError(w, http.StatusConflict, "too_early", err.Error())
	default:
		d.internalError(w, r, err)
	}
}

func viewerOf(u auth.User) contests.Viewer {
	return contests.Viewer{UserID: u.ID, Email: u.Email, Organiser: u.Role.AtLeast(auth.Admin)}
}

// studentContest loads the contest named by {slug} and checks the viewer
// may enter it. organiser is true for owners, admins and its co-organisers.
func (d Deps) studentContest(w http.ResponseWriter, r *http.Request) (*contests.Contest, bool, bool) {
	c, err := d.Contests.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		d.contestError(w, r, err)
		return nil, false, false
	}
	organiser, err := d.Contests.MayEnter(r.Context(), c, viewerOf(userFrom(r.Context())))
	if err != nil {
		d.contestError(w, r, err)
		return nil, false, false
	}
	return c, organiser, true
}

func ptr[T any](v T) *T { return &v }

// summary builds ContestSummary for the viewer, and returns their state.
func (d Deps) summary(ctx context.Context, c *contests.Contest, userID int64, organiser bool) (contestSummaryJSON, contests.StudentState, error) {
	now := d.Contests.Clock()
	out := contestSummaryJSON{Slug: c.Slug, Title: c.Title, StartsAt: c.Schedule.StartsAt.UnixMilli(),
		EndsAt: c.Schedule.Close().UnixMilli(), StartMode: c.Schedule.Mode,
		DurationMinutes: int(c.Schedule.Duration / time.Minute), ProblemCount: len(c.Problems)}
	st, err := d.Contests.State(ctx, c, userID)
	if err != nil {
		return out, st, err
	}
	if organiser {
		out.State = c.Schedule.State(now, c.PracticeAfterEnd)
		return out, st, nil
	}
	out.State = c.StateFor(st, now)
	if st.HasClock {
		out.MyWindow = &windowJSON{StartedAt: st.Window.Opens.UnixMilli(), EndsAt: st.Window.Ends.UnixMilli()}
	}
	if st.Accepted && !c.ViewOptions(st).Hide {
		total, _, err := d.Contests.MyScore(ctx, c, st, userID)
		if err != nil {
			return out, st, err
		}
		out.MyScore = &total
	}
	return out, st, nil
}

func (d Deps) listContests(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	all, err := d.Contests.List(r.Context(), false)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := map[string][]contestSummaryJSON{"live": {}, "upcoming": {}, "past": {}}
	for _, c := range all {
		if !c.Published {
			continue
		}
		organiser, err := d.Contests.MayEnter(r.Context(), c, viewerOf(u))
		if errors.Is(err, contests.ErrForbidden) || errors.Is(err, contests.ErrNotFound) {
			continue
		}
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		s, _, err := d.summary(r.Context(), c, u.ID, organiser)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		switch s.State {
		case contests.StateLive, contests.StateFrozen:
			out["live"] = append(out["live"], s)
		case contests.StateUpcoming:
			out["upcoming"] = append(out["upcoming"], s)
		default:
			out["past"] = append(out["past"], s)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"live": out["live"], "upcoming": out["upcoming"], "past": out["past"],
		"server_now": d.Contests.Clock().UnixMilli()})
}

// contestDetail builds Contest for the viewer.
func (d Deps) contestDetail(ctx context.Context, c *contests.Contest, u auth.User, organiser bool) (*contestJSON, error) {
	s, st, err := d.summary(ctx, c, u.ID, organiser)
	if err != nil {
		return nil, err
	}
	now := d.Contests.Clock()
	out := &contestJSON{contestSummaryJSON: s, DescriptionMD: c.Description, HonourText: c.Honour(), Accepted: st.Accepted,
		Organiser: organiser, Problems: []contestProblemJSON{},
		Rules: rulesJSON{FeedbackMode: c.Feedback, ScoringMode: c.Scoring, MinGapS: int(c.MinGap / time.Second),
			MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, FreezeMinutes: int(c.Schedule.Freeze / time.Minute),
			Graded: c.Graded, Practice: c.PracticeAfterEnd},
		ServerNow: now.UnixMilli()}
	if organiser || c.ProblemsVisible(st, now) {
		var cells map[string]contests.Cell
		hide := organiser || c.ViewOptions(st).Hide
		if !hide {
			if _, cells, err = d.Contests.MyScore(ctx, c, st, u.ID); err != nil {
				return nil, err
			}
		}
		counts, err := d.Contests.SubmissionCounts(ctx, c, u.ID)
		if err != nil {
			return nil, err
		}
		for _, p := range c.Problems {
			pj := contestProblemJSON{Label: p.Label, Title: p.Title, MaxScore: p.MaxScore(), MySubmissionCount: counts[p.ProblemID]}
			if cell, ok := cells[p.Label]; ok {
				pj.MyScore = ptr(cell.Score)
			}
			out.Problems = append(out.Problems, pj)
		}
	}
	if out.Announcements, err = d.Contests.Announcements(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.MyClarifications, out.PublicClarifications, err = d.Contests.StudentClarifications(ctx, c.ID, u.ID); err != nil {
		return nil, err
	}
	if organiser || c.EditorialVisible(st, now) {
		if c.Editorial != "" && (c.EditorialPublished || organiser) {
			out.EditorialMD = ptr(c.Editorial)
		}
	}
	return out, nil
}

func (d Deps) getContest(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	out, err := d.contestDetail(r.Context(), c, userFrom(r.Context()), organiser)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (d Deps) startContest(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	var req struct {
		AcceptHonour bool `json:"accept_honour"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	if err := d.Contests.Start(r.Context(), c, u.ID, req.AcceptHonour); err != nil {
		d.contestError(w, r, err)
		return
	}
	out, err := d.contestDetail(r.Context(), c, u, organiser)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// contestTarget is the contest problem a workspace request is about and
// the rules that apply to the viewer.
type contestTarget struct {
	c         *contests.Contest
	problem   contests.ContestProblem
	scope     submissions.Scope
	st        contests.StudentState
	organiser bool
	// open is true when the viewer may run and submit now (for the score,
	// or as practice when scope.Practice is set); refusal says why not.
	open    bool
	refusal error
}

// contestProblem resolves {slug} and {label} and applies the time and
// access rules. Students see a problem only once their window started (or
// is over); organisers always.
func (d Deps) contestProblem(w http.ResponseWriter, r *http.Request) (*contestTarget, bool) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return nil, false
	}
	t := &contestTarget{c: c, organiser: organiser}
	found := false
	for _, p := range c.Problems {
		if p.Label == r.PathValue("label") {
			t.problem, found = p, true
		}
	}
	if !found {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest.")
		return nil, false
	}
	u := userFrom(r.Context())
	st, err := d.Contests.State(r.Context(), c, u.ID)
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	t.st = st
	t.scope = submissions.Scope{ContestID: c.ID, Languages: c.Languages, NoStopOnSample: !c.StopOnSampleFail}
	if organiser {
		t.open = true
		if u.Role.AtLeast(auth.Admin) {
			t.scope.MinGap = organiserGap
		} else {
			// A co-organiser (often a student TA) may try the contest's
			// problems at any time, but is not trusted with hidden data:
			// the student view of their own submissions and the contest's
			// limits.
			t.scope.MinGap, t.scope.MaxPerProblem = c.MinGap, c.MaxPerProblem
			t.scope.View = c.ViewOptions(contests.StudentState{Over: true})
		}
		if !t.problem.Ready {
			t.open, t.refusal = false, contests.ErrNotReady
		}
		return t, true
	}
	now := d.Contests.Clock()
	if !c.ProblemsVisible(st, now) {
		d.contestError(w, r, contests.ErrNotStarted)
		return nil, false
	}
	t.scope.MinGap, t.scope.MaxPerProblem, t.scope.View = c.MinGap, c.MaxPerProblem, c.ViewOptions(st)
	switch {
	case !t.problem.Ready:
		t.refusal = contests.ErrNotReady
	case st.Open:
		t.open = true
	case st.Over && c.PracticeOpen(st, now):
		t.open, t.scope.Practice, t.scope.MaxPerProblem = true, true, 0
	case st.Over && c.PracticeAfterEnd:
		t.refusal = contests.ErrPracticeWait
	case st.Over:
		t.refusal = contests.ErrContestOver
	default:
		t.refusal = contests.ErrNotStarted
	}
	return t, true
}

// contestWorkspaceJSON is the M3 Workspace plus the contest's context.
type contestWorkspaceJSON struct {
	*submissions.Workspace
	Label        string      `json:"label"`
	Contest      string      `json:"contest"`
	Window       *windowJSON `json:"window"`
	FeedbackMode string      `json:"feedback_mode"`
	CanSubmit    bool        `json:"can_submit"`
	Practice     bool        `json:"practice"`
	// Refusal says why Run and Submit are off, when they are.
	Refusal *string `json:"refusal"`
}

func (d Deps) getContestWorkspace(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	ws, err := d.Submissions.Workspace(r.Context(), t.problem.ProblemID, userFrom(r.Context()).ID, t.scope)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	out := contestWorkspaceJSON{Workspace: ws, Label: t.problem.Label, Contest: t.c.Slug, FeedbackMode: t.c.Feedback,
		CanSubmit: t.open, Practice: t.scope.Practice}
	if t.st.HasClock && !t.organiser {
		out.Window = &windowJSON{StartedAt: t.st.Window.Opens.UnixMilli(), EndsAt: t.st.Window.Ends.UnixMilli()}
	}
	if t.refusal != nil {
		out.Refusal = ptr(t.refusal.Error())
	}
	WriteJSON(w, http.StatusOK, out)
}

func (d Deps) saveContestDraft(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language string `json:"language"`
		Source   string `json:"source"`
	}
	if !decodeJSONLimit(w, r, &req, maxRunBody) {
		return
	}
	if err := d.Submissions.SaveDraft(r.Context(), t.problem.ProblemID, userFrom(r.Context()).ID, t.scope, req.Language, req.Source); err != nil {
		d.submissionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) runContestCode(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	if !t.open {
		d.contestError(w, r, t.refusal)
		return
	}
	var req struct {
		Language string  `json:"language"`
		Source   string  `json:"source"`
		Input    *string `json:"input"`
	}
	if !decodeJSONLimit(w, r, &req, maxRunBody) {
		return
	}
	v, err := d.Submissions.Run(r.Context(), submissions.RunRequest{ProblemID: t.problem.ProblemID, UserID: userFrom(r.Context()).ID,
		Scope: t.scope, Language: req.Language, Source: req.Source, Input: req.Input})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusAccepted, v)
}

func (d Deps) submitContestCode(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	if !t.open {
		d.contestError(w, r, t.refusal)
		return
	}
	var req struct {
		Language    string `json:"language"`
		Source      string `json:"source"`
		ClientKey   string `json:"client_key"`
		TypedChars  int    `json:"typed_chars"`
		PastedChars int    `json:"pasted_chars"`
		FocusLosses int    `json:"focus_losses"`
	}
	if !decodeJSONLimit(w, r, &req, maxRunBody) {
		return
	}
	u := userFrom(r.Context())
	v, dup, err := d.Submissions.Submit(r.Context(), submissions.SubmitRequest{ProblemID: t.problem.ProblemID, UserID: u.ID,
		Scope: t.scope, Language: req.Language, Source: req.Source, ClientKey: req.ClientKey,
		Typed: req.TypedChars, Pasted: req.PastedChars, Blur: req.FocusLosses,
		ClientHash: d.Submissions.ClientHash(clientIP(r), r.UserAgent())})
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	// A new submission does not touch the board: it changes nothing until
	// its verdict arrives (the "+1" pending cells follow with the next
	// refresh). Invalidating here let one student force a recompute per
	// submit (m4-security finding 1).
	status := http.StatusAccepted
	if dup {
		status = http.StatusOK
	}
	WriteJSON(w, status, v)
}

func (d Deps) recordContestEvents(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Events []submissions.Event `json:"events"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !t.open {
		w.WriteHeader(http.StatusNoContent) // nothing to record outside the window
		return
	}
	if err := d.Submissions.RecordEvents(r.Context(), t.problem.ProblemID, userFrom(r.Context()).ID, t.scope, req.Events); err != nil {
		d.submissionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// standingsJSON is Standings in docs/API.md.
type standingsJSON struct {
	Variant          string         `json:"variant"`
	Version          int64          `json:"version"`
	FrozenAt         *int64         `json:"frozen_at"`
	Final            bool           `json:"final"`
	JudgingRemaining int            `json:"judging_remaining"`
	Labels           []string       `json:"labels"`
	Rows             []contests.Row `json:"rows"`
	Me               int64          `json:"me"`
	ServerNow        int64          `json:"server_now"`
}

func (d Deps) standingsOut(ctx context.Context, c *contests.Contest, variant string, me int64) (*standingsJSON, error) {
	b, err := d.Contests.Board(ctx, c, variant)
	if err != nil {
		return nil, err
	}
	final, err := d.Contests.FinalAt(ctx, c)
	if err != nil {
		return nil, err
	}
	now := d.Contests.Clock()
	out := &standingsJSON{Variant: variant, Version: b.Version, JudgingRemaining: b.JudgingRemaining, Labels: b.Labels,
		Rows: b.Rows, Me: me, ServerNow: now.UnixMilli(),
		Final: variant == contests.VariantFull && !now.Before(final) && b.JudgingRemaining == 0}
	if out.Rows == nil {
		out.Rows = []contests.Row{}
	}
	if variant == contests.VariantFrozen {
		out.FrozenAt = millis(c.FrozenAt())
	}
	return out, nil
}

func (d Deps) getStandings(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	u := userFrom(r.Context())
	variant := contests.VariantFull
	if !organiser {
		st, err := d.Contests.State(r.Context(), c, u.ID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		if variant, err = d.Contests.StudentVariant(r.Context(), c, st); err != nil {
			d.contestError(w, r, err)
			return
		}
	}
	out, err := d.standingsOut(r.Context(), c, variant, u.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

func (d Deps) askClarification(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	var req struct {
		ProblemLabel *string `json:"problem_label"`
		Question     string  `json:"question"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	if !organiser {
		st, err := d.Contests.State(r.Context(), c, u.ID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		if st.Over {
			WriteError(w, http.StatusForbidden, "contest_over", "The contest has ended, so questions are closed.")
			return
		}
	}
	cl, err := d.Contests.Ask(r.Context(), c, u.ID, req.ProblemLabel, req.Question)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	cl.AskedBy = nil
	WriteJSON(w, http.StatusCreated, map[string]any{"clarification": cl, "server_now": d.Contests.Clock().UnixMilli()})
}
