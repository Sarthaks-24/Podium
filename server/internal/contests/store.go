package contests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sarthaks-24/podium/internal/events"
	"github.com/Sarthaks-24/podium/internal/judge"
	"github.com/Sarthaks-24/podium/internal/problems"
)

// Errors the API maps to status codes.
var (
	ErrNotFound  = errors.New("contest not found")
	ErrForbidden = errors.New("not allowed in this contest")
)

// FieldErrors maps a field to a sentence saying what to fix.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	parts := make([]string, 0, len(f))
	for _, m := range f {
		parts = append(parts, m)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// DefaultHonour is the honour code shown when a contest has none of its
// own; the owner may change it per contest.
const DefaultHonour = `This contest counts. By starting you agree to these rules:

- Work alone. Do not share code or ideas with anyone until the contest ends.
- You may use your own templates and the documentation of your language.
- Do not use AI assistants, chatbots or code generators.
- Podium records your submissions, the number of characters you type and paste in its editor, paste sizes, and when you leave the page. Organisers use this only to review possible copying, and it is deleted 180 days after the contest unless a review is open.
- If a review is opened you will see the evidence and can explain your work before any decision.`

// Contest is one contest with its problems.
type Contest struct {
	ID                 int64
	Slug               string
	Title              string
	Description        string
	Schedule           Schedule
	Published          bool
	PracticeAfterEnd   bool
	MinGap             time.Duration
	MaxPerProblem      int
	Languages          []string
	HonourText         string
	Editorial          string
	EditorialPublished bool
	Paused             bool
	// HoldReveal keeps students on the frozen board after the end until
	// an organiser reveals it (RevealedAt).
	HoldReveal bool
	RevealedAt time.Time
	Exam       ExamOptions
	// MaxExtension is the longest personal extension, loaded with the
	// contest: every window is over at the close plus this.
	MaxExtension time.Duration
	Archived     bool
	CreatedBy    int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Problems     []ContestProblem
}

// ExamOptions are the measures an organiser can turn on for an
// examination. All are off by default, and a contest with none on
// is an ordinary contest. The JSON names are the API's, in the organiser's
// contest, its edit body and the student's rules.
type ExamOptions struct {
	// FullScreen asks for full screen when the student starts; each exit
	// is counted and shown to organisers.
	FullScreen bool `json:"full_screen"`
	// CountFocus shows organisers the focus losses, which are recorded in
	// every contest.
	CountFocus bool `json:"count_focus"`
	// BlockPaste refuses pastes into the editor.
	BlockPaste bool `json:"block_paste"`
	// BlockCopy refuses copies out of the statement.
	BlockCopy bool `json:"block_copy"`
}

// Honour returns the contest's honour code, or the default.
func (c *Contest) Honour() string {
	if strings.TrimSpace(c.HonourText) == "" {
		return DefaultHonour
	}
	return c.HonourText
}

// ContestProblem is a problem in a contest.
type ContestProblem struct {
	Label     string
	ProblemID int64
	// Title is the pinned revision's title (the bank's working copy for a
	// pin without a revision).
	Title string
	// PinnedHash is the pinned revision's content hash.
	PinnedHash string
	// Ready is false when the problem changed since it was added, or lost
	// its validation: the contest refuses to publish until it is re-added.
	Ready  bool
	Points []int
	// RevisionID and Rev name the pinned revision; Reconstructed
	// marks one the upgrade to v1.2.0 rebuilt from judging jobs.
	RevisionID    int64
	Rev           int
	Reconstructed bool
	// LatestRevisionID and LatestRev are the problem's newest revision.
	// LatestReady is true when it is the validated content of the problem
	// now (an update to it would be accepted); LatestStatementOnly when it
	// judges exactly as the pinned one (no rejudge after the update).
	LatestRevisionID    int64
	LatestRev           int
	LatestReady         bool
	LatestStatementOnly bool
}

// MaxScore is the sum of the problem's subtask points.
func (p ContestProblem) MaxScore() int {
	n := 0
	for _, v := range p.Points {
		n += v
	}
	return n
}

// Edit is everything the contest form can change in one save. The slug
// is derived from the title when the contest is created and never edited:
// Update ignores Slug.
type Edit struct {
	Slug, Title, Description string
	StartsAt, EndsAt         time.Time
	FreezeMinutes            int
	Published                bool
	PracticeAfterEnd         bool
	HoldReveal               bool
	MinGapS, MaxPerProblem   int
	Languages                []string
	HonourText               string
	Exam                     ExamOptions
}

// Service runs contests.
type Service struct {
	DB       *sql.DB
	Problems *problems.Service
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Changed is called after anything that changes a contest's standings
	// or schedule, outside any transaction.
	Changed func(contestID int64)
	// Hub, when set, carries contest events (standings, announcements,
	// clarifications, new end times) to the contest's connected members.
	Hub *events.Hub
	// BoardEvery is the shortest time between two recomputes of a
	// contest's board (DefaultBoardEvery when zero).
	BoardEvery time.Duration

	board boardCache
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Clock is the service's current time, for handlers that answer with
// server_now and compare against contest times.
func (s *Service) Clock() time.Time { return s.now() }

// changed marks the contest's boards stale and schedules one refresh for
// the burst of changes; the standings event follows the refresh.
func (s *Service) changed(id int64) {
	s.board.bump(id)
	s.scheduleRefresh(id, standingsDelay)
	if s.Changed != nil {
		s.Changed(id)
	}
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func (e *Edit) check() FieldErrors {
	errs := FieldErrors{}
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" || utf8.RuneCountInString(e.Title) > 120 {
		errs["title"] = "Enter a title of at most 120 characters."
	}
	if len(e.Description) > 32<<10 || len(e.HonourText) > 8<<10 {
		errs["description_md"] = "The description or honour code is too long."
	}
	if e.StartsAt.IsZero() || e.EndsAt.IsZero() || !e.EndsAt.After(e.StartsAt) {
		errs["ends_at"] = "The contest must end after it starts."
	} else if e.EndsAt.Sub(e.StartsAt) > 14*24*time.Hour {
		errs["ends_at"] = "A contest can last at most 14 days."
	}
	if e.FreezeMinutes < 0 || time.Duration(e.FreezeMinutes)*time.Minute >= e.EndsAt.Sub(e.StartsAt) {
		errs["freeze_minutes"] = "The freeze must be shorter than the contest."
	}
	if e.MinGapS < 0 || e.MinGapS > 3600 {
		errs["min_gap_s"] = "Set a gap from 0 to 3600 seconds."
	}
	if e.MaxPerProblem < 1 || e.MaxPerProblem > 1000 {
		errs["max_per_problem"] = "Allow from 1 to 1000 submissions per problem."
	}
	if len(e.Languages) == 0 {
		errs["languages"] = "Allow at least one language."
	}
	for _, l := range e.Languages {
		if _, ok := judge.Languages[l]; !ok {
			errs["languages"] = "Choose languages from the list."
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Create adds a contest (unpublished). Its slug is derived from the
// title (Slugify), with -2, -3 added when another contest has it.
func (s *Service) Create(ctx context.Context, e Edit, by int64) (*Contest, error) {
	e.Published = false
	if errs := e.check(); errs != nil {
		return nil, errs
	}
	now := s.now().UnixMilli()
	langs, _ := json.Marshal(e.Languages)
	base := Slugify(e.Title)
	for n := 1; n <= 100; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		var id int64
		err := s.DB.QueryRowContext(ctx, `INSERT INTO contests (slug, title, description_md, starts_at, ends_at,
			freeze_minutes, published, practice_after_end, min_gap_s, max_per_problem, languages, honour_text, hold_reveal,
			full_screen, count_focus, block_paste, block_copy, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			slug, e.Title, e.Description, e.StartsAt.UnixMilli(), e.EndsAt.UnixMilli(),
			e.FreezeMinutes, e.PracticeAfterEnd, e.MinGapS, e.MaxPerProblem, string(langs), e.HonourText, e.HoldReveal,
			e.Exam.FullScreen, e.Exam.CountFocus, e.Exam.BlockPaste, e.Exam.BlockCopy,
			nullID(by), now, now).Scan(&id)
		if isUniqueSlug(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return s.Get(ctx, id)
	}
	return nil, FieldErrors{"title": "Too many contests have this title. Change it a little."}
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify makes a contest's address from its title: lowercase letters and
// digits joined by single hyphens, at most 48 characters, "contest" when
// nothing is left.
func Slugify(title string) string {
	s := strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 48 {
		s = strings.TrimRight(s[:48], "-")
	}
	if s == "" {
		return "contest"
	}
	return s
}

// Update saves the contest form for by. Publishing checks every problem is
// still the validated version that was added. The settings that
// changed are recorded in the audit log in the same transaction.
func (s *Service) Update(ctx context.Context, id int64, e Edit, by Actor) (*Contest, error) {
	if errs := e.check(); errs != nil {
		return nil, errs
	}
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.Published {
		if len(c.Problems) == 0 {
			return nil, FieldErrors{"published": "Add at least one problem before publishing."}
		}
		for _, p := range c.Problems {
			if !p.Ready {
				return nil, FieldErrors{"published": fmt.Sprintf("Problem %s changed or lost its validation since it was added. Validate it and add it again.", p.Label)}
			}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// The slug was derived at create and stays, so links keep working.
	e.Slug = c.Slug
	langs, _ := json.Marshal(e.Languages)
	_, err = tx.ExecContext(ctx, `UPDATE contests SET title = ?, description_md = ?, starts_at = ?, ends_at = ?,
		freeze_minutes = ?, published = ?, practice_after_end = ?, min_gap_s = ?, max_per_problem = ?, languages = ?, honour_text = ?,
		hold_reveal = ?, full_screen = ?, count_focus = ?, block_paste = ?, block_copy = ?, updated_at = ? WHERE id = ?`,
		e.Title, e.Description, e.StartsAt.UnixMilli(), e.EndsAt.UnixMilli(),
		e.FreezeMinutes, e.Published, e.PracticeAfterEnd,
		e.MinGapS, e.MaxPerProblem, string(langs), e.HonourText, e.HoldReveal,
		e.Exam.FullScreen, e.Exam.CountFocus, e.Exam.BlockPaste, e.Exam.BlockCopy, s.now().UnixMilli(), id)
	if err != nil {
		return nil, err
	}
	if changes := settingChanges(c, e); len(changes) > 0 {
		if err := s.recordTx(ctx, tx, id, by, AuditSettings, map[string]any{"changes": changes}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.changed(id)
	return s.Get(ctx, id)
}

func nullID(id int64) any {
	if id > 0 {
		return id
	}
	return nil
}

func isUniqueSlug(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: contests.slug")
}

const contestColumns = `id, slug, title, description_md, starts_at, ends_at, freeze_minutes,
	published, practice_after_end, min_gap_s, max_per_problem,
	languages, honour_text, editorial_md, editorial_published, paused, extended_minutes, hold_reveal, coalesce(revealed_at, 0),
	full_screen, count_focus, block_paste, block_copy, archived,
	coalesce(created_by, 0), created_at, updated_at,
	(SELECT coalesce(max(minutes), 0) FROM contest_extensions e WHERE e.contest_id = contests.id)`

func scanContest(sc interface{ Scan(...any) error }) (*Contest, error) {
	var c Contest
	var starts, ends, revealed, created, updated int64
	var freeze, gap, extended, maxExt int
	var langs string
	err := sc.Scan(&c.ID, &c.Slug, &c.Title, &c.Description, &starts, &ends, &freeze,
		&c.Published, &c.PracticeAfterEnd, &gap,
		&c.MaxPerProblem, &langs, &c.HonourText, &c.Editorial, &c.EditorialPublished, &c.Paused, &extended, &c.HoldReveal, &revealed,
		&c.Exam.FullScreen, &c.Exam.CountFocus, &c.Exam.BlockPaste, &c.Exam.BlockCopy, &c.Archived, &c.CreatedBy, &created, &updated, &maxExt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Schedule.StartsAt, c.Schedule.EndsAt = time.UnixMilli(starts), time.UnixMilli(ends)
	c.Schedule.Freeze = time.Duration(freeze) * time.Minute
	c.Schedule.Extended = time.Duration(extended) * time.Minute
	c.MinGap = time.Duration(gap) * time.Second
	c.MaxExtension = time.Duration(maxExt) * time.Minute
	if err := json.Unmarshal([]byte(langs), &c.Languages); err != nil {
		return nil, fmt.Errorf("contest %d: languages: %w", c.ID, err)
	}
	if revealed > 0 {
		c.RevealedAt = time.UnixMilli(revealed)
	}
	c.CreatedAt, c.UpdatedAt = time.UnixMilli(created), time.UnixMilli(updated)
	return &c, nil
}

// Get loads a contest with its problems.
func (s *Service) Get(ctx context.Context, id int64) (*Contest, error) {
	c, err := scanContest(s.DB.QueryRowContext(ctx, "SELECT "+contestColumns+" FROM contests WHERE id = ?", id))
	if err != nil {
		return nil, err
	}
	return c, s.loadProblems(ctx, c)
}

// GetBySlug loads a contest by its slug.
func (s *Service) GetBySlug(ctx context.Context, slug string) (*Contest, error) {
	c, err := scanContest(s.DB.QueryRowContext(ctx, "SELECT "+contestColumns+" FROM contests WHERE slug = ?", slug))
	if err != nil {
		return nil, err
	}
	return c, s.loadProblems(ctx, c)
}

func (s *Service) loadProblems(ctx context.Context, c *Contest) error {
	return s.loadProblemsOf(ctx, []*Contest{c})
}

// loadProblemsOf fills in the problems of several contests with one query.
// Points are the subtask points pinned with each problem, so an edit to a
// problem never changes a contest's board until it is added again.
func (s *Service) loadProblemsOf(ctx context.Context, list []*Contest) error {
	if len(list) == 0 {
		return nil
	}
	byID := map[int64]*Contest{}
	ids := make([]int64, 0, len(list))
	for _, c := range list {
		c.Problems = []ContestProblem{}
		byID[c.ID] = c
		ids = append(ids, c.ID)
	}
	idsJSON, _ := json.Marshal(ids)
	rows, err := s.DB.QueryContext(ctx, `SELECT cp.contest_id, cp.label, cp.problem_id, coalesce(r.title, p.title), coalesce(r.content_hash, ''), cp.points,
		coalesce(coalesce(p.validated_hash, '') = r.content_hash AND p.content_hash = r.content_hash, 0),
		coalesce(r.id, 0), coalesce(r.rev, 0), coalesce(r.reconstructed, 0), coalesce(l.id, 0), coalesce(l.rev, 0),
		l.id IS NOT NULL AND l.reconstructed = 0 AND coalesce(p.validated_hash, '') = l.content_hash AND p.content_hash = l.content_hash,
		coalesce(l.id IS NOT NULL AND l.content_hash = r.content_hash, 0)
		FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id
		LEFT JOIN problem_revisions r ON r.id = cp.revision_id
		LEFT JOIN problem_revisions l ON l.problem_id = cp.problem_id
			AND l.rev = (SELECT max(m.rev) FROM problem_revisions m WHERE m.problem_id = cp.problem_id)
		WHERE cp.contest_id IN (SELECT value FROM json_each(?)) ORDER BY cp.contest_id, cp.ord`, string(idsJSON))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p ContestProblem
		var cid int64
		var points string
		if err := rows.Scan(&cid, &p.Label, &p.ProblemID, &p.Title, &p.PinnedHash, &points, &p.Ready,
			&p.RevisionID, &p.Rev, &p.Reconstructed, &p.LatestRevisionID, &p.LatestRev, &p.LatestReady, &p.LatestStatementOnly); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(points), &p.Points); err != nil {
			return fmt.Errorf("contest %d problem %s: points: %w", cid, p.Label, err)
		}
		if c := byID[cid]; c != nil {
			c.Problems = append(c.Problems, p)
		}
	}
	return rows.Err()
}

// List returns contests, newest start first, with their problems: two
// queries however many contests there are.
func (s *Service) List(ctx context.Context, archived bool) ([]*Contest, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+contestColumns+" FROM contests WHERE archived = 0 OR ? ORDER BY starts_at DESC, id DESC", archived)
	if err != nil {
		return nil, err
	}
	var out []*Contest
	for rows.Next() {
		c, err := scanContest(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.loadProblemsOf(ctx, out)
}

// ProblemChoice is one row of the problem picker.
type ProblemChoice struct {
	ProblemID int64
	Label     string
	// AIReason is an admin's reason to grade a problem the AI wizard made
	// when no person has written a solution for it. It is recorded in the
	// audit log; once recorded for this contest it is not asked again.
	AIReason string
}

var labelPattern = regexp.MustCompile(`^[A-Z][0-9]?$`)

// pin is one problem as the audit log records a problem change.
type pin struct {
	Label      string `json:"label"`
	ProblemID  int64  `json:"problem_id"`
	Hash       string `json:"pinned_hash"`
	RevisionID int64  `json:"revision_id"`
	Rev        int    `json:"rev"`
}

// SetProblems replaces the contest's problems for by. Each must be
// validated; its current content hash and newest revision are pinned, and
// every label whose problem or revision changed gets a pin history row. A
// problem the AI wizard made needs a person's solution (problems saved with
// a changed reference or brute force) or an admin's reason (AIReason),
// else the whole change is refused with AIProblemError. The change, and
// each new reason, is recorded in the audit log in the same transaction.
func (s *Service) SetProblems(ctx context.Context, id int64, choices []ProblemChoice, by Actor) (*Contest, error) {
	if len(choices) > 26 {
		return nil, FieldErrors{"problems": "Use at most 26 problems."}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var published, practice bool
	var startsAt, endsAt int64
	err = tx.QueryRowContext(ctx, "SELECT published, practice_after_end, starts_at, ends_at FROM contests WHERE id = ?", id).
		Scan(&published, &practice, &startsAt, &endsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Once students could see hidden tests here (practice after the end),
	// its problems stay: the bank marks a problem as shown from the
	// contests that hold it, and taking it out would hide that
	// before an exam.
	nowMs := s.now().UnixMilli()
	shown := published && practice && endsAt <= nowMs
	var before []pin
	rows, err := tx.QueryContext(ctx, `SELECT cp.label, cp.problem_id, coalesce(r.content_hash, ''), coalesce(cp.revision_id, 0), coalesce(r.rev, 0)
		FROM contest_problems cp LEFT JOIN problem_revisions r ON r.id = cp.revision_id WHERE cp.contest_id = ? ORDER BY cp.ord`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p pin
		if err := rows.Scan(&p.Label, &p.ProblemID, &p.Hash, &p.RevisionID, &p.Rev); err != nil {
			rows.Close()
			return nil, err
		}
		before = append(before, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if shown {
		kept := map[int64]bool{}
		for _, ch := range choices {
			kept[ch.ProblemID] = true
		}
		for _, p := range before {
			if !kept[p.ProblemID] {
				return nil, FieldErrors{"problems": fmt.Sprintf("Problem %s stays in this contest: students have seen its hidden tests here in practice, and the problem bank shows that from this contest.", p.Label)}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM contest_problems WHERE contest_id = ?", id); err != nil {
		return nil, err
	}
	after := []pin{}
	var unconfirmed []string
	type confirmation struct {
		label     string
		problemID int64
		reason    string
	}
	var confirmations []confirmation
	seenLabel, seenProblem := map[string]bool{}, map[int64]bool{}
	for i, ch := range choices {
		if !labelPattern.MatchString(ch.Label) || seenLabel[ch.Label] {
			return nil, FieldErrors{"problems": fmt.Sprintf("Label %q must be a capital letter, used once.", ch.Label)}
		}
		if seenProblem[ch.ProblemID] {
			return nil, FieldErrors{"problems": "A problem appears twice."}
		}
		seenLabel[ch.Label], seenProblem[ch.ProblemID] = true, true
		var hash, tags string
		var validated sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT content_hash, validated_hash, tags FROM problems WHERE id = ? AND archived = 0", ch.ProblemID).
			Scan(&hash, &validated, &tags)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, FieldErrors{"problems": fmt.Sprintf("The problem for label %s no longer exists.", ch.Label)}
		}
		if err != nil {
			return nil, err
		}
		// The message names the label the organiser chose, never the
		// problem's title, so ids cannot be used to list the bank.
		if validated.String != hash {
			return nil, FieldErrors{"problems": fmt.Sprintf("The problem for label %s is not validated. Validate it before adding it to a contest.", ch.Label)}
		}
		// The wizard's chain can validate a draft on its own; a person
		// must still read and save it before students can see it.
		// Unreadable tags are an error, so the guard never fails open.
		var tagList []string
		if err := json.Unmarshal([]byte(tags), &tagList); err != nil {
			return nil, fmt.Errorf("problem %d tags: %w", ch.ProblemID, err)
		}
		for _, t := range tagList {
			if t == problems.AIDraftTag {
				return nil, FieldErrors{"problems": fmt.Sprintf("The problem for label %s is an AI draft. Read it and save it in the editor before adding it to a contest.", ch.Label)}
			}
		}
		needs, err := unconfirmedAI(ctx, tx, id, ch.ProblemID)
		if err != nil {
			return nil, err
		}
		reason := strings.TrimSpace(ch.AIReason)
		switch {
		case !needs:
		case utf8.RuneCountInString(reason) >= minAIReason:
			confirmations = append(confirmations, confirmation{ch.Label, ch.ProblemID, limitStr(reason, 1000)})
		default:
			unconfirmed = append(unconfirmed, ch.Label)
		}
		points, err := subtaskPoints(ctx, tx, ch.ProblemID)
		if err != nil {
			return nil, err
		}
		// The newest revision records the working copy (every save writes
		// one), so it is the revision with this content hash.
		var revID int64
		var rev int
		err = tx.QueryRowContext(ctx, `SELECT id, rev FROM problem_revisions WHERE problem_id = ? AND content_hash = ? AND reconstructed = 0
			ORDER BY rev DESC LIMIT 1`, ch.ProblemID, hash).Scan(&revID, &rev)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("problem %d has no revision for its content", ch.ProblemID)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO contest_problems (contest_id, problem_id, label, ord, points, revision_id)
			VALUES (?, ?, ?, ?, ?, ?)`, id, ch.ProblemID, ch.Label, i, points, revID); err != nil {
			return nil, err
		}
		after = append(after, pin{ch.Label, ch.ProblemID, hash, revID, rev})
	}
	if len(unconfirmed) > 0 {
		return nil, &AIProblemError{Labels: unconfirmed}
	}
	for _, cf := range confirmations {
		if err := s.recordTx(ctx, tx, id, by, AuditAIProblem, map[string]any{"label": cf.label, "problem_id": cf.problemID,
			"reason": cf.reason}); err != nil {
			return nil, err
		}
	}
	if !slices.Equal(before, after) {
		if err := s.recordTx(ctx, tx, id, by, AuditProblems, map[string]any{"from": nonNilPins(before), "to": after}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.changed(id)
	return s.Get(ctx, id)
}

func nonNilPins(p []pin) []pin {
	if p == nil {
		return []pin{}
	}
	return p
}

// subtaskPoints returns a problem's subtask points as a JSON array, for
// pinning with the problem.
func subtaskPoints(ctx context.Context, tx *sql.Tx, problemID int64) (string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT points FROM subtasks WHERE problem_id = ? ORDER BY idx", problemID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return "", err
		}
		out = append(out, p)
	}
	b, _ := json.Marshal(out)
	return string(b), rows.Err()
}

// SubmissionCounts returns how many submissions a student made per problem
// id in the contest, practice and judge failures excluded. It counts rows
// by creation and never looks at how far judging got, so in exam mode it
// says nothing about hidden results.
func (s *Service) SubmissionCounts(ctx context.Context, c *Contest, userID int64) (map[int64]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT problem_id, count(*) FROM submissions
		WHERE contest_id = ? AND user_id = ? AND kind = 'submit' AND practice = 0 AND state <> 'error' GROUP BY problem_id`, c.ID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var pid int64
		var n int
		if err := rows.Scan(&pid, &n); err != nil {
			return nil, err
		}
		out[pid] = n
	}
	return out, rows.Err()
}

func limitStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// SetExtension gives one student extra time (0 removes it), audited for
// by. The student is named by id only: the audit log outlives an erasure
// (forget-user rewrites no audit row).
func (s *Service) SetExtension(ctx context.Context, id, userID int64, minutes int, reason string, by Actor) error {
	if minutes < 0 || minutes > 1440 {
		return FieldErrors{"minutes": "Give from 0 to 1440 extra minutes."}
	}
	err := s.audited(ctx, id, by, AuditExtension, func(tx *sql.Tx) (any, bool, error) {
		var from int
		if err := tx.QueryRowContext(ctx, "SELECT coalesce((SELECT minutes FROM contest_extensions WHERE contest_id = ? AND user_id = ?), 0)",
			id, userID).Scan(&from); err != nil {
			return nil, false, err
		}
		var err error
		if minutes == 0 {
			_, err = tx.ExecContext(ctx, "DELETE FROM contest_extensions WHERE contest_id = ? AND user_id = ?", id, userID)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO contest_extensions (contest_id, user_id, minutes, reason) VALUES (?, ?, ?, ?)
				ON CONFLICT (contest_id, user_id) DO UPDATE SET minutes = excluded.minutes, reason = excluded.reason`,
				id, userID, minutes, limitStr(reason, 200))
		}
		return map[string]any{"user_id": userID, "from_minutes": from, "minutes": minutes, "reason_given": strings.TrimSpace(reason) != ""}, true, err
	})
	if err == nil {
		s.changed(id)
	}
	return err
}

