package integrity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Run is a check as the review page shows it.
type Run struct {
	ID         int64    `json:"id"`
	State      string   `json:"state"`
	StartedAt  int64    `json:"started_at"`
	FinishedAt *int64   `json:"finished_at"`
	Error      string   `json:"error,omitempty"`
	Stats      Stats    `json:"stats"`
	Signals    []Signal `json:"signals"`
}

// Latest returns a contest's most recent check with its signals, or
// ErrNotFound when it was never checked.
func (s *Service) Latest(ctx context.Context, contestID int64) (*Run, error) {
	var r Run
	var stats string
	var finished sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, state, started_at, finished_at, error, stats FROM integrity_runs
		WHERE contest_id = ? ORDER BY id DESC LIMIT 1`, contestID).Scan(&r.ID, &r.State, &r.StartedAt, &finished, &r.Error, &stats)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		r.FinishedAt = &finished.Int64
	}
	json.Unmarshal([]byte(stats), &r.Stats)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, coalesce(problem_id, 0), kind, strength, score, user_a, coalesce(user_b, 0),
		coalesce(submission_a, 0), coalesce(submission_b, 0), evidence FROM integrity_signals WHERE contest_id = ?
		ORDER BY CASE strength WHEN 'strong' THEN 0 WHEN 'moderate' THEN 1 ELSE 2 END, score DESC, id`, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	r.Signals = []Signal{}
	for rows.Next() {
		var sg Signal
		var ev string
		if err := rows.Scan(&sg.ID, &sg.ProblemID, &sg.Kind, &sg.Strength, &sg.Score, &sg.UserA, &sg.UserB,
			&sg.SubmissionA, &sg.SubmissionB, &ev); err != nil {
			return nil, err
		}
		sg.Evidence = json.RawMessage(ev)
		r.Signals = append(r.Signals, sg)
	}
	return &r, rows.Err()
}

// Side is one program of a compared pair.
type Side struct {
	SubmissionID int64  `json:"submission_id"`
	UserID       int64  `json:"user_id"`
	Language     string `json:"language"`
	Source       string `json:"source"`
	CreatedAt    int64  `json:"created_at"`
	Spans        []Span `json:"spans"`
}

