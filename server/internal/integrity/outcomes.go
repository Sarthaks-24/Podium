package integrity

// Outcomes, the two-person rule and appeals (v2 plan B4.3 and B4.3b;
// D77 as amended, D110). An outcome is proposed by one reviewer and
// confirmed by a second who neither opened the case nor proposed it; the
// owner alone may confirm only when no other owner or admin exists, with a
// written reason. A confirmed outcome lowers the student's rating, and a
// zero for a problem changes the board. A student may appeal within 14
// days; a reviewer who took no part rules, and an upheld appeal undoes
// the outcome: the standing line, the rating change, the zero on the
// board.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/contests"
	"github.com/Sarthaks-24/podium/internal/notifications"
)

// Outcome kinds.
const (
	OutcomeWarning  = "warning"
	OutcomeZero     = "zero_problem"
	OutcomeReferred = "referred"
)

// outcomeCost is what a confirmed outcome takes from the rating, in tenths
// (D110: 0.5, 1.5, 2.5).
var outcomeCost = map[string]int{OutcomeWarning: 5, OutcomeZero: 15, OutcomeReferred: 25}

// Proposal states.
const (
	ProposalPending    = "pending"
	ProposalConfirmed  = "confirmed"
	ProposalSuperseded = "superseded"
)

// Appeal states.
const (
	AppealOpen     = "open"
	AppealUpheld   = "upheld"
	AppealRejected = "rejected"
)

// Refusals the API maps to their own codes.
var (
	ErrProposalPending   = errors.New("a proposal is waiting for confirmation")
	ErrTwoPersonRule     = errors.New("a second reviewer who did not open the case or propose this outcome confirms it")
	ErrUninvolved        = errors.New("a reviewer who took no part in the case rules on the appeal")
	ErrContestNotOver    = errors.New("outcomes are decided after the contest ends for everyone")
	ErrSoleOrganiserNote = errors.New("the sole-organiser reason is missing or too short")
)

// Proposal is an outcome waiting for, or given, a second reviewer.
type Proposal struct {
	ID           int64   `json:"id"`
	By           int64   `json:"by"`
	ByName       string  `json:"by_name"`
	Outcome      string  `json:"outcome"`
	ProblemLabel string  `json:"problem_label"`
	UserIDs      []int64 `json:"user_ids"`
	Note         string  `json:"note"`
	Reason       string  `json:"reason"`
	CreatedAt    int64   `json:"created_at"`
	State        string  `json:"state"`
	problemID    int64
	after        int64
}