// Extend adds minutes to everyone's end, audited for by. Once the contest
// has closed it refuses with ErrExtendEnded (walk bug B3): the check and
// the change are one statement, so an extend sent just after the end
// cannot slip through.
func (s *Service) Extend(ctx context.Context, id int64, minutes int, by Actor) error {
	if minutes < 1 || minutes > 240 {
		return FieldErrors{"minutes": "Extend by 1 to 240 minutes."}
	}
	err := s.audited(ctx, id, by, AuditExtend, func(tx *sql.Tx) (any, bool, error) {
		var total int
		now := s.now().UnixMilli()
		err := tx.QueryRowContext(ctx, `UPDATE contests SET extended_minutes = extended_minutes + ?, updated_at = ?
			WHERE id = ? AND ends_at + extended_minutes * 60000 > ? RETURNING extended_minutes`,
			minutes, now, id, now).Scan(&total)
		if errors.Is(err, sql.ErrNoRows) {
			var one int
			if tx.QueryRowContext(ctx, "SELECT 1 FROM contests WHERE id = ?", id).Scan(&one) == nil {
				return nil, false, ErrExtendEnded
			}
			return nil, false, ErrNotFound
		}
		return map[string]int{"minutes": minutes, "extended_minutes": total}, true, err
	})
	if err == nil {
		s.changed(id)
	}
	return err
}

