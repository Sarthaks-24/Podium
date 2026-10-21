package httpapi

// The organisers' students pages (v2 plan B4.5, B4.6, B4.9; contracts C9
// and C12): the students table, a student's page with the class median,
// the record page with Standing, the rating and its history (logged on
// every read, D97), the rating edit under the two-person rule, and the
// class view of a final contest (totals only, no names). Owners and
// admins only, except the class view, which follows the stats route.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/integrity"
)

func (d Deps) studentsRoutes(mux *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.HandlerFunc { return d.requireRole(auth.Admin, h) }
	mux.HandleFunc("GET /api/admin/students", admin(d.limitReads(d.listStudents)))
	mux.HandleFunc("GET /api/admin/students/{id}", admin(d.limitReads(d.studentPage)))
	mux.HandleFunc("GET /api/admin/students/{id}/record", admin(d.limitReads(d.studentRecord)))
	mux.HandleFunc("PUT /api/admin/students/{id}/rating", admin(d.editRating))
	mux.HandleFunc("POST /api/admin/students/{id}/rating/proposals/{pid}/confirm", admin(d.confirmRating))
	mux.HandleFunc("POST /api/admin/students/{id}/rating/proposals/{pid}/decline", admin(d.declineRating))
	mux.HandleFunc("GET /api/admin/contests/{id}/class", d.requireRole(auth.Student, d.limitReads(d.classView)))
	mux.HandleFunc("GET /api/me/progress", d.requireRole(auth.Student, d.limitReads(d.myProgress)))
	mux.HandleFunc("GET /api/admin/users/{id}/progress", admin(d.limitReads(d.userProgress)))
	mux.HandleFunc("GET /api/me/notifications", d.requireRole(auth.Student, d.limitReads(d.myNotifications)))
	mux.HandleFunc("POST /api/me/notifications/read", d.requireRole(auth.Student, d.readNotifications))
}

// studentRef is a person on the students pages.
type studentRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Disabled bool   `json:"disabled,omitempty"`
}

// student loads the account named by {id}; erased accounts and unknown
// ids are 404.
func (d Deps) student(w http.ResponseWriter, r *http.Request) (*studentRef, bool) {
	id, ok := parseID(r.PathValue("id"))
	var s studentRef
	if ok {
		err := d.DB.QueryRowContext(r.Context(), `SELECT id, coalesce(nullif(name, ''), email), email, disabled FROM users WHERE id = ?
			AND email NOT LIKE '%@removed.invalid'`, id).Scan(&s.ID, &s.Name, &s.Email, &s.Disabled)
		ok = err == nil
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "not_found", "There is no such student.")
		return nil, false
	}
	return &s, true
}

