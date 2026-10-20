package httpapi

// Manual review routes (v2 plan P4, contract C9): private notes and
// manual cases on a contest submission, outcome proposals and their
// confirmation under the two-person rule, appeals and rulings, the
// student's standing, and the owner's oversight and record view log.
// Reviewers are those who may review the submission's contest (see
// integrity.go); every read of notes or a case is logged (D97).

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Sarthaks-24/podium/internal/integrity"
)

// reviewedSubmission loads the contest submission named by {id} for a
// reviewer of its contest. Anyone who does not organise the contest gets
// 404; a co-organiser who may not review gets 403.
func (d Deps) reviewedSubmission(w http.ResponseWriter, r *http.Request) (*integrity.SubmissionRef, bool) {
	const missing = "There is no such contest submission."
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return nil, false
	}
	sub, err := d.Integrity.Submission(r.Context(), id)
	if errors.Is(err, integrity.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "not_found", missing)
		return nil, false
	}
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	u := userFrom(r.Context())
	may, err := d.mayReview(r.Context(), u, sub.ContestID)
	if err != nil {
		d.internalError(w, r, err)
		return nil, false
	}
	if !may {
		organiser, err := d.Contests.IsContestAdmin(r.Context(), sub.ContestID, u.ID)
		if err != nil {
			d.internalError(w, r, err)
			return nil, false
		}
		if organiser {
			WriteError(w, http.StatusForbidden, "forbidden", "Only owners and admins write notes and open cases.")
		} else {
			WriteError(w, http.StatusNotFound, "not_found", missing)
		}
		return nil, false
	}
	return sub, true
}

