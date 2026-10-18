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
)

// Case states.
const (
	Open            = "open"
	AwaitingStudent = "awaiting_student"
	Cleared         = "cleared"
	Actioned        = "actioned"
)

// ResponseWindow is how long each student has, from the latest share, to
// explain before a reviewer may take action without their explanation. It
// is the policy template's default (docs/INTEGRITY.md, "Student's side");
// the owner changes it here.
const ResponseWindow = 7 * 24 * time.Hour

// ErrExplanationChanged is returned by Decide when a student saved an
// explanation after the version the reviewer read.
var ErrExplanationChanged = errors.New("a student changed their explanation")

// transitions are the state changes Decide accepts. Clearing is allowed
// from any open state, since it never hurts a student; action only after
// the case was shared; a decided case only reopens.
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

// CaseUser is a student in a case.
type CaseUser struct {
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	// SharedAt is the first share: from then on the student sees the case.
	SharedAt *int64 `json:"shared_at"`
	// AskedAt is the latest share, which starts the student's turn, and
	// RespondBy the end of their response window; both are null while no
	// turn is open (before the first share and after a reopen).
	AskedAt   *int64 `json:"asked_at"`
	RespondBy *int64 `json:"respond_by"`
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
	By        int64  `json:"by"`
	ByName    string `json:"by_name"`
	State     string `json:"state"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
	// ExplanationsSeen is the explanation version of each student the
	// reviewer had read, by user id.
	ExplanationsSeen map[string]int64 `json:"explanations_seen"`
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
	if err := appendDecision(ctx, tx, id, by, Open, note, nil, now); err != nil {
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

func appendDecision(ctx context.Context, tx *sql.Tx, caseID, by int64, state, note string, seen map[int64]int64, now int64) error {
	out := map[string]int64{}
	for u, at := range seen {
		out[strconv.FormatInt(u, 10)] = at
	}
	js, _ := json.Marshal(out)
	_, err := tx.ExecContext(ctx, `INSERT INTO integrity_decisions (case_id, by_user, state, note, explanations_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, caseID, by, state, strings.TrimSpace(note), string(js), now)
	return err
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
//     starts their turn: they explain, or ResponseWindow passes.
//   - Clearing is allowed whenever the case is open or shared.
//   - Action needs a shared case where every student explained after the
//     latest share or had the response window pass since it.
//   - Reopening a decided case ends every student's turn: it must be
//     shared again before any action.
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
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM integrity_cases WHERE id = ?", caseID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
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
	latest, err := latestExplanations(ctx, tx, caseID)
	if err != nil {
		return nil, err
	}
	for u, at := range latest {
		if seen[u] < at {
			return nil, ErrExplanationChanged
		}
	}
	now := s.now().UnixMilli()
	switch state {
	case AwaitingStudent:
		if _, err := tx.ExecContext(ctx, "UPDATE integrity_case_users SET asked_at = ?, shared_at = coalesce(shared_at, ?) WHERE case_id = ?",
			now, now, caseID); err != nil {
			return nil, err
		}
	case Open:
		if _, err := tx.ExecContext(ctx, "UPDATE integrity_case_users SET asked_at = NULL WHERE case_id = ?", caseID); err != nil {
			return nil, err
		}
	case Actioned:
		var waiting int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM integrity_case_users cu WHERE cu.case_id = ? AND (cu.asked_at IS NULL
			OR (cu.asked_at > ? AND NOT EXISTS (SELECT 1 FROM integrity_explanations e
				WHERE e.case_id = cu.case_id AND e.user_id = cu.user_id AND e.created_at >= cu.asked_at)))`,
			caseID, now-ResponseWindow.Milliseconds()).Scan(&waiting); err != nil {
			return nil, err
		}
		if waiting > 0 {
			return nil, &InputError{"Every student needs to explain, or to have had " + itoa(int(ResponseWindow/(24*time.Hour))) +
				" days since the case was last shared, before you take action."}
		}
	}
	closed := any(nil)
	if state == Cleared || state == Actioned {
		closed = now
	}
	if _, err := tx.ExecContext(ctx, "UPDATE integrity_cases SET state = ?, updated_at = ?, closed_at = ? WHERE id = ?",
		state, now, closed, caseID); err != nil {
		return nil, err
	}
	if err := appendDecision(ctx, tx, caseID, by, state, note, latest, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, caseID)
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
// explanations, and the decision log.
func (s *Service) GetCase(ctx context.Context, id int64) (*Case, error) {
	var c Case
	var ev string
	err := s.DB.QueryRowContext(ctx, `SELECT id, contest_id, state, title, evidence, created_at, updated_at FROM integrity_cases WHERE id = ?`, id).
		Scan(&c.ID, &c.ContestID, &c.State, &c.Title, &ev, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Evidence = json.RawMessage(ev)
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
	rows, err = s.DB.QueryContext(ctx, `SELECT cu.user_id, coalesce(nullif(u.name, ''), u.email), cu.shared_at, cu.asked_at, cu.explanation, cu.explained_at
		FROM integrity_case_users cu JOIN users u ON u.id = cu.user_id WHERE cu.case_id = ? ORDER BY cu.user_id`, id)
	if err != nil {
		return nil, err
	}
	c.Users = []CaseUser{}
	for rows.Next() {
		var u CaseUser
		var shared, asked, explained sql.NullInt64
		if err := rows.Scan(&u.UserID, &u.Name, &shared, &asked, &u.Explanation, &explained); err != nil {
			rows.Close()
			return nil, err
		}
		if shared.Valid {
			u.SharedAt = &shared.Int64
		}
		if asked.Valid {
			by := asked.Int64 + ResponseWindow.Milliseconds()
			u.AskedAt, u.RespondBy = &asked.Int64, &by
		}
		if explained.Valid {
			u.ExplainedAt = &explained.Int64
		}
		u.Explanations = versions[u.UserID]
		if u.Explanations == nil {
			u.Explanations = []Explanation{}
		}
		c.Users = append(c.Users, u)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT coalesce(d.by_user, 0), coalesce(nullif(u.name, ''), u.email, ''), d.state, d.note,
		d.explanations_seen, d.created_at FROM integrity_decisions d LEFT JOIN users u ON u.id = d.by_user WHERE d.case_id = ? ORDER BY d.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	c.Decisions = []Decision{}
	for rows.Next() {
		var d Decision
		var seen string
		if err := rows.Scan(&d.By, &d.ByName, &d.State, &d.Note, &seen, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.ExplanationsSeen = map[string]int64{}
		json.Unmarshal([]byte(seen), &d.ExplanationsSeen)
		c.Decisions = append(c.Decisions, d)
	}
	return &c, rows.Err()
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
}

// StudentSignal is one piece of evidence worded for the student.
type StudentSignal struct {
	ProblemID    int64  `json:"problem_id"`
	Kind         string `json:"kind"`
	Text         string `json:"text"`
	SubmissionID int64  `json:"submission_id,omitempty"`
	Source       string `json:"source,omitempty"`
	Spans        []Span `json:"spans,omitempty"`
}

// MyCases lists the cases shared with a student.
func (s *Service) MyCases(ctx context.Context, userID int64) ([]StudentCase, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.contest_id, c.state, ct.title, c.evidence, cu.explanation, cu.shared_at
		FROM integrity_cases c JOIN integrity_case_users cu ON cu.case_id = c.id JOIN contests ct ON ct.id = c.contest_id
		WHERE cu.user_id = ? AND cu.shared_at IS NOT NULL ORDER BY c.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	type raw struct {
		c  StudentCase
		ev string
	}
	var list []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.c.ID, &r.c.ContestID, &r.c.State, &r.c.Title, &r.ev, &r.c.Explanation, &r.c.SharedAt); err != nil {
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
		out = append(out, r.c)
	}
	return out, nil
}

func (s *Service) studentSignal(ctx context.Context, sg Signal, userID int64) (StudentSignal, error) {
	ss := StudentSignal{ProblemID: sg.ProblemID, Kind: sg.Kind}
	var ev struct {
		Tests    []int  `json:"tests"`
		Distinct int    `json:"distinct"`
		SpansA   []Span `json:"spans_a"`
		SpansB   []Span `json:"spans_b"`
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
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var last sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT (SELECT max(created_at) FROM integrity_explanations WHERE case_id = cu.case_id AND user_id = cu.user_id)
		FROM integrity_case_users cu JOIN integrity_cases c ON c.id = cu.case_id
		WHERE cu.case_id = ? AND cu.user_id = ? AND cu.shared_at IS NOT NULL AND c.state IN ('open', 'awaiting_student')`,
		caseID, userID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	at := s.now().UnixMilli()
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
