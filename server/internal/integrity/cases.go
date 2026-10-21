package integrity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/notifications"
)

// Case states.
const (
	Open            = "open"
	AwaitingStudent = "awaiting_student"
	Cleared         = "cleared"
	Actioned        = "actioned"
)

// Case sources.
const (
	SourceSignals = "signals"
	SourceManual  = "manual"
)

// ResponseWindow is how long each student has, from first opening the
// latest share's notice, to explain before a reviewer may propose an
// outcome without their explanation (ReplyCap bounds it from when the
// notice became available). It is the policy template's default
// (docs/INTEGRITY.md, "Student's side"); the owner changes it here.
const ResponseWindow = 7 * 24 * time.Hour

// ErrExplanationChanged is returned by Decide when a student saved an
// explanation after the version the reviewer read.
var ErrExplanationChanged = errors.New("a student changed their explanation")

// ErrProposalNeeded refuses taking action in one step: an outcome is
// proposed by one reviewer and confirmed by another (outcomes.go).
var ErrProposalNeeded = errors.New("propose the outcome instead: a second reviewer confirms it")

// transitions are the state changes Decide accepts. Clearing is allowed
// from any open state, since it never hurts a student; action only through
// a confirmed proposal; a decided case only reopens.
var transitions = map[string]map[string]bool{
	Open:            {AwaitingStudent: true, Cleared: true},
	AwaitingStudent: {Cleared: true, Actioned: true, Open: true},
	Cleared:         {Open: true},
	Actioned:        {Open: true},
}

// InputError is a refusal the reviewer can fix.
type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

// minNote is the shortest reason accepted for a decision.
const minNote = 20

// Case is a review of possible copying.
type Case struct {
	ID        int64           `json:"id"`
	ContestID int64           `json:"contest_id"`
	State     string          `json:"state"`
	Title     string          `json:"title"`
	Evidence  json.RawMessage `json:"evidence"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
	Users     []CaseUser      `json:"users"`
	Decisions []Decision      `json:"decisions"`
	// Source is signals or manual; SubmissionID names a manual case's
	// submission; CreatedBy is who opened the case.
	Source       string `json:"source"`
	SubmissionID *int64 `json:"submission_id"`
	CreatedBy    int64  `json:"created_by"`
	// ReadyAt, DecideBy and Overdue are the reviewers' clock (deadlines.go).
	ReadyAt  *int64 `json:"ready_at"`
	DecideBy *int64 `json:"decide_by"`
	Overdue  bool   `json:"overdue"`
	// Policy is "referral_suggested" when a student has a prior outcome
	// above a warning in the last 2 years.
	Policy    *string    `json:"policy"`
	Proposals []Proposal `json:"proposals"`
	Appeals   []Appeal   `json:"appeals"`
	// Set for one viewer by ForViewer.
	MayConfirm    bool `json:"may_confirm"`
	MayRule       bool `json:"may_rule"`
	SoleOrganiser bool `json:"sole_organiser"`
}

// Involves reports whether a user is one of the case's students. Such a
// user never reviews the case, whatever their role is now.
func (c *Case) Involves(userID int64) bool {
	for _, u := range c.Users {
		if u.UserID == userID {
			return true
		}
	}
	return false
}

// tookPart reports whether a user opened the case, proposed an outcome or
// wrote any decision on it: such a user may not rule on its appeals.
func (c *Case) tookPart(userID int64) bool {
	if c.CreatedBy == userID || c.Involves(userID) {
		return true
	}
	for _, d := range c.Decisions {
		if d.By == userID {
			return true
		}
	}
	for _, p := range c.Proposals {
		if p.By == userID {
			return true
		}
	}
	return false
}

// CaseUser is a student in a case.
type CaseUser struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	// SharedAt is the first share: from then on the student sees the case.
	SharedAt *int64 `json:"shared_at"`
	// AskedAt is the latest share, which starts the student's turn, and
	// RespondBy the end of their reply window; both are null while no
	// turn is open (before the first share and after a reopen).
	AskedAt   *int64 `json:"asked_at"`
	RespondBy *int64 `json:"respond_by"`
	// NoticeAvailableAt is when the student could first see the latest
	// share (held while they were in a live contest); FirstOpenedAt when
	// they first opened it.
	NoticeAvailableAt *int64 `json:"notice_available_at"`
	FirstOpenedAt     *int64 `json:"first_opened_at"`
	// PriorOutcomes counts their confirmed outcomes above a warning in
	// other cases in the last 2 years.
	PriorOutcomes int `json:"prior_outcomes"`
	// Explanation and ExplainedAt are the latest version; Explanations
	// holds every version, oldest first.
	Explanation  string        `json:"explanation"`
	ExplainedAt  *int64        `json:"explained_at"`
	Explanations []Explanation `json:"explanations"`
}

// Explanation is one saved version of a student's explanation. CreatedAt
// is unique per case and student and names the version.
type Explanation struct {
	Text      string `json:"text"`
	CreatedAt int64  `json:"created_at"`
}

// Decision is one entry of a case's log.
type Decision struct {
	ID        int64  `json:"id"`
	By        int64  `json:"by"`
	ByName    string `json:"by_name"`
	State     string `json:"state"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
	// ExplanationsSeen is the explanation version of each student the
	// reviewer had read, by user id.
	ExplanationsSeen map[string]int64 `json:"explanations_seen"`
	// Outcome is set on a confirmed outcome: warning, zero_problem or
	// referred, for OutcomeUsers; ProposalID names the proposal.
	Outcome             string  `json:"outcome"`
	OutcomeProblemLabel string  `json:"outcome_problem_label"`
	OutcomeUsers        []int64 `json:"outcome_users"`
	ProposalID          *int64  `json:"proposal_id"`
	SoleOrganiser       string  `json:"sole_organiser"`
	outcomeProblemID    int64
}