func (d Deps) listNotes(w http.ResponseWriter, r *http.Request) {
	sub, ok := d.reviewedSubmission(w, r)
	if !ok {
		return
	}
	if err := d.Integrity.LogView(r.Context(), userFrom(r.Context()).ID, sub.UserID, integrity.ViewNotes, sub.ID); err != nil {
		d.internalError(w, r, err)
		return
	}
	notes, err := d.Integrity.Notes(r.Context(), sub.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	caseID, err := d.Integrity.ManualCaseOf(r.Context(), sub.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	var cid *int64
	if caseID != 0 {
		cid = &caseID
	}
	WriteJSON(w, http.StatusOK, map[string]any{"submission": sub, "notes": notes, "case_id": cid, "server_now": d.serverNow()})
}

func (d Deps) addNote(w http.ResponseWriter, r *http.Request) {
	sub, ok := d.reviewedSubmission(w, r)
	if !ok {
		return
	}
	var req struct {
		Text  string                `json:"text"`
		Lines []integrity.LineRange `json:"lines"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	n, err := d.Integrity.AddNote(r.Context(), sub, u.ID, req.Text, req.Lines)
	if err != nil {
		d.integrityError(w, r, err, "")
		return
	}
	d.Log.Info("submission note written", "by", u.ID, "submission_id", sub.ID, "note_id", n.ID)
	WriteJSON(w, http.StatusCreated, n)
}

func (d Deps) openManualCase(w http.ResponseWriter, r *http.Request) {
	sub, ok := d.reviewedSubmission(w, r)
	if !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
		Note  string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	cs, err := d.Integrity.OpenManualCase(r.Context(), sub, u.ID, req.Title, req.Note)
	if err != nil {
		d.integrityError(w, r, err, "")
		return
	}
	d.Log.Info("integrity case opened", "by", u.ID, "contest_id", cs.ContestID, "case_id", cs.ID, "source", "manual")
	d.writeCase(w, r, cs, http.StatusCreated)
}

func (d Deps) proposeOutcome(w http.ResponseWriter, r *http.Request) {
	cs, ok := d.reviewedCase(w, r)
	if !ok {
		return
	}
	var req struct {
		Outcome          string          `json:"outcome"`
		ProblemLabel     string          `json:"problem_label"`
		UserIDs          []int64         `json:"user_ids"`
		Note             string          `json:"note"`
		Reason           string          `json:"reason"`
		SeenExplanations map[int64]int64 `json:"seen_explanations"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	out, err := d.Integrity.Propose(r.Context(), cs.ID, u.ID, integrity.ProposeInput{Outcome: req.Outcome, ProblemLabel: req.ProblemLabel,
		UserIDs: req.UserIDs, Note: req.Note, Reason: req.Reason, Seen: req.SeenExplanations})
	if err != nil {
		d.integrityError(w, r, err, "There is no such case.")
		return
	}
	d.Log.Info("integrity outcome proposed", "by", u.ID, "case_id", cs.ID, "outcome", req.Outcome)
	d.writeCase(w, r, out, http.StatusCreated)
}

func (d Deps) confirmOutcome(w http.ResponseWriter, r *http.Request) {
	cs, ok := d.reviewedCase(w, r)
	if !ok {
		return
	}
	pid, ok := parseID(r.PathValue("proposal"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such proposal.")
		return
	}
	var req struct {
		Note                string          `json:"note"`
		SeenExplanations    map[int64]int64 `json:"seen_explanations"`
		SoleOrganiserReason string          `json:"sole_organiser_reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	out, err := d.Integrity.Confirm(r.Context(), cs.ID, pid, u.ID, req.Note, req.SoleOrganiserReason, req.SeenExplanations)
	if err != nil {
		d.integrityError(w, r, err, "There is no such case.")
		return
	}
	d.Log.Info("integrity outcome confirmed", "by", u.ID, "case_id", cs.ID, "proposal_id", pid)
	d.writeCase(w, r, out, http.StatusOK)
}

// openedIntegrityCase records that the student opened a case shared with
// them: their 7 days start (C9 deadlines).
func (d Deps) openedIntegrityCase(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("case"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such case shared with you.")
		return
	}
	if err := d.Integrity.Opened(r.Context(), id, userFrom(r.Context()).ID); err != nil {
		d.integrityError(w, r, err, "There is no such case shared with you.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) fileAppeal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CaseID       int64  `json:"case_id"`
		AdjustmentID int64  `json:"adjustment_id"`
		Text         string `json:"text"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	a, err := d.Integrity.FileAppeal(r.Context(), u.ID, integrity.AppealInput{CaseID: req.CaseID, AdjustmentID: req.AdjustmentID, Text: req.Text})
	if err != nil {
		d.integrityError(w, r, err, "There is no such outcome or adjustment of yours to appeal.")
		return
	}
	d.Log.Info("appeal filed", "by", u.ID, "appeal_id", a.ID)
	WriteJSON(w, http.StatusCreated, a)
}

func (d Deps) listAppeals(w http.ResponseWriter, r *http.Request) {
	list, err := d.Integrity.Appeals(r.Context(), r.URL.Query().Get("state") == "open")
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	// A reviewer never sees here an appeal of a case they are a student
	// of, their own included.
	me := userFrom(r.Context()).ID
	mine, err := d.Integrity.CasesAbout(r.Context(), me)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	involved := map[int64]bool{}
	for _, c := range mine {
		involved[c.ID] = true
	}
	out := []integrity.Appeal{}
	for _, a := range list {
		if a.UserID != me && (a.CaseID == nil || !involved[*a.CaseID]) {
			out = append(out, a)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"appeals": out, "server_now": d.serverNow()})
}

func (d Deps) ruleAppeal(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such appeal.")
		return
	}
	var req struct {
		Upheld              bool   `json:"upheld"`
		Reason              string `json:"reason"`
		SoleOrganiserReason string `json:"sole_organiser_reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	a, err := d.Integrity.Rule(r.Context(), id, u.ID, req.Upheld, req.Reason, req.SoleOrganiserReason)
	if err != nil {
		d.integrityError(w, r, err, "There is no such appeal.")
		return
	}
	d.Log.Info("appeal ruled", "by", u.ID, "appeal_id", id, "upheld", req.Upheld)
	WriteJSON(w, http.StatusOK, a)
}

// myStanding is the signed-in person's standing: the band and the dated
// lines, never the rating's number.
func (d Deps) myStanding(w http.ResponseWriter, r *http.Request) {
	st, err := d.Integrity.StandingOf(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"band": st.Band, "band_name": st.BandName, "lines": st.Lines, "server_now": d.serverNow()})
}

func (d Deps) oversight(w http.ResponseWriter, r *http.Request) {
	list, err := d.Integrity.Oversight(r.Context())
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"reviewers": list, "server_now": d.serverNow()})
}

func (d Deps) recordViews(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	num := func(k string) int64 { n, _ := strconv.ParseInt(q.Get(k), 10, 64); return n }
	list, more, err := d.Integrity.RecordViews(r.Context(), num("subject"), num("viewer"), num("before"))
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"views": list, "has_more": more, "server_now": d.serverNow()})
}