// SetPaused pauses or resumes judging of the contest's submissions,
// audited for by.
func (s *Service) SetPaused(ctx context.Context, id int64, paused bool, by Actor) error {
	action := AuditResume
	if paused {
		action = AuditPause
	}
	return s.audited(ctx, id, by, action, func(tx *sql.Tx) (any, bool, error) {
		res, err := tx.ExecContext(ctx, "UPDATE contests SET paused = ?, updated_at = ? WHERE id = ?", paused, s.now().UnixMilli(), id)
		if err != nil {
			return nil, false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, false, ErrNotFound
		}
		return nil, true, nil
	})
}

// extension returns a student's own extra time.
func (s *Service) extension(ctx context.Context, id, userID int64) (time.Duration, error) {
	var m int
	err := s.DB.QueryRowContext(ctx, "SELECT minutes FROM contest_extensions WHERE contest_id = ? AND user_id = ?", id, userID).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return time.Duration(m) * time.Minute, err
}

// Viewer is the person asking.
type Viewer struct {
	UserID    int64
	Email     string
	Organiser bool // owner or admin
}

// MayEnter reports whether a viewer may open the contest (as a student, or
// as an organiser).
func (s *Service) MayEnter(ctx context.Context, c *Contest, v Viewer) (organiser bool, err error) {
	if v.Organiser {
		return true, nil
	}
	if !c.Published || c.Archived {
		return false, ErrNotFound
	}
	return false, nil
}

