package contests

// Student-facing contest routes: the list,
// one contest, the honour code and start, the contest workspace, the
// standings and clarifications. Every contest-scoped id is resolved through
// the contest named in the URL, so an id from another contest is a 404.
// Times come from the server only, and every answer carries server_now.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
	"github.com/Sarthaks-24/podium/internal/problems"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

type windowJSON struct {
	StartedAt int64 `json:"started_at"`
	EndsAt    int64 `json:"ends_at"`
}

// contestSummaryJSON is ContestSummary in docs/API.md.
type contestSummaryJSON struct {
	Slug         string      `json:"slug"`
	Title        string      `json:"title"`
	StartsAt     int64       `json:"starts_at"`
	EndsAt       int64       `json:"ends_at"`
	ProblemCount int         `json:"problem_count"`
	State        string      `json:"state"`
	MyScore      *int        `json:"my_score"`
	MyWindow     *windowJSON `json:"my_window"`
}

type rulesJSON struct {
	MinGapS       int      `json:"min_gap_s"`
	MaxPerProblem int      `json:"max_per_problem"`
	Languages     []string `json:"languages"`
	FreezeMinutes int      `json:"freeze_minutes"`
	Practice      bool     `json:"practice_after_end"`
	// The exam options, so the workspace knows which measures are on and
	// says so at the start.
	ExamOptions
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
	// RevisionID is the pinned revision.
	RevisionID int64 `json:"revision_id"`
}

// contestJSON is Contest in docs/API.md.
type contestJSON struct {
	contestSummaryJSON
	DescriptionMD string    `json:"description_md"`
	Rules         rulesJSON `json:"rules"`
	HonourText    string    `json:"honour_text"`
	Accepted      bool      `json:"accepted"`
	Organiser     bool      `json:"organiser"`
	// ID is sent to organisers only, so Student view can link back to the live view.
	ID                   int64                `json:"id,omitempty"`
	Problems             []contestProblemJSON `json:"problems"`
	Announcements        []Announcement       `json:"announcements"`
	MyClarifications     []Clarification      `json:"my_clarifications"`
	PublicClarifications []Clarification      `json:"public_clarifications"`
	EditorialMD          *string              `json:"editorial_md"`
	ServerNow            int64                `json:"server_now"`
}

// Handlers serve the contest routes, the students' and the organisers'.
type Handlers struct {
	Contests    *Service
	Submissions *submissions.Service
	Problems    *problems.Service
	Log         *slog.Logger
}

func (d *Handlers) internalError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.InternalError(d.Log, w, r, err)
}

// contestError maps the contests package's errors to responses.
func (d *Handlers) contestError(w http.ResponseWriter, r *http.Request, err error) {
	var fields FieldErrors
	var aiErr *AIProblemError
	switch {
	case errors.As(err, &aiErr):
		httpx.WriteJSON(w, http.StatusConflict, map[string]httpx.APIError{"error": {Code: "ai_unconfirmed", Message: aiErr.Error(), Problems: aiErr.Labels}})
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such contest, or it is not open to you.")
	case errors.As(err, &fields):
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]httpx.APIError{"error": {Code: "bad_request", Message: fields.Error(), Fields: fields}})
	case errors.Is(err, ErrNotStarted), errors.Is(err, ErrBeforeStart):
		httpx.WriteError(w, http.StatusForbidden, "not_started", err.Error())
	case errors.Is(err, ErrContestOver), errors.Is(err, ErrPracticeWait):
		httpx.WriteError(w, http.StatusForbidden, "contest_over", err.Error())
	case errors.Is(err, ErrNotReady):
		httpx.WriteError(w, http.StatusConflict, "not_ready", err.Error())
	case errors.Is(err, ErrRevealEarly):
		httpx.WriteError(w, http.StatusConflict, "too_early", err.Error())
	case errors.Is(err, ErrExtendEnded):
		httpx.WriteError(w, http.StatusConflict, "contest_ended", err.Error())
	default:
		d.internalError(w, r, err)
	}
}

