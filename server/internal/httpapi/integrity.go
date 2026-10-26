package httpapi

// Integrity routes (cache/v1/integrity-plan.md section 7, decisions D47
// and D49, cut down in v3). Owners and admins run the similarity check
// after a contest, read the ranked signals and the side-by-side pair
// view, download the similar pairs as CSV, and record one decision per
// student with a reason (integrity/cases.go). Anyone else gets the same
// 404 as a missing contest. A reviewer named in a signal (a student since
// made an admin) never sees it.

import (
	"errors"
	"net/http"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/integrity"
)

// integrityRoutes mounts the integrity API. It needs Contests as well.
func (d Deps) integrityRoutes(mux *http.ServeMux) {
	organiser := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	mux.HandleFunc("POST /api/admin/contests/{id}/integrity/run", organiser(d.runIntegrity))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity", organiser(d.limitReads(d.getIntegrity)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/pairs.csv", organiser(d.limitReads(d.exportPairs)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/signals/{signal}/pair", organiser(d.limitReads(d.getIntegrityPair)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/timeline/{user}", organiser(d.limitReads(d.getIntegrityTimeline)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/cases", organiser(d.limitReads(d.listIntegrityCases)))
	mux.HandleFunc("PUT /api/admin/contests/{id}/integrity/cases/{user}", organiser(d.decideIntegrityCase))
	d.studentsRoutes(mux)
}

// reviewedContest loads the contest named by {id}; the routes already
// need the admin role.
func (d Deps) reviewedContest(w http.ResponseWriter, r *http.Request) (*contests.Contest, bool) {
	return d.organisedContest(w, r)
}

// integrityError maps the integrity package's errors to responses.
func (d Deps) integrityError(w http.ResponseWriter, r *http.Request, err error, notFound string) {
	var input *integrity.InputError
	switch {
	case errors.As(err, &input):
		WriteError(w, http.StatusBadRequest, "bad_request", input.Message)
	case errors.Is(err, integrity.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", notFound)
	case errors.Is(err, integrity.ErrBusy):
		WriteError(w, http.StatusConflict, "busy", "A check is already running. Try again in a minute.")
	default:
		d.internalError(w, r, err)
	}
}

// serverNow is the contest clock in Unix milliseconds, for server_now.
func (d Deps) serverNow() int64 { return d.Contests.Clock().UnixMilli() }

// runIntegrity starts a check. It is refused until the contest is over for
// everyone: the close plus the longest personal extension, the same moment
// the editorial and practice open.
func (d Deps) runIntegrity(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	if d.Contests.Clock().Before(c.Final()) {
		WriteError(w, http.StatusConflict, "contest_live", "Check integrity after the contest ends for everyone.")
		return
	}
	u := userFrom(r.Context())
	id, err := d.Integrity.Start(r.Context(), c.ID, u.ID)
	if err != nil {
		d.integrityError(w, r, err, "")
		return
	}
	d.Log.Info("integrity check started", "by", u.ID, "contest_id", c.ID, "run_id", id)
	WriteJSON(w, http.StatusAccepted, map[string]any{"run_id": id, "server_now": d.serverNow()})
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
	*integrity.Run
	Users     map[int64]integrity.Person `json:"users"`
	Problems  map[int64]problemRef       `json:"problems"`
	ServerNow int64                      `json:"server_now"`
}

func (d Deps) getIntegrity(w http.ResponseWriter, r *http.Request) {
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
	me := userFrom(r.Context()).ID
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
	WriteJSON(w, http.StatusOK, runJSON{Run: run, Users: users, Problems: problemRefs(c), ServerNow: d.serverNow()})
}

func (d Deps) getIntegrityPair(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such similar-code signal in this contest."
	id, ok := parseID(r.PathValue("signal"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	a, b, err := d.Integrity.Pair(r.Context(), c.ID, id)
	if err != nil {
		d.integrityError(w, r, err, missing)
		return
	}
	if me := userFrom(r.Context()).ID; a.UserID == me || b.UserID == me {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"a": a, "b": b, "server_now": d.serverNow()})
}

func (d Deps) getIntegrityTimeline(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such student in this contest."
	userID, ok := parseID(r.PathValue("user"))
	if !ok || userID == userFrom(r.Context()).ID {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	in, err := d.Integrity.Participant(r.Context(), c.ID, userID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	if !in {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	moments, err := d.Integrity.Timeline(r.Context(), c.ID, userID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"moments": moments, "server_now": d.serverNow()})
}

// caseJSON is a decision list with server_now.
func (d Deps) listIntegrityCases(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	cases, err := d.Integrity.Cases(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"cases": cases, "server_now": d.serverNow()})
}

// decideIntegrityCase records the decision about one student: no_action,
// warning or excluded, with a reason. It replaces an earlier decision.
func (d Deps) decideIntegrityCase(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such student in this contest."
	userID, ok := parseID(r.PathValue("user"))
	u := userFrom(r.Context())
	if !ok || userID == u.ID {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	cs, err := d.Integrity.Decide(r.Context(), c.ID, userID, contests.Actor{ID: u.ID, Email: u.Email, Role: string(u.Role)}, body.Decision, body.Reason)
	if err != nil {
		d.integrityError(w, r, err, missing)
		return
	}
	d.Log.Info("integrity decision", "by", u.ID, "contest_id", c.ID, "user_id", userID, "decision", cs.Decision)
	WriteJSON(w, http.StatusOK, map[string]any{"case": cs, "server_now": d.serverNow()})
}

// exportPairs writes the latest check's similar-code pairs as CSV, the
// evidence an organiser hands to the college's own process.
func (d Deps) exportPairs(w http.ResponseWriter, r *http.Request) {
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
	if err := integrity.WritePairsCSV(w, run, users, problemLabels(c)); err != nil {
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
