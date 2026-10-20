package integrity

// The rating and the Standing band (D110 as hardened by the final
// council, input 16): a number from 0 to 10, stored in tenths, that only
// owners and admins read, and a band derived from it that the student
// reads with the dated lines behind it. Nothing automatic moves the
// number: a confirmed outcome, a manual edit with a written reason, an
// upheld appeal, or the semester rollover the owner runs. Every write
// clamps to 0 to 100 and adds a rating_history row.
//
// Only the record page, the students table and the rating routes read
// this file's data. Standings, results, progress, exports and notices
// never do; a test checks that their code names nothing declared here.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/notifications"
	"github.com/Sarthaks-24/podium/internal/retention"
)

// Rating bounds and steps, in tenths.
const (
	RatingStart  = 100
	RatingMax    = 100
	RatingMin    = 0
	RolloverStep = 5
)

// StandingPeriod is how long an outcome or adjustment stays on a
// student's standing after it (the case retention, 2 years).
const StandingPeriod = retention.CaseEvidence

// Causes of a rating change.
const (
	CauseOutcome  = "outcome"
	CauseManual   = "manual"
	CauseAppeal   = "appeal"
	CauseRollover = "rollover"
)

// Bands, named by fact (D110).
const (
	BandGood     = "good"
	BandOnRecord = "on_record"
	BandRepeated = "repeated"
)

// BandNames are the words a student reads.
var BandNames = map[string]string{
	BandGood:     "Good standing",
	BandOnRecord: "Outcomes on record",
	BandRepeated: "Repeated outcomes",
}

// BandOf is the band of a rating in tenths: 90 to 100 good, 60 to 89 on
// record, below 60 repeated.
func BandOf(tenths int) string {
	switch {
	case tenths >= 90:
		return BandGood
	case tenths >= 60:
		return BandOnRecord
	}
	return BandRepeated
}

func bandRank(b string) int {
	switch b {
	case BandGood:
		return 2
	case BandOnRecord:
		return 1
	}
	return 0
}

func clampRating(v int) int { return min(max(v, RatingMin), RatingMax) }

// ratingCause is what a rating_history row records besides the change.
type ratingCause struct {
	cause                string
	at                   int64
	decisionID, appealID int64
	proposalID           int64
	actorID, confirmerID int64
	reason, label        string
}

func ratingTx(ctx context.Context, tx *sql.Tx, userID int64) (int, error) {
	v := RatingStart
	err := tx.QueryRowContext(ctx, "SELECT value_tenths FROM student_rating WHERE user_id = ?", userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return RatingStart, nil
	}
	return v, err
}

// applyRatingTx moves a student's rating by delta tenths inside the
// caller's write transaction (which the write gate serialises), clamped to
// 0 to 100, and records the change actually applied. It returns the
// history row and the new value.
func applyRatingTx(ctx context.Context, tx *sql.Tx, userID int64, delta int, rc ratingCause) (int64, int, error) {
	cur, err := ratingTx(ctx, tx, userID)
	if err != nil {
		return 0, 0, err
	}
	next := clampRating(cur + delta)
	if _, err := tx.ExecContext(ctx, `INSERT INTO student_rating (user_id, value_tenths, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET value_tenths = excluded.value_tenths, updated_at = excluded.updated_at`, userID, next, rc.at); err != nil {
		return 0, 0, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO rating_history (user_id, at, delta_tenths, new_value_tenths, cause, decision_id, appeal_id,
		proposal_id, actor_id, confirmer_id, reason, semester_label) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		userID, rc.at, next-cur, next, rc.cause, nullInt(rc.decisionID), nullInt(rc.appealID), nullInt(rc.proposalID), nullInt(rc.actorID),
		nullInt(rc.confirmerID), strings.TrimSpace(rc.reason), rc.label).Scan(&id)
	return id, next, err
}