// appliesTo reports whether a decision's outcome is for the user.
func (d *Decision) appliesTo(userID int64) bool {
	if d.Outcome == "" {
		return false
	}
	for _, u := range d.OutcomeUsers {
		if u == userID {
			return true
		}
	}
	return false
}

// OpenCase opens a case from signals of the contest's latest run. The
// signals are copied into the case, so a later run cannot change it. The
// students are those the signals name.
func (s *Service) OpenCase(ctx context.Context, contestID, by int64, title, note string, signalIDs []int64) (*Case, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 120 {
		return nil, &InputError{"Give the case a title of at most 120 characters."}
	}
	if err := checkNote(note); err != nil {
		return nil, err
	}
	if len(signalIDs) == 0 || len(signalIDs) > 50 {
		return nil, &InputError{"Choose between 1 and 50 signals for the case."}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var evidence []Signal
	users := map[int64]bool{}
	for _, id := range signalIDs {
		var sg Signal
		var ev string
		err := tx.QueryRowContext(ctx, `SELECT id, coalesce(problem_id, 0), kind, strength, score, user_a, coalesce(user_b, 0),
			coalesce(submission_a, 0), coalesce(submission_b, 0), evidence FROM integrity_signals WHERE id = ? AND contest_id = ?`,
			id, contestID).Scan(&sg.ID, &sg.ProblemID, &sg.Kind, &sg.Strength, &sg.Score, &sg.UserA, &sg.UserB,
			&sg.SubmissionA, &sg.SubmissionB, &ev)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &InputError{"A chosen signal is not in this contest's latest check. Reload and choose again."}
		}
		if err != nil {
			return nil, err
		}
		sg.Evidence = json.RawMessage(ev)
		evidence = append(evidence, sg)
		users[sg.UserA] = true
		if sg.UserB != 0 {
			users[sg.UserB] = true
		}
	}
	if users[by] {
		return nil, &InputError{"You are one of the students these signals name, so another reviewer must open the case."}
	}
	now := s.now().UnixMilli()
	js, _ := json.Marshal(evidence)
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO integrity_cases (contest_id, title, evidence, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`, contestID, title, string(js), by, now, now).Scan(&id); err != nil {
		return nil, err
	}
	for u := range users {
		if _, err := tx.ExecContext(ctx, "INSERT INTO integrity_case_users (case_id, user_id) VALUES (?, ?)", id, u); err != nil {
			return nil, err
		}
	}
	if _, err := appendDecision(ctx, tx, decisionRow{caseID: id, by: by, state: Open, note: note, at: now}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, id)
}

func checkNote(note string) error {
	if utf8.RuneCountInString(strings.TrimSpace(note)) < minNote || utf8.RuneCountInString(note) > 4000 {
		return &InputError{"Write a reason of at least 20 characters (at most 4,000)."}
	}
	return nil
}

// decisionRow is one row of integrity_decisions to append.
type decisionRow struct {
	caseID, by     int64
	state, note    string
	seen           map[int64]int64
	at             int64
	outcome        string
	problemID      int64
	users          []int64
	proposalID     int64
	soleOrganiser  string
	boardBeforeRaw string
}

func appendDecision(ctx context.Context, tx *sql.Tx, d decisionRow) (int64, error) {
	out := map[string]int64{}
	for u, at := range d.seen {
		out[strconv.FormatInt(u, 10)] = at
	}
	js, _ := json.Marshal(out)
	users := d.users
	if users == nil {
		users = []int64{}
	}
	ujs, _ := json.Marshal(users)
	board := d.boardBeforeRaw
	if board == "" {
		board = "{}"
	}
	var id int64
	err := tx.QueryRowContext(ctx, `INSERT INTO integrity_decisions (case_id, by_user, state, note, explanations_seen, created_at,
		outcome, outcome_problem_id, outcome_users, proposal_id, sole_organiser, board_before)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, d.caseID, d.by, d.state, strings.TrimSpace(d.note), string(js), d.at,
		d.outcome, nullInt(d.problemID), string(ujs), nullInt(d.proposalID), strings.TrimSpace(d.soleOrganiser), board).Scan(&id)
	return id, err
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// stateWords name a case state inside a sentence.
var stateWords = map[string]string{
	Open:            "open",
	AwaitingStudent: "shared with the students",
	Cleared:         "cleared",
	Actioned:        "closed with action taken",
}

// refuseTransition explains why a case cannot go from one state to
// another.
func refuseTransition(from, to string) error {
	switch {
	case from == to:
		return &InputError{"The case is already " + stateWords[from] + ". Reload to see its latest state."}
	case from == Open && to == Actioned:
		return &InputError{"Share the case with the students before taking action. They explain first."}
	case from == Cleared || from == Actioned:
		return &InputError{"The case is decided. Reopen it before changing the decision."}
	}
	return &InputError{"That change is not possible from the case's current state."}
}

// Decide changes a case's state with a reason, following transitions:
//
//   - Sharing (awaiting_student) lets each student see their evidence and
//     starts their turn: the notice is held while they are in a live
//     contest, and they have ResponseWindow from opening it (ReplyCap at
//     most). Each student gets a case_shared notice.
//   - Clearing is allowed whenever the case is open or shared; students
//     who were shared the case get a case_decided notice.
//   - Action is never one step: ErrProposalNeeded (outcomes.go).
//   - Reopening a decided case ends every student's turn: it must be
//     shared again before any outcome. A case closed with a confirmed
//     outcome does not reopen; an appeal changes it.
//
// seen holds, per student, the created_at of the latest explanation the
// reviewer read; a newer version makes Decide answer ErrExplanationChanged.
// The reviewer must not be one of the case's students (ErrNotFound).
func (s *Service) Decide(ctx context.Context, caseID, by int64, state, note string, seen map[int64]int64) (*Case, error) {
	if transitions[state] == nil {
		return nil, &InputError{"Unknown case state."}
	}
	if err := checkNote(note); err != nil {
		return nil, err
	}
	// Each student's notice is held until their live contest ends,
	// looked up before the write begins.
	avail := map[int64]int64{}
	now := s.now().UnixMilli()
	if state == AwaitingStudent {
		rows, err := s.DB.QueryContext(ctx, "SELECT user_id FROM integrity_case_users WHERE case_id = ?", caseID)
		if err != nil {
			return nil, err
		}
		var ids []int64
		for rows.Next() {
			var u int64
			if err := rows.Scan(&u); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, u)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, u := range ids {
			avail[u] = now
			if s.Contests != nil {
				until, live, err := s.Contests.LiveUntil(ctx, u)
				if err != nil {
					return nil, err
				}
				if live && until.UnixMilli() > now {
					avail[u] = until.UnixMilli()
				}
			}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current string
	var contestID int64
	if err := tx.QueryRowContext(ctx, "SELECT state, contest_id FROM integrity_cases WHERE id = ?", caseID).Scan(&current, &contestID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var involved int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM integrity_case_users WHERE case_id = ? AND user_id = ?", caseID, by).
		Scan(&involved); err != nil {
		return nil, err
	}
	if involved > 0 {
		return nil, ErrNotFound
	}
	if !transitions[current][state] {
		return nil, refuseTransition(current, state)
	}
	if state == Actioned {
		return nil, ErrProposalNeeded
	}
	if state == Open {
		var outcomes int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM integrity_decisions WHERE case_id = ? AND outcome <> ''", caseID).
			Scan(&outcomes); err != nil {
			return nil, err
		}
		if outcomes > 0 {
			return nil, &InputError{"An outcome is changed only through an appeal."}
		}
	}
	latest, err := latestExplanations(ctx, tx, caseID)
	if err != nil {
		return nil, err
	}
	for u, at := range latest {
		if seen[u] < at {
			return nil, ErrExplanationChanged
		}
	}
	switch state {
	case AwaitingStudent:
		for u, at := range avail {
			if _, err := tx.ExecContext(ctx, `UPDATE integrity_case_users SET asked_at = ?, shared_at = coalesce(shared_at, ?),
				notice_available_at = ?, first_opened_at = NULL, reminded_at = NULL WHERE case_id = ? AND user_id = ?`,
				now, now, at, caseID, u); err != nil {
				return nil, err
			}
		}
	case Open:
		if _, err := tx.ExecContext(ctx, "UPDATE integrity_case_users SET asked_at = NULL WHERE case_id = ?", caseID); err != nil {
			return nil, err
		}
	}
	closed := any(nil)
	if state == Cleared {
		closed = now
	}
	if _, err := tx.ExecContext(ctx, "UPDATE integrity_cases SET state = ?, updated_at = ?, closed_at = ? WHERE id = ?",
		state, now, closed, caseID); err != nil {
		return nil, err
	}
	decisionID, err := appendDecision(ctx, tx, decisionRow{caseID: caseID, by: by, state: state, note: note, seen: latest, at: now})
	if err != nil {
		return nil, err
	}
	switch state {
	case AwaitingStudent:
		for u, at := range avail {
			if err := notifications.InsertTx(ctx, tx, notifications.Row{UserID: u, Kind: notifications.CaseShared, CaseID: caseID,
				ContestID: contestID, CreatedAt: now, AvailableAt: at}); err != nil {
				return nil, err
			}
		}
	case Cleared:
		if err := notifyShared(ctx, tx, caseID, contestID, decisionID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, caseID)
}

// notifyShared sends case_decided to every student the case was ever
// shared with (the others never knew of it).
func notifyShared(ctx context.Context, tx *sql.Tx, caseID, contestID, decisionID, now int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT user_id FROM integrity_case_users WHERE case_id = ? AND shared_at IS NOT NULL", caseID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var u int64
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, u)
	}
	rows.Close()
	for _, u := range ids {
		if err := notifications.InsertTx(ctx, tx, notifications.Row{UserID: u, Kind: notifications.CaseDecided, CaseID: caseID,
			ContestID: contestID, DecisionID: decisionID, CreatedAt: now}); err != nil {
			return err
		}
	}
	return nil
}

// latestExplanations returns the created_at of each student's latest
// explanation in a case, for those who wrote one.
func latestExplanations(ctx context.Context, tx *sql.Tx, caseID int64) (map[int64]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT user_id, max(created_at) FROM integrity_explanations WHERE case_id = ? GROUP BY user_id", caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var u, at int64
		if err := rows.Scan(&u, &at); err != nil {
			return nil, err
		}
		out[u] = at
	}
	return out, rows.Err()
}

// GetCase returns a case with its students, every version of their
// explanations, the decision log, proposals and appeals, and the clocks.
func (s *Service) GetCase(ctx context.Context, id int64) (*Case, error) {
	var c Case
	var ev string
	var sub sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, contest_id, state, title, evidence, created_at, updated_at, source, submission_id,
		coalesce(created_by, 0) FROM integrity_cases WHERE id = ?`, id).
		Scan(&c.ID, &c.ContestID, &c.State, &c.Title, &ev, &c.CreatedAt, &c.UpdatedAt, &c.Source, &sub, &c.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if sub.Valid {
		c.SubmissionID = &sub.Int64
	}
	c.Evidence = json.RawMessage(ev)
	now := s.now().UnixMilli()
	labels, err := s.labels(ctx, c.ContestID)
	if err != nil {
		return nil, err
	}
	versions := map[int64][]Explanation{}
	rows, err := s.DB.QueryContext(ctx, "SELECT user_id, text, created_at FROM integrity_explanations WHERE case_id = ? ORDER BY user_id, created_at", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u int64
		var e Explanation
		if err := rows.Scan(&u, &e.Text, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		versions[u] = append(versions[u], e)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT cu.user_id, coalesce(nullif(u.name, ''), u.email), cu.shared_at, cu.asked_at, cu.explanation,
		cu.explained_at, cu.notice_available_at, cu.first_opened_at
		FROM integrity_case_users cu JOIN users u ON u.id = cu.user_id WHERE cu.case_id = ? ORDER BY cu.user_id`, id)
	if err != nil {
		return nil, err
	}
	c.Users = []CaseUser{}
	for rows.Next() {
		var u CaseUser
		var shared, asked, explained, available, opened sql.NullInt64
		if err := rows.Scan(&u.UserID, &u.Name, &shared, &asked, &u.Explanation, &explained, &available, &opened); err != nil {
			rows.Close()
			return nil, err
		}
		u.SharedAt, u.AskedAt, u.ExplainedAt = ptr(shared), ptr(asked), ptr(explained)
		u.NoticeAvailableAt, u.FirstOpenedAt = ptr(available), ptr(opened)
		if by := respondBy(asked, available, opened); by > 0 {
			u.RespondBy = &by
		}
		u.Explanations = versions[u.UserID]
		if u.Explanations == nil {
			u.Explanations = []Explanation{}
		}
		c.Users = append(c.Users, u)
	}
	rows.Close()
	for i := range c.Users {
		if c.Users[i].PriorOutcomes, err = s.priorOutcomes(ctx, c.Users[i].UserID, c.ID, now); err != nil {
			return nil, err
		}
		if c.Users[i].PriorOutcomes > 0 {
			p := "referral_suggested"
			c.Policy = &p
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT d.id, coalesce(d.by_user, 0), coalesce(nullif(u.name, ''), u.email, ''), d.state, d.note,
		d.explanations_seen, d.created_at, d.outcome, coalesce(d.outcome_problem_id, 0), d.outcome_users, d.proposal_id, d.sole_organiser
		FROM integrity_decisions d LEFT JOIN users u ON u.id = d.by_user WHERE d.case_id = ? ORDER BY d.id`, id)
	if err != nil {
		return nil, err
	}
	c.Decisions = []Decision{}
	for rows.Next() {
		var d Decision
		var seen, users string
		var proposal sql.NullInt64
		if err := rows.Scan(&d.ID, &d.By, &d.ByName, &d.State, &d.Note, &seen, &d.CreatedAt, &d.Outcome, &d.outcomeProblemID, &users,
			&proposal, &d.SoleOrganiser); err != nil {
			rows.Close()
			return nil, err
		}
		d.ExplanationsSeen = map[string]int64{}
		json.Unmarshal([]byte(seen), &d.ExplanationsSeen)
		d.OutcomeUsers = []int64{}
		json.Unmarshal([]byte(users), &d.OutcomeUsers)
		d.ProposalID = ptr(proposal)
		d.OutcomeProblemLabel = labels[d.outcomeProblemID]
		c.Decisions = append(c.Decisions, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if c.Proposals, err = s.proposals(ctx, &c, labels); err != nil {
		return nil, err
	}
	if c.Appeals, err = s.caseAppeals(ctx, c.ID); err != nil {
		return nil, err
	}
	setClocks(&c, now)
	return &c, nil
}

func ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

// labels maps a contest's problem ids to their labels.
func (s *Service) labels(ctx context.Context, contestID int64) (map[int64]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT problem_id, label FROM contest_problems WHERE contest_id = ?", contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var l string
		if err := rows.Scan(&id, &l); err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, rows.Err()
}

// ForViewer sets the fields that depend on who is looking: whether they
// may confirm the pending proposal or rule on an open appeal, and whether
// the sole-organiser exception is theirs.
func (s *Service) ForViewer(ctx context.Context, c *Case, viewer int64) error {
	sole, err := s.SoleOrganiser(ctx, viewer)
	if err != nil {
		return err
	}
	c.SoleOrganiser = sole
	for _, p := range c.Proposals {
		if p.State == ProposalPending {
			c.MayConfirm = (p.By != viewer && c.CreatedBy != viewer && !c.Involves(viewer)) || sole
		}
	}
	for _, a := range c.Appeals {
		if a.State == AppealOpen {
			c.MayRule = !c.tookPart(viewer) || sole
		}
	}
	return nil
}

// WaitingFor counts a contest's cases whose pending proposal the viewer
// may confirm, leaving out cases about the viewer. It reads only cases
// that have a proposal at all, so the console can ask often (D321).
func (s *Service) WaitingFor(ctx context.Context, contestID, viewer int64) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT c.id FROM integrity_cases c JOIN integrity_proposals p ON p.case_id = c.id
		WHERE c.contest_id = ?`, contestID)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		c, err := s.GetCase(ctx, id)
		if err != nil {
			return 0, err
		}
		if c.Involves(viewer) {
			continue
		}
		if err := s.ForViewer(ctx, c, viewer); err != nil {
			return 0, err
		}
		if c.MayConfirm {
			n++
		}
	}
	return n, nil
}

// Cases lists a contest's cases, newest first.
func (s *Service) Cases(ctx context.Context, contestID int64) ([]Case, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM integrity_cases WHERE contest_id = ? ORDER BY id DESC", contestID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []Case{}
	for _, id := range ids {
		c, err := s.GetCase(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, nil
}

// CasePair returns the two programs of a case's similar-code evidence,
// numbered from 0 in the case's evidence, with the spans stored in the
// case. It reads the case's own copy of the signal, never the live
// signals, so a re-run cannot change what it shows; the submissions must
// still belong to the case's contest and to the students the signal
// names. Anything else is ErrNotFound.
func (s *Service) CasePair(ctx context.Context, c *Case, index int) (sg *Signal, a, b *Side, err error) {
	var evidence []Signal
	if err := json.Unmarshal(c.Evidence, &evidence); err != nil || index < 0 || index >= len(evidence) {
		return nil, nil, nil, ErrNotFound
	}
	sig := evidence[index]
	if sig.Kind != "similar" || sig.SubmissionA == 0 || sig.SubmissionB == 0 {
		return nil, nil, nil, ErrNotFound
	}
	var spans struct {
		A []Span `json:"spans_a"`
		B []Span `json:"spans_b"`
	}
	json.Unmarshal(sig.Evidence, &spans)
	if a, err = s.side(ctx, c.ContestID, sig.SubmissionA, spans.A); err != nil {
		return nil, nil, nil, err
	}
	if b, err = s.side(ctx, c.ContestID, sig.SubmissionB, spans.B); err != nil {
		return nil, nil, nil, err
	}
	if a.UserID != sig.UserA || b.UserID != sig.UserB {
		return nil, nil, nil, ErrNotFound
	}
	return &sig, a, b, nil
}

// StudentCase is what a student sees of a case shared with them: their
// own evidence, never another student's name or code. Title is the
// contest's title, not the reviewer's case title, which may name people.
type StudentCase struct {
	ID          int64           `json:"id"`
	ContestID   int64           `json:"contest_id"`
	State       string          `json:"state"`
	Title       string          `json:"title"`
	Evidence    []StudentSignal `json:"evidence"`
	Explanation string          `json:"explanation"`
	SharedAt    int64           `json:"shared_at"`
	Decided     bool            `json:"decided"`
	// RespondBy is the reply deadline while the student's turn is open.
	RespondBy *int64 `json:"respond_by"`
	// Outcome is set when the case closed with an outcome for them.
	Outcome   *StudentOutcome `json:"outcome"`
	Appeal    *Appeal         `json:"appeal"`
	CanAppeal bool            `json:"can_appeal"`
}

// StudentOutcome is an outcome as its student reads it: the reason is the
// one written for them, never the reviewers' note.
type StudentOutcome struct {
	Kind         string `json:"kind"`
	ProblemLabel string `json:"problem_label"`
	DecidedAt    int64  `json:"decided_at"`
	Reason       string `json:"reason"`
	AppealBy     int64  `json:"appeal_by"`
	decisionID   int64
}

// StudentSignal is one piece of evidence worded for the student.
type StudentSignal struct {
	ProblemID    int64       `json:"problem_id"`
	Kind         string      `json:"kind"`
	Text         string      `json:"text"`
	SubmissionID int64       `json:"submission_id,omitempty"`
	Source       string      `json:"source,omitempty"`
	Spans        []Span      `json:"spans,omitempty"`
	Lines        []LineRange `json:"lines,omitempty"`
}

// MyCases lists the cases shared with a student whose notice is
// available: a case shared while they were in a live contest shows once
// that contest ends for them, whatever its state by then (a case cleared
// during the contest stays hidden too, D170).
func (s *Service) MyCases(ctx context.Context, userID int64) ([]StudentCase, error) {
	now := s.now().UnixMilli()
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.contest_id, c.state, ct.title, c.evidence, cu.explanation, cu.shared_at,
		cu.asked_at, cu.notice_available_at, cu.first_opened_at
		FROM integrity_cases c JOIN integrity_case_users cu ON cu.case_id = c.id JOIN contests ct ON ct.id = c.contest_id
		WHERE cu.user_id = ? AND cu.shared_at IS NOT NULL AND (cu.notice_available_at IS NULL OR cu.notice_available_at <= ?)
		ORDER BY c.id DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	type raw struct {
		c                        StudentCase
		ev                       string
		asked, available, opened sql.NullInt64
	}
	var list []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.c.ID, &r.c.ContestID, &r.c.State, &r.c.Title, &r.ev, &r.c.Explanation, &r.c.SharedAt,
			&r.asked, &r.available, &r.opened); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()
	out := []StudentCase{}
	for _, r := range list {
		var signals []Signal
		json.Unmarshal([]byte(r.ev), &signals)
		r.c.Decided = r.c.State == Cleared || r.c.State == Actioned
		if r.c.State == AwaitingStudent {
			if by := respondBy(r.asked, r.available, r.opened); by > 0 {
				r.c.RespondBy = &by
			}
		}
		r.c.Evidence = []StudentSignal{}
		for _, sg := range signals {
			if sg.UserA != userID && sg.UserB != userID {
				continue
			}
			ss, err := s.studentSignal(ctx, sg, userID)
			if err != nil {
				return nil, err
			}
			r.c.Evidence = append(r.c.Evidence, ss)
		}
		if err := s.studentOutcome(ctx, &r.c, userID, now); err != nil {
			return nil, err
		}
		out = append(out, r.c)
	}
	return out, nil
}

// Opened records that the student opened a case shared with them, the
// start of their 7 days (the first open after each share counts). The
// case page calls it when it shows the case; without it the 21-day cap
// alone applies, which never shortens a student's time.
func (s *Service) Opened(ctx context.Context, caseID, userID int64) error {
	now := s.now().UnixMilli()
	res, err := s.DB.ExecContext(ctx, `UPDATE integrity_case_users SET first_opened_at = ? WHERE case_id = ? AND user_id = ?
		AND asked_at IS NOT NULL AND first_opened_at IS NULL AND (notice_available_at IS NULL OR notice_available_at <= ?)`,
		now, caseID, userID, now)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var shared int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM integrity_case_users WHERE case_id = ? AND user_id = ? AND shared_at IS NOT NULL",
		caseID, userID).Scan(&shared); err != nil {
		return err
	}
	if shared == 0 {
		return ErrNotFound
	}
	return nil
}

// studentOutcome fills the outcome a closed case holds for the student,
// with their appeal.
func (s *Service) studentOutcome(ctx context.Context, sc *StudentCase, userID, now int64) error {
	if sc.State != Actioned && sc.State != Cleared {
		return nil
	}
	var o StudentOutcome
	var problemID int64
	var users string
	err := s.DB.QueryRowContext(ctx, `SELECT d.id, d.outcome, coalesce(d.outcome_problem_id, 0), d.created_at, coalesce(p.reason, ''), d.outcome_users
		FROM integrity_decisions d LEFT JOIN integrity_proposals p ON p.id = d.proposal_id
		WHERE d.case_id = ? AND d.outcome <> '' ORDER BY d.id DESC LIMIT 1`, sc.ID).
		Scan(&o.decisionID, &o.Kind, &problemID, &o.DecidedAt, &o.Reason, &users)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	d := Decision{Outcome: o.Kind}
	json.Unmarshal([]byte(users), &d.OutcomeUsers)
	if !d.appliesTo(userID) {
		return nil
	}
	labels, err := s.labels(ctx, sc.ContestID)
	if err != nil {
		return err
	}
	o.ProblemLabel = labels[problemID]
	o.AppealBy = o.DecidedAt + AppealWindow.Milliseconds()
	sc.Outcome = &o
	a, err := s.appealOf(ctx, "decision_id = ? AND user_id = ?", o.decisionID, userID)
	if err != nil {
		return err
	}
	if a != nil {
		a.studentView()
		sc.Appeal = a
	}
	sc.CanAppeal = a == nil && now < o.AppealBy
	return nil
}

func (s *Service) studentSignal(ctx context.Context, sg Signal, userID int64) (StudentSignal, error) {
	ss := StudentSignal{ProblemID: sg.ProblemID, Kind: sg.Kind}
	var ev struct {
		Tests    []int       `json:"tests"`
		Distinct int         `json:"distinct"`
		SpansA   []Span      `json:"spans_a"`
		SpansB   []Span      `json:"spans_b"`
		Lines    []LineRange `json:"lines"`
	}
	json.Unmarshal(sg.Evidence, &ev)
	switch sg.Kind {
	case "similar":
		ss.Text = "Your submission shares long passages with another student's submission. The shared parts are highlighted."
		ss.SubmissionID, ss.Spans = sg.SubmissionA, ev.SpansA
		if sg.UserB == userID {
			ss.SubmissionID, ss.Spans = sg.SubmissionB, ev.SpansB
		}
		err := s.DB.QueryRowContext(ctx, "SELECT source FROM submissions WHERE id = ? AND user_id = ?", ss.SubmissionID, userID).Scan(&ss.Source)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ss, err
		}
	case "wrong_output":
		ss.Text = "Your program printed the same wrong output as another student's on " + itoa(len(ev.Tests)) + " hidden tests."
	case "hardcoded":
		ss.Text = "Your program contains the expected output of " + itoa(len(ev.Tests)) + " hidden tests."
		ss.SubmissionID = sg.SubmissionA
	case "probing":
		ss.Text = "Nearly identical versions of your program drew " + itoa(ev.Distinct) + " different results on the hidden tests."
	case KindManual:
		// The organisers' notes stay with them; the student sees the
		// submission and the lines asked about.
		ss.Text = "An organiser asked you to explain this submission. The lines in question are highlighted."
		ss.SubmissionID, ss.Lines = sg.SubmissionA, ev.Lines
		err := s.DB.QueryRowContext(ctx, "SELECT source FROM submissions WHERE id = ? AND user_id = ?", ss.SubmissionID, userID).Scan(&ss.Source)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return ss, err
		}
	}
	return ss, nil
}

