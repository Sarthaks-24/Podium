package httpapi

// Organiser contest routes (cache/v1/m4-plan.md section 4). Owners and
// admins organise every contest; a contest's co-organisers organise only
// that one. Everyone else gets a 404, so contest ids cannot be probed.
// Creating, cloning, archiving, choosing problems and co-organisers, and
// everything that changes a running contest's results (the roster,
// extensions, extend, pause and resume, rejudge) need the admin role. A
// co-organiser whose site role is student cannot change the rules once the
// contest starts, and sees results only as a student would (security
// council S1, contests/coorganiser.go). Every change that can affect
// results is written to the contest's audit log (S9).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
	"github.com/Sarthaks-24/podium/internal/submissions"
)

// adminProblemJSON is one problem of the contest editor.
type adminProblemJSON struct {
	Label      string `json:"label"`
	ProblemID  int64  `json:"problem_id"`
	Title      string `json:"title"`
	PinnedHash string `json:"pinned_hash"`
	Ready      bool   `json:"ready"`
	MaxScore   int    `json:"max_score"`
	// ValidatedToolchain is the toolchain of the validation of the pinned
	// version; ToolchainDrift is true when it differs from the judge's
	// current one. Both are null when either is unknown (B1.5).
	ValidatedToolchain *toolchainJSON `json:"validated_toolchain"`
	ToolchainDrift     *bool          `json:"toolchain_drift"`
	// The pinned revision and the problem's newest one (C4).
	RevisionID          int64 `json:"revision_id"`
	Rev                 int   `json:"rev"`
	LatestRevisionID    int64 `json:"latest_revision_id"`
	LatestRev           int   `json:"latest_rev"`
	UpdateAvailable     bool  `json:"update_available"`
	UpdateStatementOnly bool  `json:"update_statement_only"`
	Reconstructed       bool  `json:"reconstructed"`
}

// toolchainJSON is a stored judge toolchain as the console shows it.
type toolchainJSON struct {
	judgeapi.Toolchain
	FirstSeen int64 `json:"first_seen"`
}

func toolchainOut(t *jobs.Toolchain) *toolchainJSON {
	if t == nil {
		return nil
	}
	return &toolchainJSON{Toolchain: t.Details, FirstSeen: t.FirstSeen.UnixMilli()}
}