// StudentState is one student's place in a contest.
type StudentState struct {
	Accepted bool
	Started  time.Time
	Window   Window
	HasClock bool // always true since the start window was cut; kept for the handlers
	Open     bool // may submit for the score now
	Over     bool // their window has ended
	// Extension is the student's own extra time.
	Extension time.Duration
}

// State returns a student's place in the contest at now.
func (s *Service) State(ctx context.Context, c *Contest, userID int64) (StudentState, error) {
	var st StudentState
	var started, accepted int64
	err := s.DB.QueryRowContext(ctx, "SELECT started_at, honour_accepted_at FROM contest_starts WHERE contest_id = ? AND user_id = ?", c.ID, userID).
		Scan(&started, &accepted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return st, err
	}
	if err == nil {
		st.Accepted, st.Started = true, time.UnixMilli(started)
	}
	ext, err := s.extension(ctx, c.ID, userID)
	if err != nil {
		return st, err
	}
	st.Extension = ext
	st.Window, st.HasClock = c.Schedule.StudentWindow(ext), true
	now := s.now()
	st.Over = !now.Before(st.Window.Ends)
	st.Open = st.Accepted && !now.Before(st.Window.Opens) && !st.Over
	return st, nil
}

// Start records the student's acceptance of the honour code. Starting
// twice is harmless.
func (s *Service) Start(ctx context.Context, c *Contest, userID int64, accept bool) error {
	if !accept {
		return FieldErrors{"accept_honour": "Read the rules and agree to them to start."}
	}
	now := s.now()
	if now.Before(c.Schedule.StartsAt) {
		return FieldErrors{"accept_honour": "The contest has not started yet."}
	}
	// Starting after the end would only add an empty row to the board;
	// the problems are open for reading (and practice) without it.
	ext, err := s.extension(ctx, c.ID, userID)
	if err != nil {
		return err
	}
	if !now.Before(c.Schedule.Close().Add(ext)) {
		return FieldErrors{"accept_honour": "The contest has ended."}
	}
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO contest_starts (contest_id, user_id, started_at, honour_accepted_at) VALUES (?, ?, ?, ?)`,
		c.ID, userID, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.changed(c.ID) // a new participant on the board
	}
	return nil
}

// PinnedInOpenContest reports whether a contest other than except pins
// the problem and has not closed for everyone yet (personal extensions
// included), drafts and upcoming contests too. Practice then keeps the
// problem's hidden tests to itself.
func (s *Service) PinnedInOpenContest(ctx context.Context, problemID, except int64) (bool, error) {
	var pending bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM contest_problems cp JOIN contests c ON c.id = cp.contest_id
		JOIN contest_final f ON f.contest_id = c.id
		WHERE cp.problem_id = ? AND c.id != ? AND c.archived = 0 AND f.final_at > ?)`,
		problemID, except, s.now().UnixMilli()).Scan(&pending)
	return pending, err
}
