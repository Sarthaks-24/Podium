// Package notifications is each person's inbox (v2 plan B4.4, contract
// C9): notices that a case was shared, a reminder, a decision, an appeal
// ruling, a standing adjustment, and final results. A row stores only the
// kind and the ids it is about; the sentences are written here at read
// time from fixed templates with the contest's title and a problem's
// label, so a notice can never carry code, evidence, test data, a note or
// anyone's name (a test scans every template).
//
// Notices are held while the person is in a live contest: the list is
// empty and the unread count zero until their window closes. Nothing
// counts toward an unread badge mid-contest.
//
// The package imports nothing of the server's own: contests and integrity
// write rows through InsertTx inside their own transactions, and main
// wires the two lookups it needs (Live, Due) as functions. It never reads
// the rating tables.
package notifications

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Kinds of notice.
const (
	CaseShared       = "case_shared"
	Reminder         = "reminder"
	CaseDecided      = "case_decided"
	AppealDecided    = "appeal_decided"
	StandingAdjusted = "standing_adjusted"
	ResultsFinal     = "results_final"
)

// Kinds lists every kind, for tests.
var Kinds = []string{CaseShared, Reminder, CaseDecided, AppealDecided, StandingAdjusted, ResultsFinal}

// Reassurance closes every notice that asks a student to reply.
const Reassurance = "Nothing affects your marks until a person decides."

// PageSize is how many notices one page lists.
const PageSize = 50

// Row is a notice as stored: who it is for, what it is about, and when it
// becomes visible.
type Row struct {
	UserID          int64
	Kind            string
	CaseID          int64
	ContestID       int64
	DecisionID      int64
	AppealID        int64
	RatingHistoryID int64
	CreatedAt       int64
	// AvailableAt is when the notice may be shown; 0 means CreatedAt.
	AvailableAt int64
}

// Execer is a database or a transaction.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// InsertTx adds a notice inside the caller's transaction. A second
// results_final notice for the same person and contest is ignored.
func InsertTx(ctx context.Context, ex Execer, r Row) error {
	if r.AvailableAt == 0 {
		r.AvailableAt = r.CreatedAt
	}
	_, err := ex.ExecContext(ctx, `INSERT OR IGNORE INTO notifications (user_id, kind, case_id, contest_id, decision_id, appeal_id,
		rating_history_id, created_at, available_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.UserID, r.Kind, nullID(r.CaseID), nullID(r.ContestID), nullID(r.DecisionID), nullID(r.AppealID), nullID(r.RatingHistoryID),
		r.CreatedAt, r.AvailableAt)
	return err
}

// Notice is one notice as the inbox shows it (docs/API.md, C9).
type Notice struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Happened    string `json:"happened"`
	Todo        string `json:"todo"`
	DueAt       *int64 `json:"due_at"`
	Reassurance string `json:"reassurance,omitempty"`
	Link        string `json:"link"`
	CaseID      *int64 `json:"case_id"`
	ContestID   *int64 `json:"contest_id"`
	CreatedAt   int64  `json:"created_at"`
	ReadAt      *int64 `json:"read_at"`

	row Row
}

// Service reads and marks the inbox.
type Service struct {
	DB *sql.DB
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Live reports whether the person is in a live contest now (their own
	// window open in a contest they started). Nil means never.
	Live func(ctx context.Context, userID int64) (bool, error)
	// Due is the deadline a notice names (a reply, an appeal), or nil.
	// Nil means no deadlines are shown.
	Due func(ctx context.Context, n Row) (*int64, error)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) live(ctx context.Context, userID int64) (bool, error) {
	if s.Live == nil {
		return false, nil
	}
	return s.Live(ctx, userID)
}

// Unread counts the person's unread notices that are available now; 0
// while they are in a live contest.
func (s *Service) Unread(ctx context.Context, userID int64) (int, error) {
	if live, err := s.live(ctx, userID); err != nil || live {
		return 0, err
	}
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE user_id = ? AND read_at IS NULL AND available_at <= ?`,
		userID, s.now().UnixMilli()).Scan(&n)
	return n, err
}

// Page is one page of the inbox.
type Page struct {
	Notices []Notice
	Unread  int
	Held    bool
	HasMore bool
}

