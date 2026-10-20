package httpapi

// Integrity routes (cache/v1/integrity-plan.md section 7, decisions D47
// and D49). Reviewing needs the owner or admin role, or a co-organiser of
// that contest whose own role is not student: co-organisers who are
// students (TAs) never review their classmates. Anyone who does not
// organise the contest gets the same 404 as a missing contest, and a
// signal, student or case id from another contest is a 404 too. A
// reviewer who is one of a case's students (a student later made an
// admin) gets the same 404 for that case and never sees it in the list.
//
// A case's own pair view reads the evidence copied into the case, never
// the live signals, so a re-run cannot change what it shows.
//
// Students see only cases a reviewer shared with them, worded for them:
// their own evidence and code, never another student's name or program.
// Every case state change is an append-only decision in the database; case
// opens and decisions are also logged at info with ids only.

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/integrity"
)

// integrityRoutes mounts the integrity API. It needs Contests as well.
func (d Deps) integrityRoutes(mux *http.ServeMux) {
	signedIn := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Student, h) }
	mux.HandleFunc("POST /api/admin/contests/{id}/integrity/run", signedIn(d.runIntegrity))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity", signedIn(d.limitReads(d.getIntegrity)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/signals/{signal}/pair", signedIn(d.limitReads(d.getIntegrityPair)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/timeline/{user}", signedIn(d.limitReads(d.getIntegrityTimeline)))
	mux.HandleFunc("GET /api/admin/contests/{id}/integrity/cases", signedIn(d.limitReads(d.listIntegrityCases)))
	mux.HandleFunc("POST /api/admin/contests/{id}/integrity/cases", signedIn(d.openIntegrityCase))
	mux.HandleFunc("GET /api/admin/integrity/cases/{case}", signedIn(d.limitReads(d.getIntegrityCase)))
	mux.HandleFunc("GET /api/admin/integrity/cases/{case}/evidence/{index}/pair", signedIn(d.limitReads(d.getIntegrityCasePair)))
	mux.HandleFunc("PUT /api/admin/integrity/cases/{case}", signedIn(d.decideIntegrityCase))
	mux.HandleFunc("POST /api/admin/integrity/cases/{case}/proposals", signedIn(d.proposeOutcome))
	mux.HandleFunc("POST /api/admin/integrity/cases/{case}/proposals/{proposal}/confirm", signedIn(d.confirmOutcome))
	mux.HandleFunc("GET /api/admin/submissions/{id}/notes", signedIn(d.limitReads(d.listNotes)))
	mux.HandleFunc("POST /api/admin/submissions/{id}/notes", signedIn(d.addNote))
	mux.HandleFunc("POST /api/admin/submissions/{id}/case", signedIn(d.openManualCase))
	mux.HandleFunc("GET /api/admin/appeals", d.requireRole(auth.Admin, d.listAppeals))
	mux.HandleFunc("POST /api/admin/appeals/{id}/ruling", d.requireRole(auth.Admin, d.ruleAppeal))
	mux.HandleFunc("GET /api/admin/integrity/oversight", d.requireRole(auth.Owner, d.oversight))
	mux.HandleFunc("GET /api/admin/record-views", d.requireRole(auth.Owner, d.recordViews))

	mux.HandleFunc("GET /api/integrity/cases", signedIn(d.limitReads(d.myIntegrityCases)))
	mux.HandleFunc("PUT /api/integrity/cases/{case}/explanation", signedIn(d.explainIntegrityCase))
	mux.HandleFunc("POST /api/integrity/cases/{case}/opened", signedIn(d.openedIntegrityCase))
	mux.HandleFunc("POST /api/me/appeals", signedIn(d.fileAppeal))
	mux.HandleFunc("GET /api/me/standing", signedIn(d.limitReads(d.myStanding)))
	d.studentsRoutes(mux)
}

// mayReview reports whether u may review a contest's integrity results.
func (d Deps) mayReview(ctx context.Context, u auth.User, contestID int64) (bool, error) {
	if u.Role.AtLeast(auth.Admin) {
		return true, nil
	}
	if u.Role == auth.Student {
		return false, nil
	}
	return d.Contests.IsContestAdmin(ctx, contestID, u.ID)
}

