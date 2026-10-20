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