// Rating returns a student's rating in tenths (RatingStart when never
// moved).
func (s *Service) Rating(ctx context.Context, userID int64) (int, error) {
	v := RatingStart
	err := s.DB.QueryRowContext(ctx, "SELECT value_tenths FROM student_rating WHERE user_id = ?", userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return RatingStart, nil
	}
	return v, err
}

// Ratings returns every stored rating, by user; a user missing from the
// map is at RatingStart.
func (s *Service) Ratings(ctx context.Context) (map[int64]int, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT user_id, value_tenths FROM student_rating")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var u int64
		var v int
		if err := rows.Scan(&u, &v); err != nil {
			return nil, err
		}
		out[u] = v
	}
	return out, rows.Err()
}

// Named is a person on the record page.
type Named struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// RatingLine is one rating_history row as the record page shows it.
type RatingLine struct {
	ID            int64  `json:"id"`
	At            int64  `json:"at"`
	DeltaTenths   int    `json:"delta_tenths"`
	ValueTenths   int    `json:"value_tenths"`
	Cause         string `json:"cause"`
	Reason        string `json:"reason"`
	CaseID        *int64 `json:"case_id"`
	DecisionID    *int64 `json:"decision_id"`
	AppealID      *int64 `json:"appeal_id"`
	Actor         *Named `json:"actor"`
	Confirmer     *Named `json:"confirmer"`
	SemesterLabel string `json:"semester_label"`
	BandBefore    string `json:"band_before"`
	BandAfter     string `json:"band_after"`
}

// RatingView is the rating block of the record page (C12).
type RatingView struct {
	ValueTenths int          `json:"value_tenths"`
	Band        string       `json:"band"`
	History     []RatingLine `json:"history"`
}