// gradedFinal lists the graded contests whose summaries exist, newest
// first: the contests the students table and its filters read.
func (d Deps) gradedFinal(ctx context.Context) ([]*contests.Contest, error) {
	rows, err := d.DB.QueryContext(ctx, "SELECT id FROM contests WHERE graded = 1 AND summarised_at IS NOT NULL ORDER BY ends_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := []*contests.Contest{}
	for _, id := range ids {
		c, err := d.Contests.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// StudentRow is one line of the students table.
type StudentRow struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Email             string   `json:"email"`
	GradedContests    int      `json:"graded_contests"`
	LastGradedPercent *float64 `json:"last_graded_percent"`
	LastGradedSlug    *string  `json:"last_graded_slug"`
	LastSubmissionAt  *int64   `json:"last_submission_at"`
	RatingTenths      int      `json:"rating_tenths"`
}

func (d Deps) listStudents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	filter := r.URL.Query().Get("filter")
	if filter != "" && filter != "missed_last_two" && filter != "no_accepted" {
		WriteError(w, http.StatusBadRequest, "bad_request", "Use the filter missed_last_two or no_accepted, or none.")
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	graded, err := d.gradedFinal(ctx)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	// One summary per student and graded contest: who took part, and
	// their percent.
	type took struct {
		percent     float64
		submissions int
		accepted    int
	}
	// The columns alone, in one query: the problems JSON is never decoded
	// for this list (council C1-043).
	in := map[int64]map[int64]took{} // contest, user
	for _, c := range graded {
		in[c.ID] = map[int64]took{}
	}
	srows, err := d.DB.QueryContext(ctx, `SELECT s.contest_id, s.user_id, s.percent, s.submissions, s.accepted
		FROM student_contest_summary s JOIN contests c ON c.id = s.contest_id WHERE c.graded = 1 AND c.summarised_at IS NOT NULL`)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	for srows.Next() {
		var cid, uid int64
		var t took
		if err := srows.Scan(&cid, &uid, &t.percent, &t.submissions, &t.accepted); err != nil {
			srows.Close()
			d.internalError(w, r, err)
			return
		}
		if m, ok := in[cid]; ok {
			m[uid] = t
		}
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		d.internalError(w, r, err)
		return
	}
	ratings, err := d.Integrity.Ratings(ctx)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	last := map[int64]int64{}
	rows, err := d.DB.QueryContext(ctx, "SELECT user_id, max(created_at) FROM submissions WHERE kind = 'submit' GROUP BY user_id")
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	for rows.Next() {
		var u, at int64
		rows.Scan(&u, &at)
		last[u] = at
	}
	rows.Close()
	// Who could enter a roster contest.
	roster := map[int64]map[string]bool{}
	for _, c := range graded[:min(len(graded), 2)] {
		if c.Access != "roster" {
			continue
		}
		roster[c.ID] = map[string]bool{}
		rr, err := d.DB.QueryContext(ctx, "SELECT lower(email) FROM contest_roster WHERE contest_id = ?", c.ID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		for rr.Next() {
			var e string
			rr.Scan(&e)
			roster[c.ID][e] = true
		}
		rr.Close()
	}
	urows, err := d.DB.QueryContext(ctx, `SELECT id, coalesce(nullif(name, ''), email), email FROM users WHERE role = 'student'
		AND email NOT LIKE '%@removed.invalid' ORDER BY lower(coalesce(nullif(name, ''), email)), id`)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := []StudentRow{}
	for urows.Next() {
		var s StudentRow
		if err := urows.Scan(&s.ID, &s.Name, &s.Email); err != nil {
			urows.Close()
			d.internalError(w, r, err)
			return
		}
		if q != "" && !strings.Contains(strings.ToLower(s.Name), q) && !strings.Contains(strings.ToLower(s.Email), q) {
			continue
		}
		s.RatingTenths = integrity.RatingStart
		if v, ok := ratings[s.ID]; ok {
			s.RatingTenths = v
		}
		if at, ok := last[s.ID]; ok {
			s.LastSubmissionAt = &at
		}
		for _, c := range graded {
			if t, ok := in[c.ID][s.ID]; ok {
				s.GradedContests++
				if s.LastGradedPercent == nil {
					p, slug := t.percent, c.Slug
					s.LastGradedPercent, s.LastGradedSlug = &p, &slug
				}
			}
		}
		switch filter {
		case "missed_last_two":
			if len(graded) < 2 {
				continue
			}
			missed := true
			for _, c := range graded[:2] {
				eligible := c.Access != "roster" || roster[c.ID][strings.ToLower(s.Email)]
				if _, took := in[c.ID][s.ID]; took || !eligible {
					missed = false
				}
			}
			if !missed {
				continue
			}
		case "no_accepted":
			if len(graded) == 0 {
				continue
			}
			t, ok := in[graded[0].ID][s.ID]
			if !ok || t.submissions < 10 || t.accepted > 0 {
				continue
			}
		}
		out = append(out, s)
	}
	urows.Close()
	type ref struct {
		ContestID int64  `json:"contest_id"`
		Slug      string `json:"slug"`
		Title     string `json:"title"`
	}
	used := []ref{}
	for _, c := range graded[:min(len(graded), 2)] {
		used = append(used, ref{c.ID, c.Slug, c.Title})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"students": out, "filter": filter, "last_graded": used, "server_now": d.serverNow()})
}

// StudentContest is one contest on a student's page.
type StudentContest struct {
	ContestID          int64   `json:"contest_id"`
	Slug               string  `json:"slug"`
	Title              string  `json:"title"`
	Graded             bool    `json:"graded"`
	EndedAt            int64   `json:"ended_at"`
	Score              int     `json:"score"`
	MaxScore           int     `json:"max_score"`
	Percent            float64 `json:"percent"`
	ClassMedianPercent float64 `json:"class_median_percent"`
	Rank               int     `json:"rank"`
	RankedCount        int     `json:"ranked_count"`
	ProblemsAttempted  int     `json:"problems_attempted"`
	ProblemsFull       int     `json:"problems_full"`
	Submissions        int     `json:"submissions"`
	Accepted           int     `json:"accepted"`
}

// studentPage is a student's results per contest with the class median.
// No integrity data on this page.
func (d Deps) studentPage(w http.ResponseWriter, r *http.Request) {
	st, ok := d.student(w, r)
	if !ok {
		return
	}
	sums, err := d.Contests.Summaries(r.Context(), st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	out := []StudentContest{}
	for _, sm := range sums {
		c, err := d.Contests.Get(r.Context(), sm.ContestID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		median, err := d.Contests.ClassMedian(r.Context(), c.ID)
		if err != nil {
			d.internalError(w, r, err)
			return
		}
		out = append(out, StudentContest{ContestID: c.ID, Slug: c.Slug, Title: c.Title, Graded: sm.Graded, EndedAt: c.Final().UnixMilli(),
			Score: sm.Score, MaxScore: sm.MaxScore, Percent: sm.Percent, ClassMedianPercent: median, Rank: sm.Rank,
			RankedCount: sm.RankedCount, ProblemsAttempted: sm.ProblemsAttempted, ProblemsFull: sm.ProblemsFull, Submissions: sm.Submissions,
			Accepted: sm.Accepted})
	}
	st.Disabled = false
	WriteJSON(w, http.StatusOK, map[string]any{"student": st, "contests": out, "server_now": d.serverNow()})
}

// studentRecord is the record page (C12): Standing, the rating with its
// history, a waiting rating change, cases, notes and appeals. Every read
// is logged.
func (d Deps) studentRecord(w http.ResponseWriter, r *http.Request) {
	st, ok := d.student(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	viewer := userFrom(ctx).ID
	if st.ID == viewer {
		WriteError(w, http.StatusForbidden, "forbidden", "Your own record is read by the other organisers, not by you.")
		return
	}
	if err := d.Integrity.LogView(ctx, viewer, st.ID, integrity.ViewRecord, 0); err != nil {
		d.internalError(w, r, err)
		return
	}
	standing, err := d.Integrity.StandingOf(ctx, st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	rating, err := d.Integrity.RatingOf(ctx, st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	pending, err := d.Integrity.Pending(ctx, st.ID, viewer)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	cases, err := d.Integrity.CasesAbout(ctx, st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	// A reviewer never sees a case they are a student of.
	mine, err := d.Integrity.CasesAbout(ctx, viewer)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	involved := map[int64]bool{}
	for _, m := range mine {
		involved[m.ID] = true
	}
	kept := []integrity.CaseSummary{}
	sharedContest := map[int64]bool{}
	for _, c := range cases {
		if !involved[c.ID] {
			kept = append(kept, c)
		} else {
			sharedContest[c.ContestID] = true
		}
	}
	// Nor the lines and appeals of such a case.
	lines := []integrity.StandingLine{}
	for _, l := range standing.Lines {
		if l.CaseID == nil || !involved[*l.CaseID] {
			lines = append(lines, l)
		}
	}
	standing.Lines = lines
	// Nor the rating lines of such a case (they name its reviewers), nor
	// the notes on the student's submissions in a contest where the two
	// share a case.
	history := []integrity.RatingLine{}
	for _, l := range rating.History {
		if l.CaseID == nil || !involved[*l.CaseID] {
			history = append(history, l)
		}
	}
	rating.History = history
	allNotes, err := d.Integrity.NotesAbout(ctx, st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	notes := []integrity.Note{}
	for _, n := range allNotes {
		if !sharedContest[n.ContestID] {
			notes = append(notes, n)
		}
	}
	all, err := d.Integrity.AppealsOf(ctx, st.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	appeals := []integrity.Appeal{}
	for _, a := range all {
		if a.CaseID == nil || !involved[*a.CaseID] {
			appeals = append(appeals, a)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"student": st, "standing": standing, "rating": rating, "pending_rating": pending,
		"cases": kept, "notes": notes, "appeals": appeals, "server_now": d.serverNow()})
}

// ratingOut answers a rating write with the rating and any waiting change.
func (d Deps) ratingOut(w http.ResponseWriter, r *http.Request, studentID int64, status int) {
	rating, err := d.Integrity.RatingOf(r.Context(), studentID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	pending, err := d.Integrity.Pending(r.Context(), studentID, userFrom(r.Context()).ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, status, map[string]any{"rating": rating, "pending_rating": pending, "server_now": d.serverNow()})
}

func (d Deps) editRating(w http.ResponseWriter, r *http.Request) {
	st, ok := d.student(w, r)
	if !ok {
		return
	}
	var req struct {
		ValueTenths *int   `json:"value_tenths"`
		Reason      string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ValueTenths == nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "Give a rating from 0 to 10, in steps of 0.1.")
		return
	}
	u := userFrom(r.Context())
	pending, err := d.Integrity.EditRating(r.Context(), st.ID, u.ID, *req.ValueTenths, req.Reason)
	if err != nil {
		d.integrityError(w, r, err, "There is no such student.")
		return
	}
	d.Log.Info("rating edited", "by", u.ID, "user_id", st.ID, "pending", pending)
	status := http.StatusOK
	if pending {
		status = http.StatusAccepted
	}
	d.ratingOut(w, r, st.ID, status)
}

func (d Deps) confirmRating(w http.ResponseWriter, r *http.Request) {
	st, ok := d.student(w, r)
	if !ok {
		return
	}
	pid, _ := parseID(r.PathValue("pid"))
	var req struct {
		SoleOrganiserReason string `json:"sole_organiser_reason"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	if err := d.Integrity.ConfirmRating(r.Context(), st.ID, pid, u.ID, req.SoleOrganiserReason); err != nil {
		d.integrityError(w, r, err, "There is no such change waiting.")
		return
	}
	d.Log.Info("rating change confirmed", "by", u.ID, "user_id", st.ID, "proposal_id", pid)
	d.ratingOut(w, r, st.ID, http.StatusOK)
}

func (d Deps) declineRating(w http.ResponseWriter, r *http.Request) {
	st, ok := d.student(w, r)
	if !ok {
		return
	}
	pid, _ := parseID(r.PathValue("pid"))
	if err := d.Integrity.DeclineRating(r.Context(), st.ID, pid, userFrom(r.Context()).ID); err != nil {
		d.integrityError(w, r, err, "There is no such change waiting.")
		return
	}
	d.ratingOut(w, r, st.ID, http.StatusOK)
}

// ClassProblem is one problem of the class view: totals only.
type ClassProblem struct {
	Label          string          `json:"label"`
	Title          string          `json:"title"`
	MaxScore       int             `json:"max_score"`
	Attempted      int             `json:"attempted"`
	Full           int             `json:"full"`
	MedianScore    float64         `json:"median_score"`
	Subtasks       []ClassSubtask  `json:"subtasks,omitempty"`
	MostFailedTest *MostFailedTest `json:"most_failed_test"`
}

// ClassSubtask is a subtask's solve rate.
type ClassSubtask struct {
	Idx       int     `json:"idx"`
	Points    int     `json:"points"`
	Solved    int     `json:"solved"`
	SolveRate float64 `json:"solve_rate"`
}

// MostFailedTest is the test that ended the most best submissions.
type MostFailedTest struct {
	SubtaskIdx int `json:"subtask_idx"`
	Test       int `json:"test"`
	Failed     int `json:"failed"`
}

// classView is a final contest's class view: solve rate per subtask and
// the test that failed most, per problem, without names or user ids.
func (d Deps) classView(w http.ResponseWriter, r *http.Request) {
	c, ok := d.organisedContest(w, r)
	if !ok || d.resultsHidden(w, r, c) {
		return
	}
	if c.SummarisedAt.IsZero() {
		WriteError(w, http.StatusConflict, "not_final", "The class view opens when the results are final.")
		return
	}
	sums, err := d.Contests.ContestSummaries(r.Context(), c.ID)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	exam := c.Feedback == "exam"
	n := len(sums)
	out := []ClassProblem{}
	for i, p := range c.Problems {
		cp := ClassProblem{Label: p.Label, Title: p.Title, MaxScore: p.MaxScore()}
		var scores []float64
		solved := make([]int, len(p.Points))
		for _, sm := range sums {
			if i >= len(sm.Problems) {
				continue
			}
			sp := sm.Problems[i]
			if sp.Attempts > 0 {
				cp.Attempted++
			}
			if sp.MaxScore > 0 && sp.Score >= sp.MaxScore {
				cp.Full++
			}
			scores = append(scores, float64(sp.Score))
			for j, st := range sp.Subtasks {
				if j < len(solved) && st.Points > 0 && st.Earned >= st.Points {
					solved[j]++
				}
			}
		}
		cp.MedianScore = median(scores)
		if !exam {
			for j, pts := range p.Points {
				rate := 0.0
				if n > 0 {
					rate = contests.Percent(solved[j], n)
				}
				cp.Subtasks = append(cp.Subtasks, ClassSubtask{Idx: j, Points: pts, Solved: solved[j], SolveRate: rate})
			}
			if cp.MostFailedTest, err = d.mostFailed(r.Context(), c.ID, p.ProblemID); err != nil {
				d.internalError(w, r, err)
				return
			}
		}
		out = append(out, cp)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"contest_id": c.ID, "participants": n, "problems": out, "server_now": d.serverNow()})
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

// mostFailed reads each participant's best counting submission of the
// problem (highest score, earliest) and counts the first failing test of
// each; the most common one is returned, as its place in its subtask.
func (d Deps) mostFailed(ctx context.Context, contestID, problemID int64) (*MostFailedTest, error) {
	rows, err := d.DB.QueryContext(ctx, `SELECT result FROM (SELECT result, row_number() OVER (PARTITION BY user_id ORDER BY score DESC, id) AS n
		FROM submissions WHERE contest_id = ? AND problem_id = ? AND kind = 'submit' AND practice = 0 AND state = 'done') WHERE n = 1`,
		contestID, problemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct{ sub, test int }
	counts := map[key]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var res struct {
			Subtasks []struct {
				Idx   int `json:"idx"`
				Tests []struct {
					Verdict string `json:"verdict"`
				} `json:"tests"`
			} `json:"subtasks"`
		}
		if json.Unmarshal([]byte(raw), &res) != nil {
			continue
		}
	found:
		for si, st := range res.Subtasks {
			for ti, t := range st.Tests {
				if t.Verdict != "" && t.Verdict != "AC" {
					counts[key{si, ti + 1}]++
					break found
				}
			}
		}
	}
	var best *MostFailedTest
	for k, n := range counts {
		if best == nil || n > best.Failed || (n == best.Failed && (k.sub < best.SubtaskIdx || (k.sub == best.SubtaskIdx && k.test < best.Test))) {
			best = &MostFailedTest{SubtaskIdx: k.sub, Test: k.test, Failed: n}
		}
	}
	return best, rows.Err()
}

func (d Deps) myNotifications(w http.ResponseWriter, r *http.Request) {
	before, _ := parseID(r.URL.Query().Get("before"))
	p, err := d.notices().List(r.Context(), userFrom(r.Context()).ID, before)
	if err != nil {
		d.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"notifications": p.Notices, "unread": p.Unread, "held": p.Held, "has_more": p.HasMore,
		"server_now": d.serverNow()})
}

func (d Deps) readNotifications(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) > 50 {
		WriteError(w, http.StatusBadRequest, "bad_request", "Mark at most 50 notices at once, or all of them.")
		return
	}
	if err := d.notices().MarkRead(r.Context(), userFrom(r.Context()).ID, req.IDs, req.All); err != nil && !errors.Is(err, context.Canceled) {
		d.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
