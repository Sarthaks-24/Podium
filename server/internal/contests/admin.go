package contests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// Size limits on organisers' and students' text.
const (
	maxQuestion     = 2000
	maxAnswer       = 4000
	maxAnnouncement = 4000
	maxEditorial    = 64 << 10
	// maxOpenQuestions bounds a student's unanswered clarifications in one
	// contest, so one person cannot flood the inbox.
	maxOpenQuestions = 5
)

// Archive hides a contest from students and the default list. Nothing is
// deleted: submissions and results stay for the record. by is audited.
func (s *Service) Archive(ctx context.Context, id int64, by Actor) error {
	err := s.audited(ctx, id, by, AuditArchive, func(tx *sql.Tx) (any, bool, error) {
		res, err := tx.ExecContext(ctx, "UPDATE contests SET archived = 1, updated_at = ? WHERE id = ?", s.now().UnixMilli(), id)
		if err != nil {
			return nil, false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, false, ErrNotFound
		}
		return nil, true, nil
	})
	if err == nil {
		s.board.drop(id)
		if s.Changed != nil {
			s.Changed(id)
		}
	}
	return err
}

// Clone copies a contest's settings and problems into a new, unpublished
// contest with its own slug, title and dates. Extensions and the
// editorial are not copied. An admin's reason to grade an
// AI problem carries over, quoted in the new contest's audit log.
func (s *Service) Clone(ctx context.Context, id int64, slug, title string, startsAt, endsAt time.Time, by Actor) (*Contest, error) {
	src, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" {
		title = src.Title + " (copy)"
		if r := []rune(title); len(r) > 120 {
			title = string(r[:120])
		}
	}
	e := Edit{Title: title, Description: src.Description, StartsAt: startsAt, EndsAt: endsAt,
		FreezeMinutes: int(src.Schedule.Freeze / time.Minute), PracticeAfterEnd: src.PracticeAfterEnd,
		MinGapS: int(src.MinGap / time.Second), MaxPerProblem: src.MaxPerProblem, Languages: src.Languages,
		HonourText: src.HonourText, HoldReveal: src.HoldReveal, Exam: src.Exam, Audience: src.Audience}
	if errs := e.check(); errs != nil {
		return nil, errs
	}
	aiReasons := make(map[int64]string, len(src.Problems))
	for _, p := range src.Problems {
		reason, err := s.aiConfirmedIn(ctx, src.ID, p.ProblemID)
		if err != nil {
			return nil, err
		}
		aiReasons[p.ProblemID] = reason
	}
	langs, err := json.Marshal(e.Languages)
	if err != nil {
		return nil, err
	}
	now := s.now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	base := Slugify(e.Title)
	var cloneID int64
	for n := 1; n <= 100; n++ {
		cloneSlug := base
		if n > 1 {
			cloneSlug = fmt.Sprintf("%s-%d", base, n)
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO contests (slug, title, description_md, starts_at, ends_at,
			freeze_minutes, published, practice_after_end, min_gap_s, max_per_problem, languages, honour_text, hold_reveal,
			full_screen, count_focus, block_paste, block_copy, created_by, created_at, updated_at, audience)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			cloneSlug, e.Title, e.Description, e.StartsAt.UnixMilli(), e.EndsAt.UnixMilli(),
			e.FreezeMinutes, e.PracticeAfterEnd, e.MinGapS, e.MaxPerProblem, string(langs), e.HonourText, e.HoldReveal,
			e.Exam.FullScreen, e.Exam.CountFocus, e.Exam.BlockPaste, e.Exam.BlockCopy,
			nullID(by.ID), now, now, e.Audience).Scan(&cloneID)
		if isUniqueSlug(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	if cloneID == 0 {
		return nil, FieldErrors{"title": "Too many contests have this title. Change it a little."}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO contest_organisers (contest_id, user_id, added_by, added_at)
		SELECT ?, user_id, added_by, added_at FROM contest_organisers WHERE contest_id = ?`, cloneID, id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO contest_organisers (contest_id, user_id, added_by, added_at)
		VALUES (?, ?, ?, ?)`, cloneID, by.ID, by.ID, now); err != nil {
		return nil, err
	}
	// The invites come along, so a weekly contest for one class keeps its class.
	if _, err := tx.ExecContext(ctx, `INSERT INTO contest_invites (contest_id, user_id, invited_by, invited_at)
		SELECT ?, user_id, invited_by, invited_at FROM contest_invites WHERE contest_id = ?`, cloneID, id); err != nil {
		return nil, err
	}
	pins, err := s.cloneProblems(ctx, tx, cloneID, src, aiReasons, by)
	if err != nil {
		return nil, err
	}
	if len(pins) != 0 {
		if err := s.recordTx(ctx, tx, cloneID, by, AuditProblems, map[string]any{"from": []pin{}, "to": pins}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, cloneID)
}

func (s *Service) cloneProblems(ctx context.Context, tx *sql.Tx, cloneID int64, src *Contest, aiReasons map[int64]string, by Actor) ([]pin, error) {
	var pins []pin
	var unconfirmed []string
	for i, p := range src.Problems {
		var hash, tagsJSON, origin string
		var validated sql.NullString
		var solutionByPerson bool
		err := tx.QueryRowContext(ctx, "SELECT content_hash, validated_hash, tags, origin, solution_by_person FROM problems WHERE id = ? AND archived = 0", p.ProblemID).
			Scan(&hash, &validated, &tagsJSON, &origin, &solutionByPerson)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, FieldErrors{"problems": fmt.Sprintf("The problem for label %s no longer exists.", p.Label)}
		}
		if err != nil {
			return nil, err
		}
		if validated.String != hash {
			return nil, FieldErrors{"problems": fmt.Sprintf("The problem for label %s is not validated. Validate it before adding it to a contest.", p.Label)}
		}
		var tags []string
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			return nil, fmt.Errorf("problem %d tags: %w", p.ProblemID, err)
		}
		for _, tag := range tags {
			if strings.EqualFold(strings.TrimSpace(tag), problems.AIDraftTag) {
				return nil, FieldErrors{"problems": fmt.Sprintf("The problem for label %s is an AI draft. Read it and save it in the editor before adding it to a contest.", p.Label)}
			}
		}
		needs, err := unconfirmedAI(ctx, tx, cloneID, p.ProblemID)
		if err != nil {
			return nil, err
		}
		if needs && origin == "ai" && !solutionByPerson {
			reason := strings.TrimSpace(aiReasons[p.ProblemID])
			if utf8.RuneCountInString(reason) < minAIReason {
				unconfirmed = append(unconfirmed, p.Label)
			} else {
				reason = fmt.Sprintf("Copied from contest %s, where the reason was: %s", src.Slug, reason)
				if err := s.recordTx(ctx, tx, cloneID, by, AuditAIProblem, map[string]any{"label": p.Label, "problem_id": p.ProblemID, "reason": limitStr(reason, 1000)}); err != nil {
					return nil, err
				}
			}
		}
		points, err := subtaskPoints(ctx, tx, p.ProblemID)
		if err != nil {
			return nil, err
		}
		var revisionID int64
		var rev int
		err = tx.QueryRowContext(ctx, `SELECT id, rev FROM problem_revisions WHERE problem_id = ? AND content_hash = ? AND reconstructed = 0
			ORDER BY rev DESC LIMIT 1`, p.ProblemID, hash).Scan(&revisionID, &rev)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("problem %d has no revision for its content", p.ProblemID)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO contest_problems (contest_id, problem_id, label, ord, points, revision_id)
			VALUES (?, ?, ?, ?, ?, ?)`, cloneID, p.ProblemID, p.Label, i, points, revisionID); err != nil {
			return nil, err
		}
		pins = append(pins, pin{Label: p.Label, ProblemID: p.ProblemID, Hash: hash, RevisionID: revisionID, Rev: rev})
	}
	if len(unconfirmed) > 0 {
		return nil, &AIProblemError{Labels: unconfirmed}
	}
	return pins, nil
}

// SetEditorial saves the editorial. Students see it only when it is
// published and the contest has ended for them. Publishing hands students
// the solutions, so publishing, unpublishing and a change while published
// are audited for by.
func (s *Service) SetEditorial(ctx context.Context, id int64, md string, published bool, by Actor) error {
	if len(md) > maxEditorial || !utf8.ValidString(md) {
		return FieldErrors{"editorial_md": "The editorial is longer than 64 KB."}
	}
	return s.audited(ctx, id, by, AuditEditorial, func(tx *sql.Tx) (any, bool, error) {
		var oldMD string
		var was bool
		err := tx.QueryRowContext(ctx, "SELECT editorial_md, editorial_published FROM contests WHERE id = ?", id).Scan(&oldMD, &was)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, ErrNotFound
		}
		if err != nil {
			return nil, false, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE contests SET editorial_md = ?, editorial_published = ?, updated_at = ? WHERE id = ?",
			md, published, s.now().UnixMilli(), id); err != nil {
			return nil, false, err
		}
		changed := md != oldMD
		return map[string]any{"published": published, "was_published": was, "changed": changed, "bytes": len(md)},
			published != was || (published && changed), nil
	})
}

// EditorialVisible reports whether a student may read the editorial: it
// is published and every window is over, so nobody still competing (a
// personal extension) can be handed the solutions.
func (c *Contest) EditorialVisible(st StudentState, now time.Time) bool {
	return c.EditorialPublished && strings.TrimSpace(c.Editorial) != "" && st.Over && !now.Before(c.Final())
}

// Reveal records that the organisers revealed the final board: students
// held on the frozen board (HoldReveal) now see the full one. It is refused
// until every student's time is up. The first reveal is audited for by.
func (s *Service) Reveal(ctx context.Context, c *Contest, by Actor) error {
	c, err := s.Get(ctx, c.ID) // the latest extensions
	if err != nil {
		return err
	}
	if s.now().Before(c.Final()) {
		return ErrRevealEarly
	}
	err = s.audited(ctx, c.ID, by, AuditReveal, func(tx *sql.Tx) (any, bool, error) {
		res, err := tx.ExecContext(ctx, "UPDATE contests SET revealed_at = ?, updated_at = ? WHERE id = ? AND revealed_at IS NULL",
			s.now().UnixMilli(), s.now().UnixMilli(), c.ID)
		if err != nil {
			return nil, false, err
		}
		n, _ := res.RowsAffected()
		return nil, n == 1, nil
	})
	if err == nil {
		s.changed(c.ID)
	}
	return err
}

// Person is someone named in an organiser's view.
type Person struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

// Extension is one student's extra time.
type Extension struct {
	Person
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
}

// Extensions lists the contest's personal extensions.
func (s *Service) Extensions(ctx context.Context, id int64) ([]Extension, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.id, u.email, u.name, e.minutes, e.reason FROM contest_extensions e
		JOIN users u ON u.id = e.user_id WHERE e.contest_id = ? ORDER BY u.email`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Extension{}
	for rows.Next() {
		var e Extension
		if err := rows.Scan(&e.UserID, &e.Email, &e.Name, &e.Minutes, &e.Reason); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ExtendFor gives one student extra time and tells them their new end. The
// student must be taking part (IsParticipant); removing
// an extension (0 minutes) works for anyone who has one. by is audited.
func (s *Service) ExtendFor(ctx context.Context, c *Contest, userID int64, minutes int, reason string, by Actor) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE id = ?", userID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if minutes != 0 {
		ok, err := s.IsParticipant(ctx, c, userID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotParticipant
		}
	}
	if err := s.SetExtension(ctx, c.ID, userID, minutes, strings.TrimSpace(reason), by); err != nil {
		return err
	}
	st, err := s.State(ctx, c, userID)
	if err != nil {
		return err
	}
	ev := map[string]any{"contest": c.Slug, "contest_id": c.ID, "ends_at": c.Schedule.Close().Add(st.Extension).UnixMilli()}
	if st.HasClock {
		ev["ends_at"] = st.Window.Ends.UnixMilli()
	}
	s.publishTo(EventContest, ev, userID)
	return nil
}

// ExtendAll adds minutes to everyone's end and tells the contest. by is
// audited.
func (s *Service) ExtendAll(ctx context.Context, id int64, minutes int, by Actor) (*Contest, error) {
	if err := s.Extend(ctx, id, minutes, by); err != nil {
		return nil, err
	}
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.publish(ctx, c, false, EventContest, map[string]any{"contest": c.Slug, "contest_id": c.ID, "ends_at": c.Schedule.Close().UnixMilli()})
	return c, nil
}

// Announcement is a message from the organisers to the whole contest.
type Announcement struct {
	ID        int64  `json:"id"`
	BodyMD    string `json:"body_md"`
	CreatedAt int64  `json:"created_at"`
}

// Announce posts an announcement and sends it to the contest. A repeated
// request with the same key (v4 U13) returns the first announcement and
// sends nothing again.
func (s *Service) Announce(ctx context.Context, c *Contest, body string, by int64, key string) (*Announcement, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > maxAnnouncement || !utf8.ValidString(body) {
		return nil, FieldErrors{"body_md": "Write an announcement of at most 4000 characters."}
	}
	if err := checkClientKey(key); err != nil {
		return nil, err
	}
	repeat := func() (*Announcement, error) {
		var a Announcement
		err := s.DB.QueryRowContext(ctx, `SELECT id, body_md, created_at FROM announcements
			WHERE contest_id = ? AND coalesce(created_by, 0) = ? AND client_key = ? AND client_key <> ''`, c.ID, by, key).Scan(&a.ID, &a.BodyMD, &a.CreatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return &a, err
	}
	if key != "" {
		if a, err := repeat(); a != nil || err != nil {
			return a, err
		}
	}
	a := Announcement{BodyMD: body, CreatedAt: s.now().UnixMilli()}
	err := s.DB.QueryRowContext(ctx, `INSERT INTO announcements (contest_id, body_md, created_by, created_at, client_key) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		c.ID, body, nullID(by), a.CreatedAt, key).Scan(&a.ID)
	if isRepeat(err, "announcements_by_key") {
		return repeat()
	}
	if err != nil {
		return nil, err
	}
	s.publish(ctx, c, false, EventAnnouncement, map[string]any{"contest": c.Slug, "contest_id": c.ID, "id": a.ID, "body_md": a.BodyMD})
	return &a, nil
}

// Announcements lists a contest's announcements, newest first.
func (s *Service) Announcements(ctx context.Context, id int64) ([]Announcement, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, body_md, created_at FROM announcements WHERE contest_id = ? ORDER BY id DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.BodyMD, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Clarification is a student's question and the organisers' answer.
// AskedBy is filled for organisers only; students never see who asked.
type Clarification struct {
	ID           int64   `json:"id"`
	ProblemLabel *string `json:"problem_label"`
	Question     string  `json:"question"`
	Answer       string  `json:"answer"`
	Public       bool    `json:"public"`
	CreatedAt    int64   `json:"created_at"`
	AnsweredAt   *int64  `json:"answered_at"`
	AskedBy      *Person `json:"asked_by,omitempty"`
	askerID      int64
}

// Ask records a student's question and tells the organisers. A repeated
// request with the same key (v4 U13) returns the first question and tells
// nobody again.
func (s *Service) Ask(ctx context.Context, c *Contest, userID int64, label *string, question, key string) (*Clarification, error) {
	question = strings.TrimSpace(question)
	if question == "" || utf8.RuneCountInString(question) > maxQuestion || !utf8.ValidString(question) {
		return nil, FieldErrors{"question": "Write a question of at most 2000 characters."}
	}
	if err := checkClientKey(key); err != nil {
		return nil, err
	}
	repeat := func() (*Clarification, error) {
		found, err := s.clarifications(ctx, c.ID, "c.asked_by = ? AND c.client_key = ? AND c.client_key <> ''", userID, key)
		if err != nil || len(found) == 0 {
			return nil, err
		}
		return &found[0], nil
	}
	if key != "" {
		if cl, err := repeat(); cl != nil || err != nil {
			return cl, err
		}
	}
	if label != nil {
		found := false
		for _, p := range c.Problems {
			if p.Label == *label {
				found = true
			}
		}
		if !found {
			return nil, FieldErrors{"problem_label": "Choose a problem of this contest, or none for a general question."}
		}
	}
	cl := Clarification{ProblemLabel: label, Question: question, CreatedAt: s.now().UnixMilli(), askerID: userID}
	var lbl any
	if label != nil {
		lbl = *label
	}
	// The cap and the insert are one statement, so parallel requests
	// cannot all pass the count before any of them is stored.
	err := s.DB.QueryRowContext(ctx, `INSERT INTO clarifications (contest_id, problem_label, asked_by, question, created_at, client_key)
		SELECT ?, ?, ?, ?, ?, ? WHERE (SELECT count(*) FROM clarifications WHERE contest_id = ? AND asked_by = ? AND answered_at IS NULL) < ?
		RETURNING id`, c.ID, lbl, userID, question, cl.CreatedAt, key, c.ID, userID, maxOpenQuestions).Scan(&cl.ID)
	if isRepeat(err, "clarifications_by_key") {
		return repeat()
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, FieldErrors{"question": "You have 5 questions waiting for an answer. Wait for one to be answered first."}
	}
	if err != nil {
		return nil, err
	}
	s.publish(ctx, c, true, EventClarification, map[string]any{"contest": c.Slug, "contest_id": c.ID, "id": cl.ID})
	return &cl, nil
}

// Answer saves the organisers' answer and whether everyone sees it. The
// clarification must belong to the contest. The asker is told; when it is
// public, so is the whole contest.
func (s *Service) Answer(ctx context.Context, c *Contest, clarID int64, answer string, public bool, by int64) (*Clarification, error) {
	answer = strings.TrimSpace(answer)
	if utf8.RuneCountInString(answer) > maxAnswer || !utf8.ValidString(answer) {
		return nil, FieldErrors{"answer": "Write an answer of at most 4000 characters."}
	}
	var answeredAt any
	if answer != "" {
		answeredAt = s.now().UnixMilli()
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE clarifications SET answer = ?, public = ?, answered_by = ?, answered_at = ?
		WHERE id = ? AND contest_id = ?`, answer, public, nullID(by), answeredAt, clarID, c.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	list, err := s.clarifications(ctx, c.ID, "c.id = ?", clarID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	cl := list[0]
	ev := map[string]any{"contest": c.Slug, "contest_id": c.ID, "id": cl.ID}
	if cl.Public {
		s.publish(ctx, c, false, EventClarification, ev)
	} else {
		s.publishTo(EventClarification, ev, cl.askerID)
		s.publish(ctx, c, true, EventClarification, ev)
	}
	return &cl, nil
}

// Clarifications lists every clarification of a contest with who asked,
// for organisers, newest first.
func (s *Service) Clarifications(ctx context.Context, id int64) ([]Clarification, error) {
	return s.clarifications(ctx, id, "1 = 1")
}

// StudentClarifications returns a student's own questions and the public
// ones, without anyone's identity.
func (s *Service) StudentClarifications(ctx context.Context, id, userID int64) (mine, public []Clarification, err error) {
	list, err := s.clarifications(ctx, id, "(c.asked_by = ? OR (c.public = 1 AND c.answered_at IS NOT NULL))", userID)
	if err != nil {
		return nil, nil, err
	}
	mine, public = []Clarification{}, []Clarification{}
	for _, cl := range list {
		asker := cl.askerID
		cl.AskedBy = nil
		if asker == userID {
			mine = append(mine, cl)
		} else {
			public = append(public, cl)
		}
	}
	return mine, public, nil
}

func (s *Service) clarifications(ctx context.Context, id int64, where string, args ...any) ([]Clarification, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT c.id, c.problem_label, c.question, c.answer, c.public, c.created_at, c.answered_at,
		u.id, u.email, u.name FROM clarifications c JOIN users u ON u.id = c.asked_by
		WHERE c.contest_id = ? AND `+where+` ORDER BY c.id DESC`, append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Clarification{}
	for rows.Next() {
		var cl Clarification
		var label sql.NullString
		var answered sql.NullInt64
		var p Person
		if err := rows.Scan(&cl.ID, &label, &cl.Question, &cl.Answer, &cl.Public, &cl.CreatedAt, &answered, &p.UserID, &p.Email, &p.Name); err != nil {
			return nil, err
		}
		if label.Valid {
			cl.ProblemLabel = &label.String
		}
		if answered.Valid {
			cl.AnsweredAt = &answered.Int64
		}
		cl.askerID = p.UserID
		cl.AskedBy = &p
		out = append(out, cl)
	}
	return out, rows.Err()
}