func viewerOf(u auth.User) Viewer {
	return Viewer{UserID: u.ID, Email: u.Email, Organiser: u.Role.AtLeast(auth.Organiser)}
}

// studentContest loads the contest named by {slug} and checks the viewer
// may enter it. organiser is true for owners and admins.
func (d *Handlers) studentContest(w http.ResponseWriter, r *http.Request) (*Contest, bool, bool) {
	c, err := d.Contests.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		d.contestError(w, r, err)
		return nil, false, false
	}
	organiser, err := d.Contests.MayEnter(r.Context(), c, viewerOf(auth.UserFrom(r.Context())))
	if err != nil {
		d.contestError(w, r, err)
		return nil, false, false
	}
	return c, organiser, true
}

// summary builds ContestSummary for the viewer, and returns their state.
func (d *Handlers) summary(ctx context.Context, c *Contest, userID int64, organiser bool) (contestSummaryJSON, StudentState, error) {
	now := d.Contests.Clock()
	out := contestSummaryJSON{Slug: c.Slug, Title: c.Title, StartsAt: c.Schedule.StartsAt.UnixMilli(),
		EndsAt: c.Schedule.Close().UnixMilli(), ProblemCount: len(c.Problems)}
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

func (d *Handlers) ListContests(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
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
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
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
		case StateLive, StateFrozen:
			out["live"] = append(out["live"], s)
		case StateUpcoming:
			out["upcoming"] = append(out["upcoming"], s)
		default:
			out["past"] = append(out["past"], s)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"live": out["live"], "upcoming": out["upcoming"], "past": out["past"],
		"server_now": d.Contests.Clock().UnixMilli()})
}

// contestDetail builds Contest for the viewer.
func (d *Handlers) contestDetail(ctx context.Context, c *Contest, u auth.User, organiser bool) (*contestJSON, error) {
	s, st, err := d.summary(ctx, c, u.ID, organiser)
	if err != nil {
		return nil, err
	}
	now := d.Contests.Clock()
	out := &contestJSON{contestSummaryJSON: s, DescriptionMD: c.Description, HonourText: c.Honour(), Accepted: st.Accepted,
		Organiser: organiser, Problems: []contestProblemJSON{},
		Rules: rulesJSON{MinGapS: int(c.MinGap / time.Second),
			MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, FreezeMinutes: int(c.Schedule.Freeze / time.Minute),
			Practice: c.PracticeAfterEnd, ExamOptions: c.Exam},
		ServerNow: now.UnixMilli()}
	if organiser {
		out.ID = c.ID
	}
	if organiser || c.ProblemsVisible(st, now) {
		var cells map[string]Cell
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
			pj := contestProblemJSON{Label: p.Label, Title: p.Title, MaxScore: p.MaxScore(), MySubmissionCount: counts[p.ProblemID],
				RevisionID: p.RevisionID}
			if cell, ok := cells[p.Label]; ok {
				pj.MyScore = httpx.Ptr(cell.Score)
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
			out.EditorialMD = httpx.Ptr(c.Editorial)
		}
	}
	return out, nil
}

