package contests

// Organiser contest routes. Owners and admins organise every contest;
// everyone else gets a 404, so contest ids cannot be probed. Every change
// that can affect results is written to the contest's audit log in the
// same transaction as the change.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/httpx"
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
	// The pinned revision and the problem's newest one.
	RevisionID          int64 `json:"revision_id"`
	Rev                 int   `json:"rev"`
	LatestRevisionID    int64 `json:"latest_revision_id"`
	LatestRev           int   `json:"latest_rev"`
	UpdateAvailable     bool  `json:"update_available"`
	UpdateStatementOnly bool  `json:"update_statement_only"`
	Reconstructed       bool  `json:"reconstructed"`
}

// adminContestJSON is AdminContest in docs/API.md: every setting, the
// problems with their pins, and extensions.
type adminContestJSON struct {
	ID                 int64              `json:"id"`
	Slug               string             `json:"slug"`
	Title              string             `json:"title"`
	DescriptionMD      string             `json:"description_md"`
	StartsAt           int64              `json:"starts_at"`
	EndsAt             int64              `json:"ends_at"`
	CloseAt            int64              `json:"close_at"`
	FreezeMinutes      int                `json:"freeze_minutes"`
	Published          bool               `json:"published"`
	PracticeAfterEnd   bool               `json:"practice_after_end"`
	MinGapS            int                `json:"min_gap_s"`
	MaxPerProblem      int                `json:"max_per_problem"`
	Languages          []string           `json:"languages"`
	HonourText         string             `json:"honour_text"`
	DefaultHonourText  string             `json:"default_honour_text"`
	HoldReveal         bool               `json:"hold_reveal"`
	EditorialMD        string             `json:"editorial_md"`
	EditorialPublished bool               `json:"editorial_published"`
	Paused             bool               `json:"paused"`
	ExtendedMinutes    int                `json:"extended_minutes"`
	RevealedAt         *int64             `json:"revealed_at"`
	Archived           bool               `json:"archived"`
	State              string             `json:"state"`
	Problems           []adminProblemJSON `json:"problems"`
	Extensions         []Extension        `json:"extensions,omitempty"`
	CreatedAt          int64              `json:"created_at"`
	UpdatedAt          int64              `json:"updated_at"`
	ServerNow          int64              `json:"server_now"`
	// Queue health for the console's attention strip.
	Queue            *queueJSON `json:"queue,omitempty"`
	JudgeLastClaimAt *int64     `json:"judge_last_claim_at"`
	Unanswered       int        `json:"unanswered"`
	// The exam options: full_screen, count_focus, block_paste, block_copy.
	ExamOptions
}

// queueJSON is the contest's share of the judge queue.
type queueJSON struct {
	Waiting     int     `json:"waiting"`
	Judging     int     `json:"judging"`
	Errors      int     `json:"errors"`
	OldestWaitS float64 `json:"oldest_wait_s"`
}

// adminContestBase is AdminContest from the loaded contest alone, with no
// query: what the contest list shows (no query per contest).
func (d *Handlers) adminContestBase(c *Contest) *adminContestJSON {
	now := d.Contests.Clock()
	out := &adminContestJSON{ID: c.ID, Slug: c.Slug, Title: c.Title, DescriptionMD: c.Description,
		StartsAt: c.Schedule.StartsAt.UnixMilli(), EndsAt: c.Schedule.EndsAt.UnixMilli(), CloseAt: c.Schedule.Close().UnixMilli(),
		FreezeMinutes: int(c.Schedule.Freeze / time.Minute), Published: c.Published,
		PracticeAfterEnd: c.PracticeAfterEnd, MinGapS: int(c.MinGap / time.Second),
		MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, HonourText: c.HonourText, DefaultHonourText: DefaultHonour,
		HoldReveal: c.HoldReveal, ExamOptions: c.Exam, EditorialMD: c.Editorial, EditorialPublished: c.EditorialPublished, Paused: c.Paused,
		ExtendedMinutes: int(c.Schedule.Extended / time.Minute), RevealedAt: httpx.Millis(c.RevealedAt), Archived: c.Archived,
		State: c.Schedule.State(now, c.PracticeAfterEnd), Problems: []adminProblemJSON{},
		CreatedAt: c.CreatedAt.UnixMilli(), UpdatedAt: c.UpdatedAt.UnixMilli(), ServerNow: now.UnixMilli()}
	for _, p := range c.Problems {
		ap := adminProblemJSON{Label: p.Label, ProblemID: p.ProblemID, Title: p.Title,
			PinnedHash: p.PinnedHash, Ready: p.Ready, MaxScore: p.MaxScore(),
			RevisionID: p.RevisionID, Rev: p.Rev, LatestRevisionID: p.LatestRevisionID, LatestRev: p.LatestRev,
			UpdateAvailable:     p.LatestRevisionID != p.RevisionID && p.LatestReady,
			UpdateStatementOnly: p.LatestRevisionID != p.RevisionID && p.LatestStatementOnly, Reconstructed: p.Reconstructed}
		out.Problems = append(out.Problems, ap)
	}
	return out
}

