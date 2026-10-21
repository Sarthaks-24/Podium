package integrity

// Manual review (v2 plan B4.2): a private note on a contest submission,
// for reviewers only, and a case opened from a submission with its notes
// as evidence. A note is not a case: the student is never told about one,
// and it is deleted after 30 days unless a case is opened from its
// submission. The record view log (D97) and the owner's reviewer
// oversight live here too.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// KindManual is the evidence kind of a case opened from a submission.
const KindManual = "manual"

// NoteRetention is how long a note is kept when no case is opened from
// its submission (retention.PrivateNotes; the job applies it).
const NoteRetention = 30 * 24 * time.Hour

// LineRange is a 1-based range of lines of a submission's code.
type LineRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// SubmissionRef is the submission a note or a manual case is about.
type SubmissionRef struct {
	ID           int64  `json:"id"`
	ContestID    int64  `json:"contest_id"`
	UserID       int64  `json:"user_id"`
	ProblemID    int64  `json:"-"`
	ProblemLabel string `json:"problem_label"`
	Language     string `json:"language"`
	Practice     bool   `json:"practice"`
	CreatedAt    int64  `json:"created_at"`
}

// Note is a private note on a submission.
type Note struct {
	ID           int64 `json:"id"`
	SubmissionID int64 `json:"submission_id"`
	// ContestID is the submission's contest, for the involvement filter.
	ContestID int64       `json:"-"`
	By        int64       `json:"by"`
	ByName    string      `json:"by_name"`
	Text      string      `json:"text"`
	Lines     []LineRange `json:"lines"`
	CreatedAt int64       `json:"created_at"`
	// ExpiresAt is when the retention job deletes the note; nil once a
	// case was opened from its submission.
	ExpiresAt *int64 `json:"expires_at"`
}

