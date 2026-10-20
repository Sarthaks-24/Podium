package httpapi

// History routes (v2 plan B2.5, B2.6 and B2.8; contracts C3 and C4): a
// problem's revisions for owners and admins, the statement history of a
// contest problem for its students, "Update contest to revision N", and
// the site audit. No route here returns a test's input, output or hash
// (D47): revisions carry titles, statements, limits and counts only.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// revisionListLimit is how many revisions Problem lists.
const revisionListLimit = 50

type revisionRefJSON struct {
	ID  int64 `json:"id"`
	Rev int   `json:"rev"`
}

type personRefJSON struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

type pinRefJSON struct {
	ContestID int64  `json:"contest_id"`
	Slug      string `json:"slug"`
	Label     string `json:"label"`
}

// revisionJSON is Revision in docs/API.md.
type revisionJSON struct {
	ID               int64          `json:"id"`
	Rev              int            `json:"rev"`
	CreatedAt        int64          `json:"created_at"`
	CreatedBy        *personRefJSON `json:"created_by"`
	Reason           string         `json:"reason"`
	ContentHash      string         `json:"content_hash"`
	StatementChanged bool           `json:"statement_changed"`
	JudgingChanged   bool           `json:"judging_changed"`
	Reconstructed    bool           `json:"reconstructed"`
	Validated        bool           `json:"validated"`
	PinnedIn         []pinRefJSON   `json:"pinned_in"`
}

// revisionDetailJSON is one revision with what students read and how it
// judges, never its tests.
type revisionDetailJSON struct {
	revisionJSON
	Title             string                     `json:"title"`
	StatementMD       string                     `json:"statement_md"`
	StatementRecorded bool                       `json:"statement_recorded"`
	TimeLimitMS       *int                       `json:"time_limit_ms"`
	MemoryLimitMB     *int                       `json:"memory_limit_mb"`
	Checker           *string                    `json:"checker"`
	Epsilon           *float64                   `json:"epsilon"`
	Samples           []int                      `json:"samples"`
	Open              []int                      `json:"open"`
	Subtasks          []problems.RevisionSubtask `json:"subtasks"`
	TestCount         int                        `json:"test_count"`
}