func (d *Handlers) GetContest(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	out, err := d.contestDetail(r.Context(), c, auth.UserFrom(r.Context()), organiser)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (d *Handlers) StartContest(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	var req struct {
		AcceptHonour bool `json:"accept_honour"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u := auth.UserFrom(r.Context())
	if err := d.Contests.Start(r.Context(), c, u.ID, req.AcceptHonour); err != nil {
		d.contestError(w, r, err)
		return
	}
	out, err := d.contestDetail(r.Context(), c, u, organiser)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// contestTarget is the contest problem a workspace request is about and
// the rules that apply to the viewer.
type contestTarget struct {
	c         *Contest
	problem   ContestProblem
	scope     submissions.Scope
	st        StudentState
	organiser bool
	// open is true when the viewer may run and submit now (for the score,
	// or as practice when scope.Practice is set); refusal says why not.
	open    bool
	refusal error
}

// contestProblem resolves {slug} and {label} and applies the time and
// access rules. Students see a problem only once their window started (or
// is over); organisers always.
func (d *Handlers) contestProblem(w http.ResponseWriter, r *http.Request) (*contestTarget, bool) {
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
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest.")
		return nil, false
	}
	u := auth.UserFrom(r.Context())
	st, err := d.Contests.State(r.Context(), c, u.ID)
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	t.st = st
	t.scope = submissions.Scope{ContestID: c.ID, Languages: c.Languages, RevisionID: t.problem.RevisionID}
	if organiser {
		t.open = true
		t.scope.MinGap = submissions.OrganiserGap
		if !t.problem.Ready {
			t.open, t.refusal = false, ErrNotReady
		}
		return t, true
	}
	now := d.Contests.Clock()
	if !c.ProblemsVisible(st, now) {
		if now.Before(c.Schedule.StartsAt) {
			d.contestError(w, r, ErrBeforeStart)
		} else {
			d.contestError(w, r, ErrNotStarted)
		}
		return nil, false
	}
	t.scope.MinGap, t.scope.MaxPerProblem, t.scope.View = c.MinGap, c.MaxPerProblem, c.ViewOptions(st)
	switch {
	case !t.problem.Ready:
		t.refusal = ErrNotReady
	case st.Open:
		t.open = true
	case st.Over && c.PracticeOpen(st, now):
		// Practice shows the first failing hidden test, except while
		// another contest that pins the problem has not closed.
		pending, err := d.Contests.PinnedInOpenContest(r.Context(), t.problem.ProblemID, c.ID)
		if err != nil {
			d.contestError(w, r, err)
			return nil, false
		}
		t.open, t.scope.Practice, t.scope.MaxPerProblem, t.scope.Reveal = true, true, 0, !pending
	case st.Over && c.PracticeAfterEnd:
		t.refusal = ErrPracticeWait
	case st.Over:
		t.refusal = ErrContestOver
	default:
		t.refusal = ErrNotStarted
	}
	return t, true
}

// contestWorkspaceJSON is the organisers' workspace plus the contest's
// context.
type contestWorkspaceJSON struct {
	*submissions.Workspace
	Label     string      `json:"label"`
	Contest   string      `json:"contest"`
	Window    *windowJSON `json:"window"`
	CanSubmit bool        `json:"can_submit"`
	Practice  bool        `json:"practice"`
	// Refusal says why Run and Submit are off, when they are.
	Refusal *string `json:"refusal"`
	// RevisionID is the pinned revision served here.
	RevisionID int64 `json:"revision_id"`
}

func (d *Handlers) GetContestWorkspace(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	ws, err := d.Submissions.Workspace(r.Context(), t.problem.ProblemID, auth.UserFrom(r.Context()).ID, t.scope)
	if err != nil {
		submissions.WriteError(d.Log, w, r, err)
		return
	}
	out := contestWorkspaceJSON{Workspace: ws, Label: t.problem.Label, Contest: t.c.Slug,
		CanSubmit: t.open, Practice: t.scope.Practice, RevisionID: t.problem.RevisionID}
	if t.st.HasClock && !t.organiser {
		out.Window = &windowJSON{StartedAt: t.st.Window.Opens.UnixMilli(), EndsAt: t.st.Window.Ends.UnixMilli()}
	}
	if t.refusal != nil {
		out.Refusal = httpx.Ptr(t.refusal.Error())
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (d *Handlers) SaveContestDraft(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Language string `json:"language"`
		Source   string `json:"source"`
	}
	if !httpx.DecodeJSONLimit(w, r, &req, submissions.MaxRunBody) {
		return
	}
	if err := d.Submissions.SaveDraft(r.Context(), t.problem.ProblemID, auth.UserFrom(r.Context()).ID, t.scope, req.Language, req.Source); err != nil {
		submissions.WriteError(d.Log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Handlers) RunContestCode(w http.ResponseWriter, r *http.Request) {
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
	if !httpx.DecodeJSONLimit(w, r, &req, submissions.MaxRunBody) {
		return
	}
	v, err := d.Submissions.Run(r.Context(), submissions.RunRequest{ProblemID: t.problem.ProblemID, UserID: auth.UserFrom(r.Context()).ID,
		Scope: t.scope, Language: req.Language, Source: req.Source, Input: req.Input})
	if err != nil {
		submissions.WriteError(d.Log, w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, v)
}

func (d *Handlers) SubmitContestCode(w http.ResponseWriter, r *http.Request) {
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
		Exits       int    `json:"fullscreen_exits"`
	}
	if !httpx.DecodeJSONLimit(w, r, &req, submissions.MaxRunBody) {
		return
	}
	u := auth.UserFrom(r.Context())
	v, dup, err := d.Submissions.Submit(r.Context(), submissions.SubmitRequest{ProblemID: t.problem.ProblemID, UserID: u.ID,
		Scope: t.scope, Language: req.Language, Source: req.Source, ClientKey: req.ClientKey,
		Typed: req.TypedChars, Pasted: req.PastedChars, Blur: req.FocusLosses, Exits: req.Exits,
		ClientHash: d.Submissions.ClientHash(httpx.ClientIP(r), r.UserAgent())})
	if err != nil {
		submissions.WriteError(d.Log, w, r, err)
		return
	}
	// A new submission does not touch the board: it changes nothing until
	// its verdict arrives (the "+1" pending cells follow with the next
	// refresh). Invalidating here let one student force a recompute per
	// submit.
	status := http.StatusAccepted
	if dup {
		status = http.StatusOK
	}
	httpx.WriteJSON(w, status, v)
}

func (d *Handlers) RecordContestEvents(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	var req struct {
		Events []submissions.Event `json:"events"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if !t.open {
		w.WriteHeader(http.StatusNoContent) // nothing to record outside the window
		return
	}
	if err := d.Submissions.RecordEvents(r.Context(), t.problem.ProblemID, auth.UserFrom(r.Context()).ID, t.scope, req.Events); err != nil {
		submissions.WriteError(d.Log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// standingsJSON is Standings in docs/API.md.
type standingsJSON struct {
	Variant          string   `json:"variant"`
	Version          int64    `json:"version"`
	FrozenAt         *int64   `json:"frozen_at"`
	Final            bool     `json:"final"`
	JudgingRemaining int      `json:"judging_remaining"`
	Labels           []string `json:"labels"`
	Rows             []Row    `json:"rows"`
	Me               int64    `json:"me"`
	ServerNow        int64    `json:"server_now"`
}

func (d *Handlers) standingsOut(ctx context.Context, c *Contest, variant string, me int64) (*standingsJSON, error) {
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
		Final: variant == VariantFull && !now.Before(final) && b.JudgingRemaining == 0}
	if out.Rows == nil {
		out.Rows = []Row{}
	}
	if variant == VariantFrozen {
		out.FrozenAt = httpx.Millis(c.FrozenAt())
	}
	return out, nil
}

func (d *Handlers) GetStandings(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	u := auth.UserFrom(r.Context())
	variant := VariantFull
	switch {
	// An organiser gets the full board, or with ?as=student the board
	// students get now ("Student view", pilot P10).
	case !organiser || r.URL.Query().Get("as") == "student":
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
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (d *Handlers) AskClarification(w http.ResponseWriter, r *http.Request) {
	c, organiser, ok := d.studentContest(w, r)
	if !ok {
		return
	}
	var req struct {
		ProblemLabel *string `json:"problem_label"`
		Question     string  `json:"question"`
		ClientKey    string  `json:"client_key"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u := auth.UserFrom(r.Context())
	if !organiser {
		st, err := d.Contests.State(r.Context(), c, u.ID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		// Practice after the end keeps Messages open (walk bug B4); otherwise the end closes questions.
		if st.Over && !c.PracticeOpen(st, d.Contests.Clock()) {
			httpx.WriteError(w, http.StatusForbidden, "contest_over", "The contest has ended, so questions are closed.")
			return
		}
	}
	cl, err := d.Contests.Ask(r.Context(), c, u.ID, req.ProblemLabel, req.Question, req.ClientKey)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	cl.AskedBy = nil
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"clarification": cl, "server_now": d.Contests.Clock().UnixMilli()})
}