// reviewedContest loads the contest named by {id} for a reviewer. People
// who do not organise it get a 404; co-organisers who may not review get
// a 403, since they already know the contest exists.
func (d Deps) reviewedContest(w http.ResponseWriter, r *http.Request) (*contests.Contest, bool) {
	c, ok := d.organisedContest(w, r)
	if !ok {
		return nil, false
	}
	may, err := d.mayReview(r.Context(), userFrom(r.Context()), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	if !may {
		WriteError(w, http.StatusForbidden, "forbidden", "Only owners and admins review integrity checks.")
		return nil, false
	}
	return c, true
}

// reviewedCase loads the case named by {case} for someone who may review
// its contest; anyone else gets the same 404 as a missing case.
func (d Deps) reviewedCase(w http.ResponseWriter, r *http.Request) (*integrity.Case, bool) {
	id, ok := parseID(r.PathValue("case"))
	if !ok {
		writeNoCase(w)
		return nil, false
	}
	c, err := d.Integrity.GetCase(r.Context(), id)
	if errors.Is(err, integrity.ErrNotFound) {
		writeNoCase(w)
		return nil, false
	}
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	u := userFrom(r.Context())
	may, err := d.mayReview(r.Context(), u, c.ContestID)
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	if !may || c.Involves(u.ID) {
		writeNoCase(w)
		return nil, false
	}
	return c, true
}

func writeNoCase(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, "not_found", "There is no such case.")
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
	case errors.Is(err, integrity.ErrExplanationChanged):
		WriteError(w, http.StatusConflict, "explanation_changed", "A student changed their explanation. Reload before deciding.")
	case errors.Is(err, integrity.ErrProposalNeeded):
		WriteError(w, http.StatusBadRequest, "proposal_needed", "Propose the outcome instead: a second reviewer confirms it.")
	case errors.Is(err, integrity.ErrProposalPending):
		WriteError(w, http.StatusConflict, "proposal_pending", "A proposal is waiting for a second reviewer. Confirm it, or clear or reopen the case first.")
	case errors.Is(err, integrity.ErrTwoPersonRule):
		WriteError(w, http.StatusForbidden, "two_person_rule", "A second reviewer who did not open the case or propose this outcome confirms it.")
	case errors.Is(err, integrity.ErrUninvolved):
		WriteError(w, http.StatusForbidden, "uninvolved_reviewer", "A reviewer who took no part in the case rules on the appeal.")
	case errors.Is(err, integrity.ErrContestNotOver):
		WriteError(w, http.StatusConflict, "contest_live", "Decide outcomes after the contest ends for everyone.")
	case errors.Is(err, integrity.ErrRatingChanged):
		WriteError(w, http.StatusConflict, "rating_changed", "The rating changed since this was proposed. Propose it again.")
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
	WriteJSON(w, http.StatusOK, map[string]any{"a": a, "b": b, "server_now": d.serverNow()})
}