// Ruling is the decision on an appeal.
type Ruling struct {
	By        int64  `json:"by,omitempty"`
	ByName    string `json:"by_name,omitempty"`
	Upheld    bool   `json:"upheld"`
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"created_at"`
}

// Appeal is a student's appeal of an outcome or of an adjustment.
type Appeal struct {
	ID           int64   `json:"id"`
	CaseID       *int64  `json:"case_id"`
	AdjustmentID *int64  `json:"adjustment_id"`
	UserID       int64   `json:"user_id"`
	User         *Person `json:"user,omitempty"`
	Text         string  `json:"text"`
	CreatedAt    int64   `json:"created_at"`
	DecideBy     int64   `json:"decide_by"`
	Overdue      bool    `json:"overdue"`
	State        string  `json:"state"`
	Ruling       *Ruling `json:"ruling"`
	decisionID   int64
}

// studentView removes who ruled: a student reads the ruling, not the
// reviewer's name.
func (a *Appeal) studentView() {
	a.User = nil
	if a.Ruling != nil {
		a.Ruling.By, a.Ruling.ByName = 0, ""
	}
}

// proposals loads a case's proposals with their states.
func (s *Service) proposals(ctx context.Context, c *Case, labels map[int64]string) ([]Proposal, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.by_user, coalesce(nullif(u.name, ''), u.email), p.outcome, coalesce(p.outcome_problem_id, 0),
		p.user_ids, p.note, p.reason, p.created_at, p.after_decision FROM integrity_proposals p JOIN users u ON u.id = p.by_user
		WHERE p.case_id = ? ORDER BY p.id`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proposal{}
	for rows.Next() {
		var p Proposal
		var users string
		if err := rows.Scan(&p.ID, &p.By, &p.ByName, &p.Outcome, &p.problemID, &users, &p.Note, &p.Reason, &p.CreatedAt, &p.after); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(users), &p.UserIDs)
		p.ProblemLabel = labels[p.problemID]
		p.State = ProposalPending
		for _, d := range c.Decisions {
			if d.ProposalID != nil && *d.ProposalID == p.ID {
				p.State = ProposalConfirmed
				break
			}
			if d.ID > p.after {
				p.State = ProposalSuperseded
			}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Only the newest proposal can be pending.
	for i := 0; i < len(out)-1; i++ {
		if out[i].State == ProposalPending {
			out[i].State = ProposalSuperseded
		}
	}
	return out, nil
}

func (s *Service) pendingProposal(c *Case) *Proposal {
	for i := range c.Proposals {
		if c.Proposals[i].State == ProposalPending {
			return &c.Proposals[i]
		}
	}
	return nil
}

const appealColumns = `a.id, a.case_id, a.rating_history_id, coalesce(a.decision_id, 0), a.user_id, a.text, a.created_at,
	r.by_user, coalesce(nullif(ru.name, ''), ru.email, ''), r.upheld, coalesce(r.reason, ''), r.created_at`

const appealJoins = `integrity_appeals a LEFT JOIN integrity_appeal_rulings r ON r.appeal_id = a.id LEFT JOIN users ru ON ru.id = r.by_user`

func (s *Service) scanAppeals(rows *sql.Rows) ([]Appeal, error) {
	defer rows.Close()
	now := s.now().UnixMilli()
	out := []Appeal{}
	for rows.Next() {
		var a Appeal
		var caseID, histID, rBy, rUpheld, rAt sql.NullInt64
		var rName, rReason string
		if err := rows.Scan(&a.ID, &caseID, &histID, &a.decisionID, &a.UserID, &a.Text, &a.CreatedAt, &rBy, &rName, &rUpheld, &rReason, &rAt); err != nil {
			return nil, err
		}
		a.CaseID, a.AdjustmentID = ptr(caseID), ptr(histID)
		a.DecideBy = a.CreatedAt + RulingWindow.Milliseconds()
		a.State = AppealOpen
		if rAt.Valid {
			a.Ruling = &Ruling{By: rBy.Int64, ByName: rName, Upheld: rUpheld.Int64 == 1, Reason: rReason, CreatedAt: rAt.Int64}
			a.State = AppealRejected
			if a.Ruling.Upheld {
				a.State = AppealUpheld
			}
		}
		a.Overdue = a.State == AppealOpen && now > a.DecideBy
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) caseAppeals(ctx context.Context, caseID int64) ([]Appeal, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+appealColumns+" FROM "+appealJoins+" WHERE a.case_id = ? ORDER BY a.id", caseID)
	if err != nil {
		return nil, err
	}
	return s.scanAppeals(rows)
}

// appealOf returns the one appeal matching where, or nil.
func (s *Service) appealOf(ctx context.Context, where string, args ...any) (*Appeal, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+appealColumns+" FROM "+appealJoins+" WHERE a."+strings.ReplaceAll(where, " AND ", " AND a.")+
		" ORDER BY a.id LIMIT 1", args...)
	if err != nil {
		return nil, err
	}
	list, err := s.scanAppeals(rows)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// Appeals lists appeals for reviewers, open ones first, then newest, with
// the student's name.
func (s *Service) Appeals(ctx context.Context, openOnly bool) ([]Appeal, error) {
	list, _, err := s.AppealsPage(ctx, openOnly, 0, 500)
	return list, err
}

// AppealsPage is one page of Appeals: limit rows after skipping offset,
// and whether more follow. The appellants' names come in one query, not
// one per row (council C1-044).
func (s *Service) AppealsPage(ctx context.Context, openOnly bool, offset, limit int) ([]Appeal, bool, error) {
	q := "SELECT " + appealColumns + " FROM " + appealJoins
	if openOnly {
		q += " WHERE r.id IS NULL"
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY (r.id IS NULL) DESC, a.id DESC LIMIT ? OFFSET ?", limit+1, offset)
	if err != nil {
		return nil, false, err
	}
	list, err := s.scanAppeals(rows)
	if err != nil {
		return nil, false, err
	}
	more := len(list) > limit
	if more {
		list = list[:limit]
	}
	if len(list) == 0 {
		return list, more, nil
	}
	ids := make([]any, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.UserID)
	}
	urows, err := s.DB.QueryContext(ctx, "SELECT id, coalesce(nullif(name, ''), email), email FROM users WHERE id IN ("+
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", ids...)
	if err != nil {
		return nil, false, err
	}
	people := map[int64]Person{}
	for urows.Next() {
		var id int64
		var p Person
		if err := urows.Scan(&id, &p.Name, &p.Email); err != nil {
			urows.Close()
			return nil, false, err
		}
		people[id] = p
	}
	urows.Close()
	if err := urows.Err(); err != nil {
		return nil, false, err
	}
	for i := range list {
		if p, ok := people[list[i].UserID]; ok {
			list[i].User = &p
		}
	}
	return list, more, nil
}

// MyAppeals lists a student's own appeals, as they may read them.
func (s *Service) MyAppeals(ctx context.Context, userID int64) ([]Appeal, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+appealColumns+" FROM "+appealJoins+" WHERE a.user_id = ? ORDER BY a.id DESC", userID)
	if err != nil {
		return nil, err
	}
	list, err := s.scanAppeals(rows)
	for i := range list {
		list[i].studentView()
	}
	return list, err
}

// liveOutcome is the SQL condition, on a decision d and a user given as
// the parameter, that d is a confirmed outcome for that user that no
// upheld appeal overturned.
const liveOutcome = `d.state = 'actioned' AND d.outcome <> '' AND EXISTS (SELECT 1 FROM json_each(d.outcome_users) WHERE value = ?)
	AND NOT EXISTS (SELECT 1 FROM integrity_appeals a JOIN integrity_appeal_rulings r ON r.appeal_id = a.id
		WHERE a.decision_id = d.id AND a.user_id = ? AND r.upheld = 1)`

// priorOutcomes counts a student's live outcomes above a warning in other
// cases within the standing period.
func (s *Service) priorOutcomes(ctx context.Context, userID, exceptCase, now int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM integrity_decisions d WHERE d.case_id <> ? AND d.outcome IN ('zero_problem', 'referred')
		AND d.created_at >= ? AND `+liveOutcome, exceptCase, now-StandingPeriod.Milliseconds(), userID, userID).Scan(&n)
	return n, err
}

// SoleOrganiser reports whether the user is an owner and no other enabled
// owner or admin account exists: then the two-person rule cannot be met
// and the owner may act alone with a written reason (input 18 default).
func (s *Service) SoleOrganiser(ctx context.Context, userID int64) (bool, error) {
	var role string
	err := s.DB.QueryRowContext(ctx, "SELECT role FROM users WHERE id = ?", userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) || role != "owner" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var others int
	err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id <> ? AND role IN ('owner', 'admin') AND disabled = 0
		AND email NOT LIKE '%@removed.invalid'`, userID).Scan(&others)
	return others == 0, err
}