// RatingOf returns a student's rating with its history, newest first.
func (s *Service) RatingOf(ctx context.Context, userID int64) (*RatingView, error) {
	v, err := s.Rating(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &RatingView{ValueTenths: v, Band: BandOf(v), History: []RatingLine{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT h.id, h.at, h.delta_tenths, h.new_value_tenths, h.cause, h.reason, d.case_id, h.decision_id,
		h.appeal_id, h.actor_id, coalesce(nullif(a.name, ''), a.email, ''), h.confirmer_id, coalesce(nullif(cf.name, ''), cf.email, ''),
		h.semester_label FROM rating_history h LEFT JOIN integrity_decisions d ON d.id = h.decision_id
		LEFT JOIN users a ON a.id = h.actor_id LEFT JOIN users cf ON cf.id = h.confirmer_id
		WHERE h.user_id = ? ORDER BY h.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l RatingLine
		var caseID, decisionID, appealID, actor, confirmer sql.NullInt64
		var actorName, confirmerName string
		if err := rows.Scan(&l.ID, &l.At, &l.DeltaTenths, &l.ValueTenths, &l.Cause, &l.Reason, &caseID, &decisionID, &appealID,
			&actor, &actorName, &confirmer, &confirmerName, &l.SemesterLabel); err != nil {
			return nil, err
		}
		l.CaseID, l.DecisionID, l.AppealID = ptr(caseID), ptr(decisionID), ptr(appealID)
		if actor.Valid {
			l.Actor = &Named{actor.Int64, actorName}
		}
		if confirmer.Valid {
			l.Confirmer = &Named{confirmer.Int64, confirmerName}
		}
		l.BandBefore, l.BandAfter = BandOf(l.ValueTenths-l.DeltaTenths), BandOf(l.ValueTenths)
		out.History = append(out.History, l)
	}
	return out, rows.Err()
}

// PendingRating is a band-lowering edit waiting for a second organiser.
type PendingRating struct {
	ID         int64  `json:"id"`
	FromTenths int    `json:"from_tenths"`
	ToTenths   int    `json:"to_tenths"`
	Reason     string `json:"reason"`
	By         Named  `json:"by"`
	CreatedAt  int64  `json:"created_at"`
	MayConfirm bool   `json:"may_confirm"`
}

// Pending returns the student's waiting proposal, or nil. viewer sets
// MayConfirm.
func (s *Service) Pending(ctx context.Context, userID, viewer int64) (*PendingRating, error) {
	var p PendingRating
	err := s.DB.QueryRowContext(ctx, `SELECT p.id, p.from_tenths, p.to_tenths, p.reason, p.by_user, coalesce(nullif(u.name, ''), u.email),
		p.created_at FROM rating_proposals p JOIN users u ON u.id = p.by_user
		WHERE p.user_id = ? AND NOT EXISTS (SELECT 1 FROM rating_proposal_closures c WHERE c.proposal_id = p.id)
		ORDER BY p.id DESC LIMIT 1`, userID).Scan(&p.ID, &p.FromTenths, &p.ToTenths, &p.Reason, &p.By.ID, &p.By.Name, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.MayConfirm = p.By.ID != viewer
	if !p.MayConfirm {
		if p.MayConfirm, err = s.SoleOrganiser(ctx, viewer); err != nil {
			return nil, err
		}
	}
	return &p, nil
}

// ErrRatingChanged refuses confirming a proposal after the rating moved.
var ErrRatingChanged = errors.New("the rating changed since this was proposed")

// closePendingTx closes every waiting proposal of the student with state.
func closePendingTx(ctx context.Context, tx *sql.Tx, userID, by int64, state string, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO rating_proposal_closures (proposal_id, state, by_user, at)
		SELECT id, ?, ?, ? FROM rating_proposals p WHERE p.user_id = ?
		AND NOT EXISTS (SELECT 1 FROM rating_proposal_closures c WHERE c.proposal_id = p.id)`, state, nullInt(by), now, userID)
	return err
}

func checkRatingReason(reason string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(reason)); n < minNote || utf8.RuneCountInString(reason) > 1000 {
		return &InputError{"Write the reason the student will read: at least 20 characters (at most 1,000)."}
	}
	return nil
}

// EditRating sets a student's rating by hand (owners and admins; the
// route checks the role). An edit that lowers the band becomes a proposal
// for a second owner or admin (pending true); any other edit applies at
// once. Either replaces a waiting proposal. An applied edit writes a line
// on the student's standing and sends standing_adjusted.
func (s *Service) EditRating(ctx context.Context, userID, by int64, to int, reason string) (pending bool, err error) {
	if to < RatingMin || to > RatingMax {
		return false, &InputError{"Give a rating from 0 to 10, in steps of 0.1."}
	}
	if err := checkRatingReason(reason); err != nil {
		return false, err
	}
	if userID == by {
		return false, &InputError{"Nobody edits their own rating."}
	}
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE id = ?", userID).Scan(&exists); err != nil {
		return false, err
	}
	if exists == 0 {
		return false, ErrNotFound
	}
	cur, err := ratingTx(ctx, tx, userID)
	if err != nil {
		return false, err
	}
	if to == cur {
		return false, &InputError{"That is the rating already."}
	}
	if err := closePendingTx(ctx, tx, userID, by, "superseded", now); err != nil {
		return false, err
	}
	if bandRank(BandOf(to)) < bandRank(BandOf(cur)) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO rating_proposals (user_id, from_tenths, to_tenths, reason, by_user, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, userID, cur, to, strings.TrimSpace(reason), by, now); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if err := manualTx(ctx, tx, userID, to-cur, ratingCause{cause: CauseManual, at: now, actorID: by, reason: reason}); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

// manualTx applies a manual change and tells the student.
func manualTx(ctx context.Context, tx *sql.Tx, userID int64, delta int, rc ratingCause) error {
	id, _, err := applyRatingTx(ctx, tx, userID, delta, rc)
	if err != nil {
		return err
	}
	return notifications.InsertTx(ctx, tx, notifications.Row{UserID: userID, Kind: notifications.StandingAdjusted, RatingHistoryID: id,
		CreatedAt: rc.at})
}

// ConfirmRating applies a waiting proposal. The confirmer is another owner
// or admin, or the sole organiser with soleReason (recorded in the
// history's reason after the proposer's). A rating that moved since the
// proposal is ErrRatingChanged.
func (s *Service) ConfirmRating(ctx context.Context, userID, proposalID, by int64, soleReason string) error {
	p, err := s.Pending(ctx, userID, by)
	if err != nil {
		return err
	}
	if p == nil || p.ID != proposalID {
		return &InputError{"That change is no longer waiting for confirmation. Reload the page."}
	}
	reason := p.Reason
	if p.By.ID == by {
		sole, err := s.SoleOrganiser(ctx, by)
		if err != nil {
			return err
		}
		if !sole {
			return ErrTwoPersonRule
		}
		if err := checkSoleReason(soleReason); err != nil {
			return err
		}
		reason += " (Confirmed alone as the only organiser: " + strings.TrimSpace(soleReason) + ")"
	}
	if userID == by {
		return &InputError{"Nobody confirms a change to their own rating."}
	}
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cur, err := ratingTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if cur != p.FromTenths {
		return ErrRatingChanged
	}
	res, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO rating_proposal_closures (proposal_id, state, by_user, at) VALUES (?, 'confirmed', ?, ?)",
		proposalID, by, now)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &InputError{"That change is no longer waiting for confirmation. Reload the page."}
	}
	if err := manualTx(ctx, tx, userID, p.ToTenths-cur, ratingCause{cause: CauseManual, at: now, actorID: p.By.ID, confirmerID: by,
		proposalID: proposalID, reason: reason}); err != nil {
		return err
	}
	return tx.Commit()
}

// DeclineRating closes a waiting proposal without applying it.
func (s *Service) DeclineRating(ctx context.Context, userID, proposalID, by int64) error {
	if userID == by {
		return &InputError{"Nobody declines a change to their own rating."}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO rating_proposal_closures (proposal_id, state, by_user, at)
		SELECT id, 'declined', ?, ? FROM rating_proposals WHERE id = ? AND user_id = ?`, by, s.now().UnixMilli(), proposalID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &InputError{"That change is no longer waiting for confirmation. Reload the page."}
	}
	return nil
}

// RolloverReport is what a semester rollover did, or would do.
type RolloverReport struct {
	Label       string
	From, To    time.Time
	Students    int // given the step (some already at 10 gain nothing)
	Blocked     int // had a confirmed outcome in the period
	AlreadyDone bool
	Applied     bool
}

// Rollover runs the semester rollover of the rating for label: every
// student (site role student, not erased) with no confirmed outcome since
// the previous rollover gets RolloverStep, up to the maximum, with cause
// rollover and the label. A label runs once; a second run reports
// AlreadyDone and changes nothing. With apply false nothing is written.
func (s *Service) Rollover(ctx context.Context, label string, apply bool) (RolloverReport, error) {
	label = strings.TrimSpace(label)
	r := RolloverReport{Label: label}
	if label == "" || utf8.RuneCountInString(label) > 40 {
		return r, &InputError{"Give the semester a label of 1 to 40 characters, like 2026-odd."}
	}
	now := s.now()
	r.To = now
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	var done int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM rating_rollovers WHERE label = ?", label).Scan(&done); err != nil {
		return r, err
	}
	if done > 0 {
		r.AlreadyDone = true
		return r, nil
	}
	var from int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(to_at), 0) FROM rating_rollovers").Scan(&from); err != nil {
		return r, err
	}
	r.From = time.UnixMilli(from)
	// Every student; liveOutcome is asked per student below.
	urows, err := tx.QueryContext(ctx, `SELECT id FROM users WHERE role = 'student' AND email NOT LIKE '%@removed.invalid' ORDER BY id`)
	if err != nil {
		return r, err
	}
	var ids []int64
	for urows.Next() {
		var id int64
		if err := urows.Scan(&id); err != nil {
			urows.Close()
			return r, err
		}
		ids = append(ids, id)
	}
	urows.Close()
	at := now.UnixMilli()
	for _, u := range ids {
		var blocked bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM integrity_decisions d WHERE d.created_at > ? AND d.created_at <= ? AND `+
			liveOutcome+`)`, from, at, u, u).Scan(&blocked); err != nil {
			return r, err
		}
		if blocked {
			r.Blocked++
			continue
		}
		r.Students++
		if _, _, err := applyRatingTx(ctx, tx, u, RolloverStep, ratingCause{cause: CauseRollover, at: at, label: label,
			reason: "Semester " + label + " ended with no outcome."}); err != nil {
			return r, err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO rating_rollovers (label, from_at, to_at, ran_at, students) VALUES (?, ?, ?, ?, ?)",
		label, from, at, at, r.Students); err != nil {
		return r, err
	}
	if !apply {
		return r, nil
	}
	r.Applied = true
	return r, tx.Commit()
}

// rolloverAfterUpheld re-evaluates the rollovers an overturned outcome
// blocked: for each rollover whose period holds the outcome, a student
// who now has no other confirmed outcome in it and was not given that
// rollover gets it now.
func rolloverAfterUpheld(ctx context.Context, tx *sql.Tx, userID, decidedAt, appealID, now int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT label, from_at, to_at FROM rating_rollovers WHERE from_at < ? AND to_at >= ?", decidedAt, decidedAt)
	if err != nil {
		return err
	}
	type period struct {
		label    string
		from, to int64
	}
	var list []period
	for rows.Next() {
		var p period
		if err := rows.Scan(&p.label, &p.from, &p.to); err != nil {
			rows.Close()
			return err
		}
		list = append(list, p)
	}
	rows.Close()
	for _, p := range list {
		var blocked, given bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM integrity_decisions d WHERE d.created_at > ? AND d.created_at <= ? AND `+
			liveOutcome+`), EXISTS (SELECT 1 FROM rating_history WHERE user_id = ? AND cause = 'rollover' AND semester_label = ?)`,
			p.from, p.to, userID, userID, userID, p.label).Scan(&blocked, &given); err != nil {
			return err
		}
		if blocked || given {
			continue
		}
		if _, _, err := applyRatingTx(ctx, tx, userID, RolloverStep, ratingCause{cause: CauseRollover, at: now, label: p.label, appealID: appealID,
			reason: "Semester " + p.label + " rollover, applied after an upheld appeal."}); err != nil {
			return err
		}
	}
	return nil
}