// adminContestJSON is AdminContest in docs/API.md: every setting, the
// problems with their pins, co-organisers and extensions.
type adminContestJSON struct {
	ID                 int64                `json:"id"`
	Slug               string               `json:"slug"`
	Title              string               `json:"title"`
	DescriptionMD      string               `json:"description_md"`
	StartsAt           int64                `json:"starts_at"`
	EndsAt             int64                `json:"ends_at"`
	CloseAt            int64                `json:"close_at"`
	StartMode          string               `json:"start_mode"`
	DurationMinutes    int                  `json:"duration_minutes"`
	FreezeMinutes      int                  `json:"freeze_minutes"`
	ScoringMode        string               `json:"scoring_mode"`
	Access             string               `json:"access"`
	FeedbackMode       string               `json:"feedback_mode"`
	Graded             bool                 `json:"graded"`
	Published          bool                 `json:"published"`
	PracticeAfterEnd   bool                 `json:"practice_after_end"`
	StopOnSampleFail   bool                 `json:"stop_on_sample_fail"`
	MinGapS            int                  `json:"min_gap_s"`
	MaxPerProblem      int                  `json:"max_per_problem"`
	Languages          []string             `json:"languages"`
	HonourText         string               `json:"honour_text"`
	DefaultHonourText  string               `json:"default_honour_text"`
	HoldReveal         bool                 `json:"hold_reveal"`
	EditorialMD        string               `json:"editorial_md"`
	EditorialPublished bool                 `json:"editorial_published"`
	Paused             bool                 `json:"paused"`
	ExtendedMinutes    int                  `json:"extended_minutes"`
	RevealedAt         *int64               `json:"revealed_at"`
	Archived           bool                 `json:"archived"`
	State              string               `json:"state"`
	Problems           []adminProblemJSON   `json:"problems"`
	Admins             []contests.Person    `json:"admins"`
	Extensions         []contests.Extension `json:"extensions"`
	RosterCount        int                  `json:"roster_count"`
	// JudgeToolchain is what the judge reported last; null before any
	// judge of v1.1.0 or later has started.
	JudgeToolchain *toolchainJSON `json:"judge_toolchain"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
	ServerNow      int64          `json:"server_now"`
	// LockedAfterStart names the settings only owners and admins may change
	// once the contest has started.
	LockedAfterStart []string `json:"locked_after_start"`
	// PinHistory is every pin change, newest first (C4).
	PinHistory []contests.PinChange `json:"pin_history"`
	// Queue health for the console's attention strip (C10, D103).
	Queue            queueJSON `json:"queue"`
	JudgeLastClaimAt *int64    `json:"judge_last_claim_at"`
	Unanswered       int       `json:"unanswered"`
}

// queueJSON is the contest's share of the judge queue (C10).
type queueJSON struct {
	Waiting     int     `json:"waiting"`
	Judging     int     `json:"judging"`
	Errors      int     `json:"errors"`
	OldestWaitS float64 `json:"oldest_wait_s"`
}

func (d Deps) adminContestOut(ctx context.Context, c *contests.Contest) (*adminContestJSON, error) {
	now := d.Contests.Clock()
	out := &adminContestJSON{ID: c.ID, Slug: c.Slug, Title: c.Title, DescriptionMD: c.Description,
		StartsAt: c.Schedule.StartsAt.UnixMilli(), EndsAt: c.Schedule.EndsAt.UnixMilli(), CloseAt: c.Schedule.Close().UnixMilli(),
		StartMode: c.Schedule.Mode, DurationMinutes: int(c.Schedule.Duration / time.Minute), FreezeMinutes: int(c.Schedule.Freeze / time.Minute),
		ScoringMode: c.Scoring, Access: c.Access, FeedbackMode: c.Feedback, Graded: c.Graded, Published: c.Published,
		PracticeAfterEnd: c.PracticeAfterEnd, StopOnSampleFail: c.StopOnSampleFail, MinGapS: int(c.MinGap / time.Second),
		MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, HonourText: c.HonourText, DefaultHonourText: contests.DefaultHonour,
		HoldReveal: c.HoldReveal, EditorialMD: c.Editorial, EditorialPublished: c.EditorialPublished, Paused: c.Paused,
		ExtendedMinutes: int(c.Schedule.Extended / time.Minute), RevealedAt: millis(c.RevealedAt), Archived: c.Archived,
		State: c.Schedule.State(now, c.PracticeAfterEnd), Problems: []adminProblemJSON{},
		CreatedAt: c.CreatedAt.UnixMilli(), UpdatedAt: c.UpdatedAt.UnixMilli(), ServerNow: now.UnixMilli(),
		LockedAfterStart: contests.LockedAfterStart}
	var current *jobs.Toolchain
	var err error
	if d.Problems != nil && d.Problems.Jobs != nil {
		if current, err = d.Problems.Jobs.CurrentToolchain(ctx); err != nil {
			return nil, err
		}
		out.JudgeToolchain = toolchainOut(current)
	}
	for _, p := range c.Problems {
		ap := adminProblemJSON{Label: p.Label, ProblemID: p.ProblemID, Title: p.Title,
			PinnedHash: p.PinnedHash, Ready: p.Ready, MaxScore: p.MaxScore(),
			RevisionID: p.RevisionID, Rev: p.Rev, LatestRevisionID: p.LatestRevisionID, LatestRev: p.LatestRev,
			UpdateAvailable:     p.LatestRevisionID != p.RevisionID && p.LatestReady,
			UpdateStatementOnly: p.LatestRevisionID != p.RevisionID && p.LatestStatementOnly, Reconstructed: p.Reconstructed}
		if d.Problems != nil && d.Problems.Jobs != nil {
			validated, err := d.Problems.Jobs.ValidatedToolchain(ctx, p.ProblemID, p.PinnedHash)
			if err != nil {
				return nil, err
			}
			ap.ValidatedToolchain = toolchainOut(validated)
			if validated != nil && current != nil {
				drift := validated.Hash != current.Hash
				ap.ToolchainDrift = &drift
			}
		}
		out.Problems = append(out.Problems, ap)
	}
	if out.Admins, err = d.Contests.Admins(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.Extensions, err = d.Contests.Extensions(ctx, c.ID); err != nil {
		return nil, err
	}
	// The reason for a classmate's extra time can be health data: a
	// co-organiser whose site role is student sees the minutes only.
	if !userFrom(ctx).Role.AtLeast(auth.Admin) {
		for i := range out.Extensions {
			out.Extensions[i].Reason = ""
		}
	}
	if out.RosterCount, err = d.Contests.RosterCount(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.PinHistory, err = d.Contests.PinHistory(ctx, c.ID); err != nil {
		return nil, err
	}
	q, err := d.Contests.QueueHealth(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	out.Queue = queueJSON{Waiting: q.Waiting, Judging: q.Judging, Errors: q.Errors, OldestWaitS: q.OldestWait.Seconds()}
	out.Unanswered = q.Unanswered
	if d.Problems != nil && d.Problems.Jobs != nil {
		if at, ok := d.Problems.Jobs.LastClaimAt(); ok {
			out.JudgeLastClaimAt = millis(at)
		}
	}
	return out, nil
}

// writeAdminContest reloads the contest and answers with AdminContest.
func (d Deps) writeAdminContest(w http.ResponseWriter, r *http.Request, id int64, status int) {
	c, err := d.Contests.Get(r.Context(), id)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	out, err := d.adminContestOut(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, status, out)
}

// organisedContest loads the contest named by {id} for one of its
// organisers; anyone else gets the same 404 as a missing contest.
func (d Deps) organisedContest(w http.ResponseWriter, r *http.Request) (*contests.Contest, bool) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		d.contestError(w, r, contests.ErrNotFound)
		return nil, false
	}
	c, err := d.Contests.Get(r.Context(), id)
	if err != nil {
		d.contestError(w, r, err)
		return nil, false
	}
	u := userFrom(r.Context())
	if !u.Role.AtLeast(auth.Admin) {
		isAdmin, err := d.Contests.IsContestAdmin(r.Context(), c.ID, u.ID)
		if err != nil {
			d.internalError(w, r, err)
			return nil, false
		}
		if !isAdmin {
			d.contestError(w, r, contests.ErrNotFound)
			return nil, false
		}
	}
	return c, true
}

// requireAdmin answers 403 unless the user is an admin or the owner.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !siteAdmin(r) {
		WriteError(w, http.StatusForbidden, "forbidden", "Only owners and admins can do this.")
		return false
	}
	return true
}

// siteAdmin reports whether the user is an owner or admin, as opposed to a
// co-organiser whose site role is student.
func siteAdmin(r *http.Request) bool { return userFrom(r.Context()).Role.AtLeast(auth.Admin) }

// actor is the signed-in user as the audit log names them.
func actor(r *http.Request) contests.Actor {
	u := userFrom(r.Context())
	return contests.Actor{ID: u.ID, Email: u.Email, Role: string(u.Role)}
}

// audit appends to a contest's audit log after a change that already
// happened. A failure cannot undo the change, so it is logged loudly for
// the operator instead of failing the request.
func (d Deps) audit(r *http.Request, contestID int64, action string, details any) {
	if err := d.Contests.Record(r.Context(), contestID, actor(r), action, details); err != nil {
		d.Log.Error("audit log write failed", "contest_id", contestID, "action", action, "by", userFrom(r.Context()).ID, "err", err)
	}
}

// resultsHidden reports whether a co-organiser without the admin role must
// not see the contest's results now (exam mode, the freeze, a held
// reveal): then the stats and the exports are refused to them. It answers
// the refusal itself.
func (d Deps) resultsHidden(w http.ResponseWriter, r *http.Request, c *contests.Contest) bool {
	if siteAdmin(r) {
		return false
	}
	v, err := d.Contests.CoOrganiserVariant(r.Context(), c)
	if err == nil && v == contests.VariantFull && !d.Contests.CoOrganiserHide(r.Context(), c).Active() {
		return false
	}
	WriteError(w, http.StatusForbidden, "results_hidden", "Co-organisers see results when students can: once every student's time is up and the board is revealed.")
	return true
}

func (d Deps) listAdminContests(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	all, err := d.Contests.List(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := []*adminContestJSON{}
	for _, c := range all {
		if !u.Role.AtLeast(auth.Admin) {
			ok, err := d.Contests.IsContestAdmin(r.Context(), c.ID, u.ID)
			if err != nil {
				d.internalError(w, r, err)
				return
			}
			if !ok {
				continue
			}
		}
		a, err := d.adminContestOut(r.Context(), c)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		out = append(out, a)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"contests": out, "server_now": d.Contests.Clock().UnixMilli()})
}

// contestEdit is the body of create and update: every field of the form.
// Fields left out keep their current value (defaults on create).
type contestEdit struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	DescriptionMD    string   `json:"description_md"`
	StartsAt         int64    `json:"starts_at"`
	EndsAt           int64    `json:"ends_at"`
	StartMode        string   `json:"start_mode"`
	DurationMinutes  int      `json:"duration_minutes"`
	FreezeMinutes    int      `json:"freeze_minutes"`
	ScoringMode      string   `json:"scoring_mode"`
	Access           string   `json:"access"`
	FeedbackMode     string   `json:"feedback_mode"`
	Graded           bool     `json:"graded"`
	Published        bool     `json:"published"`
	PracticeAfterEnd bool     `json:"practice_after_end"`
	StopOnSampleFail bool     `json:"stop_on_sample_fail"`
	MinGapS          int      `json:"min_gap_s"`
	MaxPerProblem    int      `json:"max_per_problem"`
	Languages        []string `json:"languages"`
	HonourText       string   `json:"honour_text"`
	HoldReveal       bool     `json:"hold_reveal"`
	// ConfirmUngraded confirms turning grading off after the start, which
	// shows every student still in their window the first failed hidden
	// test of each later submit (D115, council C1-015). Never stored.
	ConfirmUngraded bool `json:"confirm_ungraded"`
}

func (e contestEdit) edit() contests.Edit {
	return contests.Edit{Slug: strings.TrimSpace(e.Slug), Title: e.Title, Description: e.DescriptionMD,
		StartsAt: time.UnixMilli(e.StartsAt), EndsAt: time.UnixMilli(e.EndsAt), StartMode: e.StartMode,
		DurationMinutes: e.DurationMinutes, FreezeMinutes: e.FreezeMinutes, Scoring: e.ScoringMode, Access: e.Access,
		Feedback: e.FeedbackMode, Graded: e.Graded, Published: e.Published, PracticeAfterEnd: e.PracticeAfterEnd,
		StopOnSampleFail: e.StopOnSampleFail, MinGapS: e.MinGapS, MaxPerProblem: e.MaxPerProblem, Languages: e.Languages,
		HonourText: e.HonourText, HoldReveal: e.HoldReveal}
}

func editOf(c *contests.Contest) contestEdit {
	return contestEdit{Slug: c.Slug, Title: c.Title, DescriptionMD: c.Description, StartsAt: c.Schedule.StartsAt.UnixMilli(),
		EndsAt: c.Schedule.EndsAt.UnixMilli(), StartMode: c.Schedule.Mode, DurationMinutes: int(c.Schedule.Duration / time.Minute),
		FreezeMinutes: int(c.Schedule.Freeze / time.Minute), ScoringMode: c.Scoring, Access: c.Access, FeedbackMode: c.Feedback,
		Graded: c.Graded, Published: c.Published, PracticeAfterEnd: c.PracticeAfterEnd, StopOnSampleFail: c.StopOnSampleFail,
		MinGapS: int(c.MinGap / time.Second), MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, HonourText: c.HonourText,
		HoldReveal: c.HoldReveal}
}

func (d Deps) createContest(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	req := contestEdit{StartMode: contests.FixedStart, ScoringMode: contests.BestSubtask, Access: "domain", FeedbackMode: "subtask",
		Graded: true, PracticeAfterEnd: true, StopOnSampleFail: true, MinGapS: 30, MaxPerProblem: 15,
		Languages: []string{"cpp17", "java21", "python3", "node22"}}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	c, err := d.Contests.Create(r.Context(), req.edit(), u.ID)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest created", "by", u.ID, "contest_id", c.ID)
	d.writeAdminContest(w, r, c.ID, http.StatusCreated)
}

func (d Deps) getAdminContest(w http.ResponseWriter, r *http.Request) {
	if c, ok := d.organisedContest(w, r); ok {
		d.writeAdminContest(w, r, c.ID, http.StatusOK)
	}
}

func (d Deps) updateContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	req := editOf(c)
	if !decodeJSON(w, r, &req) {
		return
	}
	// Publishing hands the problems to students: owners and admins only.
	if req.Published != c.Published && !siteAdmin(r) {
		WriteError(w, http.StatusForbidden, "forbidden", "Only owners and admins can publish or unpublish a contest.")
		return
	}
	// Once the contest starts, the rules and the schedule are the owners'
	// and admins' to change (the web app asks them to confirm).
	if !siteAdmin(r) && c.Started(d.Contests.Clock()) {
		if locked := c.LockedChanges(req.edit()); len(locked) > 0 {
			WriteJSON(w, http.StatusForbidden, map[string]APIError{"error": {Code: "locked_after_start",
				Message:  "The contest has started, so only owners and admins can change its schedule and rules: " + strings.Join(locked, ", ") + ".",
				Problems: locked}})
			return
		}
	}
	if c.Graded && !req.Graded && c.Started(d.Contests.Clock()) && !req.ConfirmUngraded {
		WriteJSON(w, http.StatusBadRequest, map[string]APIError{"error": {Code: "confirm_ungraded",
			Message: "The contest has started. Without grading, every student still in their window sees the first hidden test " +
				"each later submission fails. Send confirm_ungraded to go on.",
			Fields: map[string]string{"graded": "The contest has started: confirm before turning grading off."}}})
		return
	}
	if _, err := d.Contests.Update(r.Context(), c.ID, req.edit(), actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d Deps) archiveContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	if err := d.Contests.Archive(r.Context(), c.ID); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.audit(r, c.ID, contests.AuditArchive, nil)
	d.Log.Info("contest archived", "by", userFrom(r.Context()).ID, "contest_id", c.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) cloneContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	var req struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		StartsAt int64  `json:"starts_at"`
		EndsAt   int64  `json:"ends_at"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := d.Contests.Clone(r.Context(), c.ID, strings.TrimSpace(req.Slug), req.Title, time.UnixMilli(req.StartsAt),
		time.UnixMilli(req.EndsAt), actor(r))
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, out.ID, http.StatusCreated)
}

