package integrity

// Integrity routes. Owners and admins run the similarity check
// after a contest, read the ranked signals and the side-by-side pair
// view, download the similar pairs as CSV, and record one decision per
// student with a reason (cases.go). Anyone else gets the same
// 404 as a missing contest. A reviewer named in a signal (a student since
// made an admin) never sees it.

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/httpx"
)

// Handlers serve the integrity routes. They need the contests too.
type Handlers struct {
	Integrity *Service
	Contests  *contests.Service
	// Organised loads the contest named by {id} as the organiser routes
	// do, answering a 404 itself when there is none.
	Organised func(http.ResponseWriter, *http.Request) (*contests.Contest, bool)
	Log       *slog.Logger
}

func (d *Handlers) internalError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.InternalError(d.Log, w, r, err)
}

// reviewedContest loads the contest named by {id}; the routes already
// need the admin role.
func (d *Handlers) reviewedContest(w http.ResponseWriter, r *http.Request) (*contests.Contest, bool) {
	return d.Organised(w, r)
}

// integrityError maps the integrity package's errors to responses.
func (d *Handlers) integrityError(w http.ResponseWriter, r *http.Request, err error, notFound string) {
	var input *InputError
	switch {
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", input.Message)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", notFound)
	case errors.Is(err, ErrBusy):
		httpx.WriteError(w, http.StatusConflict, "already_running", "A check is already running. Try again in a minute.")
	default:
		d.internalError(w, r, err)
	}
}

// serverNow is the contest clock in Unix milliseconds, for server_now.
func (d *Handlers) serverNow() int64 { return d.Contests.Clock().UnixMilli() }

// RunIntegrity starts a check. It is refused until the contest is over for
// everyone: the close plus the longest personal extension, the same moment
// the editorial and practice open.
func (d *Handlers) RunIntegrity(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	if d.Contests.Clock().Before(c.Final()) {
		httpx.WriteError(w, http.StatusConflict, "contest_live", "Check integrity after the contest ends for everyone.")
		return
	}
	u := auth.UserFrom(r.Context())
	id, err := d.Integrity.Start(r.Context(), c.ID, u.ID)
	if err != nil {
		d.integrityError(w, r, err, "")
		return
	}
	d.Log.Info("integrity check started", "by", u.ID, "contest_id", c.ID, "run_id", id)
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "server_now": d.serverNow()})
}

// problemRef names a problem as the integrity pages show it.
type problemRef struct {
	Label string `json:"label"`
	Title string `json:"title"`
}

func problemRefs(c *contests.Contest) map[int64]problemRef {
	out := map[int64]problemRef{}
	for _, p := range c.Problems {
		out[p.ProblemID] = problemRef{Label: p.Label, Title: p.Title}
	}
	return out
}

// runJSON is Run with the people and problems it names.
type runJSON struct {
	*Run
	Users     map[int64]Person     `json:"users"`
	Problems  map[int64]problemRef `json:"problems"`
	ServerNow int64                `json:"server_now"`
}

func (d *Handlers) GetIntegrity(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	run, err := d.Integrity.Latest(r.Context(), c.ID)
	if err != nil {
		d.integrityError(w, r, err, "This contest has not been checked yet.")
		return
	}
	// A reviewer named in a signal (a student since made an admin) never
	// sees it, as they never see a case they are in.
	me := auth.UserFrom(r.Context()).ID
	kept := run.Signals[:0:0]
	for _, sg := range run.Signals {
		if sg.UserA != me && sg.UserB != me {
			kept = append(kept, sg)
		}
	}
	run.Signals = kept
	var ids []int64
	for _, sg := range run.Signals {
		ids = append(ids, sg.UserA, sg.UserB)
	}
	users, err := d.Integrity.People(r.Context(), c.ID, ids)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, runJSON{Run: run, Users: users, Problems: problemRefs(c), ServerNow: d.serverNow()})
}

func (d *Handlers) GetIntegrityPair(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such similar-code signal in this contest."
	id, ok := httpx.ParseID(r.PathValue("signal"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	a, b, err := d.Integrity.Pair(r.Context(), c.ID, id)
	if err != nil {
		d.integrityError(w, r, err, missing)
		return
	}
	if me := auth.UserFrom(r.Context()).ID; a.UserID == me || b.UserID == me {
		httpx.WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"a": a, "b": b, "server_now": d.serverNow()})
}

func (d *Handlers) GetIntegrityTimeline(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such student in this contest."
	userID, ok := httpx.ParseID(r.PathValue("user"))
	if !ok || userID == auth.UserFrom(r.Context()).ID {
		httpx.WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	in, err := d.Integrity.Participant(r.Context(), c.ID, userID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if !in {
		httpx.WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	moments, err := d.Integrity.Timeline(r.Context(), c.ID, userID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"moments": moments, "server_now": d.serverNow()})
}

// GetIntegrityCounts answers the editor's counts per student for the
// similar-code page: pastes, focus losses and full-screen exits.
// The reviewer's own row is left out, as their timeline is.
func (d *Handlers) GetIntegrityCounts(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	all, err := d.Integrity.ContestCounts(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	me := auth.UserFrom(r.Context()).ID
	out := make([]Counts, 0, len(all))
	for _, x := range all {
		if x.UserID != me {
			out = append(out, x)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"counts": out, "server_now": d.serverNow()})
}

// caseJSON is a decision list with server_now.
func (d *Handlers) ListIntegrityCases(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	cases, err := d.Integrity.Cases(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"cases": cases, "server_now": d.serverNow()})
}

// DecideIntegrityCase records the decision about one student: no_action,
// warning or excluded, with a reason. It replaces an earlier decision.
func (d *Handlers) DecideIntegrityCase(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such student in this contest."
	userID, ok := httpx.ParseID(r.PathValue("user"))
	u := auth.UserFrom(r.Context())
	if !ok || userID == u.ID {
		httpx.WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	cs, err := d.Integrity.Decide(r.Context(), c.ID, userID, contests.Actor{ID: u.ID, Email: u.Email, Role: string(u.Role)}, body.Decision, body.Reason)
	if err != nil {
		d.integrityError(w, r, err, missing)
		return
	}
	d.Log.Info("integrity decision", "by", u.ID, "contest_id", c.ID, "user_id", userID, "decision", cs.Decision)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"case": cs, "server_now": d.serverNow()})
}

// ExportPairs writes the latest check's similar-code pairs as CSV, the
// evidence an organiser hands to the college's own process.
func (d *Handlers) ExportPairs(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	run, err := d.Integrity.Latest(r.Context(), c.ID)
	if err != nil {
		d.integrityError(w, r, err, "This contest has not been checked yet.")
		return
	}
	var ids []int64
	for _, sg := range run.Signals {
		ids = append(ids, sg.UserA, sg.UserB)
	}
	users, err := d.Integrity.People(r.Context(), c.ID, ids)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+c.Slug+`-similar-pairs.csv"`)
	if err := WritePairsCSV(w, run, users, problemLabels(c)); err != nil {
		d.Log.Error("similar pairs export", "contest_id", c.ID, "err", err)
	}
}

func problemLabels(c *contests.Contest) map[int64]string {
	out := map[int64]string{}
	for _, p := range c.Problems {
		out[p.ProblemID] = p.Label
	}
	return out
}