// Explain saves a new version of a student's explanation of a case shared
// with them, until the case is decided. Earlier versions are kept, and the
// version is stamped at least one millisecond after the previous one, so
// its created_at names it.
func (s *Service) Explain(ctx context.Context, caseID, userID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 8000 {
		return &InputError{"Write an explanation of at most 8,000 characters."}
	}
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var last sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT (SELECT max(created_at) FROM integrity_explanations WHERE case_id = cu.case_id AND user_id = cu.user_id)
		FROM integrity_case_users cu JOIN integrity_cases c ON c.id = cu.case_id
		WHERE cu.case_id = ? AND cu.user_id = ? AND cu.shared_at IS NOT NULL AND c.state IN ('open', 'awaiting_student')
		AND (cu.notice_available_at IS NULL OR cu.notice_available_at <= ?)`,
		caseID, userID, now).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	at := now
	if last.Valid && at <= last.Int64 {
		at = last.Int64 + 1
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO integrity_explanations (case_id, user_id, text, created_at) VALUES (?, ?, ?, ?)",
		caseID, userID, text, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE integrity_case_users SET explanation = ?, explained_at = ? WHERE case_id = ? AND user_id = ?",
		text, at, caseID, userID); err != nil {
		return err
	}
	return tx.Commit()
}
