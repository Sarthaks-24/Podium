package httpapi

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

	"github.com/Sarthaks-24/podium/internal/contests"
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
	ID                 int64                `json:"id"`
	Slug               string               `json:"slug"`
	Title              string               `json:"title"`
	DescriptionMD      string               `json:"description_md"`
	StartsAt           int64                `json:"starts_at"`
	EndsAt             int64                `json:"ends_at"`
	CloseAt            int64                `json:"close_at"`
	FreezeMinutes      int                  `json:"freeze_minutes"`
	Published          bool                 `json:"published"`
	PracticeAfterEnd   bool                 `json:"practice_after_end"`
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
	Extensions         []contests.Extension `json:"extensions,omitempty"`
	CreatedAt          int64                `json:"created_at"`
	UpdatedAt          int64                `json:"updated_at"`
	ServerNow          int64                `json:"server_now"`
	// Queue health for the console's attention strip.
	Queue            *queueJSON `json:"queue,omitempty"`
	JudgeLastClaimAt *int64     `json:"judge_last_claim_at"`
	Unanswered       int        `json:"unanswered"`
	// The exam options: full_screen, count_focus, block_paste, block_copy.
	contests.ExamOptions
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
func (d Deps) adminContestBase(c *contests.Contest) *adminContestJSON {
	now := d.Contests.Clock()
	out := &adminContestJSON{ID: c.ID, Slug: c.Slug, Title: c.Title, DescriptionMD: c.Description,
		StartsAt: c.Schedule.StartsAt.UnixMilli(), EndsAt: c.Schedule.EndsAt.UnixMilli(), CloseAt: c.Schedule.Close().UnixMilli(),
		FreezeMinutes: int(c.Schedule.Freeze / time.Minute), Published: c.Published,
		PracticeAfterEnd: c.PracticeAfterEnd, MinGapS: int(c.MinGap / time.Second),
		MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, HonourText: c.HonourText, DefaultHonourText: contests.DefaultHonour,
		HoldReveal: c.HoldReveal, ExamOptions: c.Exam, EditorialMD: c.Editorial, EditorialPublished: c.EditorialPublished, Paused: c.Paused,
		ExtendedMinutes: int(c.Schedule.Extended / time.Minute), RevealedAt: millis(c.RevealedAt), Archived: c.Archived,
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
func (d Deps) adminContestOut(ctx context.Context, c *contests.Contest) (*adminContestJSON, error) {
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
	return c, true
}

// actor is the signed-in user as the audit log names them.
func actor(r *http.Request) contests.Actor {
	u := userFrom(r.Context())
	return contests.Actor{ID: u.ID, Email: u.Email, Role: string(u.Role)}
}

func (d Deps) listAdminContests(w http.ResponseWriter, r *http.Request) {
	all, err := d.Contests.List(r.Context(), r.URL.Query().Get("archived") == "1")
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := []*adminContestJSON{}
	for _, c := range all {
		out = append(out, d.adminContestBase(c))
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
	FreezeMinutes    int      `json:"freeze_minutes"`
	Published        bool     `json:"published"`
	PracticeAfterEnd bool     `json:"practice_after_end"`
	MinGapS          int      `json:"min_gap_s"`
	MaxPerProblem    int      `json:"max_per_problem"`
	Languages        []string `json:"languages"`
	HonourText       string   `json:"honour_text"`
	HoldReveal       bool     `json:"hold_reveal"`
	contests.ExamOptions
}

func (e contestEdit) edit() contests.Edit {
	return contests.Edit{Slug: strings.TrimSpace(e.Slug), Title: e.Title, Description: e.DescriptionMD,
		StartsAt: time.UnixMilli(e.StartsAt), EndsAt: time.UnixMilli(e.EndsAt),
		FreezeMinutes: e.FreezeMinutes, Published: e.Published, PracticeAfterEnd: e.PracticeAfterEnd, MinGapS: e.MinGapS, MaxPerProblem: e.MaxPerProblem, Languages: e.Languages,
		HonourText: e.HonourText, HoldReveal: e.HoldReveal, Exam: e.ExamOptions}
}

func editOf(c *contests.Contest) contestEdit {
	return contestEdit{Slug: c.Slug, Title: c.Title, DescriptionMD: c.Description, StartsAt: c.Schedule.StartsAt.UnixMilli(),
		EndsAt: c.Schedule.EndsAt.UnixMilli(), FreezeMinutes: int(c.Schedule.Freeze / time.Minute),
		Published: c.Published, PracticeAfterEnd: c.PracticeAfterEnd,
		MinGapS: int(c.MinGap / time.Second), MaxPerProblem: c.MaxPerProblem, Languages: c.Languages, HonourText: c.HonourText,
		HoldReveal: c.HoldReveal, ExamOptions: c.Exam}
}

func (d Deps) createContest(w http.ResponseWriter, r *http.Request) {
	req := contestEdit{PracticeAfterEnd: true, MinGapS: 30, MaxPerProblem: 15,
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
	if _, err := d.Contests.Update(r.Context(), c.ID, req.edit(), actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d Deps) archiveContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	if err := d.Contests.Archive(r.Context(), c.ID, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest archived", "by", userFrom(r.Context()).ID, "contest_id", c.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) cloneContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
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

// setContestProblems chooses the contest's problems from the bank.
func (d Deps) setContestProblems(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
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

// setExtension gives one student taking part extra time.
// Owners and admins only.
func (d Deps) setExtension(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
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
	if err := d.Contests.ExtendFor(r.Context(), c, userID, req.Minutes, req.Reason, actor(r)); err != nil {
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
	d.Log.Info("contest extension set", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "user_id", userID, "minutes", req.Minutes)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d Deps) extendContest(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		Minutes int `json:"minutes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := d.Contests.ExtendAll(r.Context(), c.ID, req.Minutes, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest extended", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "minutes", req.Minutes)
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

func (d Deps) pauseContest(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := d.organisedContest(w, r)
		if !ok {
			return
		}
		if err := d.Contests.SetPaused(r.Context(), c.ID, paused, actor(r)); err != nil {
			d.contestError(w, r, err)
			return
		}
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
	f := contests.FeedFilter{Verdict: q.Get("verdict")}
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
	WriteJSON(w, http.StatusOK, map[string]any{"submissions": items, "page": max(f.Page, 1), "has_more": more,
		"server_now": d.Contests.Clock().UnixMilli()})
}

// rejudge queues a problem's or one submission's rejudge. Owners and admins
// only: a rejudge can change every score on the board.
func (d Deps) rejudge(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		ProblemLabel string `json:"problem_label"`
		SubmissionID int64  `json:"submission_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	rr := submissions.RejudgeRequest{ContestID: c.ID, SubmissionID: req.SubmissionID, By: userFrom(r.Context()).ID}
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
		return contests.RecordTx(ctx, tx, c.ID, actor(r), contests.AuditRejudge, what, d.Contests.Clock().UnixMilli())
	}
	n, err := d.Submissions.Rejudge(r.Context(), rr)
	if err != nil {
		d.submissionError(w, r, err)
		return
	}
	d.Contests.Invalidate(c.ID)
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
	out, err := d.standingsOut(r.Context(), c, variant, userFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if out.Rows, err = d.withEmails(r.Context(), out.Rows); err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, out)
}

// withEmails copies the board's rows with each student's email, for the
// organisers' board: two students may share a name, and a testing-mode
// account may have only "Student 12". The copy leaves the cached
// board untouched.
func (d Deps) withEmails(ctx context.Context, rows []contests.Row) ([]contests.Row, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	out := append([]contests.Row(nil), rows...)
	at := make(map[int64][]int, len(out))
	for i, row := range out {
		at[row.UserID] = append(at[row.UserID], i)
	}
	q, err := d.DB.QueryContext(ctx, `SELECT u.id, u.email FROM users u
		WHERE u.id IN (SELECT value FROM json_each(?))`, idList(out))
	if err != nil {
		return nil, err
	}
	defer q.Close()
	for q.Next() {
		var id int64
		var email string
		if err := q.Scan(&id, &email); err != nil {
			return nil, err
		}
		for _, i := range at[id] {
			out[i].Email = email
		}
	}
	return out, q.Err()
}

// idList is the rows' user ids as a JSON array, for json_each.
func idList(rows []contests.Row) string {
	b := make([]byte, 0, len(rows)*6)
	b = append(b, '[')
	for i, row := range rows {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, row.UserID, 10)
	}
	return string(append(b, ']'))
}

// getReveal returns the frozen and full boards for the projector walk,
// once every student's time is up.
func (d Deps) getReveal(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
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
	if err := d.Contests.Reveal(r.Context(), c, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
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
	if err := contests.WriteCSV(&buf, c, rows, contests.ExportLocation(r.URL.Query().Get("tz"))); err != nil {
		d.internalError(w, r, err)
		return
	}
	d.Log.Info("contest results exported", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "format", "csv")
	attachment(w, "text/csv; charset=utf-8", c.Slug+"-results.csv")
	w.Write(buf.Bytes())
}

func (d Deps) exportJSON(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
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
	WriteJSON(w, http.StatusOK, map[string]any{"problems": stats, "started": started, "server_now": d.Contests.Clock().UnixMilli()})
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
	if err := d.Contests.SetEditorial(r.Context(), c.ID, req.EditorialMD, req.Published, actor(r)); err != nil {
		d.contestError(w, r, err)
		return
	}
	d.writeAdminContest(w, r, c.ID, http.StatusOK)
}

// contestAudit lists the contest's audit log, newest first.
func (d Deps) contestAudit(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	list, more, err := d.Contests.AuditLog(r.Context(), c.ID, page, contests.AuditFilter{Action: q.Get("action"), Actor: q.Get("actor")})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"entries": list, "page": max(page, 1), "has_more": more,
		"server_now": d.Contests.Clock().UnixMilli()})
}