// StandingLine is one dated line of a student's standing.
type StandingLine struct {
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	At           int64         `json:"at"`
	Text         string        `json:"text"`
	Reason       string        `json:"reason"`
	CaseID       *int64        `json:"case_id"`
	AdjustmentID *int64        `json:"adjustment_id"`
	Appeal       *AppealStatus `json:"appeal"`
	AppealBy     int64         `json:"appeal_by"`
	CanAppeal    bool          `json:"can_appeal"`
}

// AppealStatus is how a standing line shows its appeal.
type AppealStatus struct {
	State     string `json:"state"`
	CreatedAt int64  `json:"created_at"`
}

// Standing is what a student reads about their own record: the band's
// name and the dated lines, never the number.
type Standing struct {
	Band     string         `json:"band"`
	BandName string         `json:"band_name"`
	Lines    []StandingLine `json:"lines"`
}

// outcomeWords phrase an outcome on a standing line.
func outcomeWords(kind, label string) string {
	switch kind {
	case OutcomeWarning:
		return "warning"
	case OutcomeZero:
		if label != "" {
			return "zero for problem " + label
		}
		return "zero for one problem"
	}
	return "referred to the academic integrity process"
}

// StandingOf returns a student's standing: the band from their rating
// and, newest first, every confirmed outcome no appeal overturned and
// every manual adjustment no appeal reversed, for StandingPeriod after
// each.
func (s *Service) StandingOf(ctx context.Context, userID int64) (*Standing, error) {
	v, err := s.Rating(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now().UnixMilli()
	since := now - StandingPeriod.Milliseconds()
	st := &Standing{Band: BandOf(v), BandName: BandNames[BandOf(v)], Lines: []StandingLine{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id, d.case_id, d.created_at, d.outcome, coalesce(cp.label, ''), ct.title, coalesce(p.reason, '')
		FROM integrity_decisions d JOIN integrity_cases c ON c.id = d.case_id JOIN contests ct ON ct.id = c.contest_id
		LEFT JOIN contest_problems cp ON cp.contest_id = c.contest_id AND cp.problem_id = d.outcome_problem_id
		LEFT JOIN integrity_proposals p ON p.id = d.proposal_id
		WHERE d.created_at >= ? AND `+liveOutcome+` ORDER BY d.created_at DESC, d.id DESC`, since, userID, userID)
	if err != nil {
		return nil, err
	}
	type outcome struct {
		decisionID, caseID, at int64
		kind, label, contest   string
		reason                 string
	}
	var outs []outcome
	for rows.Next() {
		var o outcome
		if err := rows.Scan(&o.decisionID, &o.caseID, &o.at, &o.kind, &o.label, &o.contest, &o.reason); err != nil {
			rows.Close()
			return nil, err
		}
		outs = append(outs, o)
	}
	rows.Close()
	for _, o := range outs {
		caseID := o.caseID
		l := StandingLine{ID: fmt.Sprintf("outcome-%d-%d", o.decisionID, userID), Kind: "outcome", At: o.at,
			Text: o.contest + ": " + outcomeWords(o.kind, o.label) + ".", Reason: o.reason, CaseID: &caseID,
			AppealBy: o.at + AppealWindow.Milliseconds()}
		a, err := s.appealOf(ctx, "decision_id = ? AND user_id = ?", o.decisionID, userID)
		if err != nil {
			return nil, err
		}
		if a != nil {
			l.Appeal = &AppealStatus{a.State, a.CreatedAt}
		}
		l.CanAppeal = a == nil && now < l.AppealBy
		st.Lines = append(st.Lines, l)
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT h.id, h.at, h.reason FROM rating_history h WHERE h.user_id = ? AND h.cause = 'manual' AND h.at >= ?
		AND NOT EXISTS (SELECT 1 FROM integrity_appeals a JOIN integrity_appeal_rulings r ON r.appeal_id = a.id
			WHERE a.rating_history_id = h.id AND r.upheld = 1)
		ORDER BY h.at DESC, h.id DESC`, userID, since)
	if err != nil {
		return nil, err
	}
	type adjustment struct {
		id, at int64
		reason string
	}
	var adjs []adjustment
	for rows.Next() {
		var a adjustment
		if err := rows.Scan(&a.id, &a.at, &a.reason); err != nil {
			rows.Close()
			return nil, err
		}
		adjs = append(adjs, a)
	}
	rows.Close()
	for _, adj := range adjs {
		id := adj.id
		// The proposer's reason, without a sole-organiser note.
		reason, _, _ := strings.Cut(adj.reason, " (Confirmed alone as the only organiser: ")
		l := StandingLine{ID: fmt.Sprintf("adjustment-%d", id), Kind: "adjustment", At: adj.at, Text: "Standing adjusted by an organiser.",
			Reason: reason, AdjustmentID: &id, AppealBy: adj.at + AppealWindow.Milliseconds()}
		a, err := s.appealOf(ctx, "rating_history_id = ?", id)
		if err != nil {
			return nil, err
		}
		if a != nil {
			l.Appeal = &AppealStatus{a.State, a.CreatedAt}
		}
		l.CanAppeal = a == nil && now < l.AppealBy
		st.Lines = append(st.Lines, l)
	}
	// Newest first across both kinds.
	for i := 1; i < len(st.Lines); i++ {
		for j := i; j > 0 && st.Lines[j].At > st.Lines[j-1].At; j-- {
			st.Lines[j], st.Lines[j-1] = st.Lines[j-1], st.Lines[j]
		}
	}
	return st, nil
}
