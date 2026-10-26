package httpapi

// Revisions and the site audit: the problem's current revision and the
// live contests that pin it, "Update students now" (move a contest's pin
// to the newest validated revision), and the People page's history. No
// route here returns a test's input, output or hash (D47).

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/problems"
)

type revisionRefJSON struct {
	ID  int64 `json:"id"`
	Rev int   `json:"rev"`
}

// addRevisions fills Problem's revision fields: the newest revision and
// the running contests that pin the problem.
func (d Deps) addRevisions(ctx context.Context, p *problems.Problem, out *problemJSON) error {
	list, _, _, err := d.Problems.Revisions(ctx, p.ID, 1)
	if err != nil {
		return err
	}
	if len(list) > 0 {
		out.Revision = &revisionRefJSON{list[0].ID, list[0].Rev}
	}
	out.LiveContests, err = d.Problems.LiveContests(ctx, p.ID)
	return err
}

// updatePin is "Update contest to revision N" (C4). Owners and admins
// only: choosing what a contest judges with is choosing its problems.
func (d Deps) updatePin(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || !requireAdmin(w, r) {
		return
	}
	var req struct {
		RevisionID int64 `json:"revision_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	label := r.PathValue("label")
	u, err := d.Contests.UpdatePin(r.Context(), c.ID, label, req.RevisionID, actor(r))
	switch {
	case errors.Is(err, contests.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "There is no such problem in this contest, or the revision is not one of its problem's.")
		return
	case errors.Is(err, contests.ErrSamePin):
		WriteError(w, http.StatusConflict, "conflict", "The contest already uses this revision.")
		return
	case errors.Is(err, contests.ErrPinMoved):
		WriteError(w, http.StatusConflict, "conflict", "Someone else changed this problem's revision a moment ago. Reload the console and check it again.")
		return
	case errors.Is(err, contests.ErrRevisionNotReady):
		WriteError(w, http.StatusConflict, "not_ready", "This revision is not the validated version of the problem. Validate the problem, then update the contest.")
		return
	case err != nil:
		d.contestError(w, r, err)
		return
	}
	d.Log.Info("contest pin updated", "by", userFrom(r.Context()).ID, "contest_id", c.ID, "label", label, "revision_id", u.To.ID)
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
	WriteJSON(w, http.StatusOK, map[string]any{"contest": out, "announcement_md": u.AnnouncementMD,
		"statement_changed": u.StatementChanged, "rejudge_needed": u.JudgingChanged, "server_now": d.Contests.Clock().UnixMilli()})
}

// siteAudit is the People page's History (B2.8), owners only.
func (d Deps) siteAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	until, _ := strconv.ParseInt(q.Get("until"), 10, 64)
	list, more, err := d.Auth.SiteAudit(r.Context(), auth.SiteAuditFilter{Action: q.Get("action"), Q: q.Get("q"), Since: since, Until: until, Page: page})
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"entries": list, "page": max(page, 1), "has_more": more, "server_now": time.Now().UnixMilli()})
}