func (d Deps) getIntegrityTimeline(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	const missing = "There is no such student in this contest."
	userID, ok := parseID(r.PathValue("user"))
	if !ok {
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

// caseJSON is Case with server_now.
type caseJSON struct {
	*integrity.Case
	ServerNow int64 `json:"server_now"`
}

func (d Deps) listIntegrityCases(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	all, err := d.Integrity.Cases(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	me := userFrom(r.Context()).ID
	list := []integrity.Case{}
	for _, cs := range all {
		if !cs.Involves(me) {
			list = append(list, cs)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"cases": list, "server_now": d.serverNow()})
}

func (d Deps) openIntegrityCase(w http.ResponseWriter, r *http.Request) {
	c, ok := d.reviewedContest(w, r)
	if !ok {
		return
	}
	var req struct {
		Title     string  `json:"title"`
		Note      string  `json:"note"`
		SignalIDs []int64 `json:"signal_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	cs, err := d.Integrity.OpenCase(r.Context(), c.ID, u.ID, req.Title, req.Note, req.SignalIDs)
	if err != nil {
		d.integrityError(w, r, err, "")
		return
	}
	d.Log.Info("integrity case opened", "by", u.ID, "contest_id", c.ID, "case_id", cs.ID, "signals", len(req.SignalIDs))
	WriteJSON(w, http.StatusCreated, caseJSON{Case: cs, ServerNow: d.serverNow()})
}

func (d Deps) getIntegrityCase(w http.ResponseWriter, r *http.Request) {
	cs, ok := d.reviewedCase(w, r)
	if !ok {
		return
	}
	// Every read of a case is logged per student it is about (D97).
	viewer := userFrom(r.Context()).ID
	for _, u := range cs.Users {
		if err := d.Integrity.LogView(r.Context(), viewer, u.UserID, integrity.ViewCase, cs.ID); err != nil {
			d.internalError(w, r, err)
			return
		}
	}
	d.writeCase(w, r, cs, http.StatusOK)
}

// writeCase answers a case with the viewer's own flags (may confirm, may
// rule, sole organiser).
func (d Deps) writeCase(w http.ResponseWriter, r *http.Request, cs *integrity.Case, status int) {
	if err := d.Integrity.ForViewer(r.Context(), cs, userFrom(r.Context()).ID); err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, status, caseJSON{Case: cs, ServerNow: d.serverNow()})
}

// getIntegrityCasePair is the side-by-side view of a case's similar-code
// evidence, numbered from 0 in the case's evidence list: the case's own
// copy of the signal and the submissions it names, which must still
// belong to the case's contest. It answers the same Side shape as the
// live pair route, plus the copied signal.
func (d Deps) getIntegrityCasePair(w http.ResponseWriter, r *http.Request) {
	cs, ok := d.reviewedCase(w, r)
	if !ok {
		return
	}
	const missing = "There is no such similar-code evidence in this case, or its code is no longer kept."
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	sg, a, b, err := d.Integrity.CasePair(r.Context(), cs, index)
	if err != nil {
		d.integrityError(w, r, err, missing)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"a": a, "b": b, "signal": sg, "server_now": d.serverNow()})
}

// decideIntegrityCase changes a case's state with a reason: sharing it
// with the students (awaiting_student), reopening, clearing or actioning.
// seen_explanations names, per student id, the created_at of the latest
// explanation version the reviewer read; a newer one answers 409.
func (d Deps) decideIntegrityCase(w http.ResponseWriter, r *http.Request) {
	cs, ok := d.reviewedCase(w, r)
	if !ok {
		return
	}
	var req struct {
		State            string          `json:"state"`
		Note             string          `json:"note"`
		SeenExplanations map[int64]int64 `json:"seen_explanations"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	out, err := d.Integrity.Decide(r.Context(), cs.ID, u.ID, req.State, req.Note, req.SeenExplanations)
	if err != nil {
		d.integrityError(w, r, err, "There is no such case.")
		return
	}
	d.Log.Info("integrity case decided", "by", u.ID, "contest_id", cs.ContestID, "case_id", cs.ID, "from", cs.State, "state", out.State)
	d.writeCase(w, r, out, http.StatusOK)
}

// contestRef names a contest on the student's case page.
type contestRef struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// myIntegrityCases lists the cases shared with the signed-in user, with
// the contests and problems they are about.
func (d Deps) myIntegrityCases(w http.ResponseWriter, r *http.Request) {
	list, err := d.Integrity.MyCases(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	cs := map[int64]contestRef{}
	probs := map[int64]problemRef{}
	for _, sc := range list {
		if _, done := cs[sc.ContestID]; done {
			continue
		}
		c, err := d.Contests.Get(r.Context(), sc.ContestID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		cs[c.ID] = contestRef{Slug: c.Slug, Title: c.Title}
		for id, p := range problemRefs(c) {
			probs[id] = p
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"cases": list, "contests": cs, "problems": probs, "server_now": d.serverNow()})
}

// explainIntegrityCase saves the student's explanation, editable until
// the case is decided.
func (d Deps) explainIntegrityCase(w http.ResponseWriter, r *http.Request) {
	const missing = "There is no such case shared with you, or it has been decided."
	id, ok := parseID(r.PathValue("case"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return
	}
	var req struct {
		Explanation string `json:"explanation"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	if err := d.Integrity.Explain(r.Context(), id, u.ID, req.Explanation); err != nil {
		d.integrityError(w, r, err, missing)
		return
	}
	d.Log.Info("integrity explanation saved", "by", u.ID, "case_id", id)
	w.WriteHeader(http.StatusNoContent)
}