func checkSoleReason(r string) error {
	if utf8.RuneCountInString(strings.TrimSpace(r)) < minNote || utf8.RuneCountInString(r) > 1000 {
		return &InputError{"As the only organiser, write why you decide alone: at least 20 characters (at most 1,000)."}
	}
	return nil
}

func checkReason(r, what string) error {
	if utf8.RuneCountInString(strings.TrimSpace(r)) < minNote || utf8.RuneCountInString(r) > 2000 {
		return &InputError{"Write " + what + " of at least 20 characters (at most 2,000)."}
	}
	return nil
}

// ProposeInput is a reviewer's proposed outcome.
type ProposeInput struct {
	Outcome      string
	ProblemLabel string
	UserIDs      []int64
	Note         string
	Reason       string
	Seen         map[int64]int64
}

// Propose records an outcome for a second reviewer to confirm. The case
// must be shared with every student's turn done, the contest over for
// everyone, and no other proposal pending.
func (s *Service) Propose(ctx context.Context, caseID, by int64, in ProposeInput) (*Case, error) {
	c, err := s.GetCase(ctx, caseID)
	if err != nil {
		return nil, err
	}
	if c.Involves(by) {
		return nil, ErrNotFound
	}
	if _, ok := outcomeCost[in.Outcome]; !ok {
		return nil, &InputError{"Choose an outcome: a warning, zero for one problem, or referral."}
	}
	if err := checkNote(in.Note); err != nil {
		return nil, err
	}
	if err := checkReason(in.Reason, "the reason the student will read"); err != nil {
		return nil, err
	}
	if c.State != AwaitingStudent {
		if c.State == Open {
			return nil, &InputError{"Share the case with the students before taking action. They explain first."}
		}
		return nil, &InputError{"The case is decided. An outcome is changed only through an appeal."}
	}
	now := s.now().UnixMilli()
	if !turnsDone(c, now) {
		return nil, &InputError{"Every student needs to reply, or to have run out of time to reply, before you propose an outcome."}
	}
	if err := s.contestOver(ctx, c.ContestID); err != nil {
		return nil, err
	}
	if s.pendingProposal(c) != nil {
		return nil, ErrProposalPending
	}
	var problemID int64
	if in.Outcome == OutcomeZero {
		labels, err := s.labels(ctx, c.ContestID)
		if err != nil {
			return nil, err
		}
		for id, l := range labels {
			if l == in.ProblemLabel {
				problemID = id
			}
		}
		if problemID == 0 {
			return nil, &InputError{"Choose the problem of this contest that gets the zero."}
		}
	} else if in.ProblemLabel != "" {
		return nil, &InputError{"Only zero for a problem names a problem."}
	}
	users := in.UserIDs
	if len(users) == 0 {
		for _, u := range c.Users {
			users = append(users, u.UserID)
		}
	}
	slices.Sort(users)
	users = slices.Compact(users)
	for _, u := range users {
		if !c.Involves(u) {
			return nil, &InputError{"An outcome can only be for a student of this case."}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	latest, err := latestExplanations(ctx, tx, caseID)
	if err != nil {
		return nil, err
	}
	for u, at := range latest {
		if in.Seen[u] < at {
			return nil, ErrExplanationChanged
		}
	}
	var after int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(id), 0) FROM integrity_decisions WHERE case_id = ?", caseID).Scan(&after); err != nil {
		return nil, err
	}
	// A proposal made since the latest decision is pending (checked again
	// inside the write, so two at once cannot both be made).
	var pending int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM integrity_proposals WHERE case_id = ? AND after_decision = ?", caseID, after).
		Scan(&pending); err != nil {
		return nil, err
	}
	if pending > 0 {
		return nil, ErrProposalPending
	}
	seen, _ := json.Marshal(latest)
	ujs, _ := json.Marshal(users)
	if _, err := tx.ExecContext(ctx, `INSERT INTO integrity_proposals (case_id, after_decision, by_user, outcome, outcome_problem_id, user_ids,
		note, reason, explanations_seen, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, caseID, after, by, in.Outcome, nullInt(problemID),
		string(ujs), strings.TrimSpace(in.Note), strings.TrimSpace(in.Reason), string(seen), now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE integrity_cases SET updated_at = ? WHERE id = ?", now, caseID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, caseID)
}

// contestOver refuses an outcome while any student's window is open.
func (s *Service) contestOver(ctx context.Context, contestID int64) error {
	if s.Contests == nil {
		return nil
	}
	ct, err := s.Contests.Get(ctx, contestID)
	if err != nil {
		return err
	}
	if s.now().Before(ct.Final()) {
		return ErrContestNotOver
	}
	return nil
}

// actorOf is a user as a contest audit row names them.
func (s *Service) actorOf(ctx context.Context, userID int64) (contests.Actor, error) {
	a := contests.Actor{ID: userID}
	err := s.DB.QueryRowContext(ctx, "SELECT email, role FROM users WHERE id = ?", userID).Scan(&a.Email, &a.Role)
	return a, err
}

// boardBefore is one student's place on the board just before a decision.
type boardBefore struct {
	Rank  int `json:"rank"`
	Total int `json:"total"`
	Score int `json:"score"`
}

// Confirm confirms the pending proposal: the decision is written (state
// actioned, with the outcome), each student's rating drops by the
// outcome's cost, a zero for a problem goes on the board, and every
// student who was shared the case is told. The confirmer must not have
// opened the case or proposed the outcome, unless they are the sole
// organiser and give soleReason.
func (s *Service) Confirm(ctx context.Context, caseID, proposalID, by int64, note, soleReason string, seen map[int64]int64) (*Case, error) {
	c, err := s.GetCase(ctx, caseID)
	if err != nil {
		return nil, err
	}
	if c.Involves(by) {
		return nil, ErrNotFound
	}
	p := s.pendingProposal(c)
	if p == nil || p.ID != proposalID {
		return nil, &InputError{"That proposal is no longer waiting for confirmation. Reload the case."}
	}
	if err := checkNote(note); err != nil {
		return nil, err
	}
	sole := ""
	if p.By == by || c.CreatedBy == by {
		ok, err := s.SoleOrganiser(ctx, by)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrTwoPersonRule
		}
		if err := checkSoleReason(soleReason); err != nil {
			return nil, err
		}
		sole = soleReason
	}
	if err := s.contestOver(ctx, c.ContestID); err != nil {
		return nil, err
	}
	// The board as it stands, for the record (boards are computed).
	before := map[string]boardBefore{}
	var ct *contests.Contest
	if s.Contests != nil {
		if ct, err = s.Contests.Get(ctx, c.ContestID); err != nil {
			return nil, err
		}
		st, err := s.Contests.BoardNow(ctx, ct)
		if err != nil {
			return nil, err
		}
		for _, row := range st.Rows {
			if !slices.Contains(p.UserIDs, row.UserID) {
				continue
			}
			b := boardBefore{Rank: row.Rank, Total: row.Total}
			for _, cell := range row.Cells {
				if cell.Label == p.ProblemLabel {
					b.Score = cell.Score
				}
			}
			before[itoa(int(row.UserID))] = b
		}
	}
	bjs, _ := json.Marshal(before)
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	latest, err := latestExplanations(ctx, tx, caseID)
	if err != nil {
		return nil, err
	}
	for u, at := range latest {
		if seen[u] < at {
			return nil, ErrExplanationChanged
		}
	}
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM integrity_cases WHERE id = ?", caseID).Scan(&state); err != nil {
		return nil, err
	}
	var newer int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM integrity_decisions WHERE case_id = ? AND id > ?", caseID, p.after).Scan(&newer); err != nil {
		return nil, err
	}
	if state != AwaitingStudent || newer > 0 {
		return nil, &InputError{"That proposal is no longer waiting for confirmation. Reload the case."}
	}
	decisionID, err := appendDecision(ctx, tx, decisionRow{caseID: caseID, by: by, state: Actioned, note: note, seen: latest, at: now,
		outcome: p.Outcome, problemID: p.problemID, users: p.UserIDs, proposalID: p.ID, soleOrganiser: sole, boardBeforeRaw: string(bjs)})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE integrity_cases SET state = ?, updated_at = ?, closed_at = ? WHERE id = ?",
		Actioned, now, now, caseID); err != nil {
		return nil, err
	}
	for _, u := range p.UserIDs {
		if _, _, err := applyRatingTx(ctx, tx, u, -outcomeCost[p.Outcome], ratingCause{cause: CauseOutcome, at: now, decisionID: decisionID,
			actorID: p.By, confirmerID: by, reason: p.Reason}); err != nil {
			return nil, err
		}
		if p.Outcome == OutcomeZero {
			if err := contests.AddZeroTx(ctx, tx, c.ContestID, u, p.problemID, decisionID, now); err != nil {
				return nil, err
			}
			if s.Contests != nil {
				a, err := s.actorOf(ctx, by)
				if err != nil {
					return nil, err
				}
				b := before[itoa(int(u))]
				if err := s.Contests.RecordTx(ctx, tx, c.ContestID, a, contests.AuditBoardAdjusted, map[string]any{"decision_id": decisionID,
					"problem_label": p.ProblemLabel, "user_id": u, "rank_before": b.Rank, "total_before": b.Total, "score_before": b.Score,
					"reversed": false}); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := notifyShared(ctx, tx, caseID, c.ContestID, decisionID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if p.Outcome == OutcomeZero {
		s.boardChanged(ctx, c.ContestID)
	}
	return s.GetCase(ctx, caseID)
}

// boardChanged refreshes a board a decision or a ruling changed and
// rewrites its summaries. The decision is committed already, so the
// rebuild does not stop when the request ends; if it fails, the
// board_adjusted audit row makes the next summary sweep build again.
func (s *Service) boardChanged(ctx context.Context, contestID int64) {
	if s.Contests == nil {
		return
	}
	s.Contests.Invalidate(contestID)
	if err := s.Contests.RebuildSummaries(context.WithoutCancel(ctx), contestID); err != nil && s.Log != nil {
		s.Log.Warn("contest summaries after a review decision", "contest_id", contestID, "err", err)
	}
}

// AppealInput is a student's appeal: of the outcome a case holds for
// them, or of a manual adjustment of their standing.
type AppealInput struct {
	CaseID       int64
	AdjustmentID int64
	Text         string
}

// FileAppeal records a student's appeal, within AppealWindow of the
// outcome or the adjustment, once per outcome or adjustment.
func (s *Service) FileAppeal(ctx context.Context, userID int64, in AppealInput) (*Appeal, error) {
	text := strings.TrimSpace(in.Text)
	if utf8.RuneCountInString(text) < minNote || utf8.RuneCountInString(text) > 4000 {
		return nil, &InputError{"Write your appeal in at least 20 characters (at most 4,000)."}
	}
	if (in.CaseID == 0) == (in.AdjustmentID == 0) {
		return nil, &InputError{"Appeal one outcome or one adjustment."}
	}
	now := s.now().UnixMilli()
	var at, decisionID int64
	if in.CaseID != 0 {
		var users string
		var outcome string
		err := s.DB.QueryRowContext(ctx, `SELECT d.id, d.created_at, d.outcome_users, d.outcome FROM integrity_decisions d
			WHERE d.case_id = ? AND d.outcome <> '' ORDER BY d.id DESC LIMIT 1`, in.CaseID).Scan(&decisionID, &at, &users, &outcome)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		d := Decision{Outcome: outcome}
		json.Unmarshal([]byte(users), &d.OutcomeUsers)
		if !d.appliesTo(userID) {
			return nil, ErrNotFound
		}
	} else {
		err := s.DB.QueryRowContext(ctx, "SELECT at FROM rating_history WHERE id = ? AND user_id = ? AND cause = 'manual'", in.AdjustmentID, userID).Scan(&at)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	if now >= at+AppealWindow.Milliseconds() {
		return nil, &InputError{"The 14 days to appeal have passed."}
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO integrity_appeals (user_id, case_id, decision_id, rating_history_id, text, created_at)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`, userID, nullInt(in.CaseID), nullInt(decisionID), nullInt(in.AdjustmentID), text, now).Scan(&id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return nil, &InputError{"You have already appealed this. The ruling will be on your integrity page."}
	}
	if err != nil {
		return nil, err
	}
	a, err := s.appealOf(ctx, "id = ?", id)
	if err == nil && a != nil {
		a.studentView()
	}
	return a, err
}