// List returns the person's notices available now, newest first, before
// the id before (0 for the newest). While they are in a live contest it
// returns nothing and Held.
func (s *Service) List(ctx context.Context, userID, before int64) (Page, error) {
	p := Page{Notices: []Notice{}}
	live, err := s.live(ctx, userID)
	if err != nil {
		return p, err
	}
	if live {
		p.Held = true
		return p, nil
	}
	if p.Unread, err = s.Unread(ctx, userID); err != nil {
		return p, err
	}
	if before <= 0 {
		before = 1<<62 - 1
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, coalesce(case_id, 0), coalesce(contest_id, 0), coalesce(decision_id, 0),
		coalesce(appeal_id, 0), coalesce(rating_history_id, 0), created_at, available_at, read_at
		FROM notifications WHERE user_id = ? AND available_at <= ? AND id < ? ORDER BY id DESC LIMIT ?`,
		userID, s.now().UnixMilli(), before, PageSize+1)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var n Notice
		var read sql.NullInt64
		if err := rows.Scan(&n.ID, &n.Kind, &n.row.CaseID, &n.row.ContestID, &n.row.DecisionID, &n.row.AppealID,
			&n.row.RatingHistoryID, &n.CreatedAt, &n.row.AvailableAt, &read); err != nil {
			rows.Close()
			return p, err
		}
		n.row.UserID, n.row.Kind, n.row.CreatedAt = userID, n.Kind, n.CreatedAt
		if read.Valid {
			n.ReadAt = &read.Int64
		}
		p.Notices = append(p.Notices, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return p, err
	}
	if len(p.Notices) > PageSize {
		p.Notices, p.HasMore = p.Notices[:PageSize], true
	}
	for i := range p.Notices {
		if err := s.word(ctx, &p.Notices[i]); err != nil {
			return p, err
		}
	}
	return p, nil
}

// MarkRead marks the person's notices as read: the ids given, or all
// available ones. Ids of other people's notices change nothing.
func (s *Service) MarkRead(ctx context.Context, userID int64, ids []int64, all bool) error {
	now := s.now().UnixMilli()
	if all {
		_, err := s.DB.ExecContext(ctx, "UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL AND available_at <= ?",
			now, userID, now)
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > PageSize {
		return errors.New("too many ids")
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{now, userID, now}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL AND available_at <= ?
		AND id IN (`+marks+`)`, args...)
	return err
}

// facts is what a notice's sentences may name: the contest's title and a
// problem's label, nothing else.
type facts struct {
	contest string
	label   string
	outcome string // a decision's outcome, "" when cleared
	upheld  bool
	adjust  bool // an appeal of a standing adjustment
}