// adminContestOut is the whole AdminContest: the base plus extensions and
// the queue's health.
func (d *Handlers) adminContestOut(ctx context.Context, c *Contest) (*adminContestJSON, error) {
	out := d.adminContestBase(c)
	var err error
	if out.Extensions, err = d.Contests.Extensions(ctx, c.ID); err != nil {
		return nil, err
	}

	q, err := d.Contests.QueueHealth(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	out.Queue = &queueJSON{Waiting: q.Waiting, Judging: q.Judging, Errors: q.Errors, OldestWaitS: q.OldestWait.Seconds()}
	out.Unanswered = q.Unanswered
	if d.Problems != nil && d.Problems.Jobs != nil {
		if at, ok := d.Problems.Jobs.LastClaimAt(); ok {
			out.JudgeLastClaimAt = httpx.Millis(at)
		}
	}
	return out, nil
}

// writeAdminContest reloads the contest and answers with AdminContest.
func (d *Handlers) writeAdminContest(w http.ResponseWriter, r *http.Request, id int64, status int) {
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
	httpx.WriteJSON(w, status, out)
}

// OrganisedContest loads the contest named by {id} for one of its
// organisers; anyone else gets the same 404 as a missing contest.
func (d *Handlers) OrganisedContest(w http.ResponseWriter, r *http.Request) (*Contest, bool) {
	id, ok := httpx.ParseID(r.PathValue("id"))
	if !ok {
		d.contestError(w, r, ErrNotFound)
		return nil, false
	}
	c, err := d.Contests.Get(r.Context(), id)
	if err != nil {
		d.contestError(w, r, err)
		return nil, false
	}
	return c, true
}

// actor is the signed-in user as the audit log names them.
func actor(r *http.Request) Actor {
	u := auth.UserFrom(r.Context())
	return Actor{ID: u.ID, Email: u.Email, Role: string(u.Role)}
}

func (d *Handlers) ListAdminContests(w http.ResponseWriter, r *http.Request) {
	all, err := d.Contests.List(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := []*adminContestJSON{}
	for _, c := range all {
		out = append(out, d.adminContestBase(c))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"contests": out, "server_now": d.Contests.Clock().UnixMilli()})
}

// contestEdit is the body of create and update: every field of the form.
// Fields left out keep their current value (defaults on create).
type contestEdit struct {
	Slug             string   `json:"slug"`
	Title            string   `json:"title"`
	DescriptionMD    string   `json:"description_md"`
	StartsAt         int64    `json:"starts_at"`
	EndsAt           int64    `json:"ends_at"`
	FreezeMinutes    int      `json:"freeze_minutes"`
	Published        bool     `json:"published"`
	PracticeAfterEnd bool     `json:"practice_after_end"`
	MinGapS          int      `json:"min_gap_s"`
	MaxPerProblem    int      `json:"max_per_problem"`
	Languages        []string `json:"languages"`
	HonourText       string   `json:"honour_text"`
	HoldReveal       bool     `json:"hold_reveal"`
	ExamOptions
}

func (e contestEdit) edit() Edit {
	return Edit{Slug: strings.TrimSpace(e.Slug), Title: e.Title, Description: e.DescriptionMD,
		StartsAt: time.UnixMilli(e.StartsAt), EndsAt: time.UnixMilli(e.EndsAt),
		FreezeMinutes: e.FreezeMinutes, Published: e.Published, PracticeAfterEnd: e.PracticeAfterEnd, MinGapS: e.MinGapS, MaxPerProblem: e.MaxPerProblem, Languages: e.Languages,
		HonourText: e.HonourText, HoldReveal: e.HoldReveal, Exam: e.ExamOptions}
}

func editOf(c *Contest) contestEdit {
	return contestEdit{Slug: c.Slug, Title: c.Title, DescriptionMD: c.Description, StartsAt: c.Schedule.StartsAt.UnixMilli(),
		EndsAt: c.Schedule.EndsAt.UnixMilli(), FreezeMinutes: int(c.Schedule.Freeze / time.Minute),
		Published: c.Published, PracticeAfterEnd: c.PracticeAfterEnd,
		MinGapS: int(c.MinGap / time.Second), MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, HonourText: c.HonourText,
		HoldReveal: c.HoldReveal, ExamOptions: c.Exam}
}

func (d *Handlers) CreateContest(w http.ResponseWriter, r *http.Request) {
	req := contestEdit{PracticeAfterEnd: true, MinGapS: 30, MaxPerProblem: 15,
		Languages: []string{"cpp17", "java21", "python3", "node22"}}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	u := auth.UserFrom(r.Context())
	c, err := d.Contests.Create(r.Context(), req.edit(), u.ID)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest created", "by", u.ID, "contest_id", c.ID)
	d.writeAdminContest(w, r, c.ID, http.StatusCreated)
}

func (d *Handlers) GetAdminContest(w http.ResponseWriter, r *http.Request) {
	if c, ok := d.OrganisedContest(w, r); ok {
		d.writeAdminContest(w, r, c.ID, http.StatusOK)
	}
}

func (d *Handlers) UpdateContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	req := editOf(c)
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if _, err := d.Contests.Update(r.Context(), c.ID, req.edit(), actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d *Handlers) ArchiveContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	if err := d.Contests.Archive(r.Context(), c.ID, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest archived", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (d *Handlers) CloneContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		StartsAt int64  `json:"starts_at"`
		EndsAt   int64  `json:"ends_at"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
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

// SetContestProblems chooses the contest's problems from the bank.
func (d *Handlers) SetContestProblems(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req []struct {
		ProblemID int64  `json:"problem_id"`
		Label     string `json:"label"`
		// AIReason confirms an AI problem no person has written a solution
		// for, in a graded contest.
		AIReason string `json:"ai_reason"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	choices := make([]ProblemChoice, 0, len(req))
	for _, p := range req {
		choices = append(choices, ProblemChoice{ProblemID: p.ProblemID, Label: p.Label, AIReason: p.AIReason})
	}
	if _, err := d.Contests.SetProblems(r.Context(), c.ID, choices, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

// SetExtension gives one student taking part extra time.
// Owners and admins only.
func (d *Handlers) SetExtension(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	userID, ok := httpx.ParseID(r.PathValue("user"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such person.")
		return
	}
	var req struct {
		Minutes int    `json:"minutes"`
		Reason  string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := d.Contests.ExtendFor(r.Context(), c, userID, req.Minutes, req.Reason, actor(r)); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such person.")
		case errors.Is(err, ErrNotParticipant):
			httpx.WriteError(w, http.StatusConflict, "not_participant", err.Error())
		default:
			d.contestError(w, r, err)
		}
		return
	}
	d.Log.Info("contest extension set", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "user_id", userID, "minutes", req.Minutes)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d *Handlers) ExtendContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		Minutes int `json:"minutes"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if _, err := d.Contests.ExtendAll(r.Context(), c.ID, req.Minutes, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest extended", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "minutes", req.Minutes)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d *Handlers) PauseContest(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := d.OrganisedContest(w, r)
		if !ok {
			return
		}
		if err := d.Contests.SetPaused(r.Context(), c.ID, paused, actor(r)); err != nil {
			d.contestError(w, r, err)
			return
		}
		d.Log.Info("contest judging paused or resumed", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "paused", paused)
		d.writeAdminContest(w, r, c.ID, http.StatusOK)
	}
}

func (d *Handlers) ContestFeed(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	f := FeedFilter{Verdict: q.Get("verdict")}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	if label := q.Get("problem"); label != "" {
		for _, p := range c.Problems {
			if p.Label == label {
				f.ProblemID = p.ProblemID
			}
		}
		if f.ProblemID == 0 {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest.")
			return
		}
	}
	if u := q.Get("user"); u != "" {
		id, ok := httpx.ParseID(u)
		if !ok {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The user filter must be a user id.")
			return
		}
		f.UserID = id
	}
	items, more, err := d.Contests.Feed(r.Context(), c, f)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"submissions": items, "page": max(f.Page, 1), "has_more": more,
		"server_now": d.Contests.Clock().UnixMilli()})
}

// Rejudge queues a problem's or one submission's rejudge. Owners and admins
// only: a rejudge can change every score on the board.
func (d *Handlers) Rejudge(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		ProblemLabel string `json:"problem_label"`
		SubmissionID int64  `json:"submission_id"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	rr := submissions.RejudgeRequest{ContestID: c.ID, SubmissionID: req.SubmissionID, By: auth.UserFrom(r.Context()).ID}
	var judgedProblem int64
	check := func(pid int64) bool {
		for _, p := range c.Problems {
			if p.ProblemID == pid && !p.Ready {
				d.contestError(w, r, ErrNotReady)
				return false
			}
		}
		return true
	}
	switch {
	case req.ProblemLabel != "" && req.SubmissionID != 0:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Rejudge a problem or one submission, not both.")
		return
	case req.ProblemLabel != "":
		for _, p := range c.Problems {
			if p.Label == req.ProblemLabel {
				rr.ProblemID = p.ProblemID
			}
		}
		if rr.ProblemID == 0 {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest.")
			return
		}
		if !check(rr.ProblemID) {
			return
		}
	case req.SubmissionID > 0:
		info, err := d.Submissions.Info(r.Context(), req.SubmissionID)
		if err != nil || info.ContestID != c.ID || info.Kind != "submit" {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such submission in this contest.")
			return
		}
		if !check(info.ProblemID) {
			return
		}
		judgedProblem = info.ProblemID
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Choose a problem or one submission to rejudge.")
		return
	}
	// The revisions the new jobs judge against and record.
	revs := []map[string]any{}
	for _, p := range c.Problems {
		if p.ProblemID == rr.ProblemID || (rr.SubmissionID > 0 && p.ProblemID == judgedProblem) {
			revs = append(revs, map[string]any{"label": p.Label, "revision_id": p.RevisionID, "rev": p.Rev})
		}
	}
	// The audit row commits with the new jobs.
	rr.Audit = func(ctx context.Context, tx *sql.Tx, queued int) error {
		what := map[string]any{"queued": queued, "revisions": revs}
		if req.ProblemLabel != "" {
			what["problem_label"] = req.ProblemLabel
		} else {
			what["submission_id"] = req.SubmissionID
		}
		return RecordTx(ctx, tx, c.ID, actor(r), AuditRejudge, what, d.Contests.Clock().UnixMilli())
	}
	n, err := d.Submissions.Rejudge(r.Context(), rr)
	if err != nil {
		submissions.WriteError(d.Log, w, r, err)
		return
	}
	d.Contests.Invalidate(c.ID)
	d.Log.Info("rejudge queued", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "count", n)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"queued": n, "revisions": revs, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d *Handlers) ListAnnouncements(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Announcements(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"announcements": list, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d *Handlers) PostAnnouncement(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		BodyMD string `json:"body_md"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	a, err := d.Contests.Announce(r.Context(), c, req.BodyMD, auth.UserFrom(r.Context()).ID)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a)
}

func (d *Handlers) ListClarifications(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	list, err := d.Contests.Clarifications(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"clarifications": list, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d *Handlers) AnswerClarification(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	cid, ok := httpx.ParseID(r.PathValue("cid"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such question in this contest.")
		return
	}
	var req struct {
		Answer string `json:"answer"`
		Public bool   `json:"public"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	cl, err := d.Contests.Answer(r.Context(), c, cid, req.Answer, req.Public, auth.UserFrom(r.Context()).ID)
	if err == ErrNotFound {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such question in this contest.")
		return
	}
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cl)
}

func (d *Handlers) AdminStandings(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	variant := VariantFull
	if r.URL.Query().Get("variant") == VariantFrozen {
		variant = VariantFrozen
	}
	out, err := d.standingsOut(r.Context(), c, variant, auth.UserFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if out.Rows, err = d.Contests.WithEmails(r.Context(), out.Rows); err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// GetReveal returns the frozen and full boards for the projector walk,
// once every student's time is up.
func (d *Handlers) GetReveal(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	final, err := d.Contests.FinalAt(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if d.Contests.Clock().Before(final) {
		d.contestError(w, r, ErrRevealEarly)
		return
	}
	me := auth.UserFrom(r.Context()).ID
	frozen, err := d.standingsOut(r.Context(), c, VariantFrozen, me)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	full, err := d.standingsOut(r.Context(), c, VariantFull, me)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"frozen": frozen, "full": full, "revealed_at": httpx.Millis(c.RevealedAt),
		"server_now": d.Contests.Clock().UnixMilli()})
}

// PostReveal releases a held board: students see the full standings.
func (d *Handlers) PostReveal(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	if err := d.Contests.Reveal(r.Context(), c, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest revealed", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d *Handlers) ExportCSV(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	rows, _, err := d.Contests.Export(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	var buf bytes.Buffer
	// The browser's zone, so the start times read as they do on the screens.
	if err := WriteCSV(&buf, c, rows, ExportLocation(r.URL.Query().Get("tz"))); err != nil {
		d.internalError(w, r, err)
		return
	}
	d.Log.Info("contest results exported", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "format", "csv")
	httpx.Attachment(w, "text/csv; charset=utf-8", c.Slug+"-results.csv")
	w.Write(buf.Bytes())
}

func (d *Handlers) ExportJSON(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	rows, _, err := d.Contests.Export(r.Context(), c)
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
	log, err := d.Contests.AuditAll(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	body, err := MarshalExport(JSONExport{Contest: meta, Standings: rows, Submissions: subs, Audit: log,
		ExportedAt: d.Contests.Clock().UnixMilli()})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	d.Log.Info("contest results exported", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "format", "json")
	httpx.Attachment(w, "application/json; charset=utf-8", c.Slug+"-results.json")
	w.Write(body)
}

func (d *Handlers) ContestStats(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	stats, err := d.Contests.Stats(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	started, err := d.Contests.Started(r.Context(), c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"problems": stats, "started": started, "server_now": d.Contests.Clock().UnixMilli()})
}

func (d *Handlers) SetEditorial(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		EditorialMD string `json:"editorial_md"`
		Published   bool   `json:"published"`
	}
	if !httpx.DecodeJSONLimit(w, r, &req, 80<<10) {
		return
	}
	if err := d.Contests.SetEditorial(r.Context(), c.ID, req.EditorialMD, req.Published, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

// ContestAudit lists the contest's audit log, newest first.
func (d *Handlers) ContestAudit(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	list, more, err := d.Contests.AuditLog(r.Context(), c.ID, page, AuditFilter{Action: q.Get("action"), Actor: q.Get("actor")})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entries": list, "page": max(page, 1), "has_more": more,
		"server_now": d.Contests.Clock().UnixMilli()})
}

// UpdatePin is "Update students now": the contest's pin of a label moves
// to the problem's newest validated revision.
func (d *Handlers) UpdatePin(w http.ResponseWriter, r *http.Request) {
	c, ok := d.OrganisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		RevisionID int64 `json:"revision_id"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	label := r.PathValue("label")
	u, err := d.Contests.UpdatePin(r.Context(), c.ID, label, req.RevisionID, actor(r))
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest, or the revision is not one of its problem's.")
		return
	case errors.Is(err, ErrSamePin):
		httpx.WriteError(w, http.StatusConflict, "conflict", "The contest already uses this revision.")
		return
	case errors.Is(err, ErrPinMoved):
		httpx.WriteError(w, http.StatusConflict, "conflict", "Someone else changed this problem's revision a moment ago. Reload the console and check it again.")
		return
	case errors.Is(err, ErrRevisionNotReady):
		httpx.WriteError(w, http.StatusConflict, "not_ready", "This revision is not the validated version of the problem. Validate the problem, then update the contest.")
		return
	case err != nil:
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest pin updated", "by", auth.UserFrom(r.Context()).ID, "contest_id", c.ID, "label", label, "revision_id", u.To.ID)
	fresh, err := d.Contests.Get(r.Context(), c.ID)
	if err != nil {
		d.contestError(w, r, err)
		return
	}
	out, err := d.adminContestOut(r.Context(), fresh)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"contest": out, "announcement_md": u.AnnouncementMD,
		"statement_changed": u.StatementChanged, "rejudge_needed": u.JudgingChanged, "server_now": d.Contests.Clock().UnixMilli()})
}