// Rule decides an appeal. The reviewer must have taken no part in the case
// (or, for an adjustment, neither made nor confirmed it), unless they are
// the sole organiser and give soleReason. An upheld appeal undoes the
// outcome for that student.
func (s *Service) Rule(ctx context.Context, appealID, by int64, upheld bool, reason, soleReason string) (*Appeal, error) {
	a, err := s.appealOf(ctx, "id = ?", appealID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrNotFound
	}
	if a.State != AppealOpen {
		return nil, &InputError{"This appeal has a ruling already."}
	}
	if err := checkReason(reason, "the reason the student will read"); err != nil {
		return nil, err
	}
	var c *Case
	involved := a.UserID == by
	if a.CaseID != nil {
		if c, err = s.GetCase(ctx, *a.CaseID); err != nil {
			return nil, err
		}
		involved = involved || c.tookPart(by)
	} else {
		var actor, confirmer sql.NullInt64
		if err := s.DB.QueryRowContext(ctx, "SELECT actor_id, confirmer_id FROM rating_history WHERE id = ?", *a.AdjustmentID).
			Scan(&actor, &confirmer); err != nil {
			return nil, err
		}
		involved = involved || actor.Int64 == by || confirmer.Int64 == by
	}
	sole := ""
	if involved {
		// A student of the case never rules on it, sole organiser or not.
		if a.UserID == by || (c != nil && c.Involves(by)) {
			return nil, ErrUninvolved
		}
		ok, err := s.SoleOrganiser(ctx, by)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrUninvolved
		}
		if err := checkSoleReason(soleReason); err != nil {
			return nil, err
		}
		sole = soleReason
	}
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var open int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM integrity_appeal_rulings WHERE appeal_id = ?", appealID).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, &InputError{"This appeal has a ruling already."}
	}
	// An upheld appeal that leaves no student of the case with an outcome
	// clears the case; the ruling names that decision.
	cleared := int64(0)
	if upheld && c != nil {
		if cleared, err = clearIfLastTx(ctx, tx, c, a, by, reason, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO integrity_appeal_rulings (appeal_id, by_user, upheld, reason, sole_organiser, decision_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, appealID, by, upheld, strings.TrimSpace(reason), strings.TrimSpace(sole), nullInt(cleared), now); err != nil {
		return nil, err
	}
	boardMoved := int64(0)
	if upheld {
		if c != nil {
			if boardMoved, err = s.undoOutcomeTx(ctx, tx, c, a, by, reason, now); err != nil {
				return nil, err
			}
		} else {
			var delta int
			if err := tx.QueryRowContext(ctx, "SELECT delta_tenths FROM rating_history WHERE id = ?", *a.AdjustmentID).Scan(&delta); err != nil {
				return nil, err
			}
			if _, _, err := applyRatingTx(ctx, tx, a.UserID, -delta, ratingCause{cause: CauseAppeal, at: now, appealID: a.ID, actorID: by,
				reason: reason}); err != nil {
				return nil, err
			}
		}
	}
	contestID := int64(0)
	if c != nil {
		contestID = c.ContestID
	}
	if err := notifications.InsertTx(ctx, tx, notifications.Row{UserID: a.UserID, Kind: notifications.AppealDecided, AppealID: a.ID,
		CaseID: deref(a.CaseID), ContestID: contestID, CreatedAt: now}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if boardMoved != 0 {
		s.boardChanged(ctx, boardMoved)
	}
	return s.appealOf(ctx, "id = ?", appealID)
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// clearIfLastTx clears the case, with a decision row, when the appealing
// student is the last of the case's students to keep the outcome. It
// returns the cleared decision's id, or 0.
func clearIfLastTx(ctx context.Context, tx *sql.Tx, c *Case, a *Appeal, by int64, reason string, now int64) (int64, error) {
	var kept int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM integrity_decisions d, json_each(d.outcome_users) u
		WHERE d.id = ? AND u.value <> ? AND NOT EXISTS (SELECT 1 FROM integrity_appeals a JOIN integrity_appeal_rulings r ON r.appeal_id = a.id
			WHERE a.decision_id = d.id AND a.user_id = u.value AND r.upheld = 1)`, a.decisionID, a.UserID).Scan(&kept); err != nil {
		return 0, err
	}
	if kept > 0 {
		return 0, nil
	}
	id, err := appendDecision(ctx, tx, decisionRow{caseID: c.ID, by: by, state: Cleared, at: now, note: "Appeal upheld: " + strings.TrimSpace(reason)})
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE integrity_cases SET state = ?, updated_at = ?, closed_at = ? WHERE id = ?", Cleared, now, now, c.ID)
	return id, err
}

// undoOutcomeTx reverses an upheld outcome for the appealing student: the
// rating gets back what the outcome took, a blocked semester rollover is
// applied, and a zero leaves the board. It returns the contest whose board
// changed, or 0.
func (s *Service) undoOutcomeTx(ctx context.Context, tx *sql.Tx, c *Case, a *Appeal, by int64, reason string, now int64) (int64, error) {
	var took int
	var decidedAt int64
	err := tx.QueryRowContext(ctx, `SELECT h.delta_tenths, d.created_at FROM rating_history h JOIN integrity_decisions d ON d.id = h.decision_id
		WHERE h.decision_id = ? AND h.user_id = ? AND h.cause = 'outcome'`, a.decisionID, a.UserID).Scan(&took, &decidedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if err == nil {
		if _, _, err := applyRatingTx(ctx, tx, a.UserID, -took, ratingCause{cause: CauseAppeal, at: now, decisionID: a.decisionID,
			appealID: a.ID, actorID: by, reason: reason}); err != nil {
			return 0, err
		}
		if err := rolloverAfterUpheld(ctx, tx, a.UserID, decidedAt, a.ID, now); err != nil {
			return 0, err
		}
	}
	cid, removed, err := contests.RemoveZeroTx(ctx, tx, a.decisionID, a.UserID, now)
	if err != nil || !removed {
		return 0, err
	}
	if s.Contests != nil {
		act, err := s.actorOf(ctx, by)
		if err != nil {
			return 0, err
		}
		var label string
		for _, d := range c.Decisions {
			if d.ID == a.decisionID {
				label = d.OutcomeProblemLabel
			}
		}
		if err := s.Contests.RecordTx(ctx, tx, cid, act, contests.AuditBoardAdjusted, map[string]any{"decision_id": a.decisionID,
			"problem_label": label, "user_id": a.UserID, "reversed": true}); err != nil {
			return 0, err
		}
	}
	return cid, nil
}