// word fills a notice's sentences, link and deadline.
func (s *Service) word(ctx context.Context, n *Notice) error {
	f, err := s.facts(ctx, n.row)
	if err != nil {
		return err
	}
	n.Happened, n.Todo, n.Reassurance, n.Link = Sentences(n.Kind, f.contest, f.label, f.outcome, f.upheld, f.adjust)
	if n.row.CaseID != 0 {
		n.CaseID = &n.row.CaseID
		n.Link = "/integrity#case-" + strconv.FormatInt(n.row.CaseID, 10)
	}
	if n.row.ContestID != 0 {
		n.ContestID = &n.row.ContestID
	}
	if n.Kind == ResultsFinal && n.row.ContestID != 0 {
		var slug string
		if err := s.DB.QueryRowContext(ctx, "SELECT slug FROM contests WHERE id = ?", n.row.ContestID).Scan(&slug); err == nil {
			n.Link = "/contests/" + slug
		}
	}
	if s.Due != nil {
		if n.DueAt, err = s.Due(ctx, n.row); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) facts(ctx context.Context, r Row) (facts, error) {
	var f facts
	contestID := r.ContestID
	if contestID == 0 && r.CaseID != 0 {
		s.DB.QueryRowContext(ctx, "SELECT contest_id FROM integrity_cases WHERE id = ?", r.CaseID).Scan(&contestID)
	}
	if contestID != 0 {
		if err := s.DB.QueryRowContext(ctx, "SELECT title FROM contests WHERE id = ?", contestID).Scan(&f.contest); err != nil &&
			!errors.Is(err, sql.ErrNoRows) {
			return f, err
		}
	}
	label := func(problemID int64) {
		if problemID != 0 && contestID != 0 {
			s.DB.QueryRowContext(ctx, "SELECT label FROM contest_problems WHERE contest_id = ? AND problem_id = ?", contestID, problemID).
				Scan(&f.label)
		}
	}
	switch r.Kind {
	case CaseShared, Reminder:
		var problemID int64
		s.DB.QueryRowContext(ctx, `SELECT coalesce(s.problem_id, 0) FROM integrity_cases c LEFT JOIN submissions s ON s.id = c.submission_id
			WHERE c.id = ?`, r.CaseID).Scan(&problemID)
		label(problemID)
	case CaseDecided:
		var problemID int64
		s.DB.QueryRowContext(ctx, "SELECT outcome, coalesce(outcome_problem_id, 0) FROM integrity_decisions WHERE id = ?", r.DecisionID).
			Scan(&f.outcome, &problemID)
		label(problemID)
	case AppealDecided:
		var rating sql.NullInt64
		s.DB.QueryRowContext(ctx, `SELECT coalesce(r.upheld, 0), a.rating_history_id FROM integrity_appeals a
			LEFT JOIN integrity_appeal_rulings r ON r.appeal_id = a.id WHERE a.id = ?`, r.AppealID).Scan(&f.upheld, &rating)
		f.adjust = rating.Valid
	}
	return f, nil
}

// Sentences returns a notice's sentences from the fixed templates: what
// happened, what to do, the reassurance line (asking notices only) and
// the link. contest is the contest's title, label a problem's label; both
// may be empty. Exported so a test can scan every template.
func Sentences(kind, contest, label, outcome string, upheld, adjustment bool) (happened, todo, reassurance, link string) {
	in := ""
	if contest != "" {
		in = " in " + contest
	}
	about := "your work" + in
	if contest == "" {
		about = "your work"
	}
	link = "/integrity"
	switch kind {
	case CaseShared:
		if label != "" {
			happened = "An organiser asked you to explain your submission to problem " + label + in + "."
		} else {
			happened = "An organiser asked you to explain " + about + "."
		}
		todo = "Read the question and reply on the case page."
		reassurance = Reassurance
	case Reminder:
		happened = "You have not replied yet about " + about + "."
		todo = "Reply on the case page."
		reassurance = Reassurance
	case CaseDecided:
		switch outcome {
		case "warning":
			happened = "The review of " + about + " is decided: a warning."
		case "zero_problem":
			if label != "" {
				happened = "The review of " + about + " is decided: zero on problem " + label + "."
			} else {
				happened = "The review of " + about + " is decided: zero on one problem."
			}
		case "referred":
			happened = "The review of " + about + " is decided: it is referred to the academic integrity process."
		default:
			happened = "The review of " + about + " is closed with no action."
		}
		if outcome == "" {
			todo = "There is nothing you need to do."
		} else {
			todo = "Read the reason on the case page. You may appeal."
		}
	case AppealDecided:
		subject := "Your appeal about " + about
		if adjustment {
			subject = "Your appeal about a standing adjustment"
		}
		switch {
		case upheld && adjustment:
			happened = subject + " was upheld. The adjustment is reversed."
			todo = "There is nothing you need to do."
		case upheld:
			happened = subject + " was upheld. The outcome is removed."
			todo = "There is nothing you need to do."
		default:
			happened = subject + " was not upheld."
			todo = "Read the reason on your integrity page."
		}
	case StandingAdjusted:
		happened = "An organiser adjusted your standing."
		todo = "Read the reason on your integrity page. You may appeal."
	case ResultsFinal:
		if contest != "" {
			happened = "Results for " + contest + " are final."
		} else {
			happened = "Results for a contest you took part in are final."
		}
		todo = "See your result and what to practise next."
		link = "/progress"
	}
	return happened, todo, reassurance, link
}

// Retain deletes notices made before cutoff (the retention job passes
// retention.Notifications ago) and returns how many.
func Retain(ctx context.Context, db *sql.DB, cutoff int64) (int64, error) {
	res, err := db.ExecContext(ctx, "DELETE FROM notifications WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