// Pair returns the two programs of a "similar" signal with their matched
// spans, for the side-by-side view.
func (s *Service) Pair(ctx context.Context, contestID, signalID int64) (a, b *Side, err error) {
	var ev string
	var subA, subB sql.NullInt64
	err = s.DB.QueryRowContext(ctx, `SELECT submission_a, submission_b, evidence FROM integrity_signals
		WHERE id = ? AND contest_id = ? AND kind = 'similar'`, signalID, contestID).Scan(&subA, &subB, &ev)
	if errors.Is(err, sql.ErrNoRows) || !subA.Valid || !subB.Valid {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var spans struct {
		A []Span `json:"spans_a"`
		B []Span `json:"spans_b"`
	}
	json.Unmarshal([]byte(ev), &spans)
	if a, err = s.side(ctx, contestID, subA.Int64, spans.A); err != nil {
		return nil, nil, err
	}
	if b, err = s.side(ctx, contestID, subB.Int64, spans.B); err != nil {
		return nil, nil, err
	}
	return a, b, nil
}

func (s *Service) side(ctx context.Context, contestID, subID int64, spans []Span) (*Side, error) {
	sd := Side{SubmissionID: subID, Spans: spans}
	err := s.DB.QueryRowContext(ctx, `SELECT user_id, language, source, created_at FROM submissions WHERE id = ? AND contest_id = ?`,
		subID, contestID).Scan(&sd.UserID, &sd.Language, &sd.Source, &sd.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if sd.Spans == nil {
		sd.Spans = []Span{}
	}
	return &sd, err
}

// Moment is one entry in a student's timeline.
type Moment struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"` // submission, paste, blur, focus, second_session, shared_device
	Problem int64  `json:"problem_id,omitempty"`
	Text    string `json:"text"`
}

// Timeline lists what happened for one student in a contest, oldest
// first: submissions with their counts, editor events, second sessions,
// and other students who used the same browser and network. Shared
// devices are listed, never clustered (integrity council ruling).
func (s *Service) Timeline(ctx context.Context, contestID, userID int64) ([]Moment, error) {
	out := []Moment{}
	rows, err := s.DB.QueryContext(ctx, `SELECT created_at, problem_id, language, verdict, score, max_score, typed_chars, pasted_chars, focus_losses
		FROM submissions WHERE contest_id = ? AND user_id = ? AND kind = 'submit' ORDER BY id`, contestID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Moment
		var lang, verdict string
		var score, max, typed, pasted, losses int
		if err := rows.Scan(&m.At, &m.Problem, &lang, &verdict, &score, &max, &typed, &pasted, &losses); err != nil {
			rows.Close()
			return nil, err
		}
		m.Kind = "submission"
		m.Text = strings.TrimSpace(lang + " " + verdict + " " + itoa(score) + " of " + itoa(max) +
			". Typed " + itoa(typed) + ", pasted " + itoa(pasted) + " characters, left the page " + itoa(losses) + " times.")
		out = append(out, m)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT created_at, problem_id, kind, size FROM integrity_events
		WHERE contest_id = ? AND user_id = ? ORDER BY created_at, id`, contestID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m Moment
		var size int
		if err := rows.Scan(&m.At, &m.Problem, &m.Kind, &size); err != nil {
			rows.Close()
			return nil, err
		}
		switch m.Kind {
		case "paste":
			m.Text = "Pasted " + itoa(size) + " characters."
		case "blur":
			m.Text = "Left the page."
		case "focus":
			m.Text = "Came back to the page."
		case "second_session":
			m.Text = "Opened the contest in a second browser while the first was open."
		}
		out = append(out, m)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT DISTINCT o.user_id, coalesce(nullif(u.name, ''), u.email), min(o.created_at)
		FROM submissions me JOIN submissions o ON o.contest_id = me.contest_id AND o.client_hash = me.client_hash AND o.user_id <> me.user_id
		JOIN users u ON u.id = o.user_id
		WHERE me.contest_id = ? AND me.user_id = ? AND me.client_hash <> '' GROUP BY o.user_id`, contestID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var other int64
		var name string
		var at int64
		if err := rows.Scan(&other, &name, &at); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, Moment{At: at, Kind: "shared_device", Text: "Same browser and network as " + name + "."})
	}
	rows.Close()
	sortMoments(out)
	return out, nil
}

func sortMoments(ms []Moment) {
	for i := 1; i < len(ms); i++ {
		for j := i; j > 0 && ms[j].At < ms[j-1].At; j-- {
			ms[j], ms[j-1] = ms[j-1], ms[j]
		}
	}
}

var itoa = strconv.Itoa

// Case states.
const (
	Open            = "open"
	AwaitingStudent = "awaiting_student"
	Cleared         = "cleared"
	Actioned        = "actioned"
)

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

// CaseUser is a student in a case.
type CaseUser struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	SharedAt    *int64 `json:"shared_at"`
	Explanation string `json:"explanation"`
	ExplainedAt *int64 `json:"explained_at"`
}

// Decision is one entry of a case's log.
type Decision struct {
	By        int64  `json:"by"`
	State     string `json:"state"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
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
	if err := appendDecision(ctx, tx, id, by, Open, note, now); err != nil {
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

func appendDecision(ctx context.Context, tx *sql.Tx, caseID, by int64, state, note string, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO integrity_decisions (case_id, by_user, state, note, created_at) VALUES (?, ?, ?, ?, ?)`,
		caseID, by, state, strings.TrimSpace(note), now)
	return err
}

