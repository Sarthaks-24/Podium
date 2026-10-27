package integrity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
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
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
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