// setContestProblems needs the admin role: choosing problems opens the
// problem bank, which co-organisers (often student TAs) never see.
func (d Deps) setContestProblems(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	var req []struct {
		ProblemID int64  `json:"problem_id"`
		Label     string `json:"label"`
		// AIReason confirms an AI problem no person has written a solution
		// for, in a graded contest (security council S10).
		AIReason string `json:"ai_reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	choices := make([]contests.ProblemChoice, 0, len(req))
	for _, p := range req {
		choices = append(choices, contests.ProblemChoice{ProblemID: p.ProblemID, Label: p.Label, AIReason: p.AIReason})
	}
	if _, err := d.Contests.SetProblems(r.Context(), c.ID, choices, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

type rosterEntryJSON struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	RollNumber string `json:"roll_number"`
	Section    string `json:"section"`
}

// setRoster replaces the roster from a CSV body (text/csv) or a JSON array.
// Owners and admins only: the roster decides who may compete.
func (d Deps) setRoster(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	var entries []contests.RosterEntry
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mt {
	case "text/csv":
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "A roster file is limited to 2 MB.")
			return
		}
		if entries, err = contests.ParseRoster(bytes.NewReader(body)); err != nil {
			d.contestError(w, r, err)
			return
		}
	default:
		var req []rosterEntryJSON
		if !decodeJSONLimit(w, r, &req, 2<<20) {
			return
		}
		raw := make([]contests.RosterEntry, 0, len(req))
		for _, e := range req {
			raw = append(raw, contests.RosterEntry{Email: e.Email, Name: e.Name, RollNumber: e.RollNumber, Section: e.Section})
		}
		var err error
		if entries, err = contests.CheckRoster(raw); err != nil {
			d.contestError(w, r, err)
			return
		}
	}
	ch, err := d.Contests.SetRoster(r.Context(), c.ID, entries)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	d.audit(r, c.ID, contests.AuditRoster, map[string]any{"count": ch.Count, "added": ch.Added, "removed": ch.Removed,
		"emails_added": ch.EmailsAdded, "emails_removed": ch.EmailsRemoved})
	d.Log.Info("contest roster replaced", "by", userFrom(r.Context()).ID, "contest_id", c.ID,
		"count", ch.Count, "added", ch.Added, "removed", ch.Removed)
	WriteJSON(w, http.StatusOK, map[string]any{"added": ch.Added, "removed": ch.Removed, "roster_count": ch.Count})
}

func (d Deps) getRoster(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Roster(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := make([]rosterEntryJSON, 0, len(list))
	for _, e := range list {
		out = append(out, rosterEntryJSON{Email: e.Email, Name: e.Name, RollNumber: e.RollNumber, Section: e.Section})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"roster": out, "count": len(out)})
}

func (d Deps) changeContestAdmin(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := d.organisedContest(w, r)
		if !ok || !requireAdmin(w, r) {
			return
		}
		var req struct {
			Email string `json:"email"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := d.Contests.SetAdmin(r.Context(), c.ID, req.Email, add); err != nil {
			d.contestError(w, r, err)
			return
		}
		d.audit(r, c.ID, contests.AuditOrganisers, map[string]any{"email": strings.ToLower(strings.TrimSpace(req.Email)), "added": add})
		d.Log.Info("contest co-organiser changed", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "added", add)
		d.writeAdminContest(w, r, c.ID, http.StatusOK)
	}
}