// person looks up a user for a created_by reference.
func (d Deps) person(ctx context.Context, id int64) (*personRefJSON, error) {
	if id <= 0 {
		return nil, nil
	}
	p := &personRefJSON{UserID: id}
	err := d.DB.QueryRowContext(ctx, "SELECT email, name FROM users WHERE id = ?", id).Scan(&p.Email, &p.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// pinsOf lists the contests that pin a revision now.
func (d Deps) pinsOf(ctx context.Context, revisionID int64) ([]pinRefJSON, error) {
	rows, err := d.DB.QueryContext(ctx, `SELECT c.id, c.slug, cp.label FROM contest_problems cp JOIN contests c ON c.id = cp.contest_id
		WHERE cp.revision_id = ? ORDER BY c.starts_at DESC, c.id`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pinRefJSON{}
	for rows.Next() {
		var p pinRefJSON
		if err := rows.Scan(&p.ContestID, &p.Slug, &p.Label); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d Deps) revisionOut(ctx context.Context, p *problems.Problem, r, prev *problems.Revision) (revisionJSON, error) {
	out := revisionJSON{ID: r.ID, Rev: r.Rev, CreatedAt: r.CreatedAt.UnixMilli(), Reason: r.Reason, ContentHash: r.ContentHash,
		Reconstructed: r.Reconstructed, Validated: p.ValidatedHash != "" && p.ValidatedHash == r.ContentHash}
	if prev != nil {
		out.StatementChanged = problems.StatementChanged(prev, r)
		out.JudgingChanged = prev.ContentHash != r.ContentHash
	}
	var err error
	if out.CreatedBy, err = d.person(ctx, r.CreatedBy); err != nil {
		return out, err
	}
	out.PinnedIn, err = d.pinsOf(ctx, r.ID)
	return out, err
}

// addRevisions fills Problem's revision fields.
func (d Deps) addRevisions(ctx context.Context, p *problems.Problem, out *problemJSON) error {
	list, prev, total, err := d.Problems.Revisions(ctx, p.ID, revisionListLimit)
	if err != nil {
		return err
	}
	out.Revisions, out.RevisionsTotal = []revisionJSON{}, total
	for i, r := range list {
		if i == 0 {
			out.Revision = &revisionRefJSON{r.ID, r.Rev}
		}
		rj, err := d.revisionOut(ctx, p, r, prev[i])
		if err != nil {
			return err
		}
		out.Revisions = append(out.Revisions, rj)
	}
	out.LiveContests, err = d.Problems.LiveContests(ctx, p.ID)
	return err
}

// problemRevision reads {id} and {rev} and loads both.
func (d Deps) problemRevision(w http.ResponseWriter, r *http.Request, raw string) (*problems.Problem, *problems.Revision, bool) {
	id, ok := problemID(w, r)
	if !ok {
		return nil, nil, false
	}
	p, err := d.Problems.Get(r.Context(), id)
	if err != nil {
		d.problemError(w, r, err)
		return nil, nil, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		WriteError(w, http.StatusNotFound, "not_found", "This problem has no such revision.")
		return nil, nil, false
	}
	rev, err := d.Problems.RevisionByNumber(r.Context(), id, n)
	if errors.Is(err, problems.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", "This problem has no such revision.")
		return nil, nil, false
	}
	if err != nil {
		d.internalError(w, r, err)
		return nil, nil, false
	}
	return p, rev, true
}

// getProblemRevision answers one revision (owners and admins).
func (d Deps) getProblemRevision(w http.ResponseWriter, r *http.Request) {
	p, rev, ok := d.problemRevision(w, r, r.PathValue("rev"))
	if !ok {
		return
	}
	var prev *problems.Revision
	if rev.Rev > 1 {
		var err error
		if prev, err = d.Problems.RevisionByNumber(r.Context(), p.ID, rev.Rev-1); err != nil && !errors.Is(err, problems.ErrNotFound) {
			d.internalError(w, r, err)
			return
		}
	}
	rj, err := d.revisionOut(r.Context(), p, rev, prev)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	tests, err := d.Problems.RevisionTests(r.Context(), rev.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, revisionDetailJSON{revisionJSON: rj, Title: rev.Title, StatementMD: rev.Statement,
		StatementRecorded: rev.StatementRecorded, TimeLimitMS: rev.TimeLimitMS, MemoryLimitMB: rev.MemoryLimitMB,
		Checker: rev.Checker, Epsilon: rev.Epsilon, Samples: rev.Samples, Open: rev.Open, Subtasks: rev.Subtasks, TestCount: len(tests)})
}

// statementDiffJSON is the line diff of two statements.
type statementDiffJSON struct {
	TitleFrom string              `json:"title_from"`
	TitleTo   string              `json:"title_to"`
	Coarse    bool                `json:"coarse"`
	Lines     []problems.DiffLine `json:"lines"`
}

func statementDiff(a, b *problems.Revision) statementDiffJSON {
	out := statementDiffJSON{TitleFrom: a.Title, TitleTo: b.Title, Lines: []problems.DiffLine{}}
	if a.StatementRecorded && b.StatementRecorded {
		out.Lines, out.Coarse = problems.LineDiff(a.Statement, b.Statement)
	}
	return out
}

// diffProblemRevisions compares two revisions by number (owners and
// admins).
func (d Deps) diffProblemRevisions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	_, from, ok := d.problemRevision(w, r, q.Get("from"))
	if !ok {
		return
	}
	_, to, ok := d.problemRevision(w, r, q.Get("to"))
	if !ok {
		return
	}
	changed := problems.RevisionChanges(from, to)
	statementOnly := true
	for _, c := range changed {
		if c != "title" && c != "statement" {
			statementOnly = false
		}
	}
	WriteJSON(w, http.StatusOK, struct {
		FromRev       int      `json:"from_rev"`
		ToRev         int      `json:"to_rev"`
		Changed       []string `json:"changed"`
		StatementOnly bool     `json:"statement_only"`
		statementDiffJSON
	}{from.Rev, to.Rev, changed, statementOnly, statementDiff(from, to)})
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

// statementHistory is C3: the revisions pinned to a contest problem, with
// a line diff of their statements. Never a test, an output or a hash.
func (d Deps) statementHistory(w http.ResponseWriter, r *http.Request) {
	t, ok := d.contestProblem(w, r)
	if !ok {
		return
	}
	entries, err := d.Contests.StatementHistory(r.Context(), t.c, t.problem.Label, t.organiser)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	updates, err := d.Contests.StatementUpdates(r.Context(), t.c)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	type diffJSON struct {
		FromRevisionID int64 `json:"from_revision_id"`
		ToRevisionID   int64 `json:"to_revision_id"`
		FromRev        int   `json:"from_rev"`
		ToRev          int   `json:"to_rev"`
		statementDiffJSON
	}
	out := struct {
		Label              string                    `json:"label"`
		CurrentRevisionID  int64                     `json:"current_revision_id"`
		StatementUpdatedAt *int64                    `json:"statement_updated_at"`
		Revisions          []contests.StatementEntry `json:"revisions"`
		Diff               *diffJSON                 `json:"diff"`
		ServerNow          int64                     `json:"server_now"`
	}{Label: t.problem.Label, CurrentRevisionID: t.problem.RevisionID, Revisions: entries, ServerNow: d.Contests.Clock().UnixMilli()}
	if at, ok := updates[t.problem.Label]; ok {
		out.StatementUpdatedAt = &at
	}
	pick := func(raw string, def int) (int, bool) {
		if raw == "" {
			return def, def >= 0
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, false
		}
		for i, e := range entries {
			if e.RevisionID == id {
				return i, true
			}
		}
		return 0, false
	}
	q := r.URL.Query()
	if len(entries) > 1 || q.Get("from") != "" || q.Get("to") != "" {
		to, okTo := pick(q.Get("to"), 0)
		from, okFrom := pick(q.Get("from"), min(1, len(entries)-1))
		if !okTo || !okFrom || len(entries) < 2 {
			WriteError(w, http.StatusNotFound, "not_found", "Choose two revisions of this problem that the contest used.")
			return
		}
		a, b := entries[from].Revision(), entries[to].Revision()
		out.Diff = &diffJSON{FromRevisionID: a.ID, ToRevisionID: b.ID, FromRev: a.Rev, ToRev: b.Rev, statementDiffJSON: statementDiff(a, b)}
	}
	WriteJSON(w, http.StatusOK, out)
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

// renamedContest answers 301 for a GET of a renamed contest's old slug on
// the web app's /contests/<old>/... paths (B2.9); the API's
// /api/contests/<old>/... are redirected by studentContest, only when the
// slug is not found, so the hot API paths pay nothing. Anything else goes
// on to next.
func (d Deps) renamedContest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && d.Contests != nil {
			if rest, ok := strings.CutPrefix(r.URL.Path, "/contests/"); ok && d.redirectRenamed(w, r, "/contests/", rest) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// redirectRenamed answers 301 when the first segment of rest is a slug
// no contest has now but a renamed one had. It reports whether it did.
func (d Deps) redirectRenamed(w http.ResponseWriter, r *http.Request, prefix, rest string) bool {
	slug, tail, _ := strings.Cut(rest, "/")
	if slug == "" {
		return false
	}
	current, err := d.Contests.RenamedTo(r.Context(), slug)
	if err != nil || current == "" {
		return false
	}
	u := *r.URL
	u.Path = prefix + current
	if tail != "" || strings.HasSuffix(rest, "/") {
		u.Path += "/" + tail
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.RequestURI(), http.StatusMovedPermanently)
	return true
}