// Decide changes a case's state with a reason. Sharing with the students
// (awaiting_student) lets them see their evidence and explain; a case is
// closed by clearing or actioning it, and only after every student in it
// was shared the case (they explain first, D47).
func (s *Service) Decide(ctx context.Context, caseID, by int64, state, note string) (*Case, error) {
	switch state {
	case Open, AwaitingStudent, Cleared, Actioned:
	default:
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
	now := s.now().UnixMilli()
	if state == AwaitingStudent {
		if _, err := tx.ExecContext(ctx, "UPDATE integrity_case_users SET shared_at = ? WHERE case_id = ? AND shared_at IS NULL", now, caseID); err != nil {
			return nil, err
		}
	}
	if state == Actioned {
		var unshared int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM integrity_case_users WHERE case_id = ? AND shared_at IS NULL", caseID).Scan(&unshared); err != nil {
			return nil, err
		}
		if unshared > 0 {
			return nil, &InputError{"Share the case with the students and give them time to explain before taking action."}
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
	if err := appendDecision(ctx, tx, caseID, by, state, note, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, caseID)
}

// GetCase returns a case with its students and decision log.
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
	rows, err := s.DB.QueryContext(ctx, `SELECT cu.user_id, coalesce(nullif(u.name, ''), u.email), cu.shared_at, cu.explanation, cu.explained_at
		FROM integrity_case_users cu JOIN users u ON u.id = cu.user_id WHERE cu.case_id = ? ORDER BY cu.user_id`, id)
	if err != nil {
		return nil, err
	}
	c.Users = []CaseUser{}
	for rows.Next() {
		var u CaseUser
		var shared, explained sql.NullInt64
		if err := rows.Scan(&u.UserID, &u.Name, &shared, &u.Explanation, &explained); err != nil {
			rows.Close()
			return nil, err
		}
		if shared.Valid {
			u.SharedAt = &shared.Int64
		}
		if explained.Valid {
			u.ExplainedAt = &explained.Int64
		}
		c.Users = append(c.Users, u)
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT coalesce(by_user, 0), state, note, created_at FROM integrity_decisions WHERE case_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	c.Decisions = []Decision{}
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.By, &d.State, &d.Note, &d.CreatedAt); err != nil {
			return nil, err
		}
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

// Explain saves a student's explanation of a case shared with them. It
// can be changed until the case is decided.
func (s *Service) Explain(ctx context.Context, caseID, userID int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 8000 {
		return &InputError{"Write an explanation of at most 8,000 characters."}
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE integrity_case_users SET explanation = ?, explained_at = ?
		WHERE case_id = ? AND user_id = ? AND shared_at IS NOT NULL
		AND (SELECT state FROM integrity_cases WHERE id = ?) IN ('open', 'awaiting_student')`,
		text, s.now().UnixMilli(), caseID, userID, caseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Participant reports whether a user took part in a contest: accepted its
// rules or submitted to it. The timeline is refused for anyone else, so a
// user id from another contest answers 404.
func (s *Service) Participant(ctx context.Context, contestID, userID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM contest_starts WHERE contest_id = ? AND user_id = ?)
		+ (SELECT count(*) FROM submissions WHERE contest_id = ? AND user_id = ?)`, contestID, userID, contestID, userID).Scan(&n)
	return n > 0, err
}

// Person is how a reviewer sees a student named in a signal: the roster's
// name and roll number when the contest has a roster entry for them,
// otherwise the account's name.
type Person struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	RollNumber string `json:"roll_number"`
}

// People returns the named users of a contest, keyed by user id. Ids that
// match no user are left out.
func (s *Service) People(ctx context.Context, contestID int64, ids []int64) (map[int64]Person, error) {
	out := map[int64]Person{}
	for _, id := range ids {
		if _, done := out[id]; done || id == 0 {
			continue
		}
		var p Person
		err := s.DB.QueryRowContext(ctx, `SELECT u.email, coalesce(nullif(r.display_name, ''), nullif(u.name, ''), u.email), coalesce(r.roll_number, '')
			FROM users u LEFT JOIN contest_roster r ON r.contest_id = ? AND r.email = u.email WHERE u.id = ?`, contestID, id).
			Scan(&p.Email, &p.Name, &p.RollNumber)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, nil
}