// setExtension gives one student on the roster, or taking part, extra time.
// Owners and admins only.
func (d Deps) setExtension(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	userID, ok := parseID(r.PathValue("user"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such person.")
		return
	}
	var req struct {
		Minutes int    `json:"minutes"`
		Reason  string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	before := contests.Extension{}
	if list, err := d.Contests.Extensions(r.Context(), c.ID); err == nil {
		for _, e := range list {
			if e.UserID == userID {
				before = e
			}
		}
	}
	if err := d.Contests.ExtendFor(r.Context(), c, userID, req.Minutes, req.Reason); err != nil {
		switch {
		case errors.Is(err, contests.ErrNotFound):
			WriteError(w, http.StatusNotFound, "not_found", "There is no such person.")
		case errors.Is(err, contests.ErrNotParticipant):
			WriteError(w, http.StatusConflict, "not_participant", err.Error())
		default:
			d.contestError(w, r, err)
		}
		return
	}
	// The student is named by id only: the audit log outlives an erasure
	// (forget-user rewrites no audit row), and the feed shows the name
	// from the contest's people (agent backlog, P3 security review).
	d.audit(r, c.ID, contests.AuditExtension, map[string]any{"user_id": userID,
		"from_minutes": before.Minutes, "minutes": req.Minutes, "reason_given": strings.TrimSpace(req.Reason) != ""})
	d.Log.Info("contest extension set", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "user_id", userID, "minutes", req.Minutes)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d Deps) extendContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	var req struct {
		Minutes int `json:"minutes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := d.Contests.ExtendAll(r.Context(), c.ID, req.Minutes)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	d.audit(r, c.ID, contests.AuditExtend, map[string]int{"minutes": req.Minutes, "extended_minutes": int(out.Schedule.Extended / time.Minute)})
	d.Log.Info("contest extended", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "minutes", req.Minutes)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d Deps) pauseContest(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := d.organisedContest(w, r)
		if !ok || !requireAdmin(w, r) {
			return
		}
		if err := d.Contests.SetPaused(r.Context(), c.ID, paused); err != nil {
			d.contestError(w, r, err)
			return
		}
		action := contests.AuditResume
		if paused {
			action = contests.AuditPause
		}
		d.audit(r, c.ID, action, nil)
		d.Log.Info("contest judging paused or resumed", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "paused", paused)
		d.writeAdminContest(w, r, c.ID, http.StatusOK)
	}
}

func (d Deps) contestFeed(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	// Typed, pasted and focus counts are integrity data: owners and admins
	// only, never a co-organiser whose site role is student. Such a
	// co-organiser also sees results only as a student would: none in exam
	// mode, none from the freeze while the board is frozen.
	f := contests.FeedFilter{Verdict: q.Get("verdict"), IntegrityCounts: siteAdmin(r)}
	if !siteAdmin(r) {
		f.Hide = d.Contests.CoOrganiserHide(r.Context(), c)
		if f.Hide.Active() && strings.TrimSpace(f.Verdict) != "" {
			WriteError(w, http.StatusForbidden, "results_hidden", "Verdicts are hidden from co-organisers until students can see them, so the feed cannot be filtered by verdict yet.")
			return
		}
	}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	if label := q.Get("problem"); label != "" {
		for _, p := range c.Problems {
			if p.Label == label {
				f.ProblemID = p.ProblemID
			}
		}
		if f.ProblemID == 0 {
			WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest.")
			return
		}
	}
	if u := q.Get("user"); u != "" {
		id, ok := parseID(u)
		if !ok {
			WriteError(w, http.StatusBadRequest, "bad_request", "The user filter must be a user id.")
			return
		}
		f.UserID = id
	}
	items, more, err := d.Contests.Feed(r.Context(), c, f)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if siteAdmin(r) {
		if err := d.annotateFeed(r.Context(), c.ID, userFrom(r.Context()).ID, items); err != nil {
			d.internalError(w, r, err)
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"submissions": items, "page": max(f.Page, 1), "has_more": more,
		"results_hidden": f.Hide.Active(), "server_now": d.Contests.Clock().UnixMilli()})
}

// rejudge queues a problem's or one submission's rejudge. Owners and admins
// only: a rejudge can change every score on the board.
func (d Deps) rejudge(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	var req struct {
		ProblemLabel string `json:"problem_label"`
		SubmissionID int64  `json:"submission_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	rr := submissions.RejudgeRequest{ContestID: c.ID, SubmissionID: req.SubmissionID, By: userFrom(r.Context()).ID,
		NoStopOnSample: !c.StopOnSampleFail}
	var judgedProblem int64
	check := func(pid int64) bool {
		for _, p := range c.Problems {
			if p.ProblemID == pid && !p.Ready {
				d.contestError(w, r, contests.ErrNotReady)
				return false
			}
		}
		return true
	}
	switch {
	case req.ProblemLabel != "" && req.SubmissionID != 0:
		WriteError(w, http.StatusBadRequest, "bad_request", "Rejudge a problem or one submission, not both.")
		return
	case req.ProblemLabel != "":
		for _, p := range c.Problems {
			if p.Label == req.ProblemLabel {
				rr.ProblemID = p.ProblemID
			}
		}
		if rr.ProblemID == 0 {
			WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest.")
			return
		}
		if !check(rr.ProblemID) {
			return
		}
	case req.SubmissionID > 0:
		info, err := d.Submissions.Info(r.Context(), req.SubmissionID)
		if err != nil || info.ContestID != c.ID || info.Kind != "submit" {
			WriteError(w, http.StatusNotFound, "not_found", "There is no such submission in this contest.")
			return
		}
		if !check(info.ProblemID) {
			return
		}
		judgedProblem = info.ProblemID
	default:
		WriteError(w, http.StatusBadRequest, "bad_request", "Choose a problem or one submission to rejudge.")
		return
	}
	n, err := d.Submissions.Rejudge(r.Context(), rr)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	d.Contests.Invalidate(c.ID)
	what := map[string]any{"queued": n}
	if req.ProblemLabel != "" {
		what["problem_label"] = req.ProblemLabel
	} else {
		what["submission_id"] = req.SubmissionID
	}
	// The revisions the new jobs judge against and record (B2.10).
	revs := []map[string]any{}
	for _, p := range c.Problems {
		if p.ProblemID == rr.ProblemID || (rr.SubmissionID > 0 && p.ProblemID == judgedProblem) {
			revs = append(revs, map[string]any{"label": p.Label, "revision_id": p.RevisionID, "rev": p.Rev})
		}
	}
	what["revisions"] = revs
	d.audit(r, c.ID, contests.AuditRejudge, what)
	d.Log.Info("rejudge queued", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "count", n)
	WriteJSON(w, http.StatusAccepted, map[string]any{"queued": n, "revisions": revs, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d Deps) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Announcements(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"announcements": list, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d Deps) postAnnouncement(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		BodyMD string `json:"body_md"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := d.Contests.Announce(r.Context(), c, req.BodyMD, userFrom(r.Context()).ID)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, a)
}

func (d Deps) listClarifications(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Clarifications(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"clarifications": list, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d Deps) answerClarification(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	cid, ok := parseID(r.PathValue("cid"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such question in this contest.")
		return
	}
	var req struct {
		Answer string `json:"answer"`
		Public bool   `json:"public"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	cl, err := d.Contests.Answer(r.Context(), c, cid, req.Answer, req.Public, userFrom(r.Context()).ID)
	if err == contests.ErrNotFound {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such question in this contest.")
		return
	}
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, cl)
}

func (d Deps) adminStandings(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	variant := contests.VariantFull
	if r.URL.Query().Get("variant") == contests.VariantFrozen {
		variant = contests.VariantFrozen
	}
	// A co-organiser whose site role is student gets the board a student
	// would see: frozen during the freeze, none yet in exam mode.
	if !siteAdmin(r) {
		v, err := d.coOrganiserVariant(w, r, c)
		if err != nil {
			return
		}
		if v == contests.VariantFrozen {
			variant = v
		}
	}
	out, err := d.standingsOut(r.Context(), c, variant, userFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// getReveal returns the frozen and full boards for the projector walk,
// once every student's time is up.
func (d Deps) getReveal(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	// A co-organiser whose site role is student sees the full board when
	// students do: with a held reveal, after it.
	if d.resultsHidden(w, r, c) {
		return
	}
	final, err := d.Contests.FinalAt(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if d.Contests.Clock().Before(final) {
		d.contestError(w, r, contests.ErrRevealEarly)
		return
	}
	me := userFrom(r.Context()).ID
	frozen, err := d.standingsOut(r.Context(), c, contests.VariantFrozen, me)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	full, err := d.standingsOut(r.Context(), c, contests.VariantFull, me)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"frozen": frozen, "full": full, "revealed_at": millis(c.RevealedAt),
		"server_now": d.Contests.Clock().UnixMilli()})
}

// postReveal releases a held board: students see the full standings.
func (d Deps) postReveal(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	if err := d.Contests.Reveal(r.Context(), c); err != nil {
		d.contestError(w, r, err)
		return
	}
	if c.RevealedAt.IsZero() {
		d.audit(r, c.ID, contests.AuditReveal, nil)
	}
	d.Log.Info("contest revealed", "by", userFrom(r.Context()).ID, "contest_id", c.ID)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func attachment(w http.ResponseWriter, contentType, name string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
}

func (d Deps) exportCSV(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || d.resultsHidden(w, r, c) {
		return
	}
	rows, _, err := d.Contests.Export(r.Context(), c, siteAdmin(r))
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	var buf bytes.Buffer
	if err := contests.WriteCSV(&buf, c, rows, siteAdmin(r)); err != nil {
		d.internalError(w, r, err)
		return
	}
	d.Log.Info("contest results exported", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "format", "csv")
	attachment(w, "text/csv; charset=utf-8", c.Slug+"-results.csv")
	w.Write(buf.Bytes())
}

func (d Deps) exportJSON(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || d.resultsHidden(w, r, c) {
		return
	}
	rows, _, err := d.Contests.Export(r.Context(), c, siteAdmin(r))
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	subs, err := d.Contests.ExportSubmissions(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	meta, err := d.adminContestOut(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	log, err := d.Contests.AuditAll(r.Context(), c.ID, !siteAdmin(r))
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	body, err := contests.MarshalExport(contests.JSONExport{Contest: meta, Standings: rows, Submissions: subs, Audit: log,
		ExportedAt: d.Contests.Clock().UnixMilli()})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	d.Log.Info("contest results exported", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "format", "json")
	attachment(w, "application/json; charset=utf-8", c.Slug+"-results.json")
	w.Write(body)
}

func (d Deps) contestStats(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || d.resultsHidden(w, r, c) {
		return
	}
	stats, err := d.Contests.Stats(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"problems": stats, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d Deps) setEditorial(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		EditorialMD string `json:"editorial_md"`
		Published   bool   `json:"published"`
	}
	if !decodeJSONLimit(w, r, &req, 80<<10) {
		return
	}
	if err := d.Contests.SetEditorial(r.Context(), c.ID, req.EditorialMD, req.Published); err != nil {
		d.contestError(w, r, err)
		return
	}
	// Publishing the editorial hands students the solutions: recorded when
	// it is published, unpublished, or changed while published.
	changed := req.EditorialMD != c.Editorial
	if req.Published != c.EditorialPublished || (req.Published && changed) {
		d.audit(r, c.ID, contests.AuditEditorial, map[string]any{"published": req.Published, "was_published": c.EditorialPublished,
			"changed": changed, "bytes": len(req.EditorialMD)})
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

// coOrganiserVariant is the board a co-organiser whose site role is
// student may see, or answers 403 board_closed itself and returns an error.
func (d Deps) coOrganiserVariant(w http.ResponseWriter, r *http.Request, c *contests.Contest) (string, error) {
	v, err := d.Contests.CoOrganiserVariant(r.Context(), c)
	if errors.Is(err, contests.ErrBoardClosed) {
		WriteError(w, http.StatusForbidden, "board_closed", "Co-organisers see the board when students do: in this contest, once every student's time is up.")
		return "", err
	}
	if err != nil {
		d.internalError(w, r, err)
	}
	return v, err
}

// contestAudit lists the contest's audit log, newest first, to its
// organisers (co-organisers included).
func (d Deps) contestAudit(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	list, more, err := d.Contests.AuditLog(r.Context(), c.ID, page, contests.AuditFilter{Action: q.Get("action"), Actor: q.Get("actor"),
		NoIntegrity: !siteAdmin(r)})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"entries": list, "page": max(page, 1), "has_more": more,
		"server_now": d.Contests.Clock().UnixMilli()})
}

// annotateFeed adds each item's note count and manual case, for reviewers
// (owners and admins) only, and never on the viewer's own submissions,
// whose notes they may not read.
func (d Deps) annotateFeed(ctx context.Context, contestID, viewer int64, items []contests.FeedItem) error {
	notes, cases := map[int64]int{}, map[int64]int64{}
	rows, err := d.DB.QueryContext(ctx, "SELECT submission_id, count(*) FROM submission_notes WHERE contest_id = ? GROUP BY submission_id", contestID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var n int
		rows.Scan(&id, &n)
		notes[id] = n
	}
	rows.Close()
	rows, err = d.DB.QueryContext(ctx, `SELECT submission_id, max(id) FROM integrity_cases WHERE contest_id = ? AND source = 'manual'
		AND submission_id IS NOT NULL GROUP BY submission_id`, contestID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sub, id int64
		rows.Scan(&sub, &id)
		cases[sub] = id
	}
	rows.Close()
	for i := range items {
		if items[i].UserID == viewer {
			continue
		}
		n := notes[items[i].ID]
		items[i].NoteCount = &n
		if id, ok := cases[items[i].ID]; ok {
			items[i].CaseID = &id
		}
	}
	return rows.Err()
}