// Submission loads a contest submission (a Submit, practice included) for
// a note or a manual case. Anything else is ErrNotFound.
func (s *Service) Submission(ctx context.Context, id int64) (*SubmissionRef, error) {
	var r SubmissionRef
	err := s.DB.QueryRowContext(ctx, `SELECT s.id, s.contest_id, s.user_id, s.problem_id, coalesce(cp.label, ''), s.language, s.practice, s.created_at
		FROM submissions s LEFT JOIN contest_problems cp ON cp.contest_id = s.contest_id AND cp.problem_id = s.problem_id
		WHERE s.id = ? AND s.contest_id IS NOT NULL AND s.kind = 'submit'`, id).
		Scan(&r.ID, &r.ContestID, &r.UserID, &r.ProblemID, &r.ProblemLabel, &r.Language, &r.Practice, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

// SubmissionSource is the program of a contest submission, for a
// reviewer's notes dialog.
func (s *Service) SubmissionSource(ctx context.Context, id int64) (string, error) {
	var src string
	err := s.DB.QueryRowContext(ctx, "SELECT source FROM submissions WHERE id = ?", id).Scan(&src)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return src, err
}

func checkLines(lines []LineRange) error {
	if len(lines) > 20 {
		return &InputError{"Highlight at most 20 ranges of lines."}
	}
	for _, l := range lines {
		if l.From < 1 || l.To < l.From || l.To > 100000 {
			return &InputError{"Each highlighted range needs a first line from 1 and a last line at or after it."}
		}
	}
	return nil
}

// AddNote writes a private note on a submission. The author of the
// submission may not note it.
func (s *Service) AddNote(ctx context.Context, sub *SubmissionRef, by int64, text string, lines []LineRange) (*Note, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 2000 {
		return nil, &InputError{"Write a note of 1 to 2,000 characters."}
	}
	if err := checkLines(lines); err != nil {
		return nil, err
	}
	if sub.UserID == by {
		return nil, &InputError{"You cannot write a note on your own submission."}
	}
	if lines == nil {
		lines = []LineRange{}
	}
	js, _ := json.Marshal(lines)
	var id int64
	if err := s.DB.QueryRowContext(ctx, `INSERT INTO submission_notes (submission_id, contest_id, user_id, by_user, text, lines, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, sub.ID, sub.ContestID, sub.UserID, by, text, string(js), s.now().UnixMilli()).Scan(&id); err != nil {
		return nil, err
	}
	notes, err := s.notesWhere(ctx, "n.id = ?", id)
	if err != nil || len(notes) == 0 {
		return nil, err
	}
	return &notes[0], nil
}

// Notes lists a submission's notes, oldest first.
func (s *Service) Notes(ctx context.Context, submissionID int64) ([]Note, error) {
	return s.notesWhere(ctx, "n.submission_id = ?", submissionID)
}

// SharesCase reports whether two people are both students of one case in
// a contest: then neither reviews the other's notes from it.
func (s *Service) SharesCase(ctx context.Context, a, b, contestID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM integrity_cases c
		JOIN integrity_case_users x ON x.case_id = c.id AND x.user_id = ?
		JOIN integrity_case_users y ON y.case_id = c.id AND y.user_id = ?
		WHERE c.contest_id = ?`, a, b, contestID).Scan(&n)
	return n > 0, err
}

// NotesAbout lists every kept note on a student's submissions, newest
// first, for the record page.
func (s *Service) NotesAbout(ctx context.Context, userID int64) ([]Note, error) {
	list, err := s.notesWhere(ctx, "n.user_id = ?", userID)
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, err
}

func (s *Service) notesWhere(ctx context.Context, where string, args ...any) ([]Note, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT n.id, n.submission_id, n.contest_id, n.by_user, coalesce(nullif(u.name, ''), u.email), n.text, n.lines, n.created_at,
		EXISTS (SELECT 1 FROM integrity_cases c WHERE c.submission_id = n.submission_id AND c.source = 'manual')
		FROM submission_notes n JOIN users u ON u.id = n.by_user WHERE `+where+` ORDER BY n.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var lines string
		var linked bool
		if err := rows.Scan(&n.ID, &n.SubmissionID, &n.ContestID, &n.By, &n.ByName, &n.Text, &lines, &n.CreatedAt, &linked); err != nil {
			return nil, err
		}
		n.Lines = []LineRange{}
		json.Unmarshal([]byte(lines), &n.Lines)
		if !linked {
			exp := n.CreatedAt + NoteRetention.Milliseconds()
			n.ExpiresAt = &exp
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ManualCaseOf returns the manual case opened from a submission, or 0.
func (s *Service) ManualCaseOf(ctx context.Context, submissionID int64) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, "SELECT id FROM integrity_cases WHERE submission_id = ? AND source = 'manual' ORDER BY id DESC LIMIT 1",
		submissionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// OpenManualCase opens a case from a submission: its student is the
// author, its evidence the submission with its notes, copied, so the case
// keeps them after the notes' own retention.
func (s *Service) OpenManualCase(ctx context.Context, sub *SubmissionRef, by int64, title, note string) (*Case, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 120 {
		return nil, &InputError{"Give the case a title of at most 120 characters."}
	}
	if err := checkNote(note); err != nil {
		return nil, err
	}
	if sub.UserID == by {
		return nil, &InputError{"You wrote this submission, so another reviewer must open the case."}
	}
	notes, err := s.Notes(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	var lines []LineRange
	type noteCopy struct {
		Text      string      `json:"text"`
		Lines     []LineRange `json:"lines"`
		By        int64       `json:"by"`
		CreatedAt int64       `json:"created_at"`
	}
	copies := []noteCopy{}
	for _, n := range notes {
		copies = append(copies, noteCopy{n.Text, n.Lines, n.By, n.CreatedAt})
		lines = append(lines, n.Lines...)
	}
	if lines == nil {
		lines = []LineRange{}
	}
	ev, _ := json.Marshal(map[string]any{"notes": copies, "lines": lines})
	evidence, _ := json.Marshal([]Signal{{ProblemID: sub.ProblemID, Kind: KindManual, UserA: sub.UserID, SubmissionA: sub.ID,
		Evidence: json.RawMessage(ev)}})
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO integrity_cases (contest_id, title, evidence, created_by, created_at, updated_at, source, submission_id)
		VALUES (?, ?, ?, ?, ?, ?, 'manual', ?) RETURNING id`, sub.ContestID, title, string(evidence), by, now, now, sub.ID).Scan(&id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO integrity_case_users (case_id, user_id) VALUES (?, ?)", id, sub.UserID); err != nil {
		return nil, err
	}
	if _, err := appendDecision(ctx, tx, decisionRow{caseID: id, by: by, state: Open, note: note, at: now}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetCase(ctx, id)
}

// Record view kinds (D97).
const (
	ViewRecord = "record"
	ViewCase   = "case"
	ViewNotes  = "notes"
)

// LogView appends a record view: viewer read subject's record page, a
// case or notes. A person reading their own is not logged.
func (s *Service) LogView(ctx context.Context, viewer, subject int64, kind string, ref int64) error {
	if viewer == subject {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "INSERT INTO record_views (at, viewer_id, subject_id, kind, ref_id) VALUES (?, ?, ?, ?, ?)",
		s.now().UnixMilli(), viewer, subject, kind, ref)
	return err
}

// View is one read for LogViews.
type View struct {
	Subject int64
	Kind    string
	Ref     int64
}

// LogViews appends several record views by one viewer in one transaction
// and one statement, so a long list costs one commit (council C1-044).
// Reads of the viewer's own record are left out, as in LogView.
func (s *Service) LogViews(ctx context.Context, viewer int64, views []View) error {
	now := s.now().UnixMilli()
	var marks []string
	var args []any
	for _, v := range views {
		if v.Subject == viewer {
			continue
		}
		marks = append(marks, "(?, ?, ?, ?, ?)")
		args = append(args, now, viewer, v.Subject, v.Kind, v.Ref)
	}
	if len(marks) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO record_views (at, viewer_id, subject_id, kind, ref_id) VALUES "+strings.Join(marks, ", "), args...); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordView is one row of the record view log.
type RecordView struct {
	ID      int64  `json:"id"`
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	RefID   int64  `json:"ref_id"`
	Viewer  Viewer `json:"viewer"`
	Subject Named  `json:"subject"`
}

// Viewer is who read a record.
type Viewer struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// RecordViews lists the log newest first, 100 at a time, optionally for
// one subject or one viewer, before the id before.
func (s *Service) RecordViews(ctx context.Context, subject, viewer, before int64) ([]RecordView, bool, error) {
	q := `SELECT v.id, v.at, v.kind, v.ref_id, v.viewer_id, coalesce(nullif(vu.name, ''), vu.email), vu.email, v.subject_id,
		coalesce(nullif(su.name, ''), su.email) FROM record_views v JOIN users vu ON vu.id = v.viewer_id JOIN users su ON su.id = v.subject_id
		WHERE v.id < ?`
	if before <= 0 {
		before = 1<<62 - 1
	}
	args := []any{before}
	if subject > 0 {
		q += " AND v.subject_id = ?"
		args = append(args, subject)
	}
	if viewer > 0 {
		q += " AND v.viewer_id = ?"
		args = append(args, viewer)
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY v.id DESC LIMIT 101", args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []RecordView{}
	for rows.Next() {
		var r RecordView
		if err := rows.Scan(&r.ID, &r.At, &r.Kind, &r.RefID, &r.Viewer.ID, &r.Viewer.Name, &r.Viewer.Email, &r.Subject.ID, &r.Subject.Name); err != nil {
			return nil, false, err
		}
		out = append(out, r)
	}
	more := len(out) > 100
	if more {
		out = out[:100]
	}
	return out, more, rows.Err()
}

// ReviewerStats is one reviewer's line on the owner's oversight page.
type ReviewerStats struct {
	UserID        int64  `json:"user_id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	ManualOpened  int    `json:"manual_opened"`
	Cleared       int    `json:"cleared"`
	Actioned      int    `json:"actioned"`
	Overdue       int    `json:"overdue"`
	Proposals     int    `json:"proposals"`
	Confirmations int    `json:"confirmations"`
	Rulings       int    `json:"rulings"`
}

// Oversight counts, per reviewer, the manual cases they opened, the cases
// they cleared or confirmed an outcome on, the cases they opened that are
// overdue now, their proposals, confirmations and rulings (D97).
func (s *Service) Oversight(ctx context.Context) ([]ReviewerStats, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, coalesce(nullif(u.name, ''), u.email), u.email,
		(SELECT count(*) FROM integrity_cases WHERE created_by = u.id AND source = 'manual'),
		(SELECT count(*) FROM integrity_decisions WHERE by_user = u.id AND state = 'cleared'),
		(SELECT count(*) FROM integrity_decisions WHERE by_user = u.id AND state = 'actioned'),
		(SELECT count(*) FROM integrity_proposals WHERE by_user = u.id),
		(SELECT count(*) FROM integrity_decisions WHERE by_user = u.id AND proposal_id IS NOT NULL),
		(SELECT count(*) FROM integrity_appeal_rulings WHERE by_user = u.id)
		FROM users u WHERE u.role IN ('owner', 'admin') OR EXISTS (SELECT 1 FROM integrity_cases WHERE created_by = u.id)
		ORDER BY coalesce(nullif(u.name, ''), u.email), u.id`)
	if err != nil {
		return nil, err
	}
	out := []ReviewerStats{}
	for rows.Next() {
		var r ReviewerStats
		if err := rows.Scan(&r.UserID, &r.Name, &r.Email, &r.ManualOpened, &r.Cleared, &r.Actioned, &r.Proposals, &r.Confirmations,
			&r.Rulings); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Overdue is a clock, not a column: count each reviewer's shared cases
	// past their decision deadline.
	crows, err := s.DB.QueryContext(ctx, "SELECT id, coalesce(created_by, 0) FROM integrity_cases WHERE state = 'awaiting_student'")
	if err != nil {
		return nil, err
	}
	type open struct{ id, by int64 }
	var cases []open
	for crows.Next() {
		var o open
		crows.Scan(&o.id, &o.by)
		cases = append(cases, o)
	}
	crows.Close()
	overdue := map[int64]int{}
	for _, o := range cases {
		c, err := s.GetCase(ctx, o.id)
		if err != nil {
			return nil, err
		}
		if c.Overdue {
			overdue[o.by]++
		}
	}
	for i := range out {
		out[i].Overdue = overdue[out[i].UserID]
	}
	return out, nil
}

// CaseSummary is one case on a student's record page.
type CaseSummary struct {
	ID           int64  `json:"id"`
	ContestID    int64  `json:"contest_id"`
	ContestTitle string `json:"contest_title"`
	State        string `json:"state"`
	Source       string `json:"source"`
	Outcome      string `json:"outcome"`
	CreatedAt    int64  `json:"created_at"`
	DecidedAt    *int64 `json:"decided_at"`
}

// CasesAbout lists every case a student is in, newest first.
func (s *Service) CasesAbout(ctx context.Context, userID int64) ([]CaseSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.contest_id, ct.title, c.state, c.source, c.created_at, c.closed_at,
		coalesce((SELECT d.outcome FROM integrity_decisions d WHERE d.case_id = c.id AND d.outcome <> ''
			AND EXISTS (SELECT 1 FROM json_each(d.outcome_users) WHERE value = ?) ORDER BY d.id DESC LIMIT 1), '')
		FROM integrity_cases c JOIN integrity_case_users cu ON cu.case_id = c.id JOIN contests ct ON ct.id = c.contest_id
		WHERE cu.user_id = ? ORDER BY c.id DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CaseSummary{}
	for rows.Next() {
		var c CaseSummary
		var closed sql.NullInt64
		if err := rows.Scan(&c.ID, &c.ContestID, &c.ContestTitle, &c.State, &c.Source, &c.CreatedAt, &closed, &c.Outcome); err != nil {
			return nil, err
		}
		c.DecidedAt = ptr(closed)
		out = append(out, c)
	}
	return out, rows.Err()
}

// AppealsOf lists a student's appeals for the record page.
func (s *Service) AppealsOf(ctx context.Context, userID int64) ([]Appeal, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+appealColumns+" FROM "+appealJoins+" WHERE a.user_id = ? ORDER BY a.id DESC", userID)
	if err != nil {
		return nil, err
	}
	return s.scanAppeals(rows)
}
