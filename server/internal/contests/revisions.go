package contests

// Pins by revision (v2 plan B2.3 and B2.6, decision D61). A contest pins a
// problem revision per label; the workspace serves that revision's title,
// statement and samples, and the pin history records every change of it,
// so "what did X see at T" is answered from data. Students see only the
// statements of revisions pinned to a contest they are in, as a line
// diff; organisers see the pin history on the contest.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Sarthaks-24/podium/internal/problems"
)

// ErrRevisionNotReady refuses a revision that is not the validated
// content of its problem now.
var ErrRevisionNotReady = errors.New("revision not validated")

// ErrSamePin refuses pinning the revision already pinned.
var ErrSamePin = errors.New("revision already pinned")

// EventStatement tells a contest's members that a label now pins another
// revision (C3).
const EventStatement = "statement"

// AuditRevision is the audit action of "Update contest to revision N".
const AuditRevision = "revision"

// PinChange is one row of a contest's pin history.
type PinChange struct {
	Label      string `json:"label"`
	ProblemID  int64  `json:"problem_id"`
	RevisionID int64  `json:"revision_id"`
	Rev        int    `json:"rev"`
	FromAt     int64  `json:"from_at"`
	// Actor is nil for the pins the upgrade to v1.2.0 mapped.
	Actor *Actor `json:"actor"`
}

// pinHistoryLimit bounds the pin history on the contest response.
const pinHistoryLimit = 200

// PinHistory lists a contest's pin changes, newest first, at most 200.
func (s *Service) PinHistory(ctx context.Context, contestID int64) ([]PinChange, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT h.label, h.problem_id, h.revision_id, r.rev, h.from_at, coalesce(h.actor_id, 0), h.actor_email, h.actor_role
		FROM contest_pin_history h JOIN problem_revisions r ON r.id = h.revision_id
		WHERE h.contest_id = ? ORDER BY h.from_at DESC, h.id DESC LIMIT ?`, contestID, pinHistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PinChange{}
	for rows.Next() {
		var p PinChange
		var a Actor
		if err := rows.Scan(&p.Label, &p.ProblemID, &p.RevisionID, &p.Rev, &p.FromAt, &a.ID, &a.Email, &a.Role); err != nil {
			return nil, err
		}
		if a.ID > 0 {
			p.Actor = &a
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// recordPinTx appends a pin change.
func (s *Service) recordPinTx(ctx context.Context, ex execer, contestID int64, label string, problemID, revisionID int64, by Actor, at time.Time) error {
	var actor any
	if by.ID > 0 {
		actor = by.ID
	}
	_, err := ex.ExecContext(ctx, `INSERT INTO contest_pin_history (contest_id, label, problem_id, revision_id, from_at, actor_id, actor_email, actor_role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, contestID, label, problemID, revisionID, at.UnixMilli(), actor, by.Email, by.Role)
	if err != nil {
		return fmt.Errorf("record pin of %s: %w", label, err)
	}
	return nil
}

// PinUpdate is what "Update contest to revision N" changed.
type PinUpdate struct {
	Label                     string
	From, To                  *problems.Revision
	StatementChanged          bool
	JudgingChanged            bool
	PointsChanged             bool
	At                        time.Time
	AnnouncementMD            string
	ProblemID, FromRevisionID int64
}

// UpdatePin moves a label of the contest to another revision of its
// problem, for by: the revision must be the validated content of the
// problem now (its content hash equals the problem's, and a passing
// validation covers it). The pin, pinned_hash and the subtask points
// change in one transaction with a pin history row and an audit row.
// Students get a statement event.
func (s *Service) UpdatePin(ctx context.Context, contestID int64, label string, revisionID int64, by Actor) (*PinUpdate, error) {
	c, err := s.Get(ctx, contestID)
	if err != nil {
		return nil, err
	}
	var cp *ContestProblem
	for i := range c.Problems {
		if c.Problems[i].Label == label {
			cp = &c.Problems[i]
		}
	}
	if cp == nil {
		return nil, ErrNotFound
	}
	to, err := s.Problems.RevisionByID(ctx, revisionID)
	if errors.Is(err, problems.ErrNotFound) || (err == nil && to.ProblemID != cp.ProblemID) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if to.ID == cp.RevisionID {
		return nil, ErrSamePin
	}
	var from *problems.Revision
	if cp.RevisionID > 0 {
		if from, err = s.Problems.RevisionByID(ctx, cp.RevisionID); err != nil {
			return nil, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var hash string
	var validated sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT content_hash, validated_hash FROM problems WHERE id = ?", cp.ProblemID).Scan(&hash, &validated); err != nil {
		return nil, err
	}
	if to.Reconstructed || to.ContentHash != hash || validated.String != hash {
		return nil, fmt.Errorf("%w: revision %d", ErrRevisionNotReady, to.Rev)
	}
	points := make([]int, 0, len(to.Subtasks))
	for _, st := range to.Subtasks {
		points = append(points, st.Points)
	}
	pointsJSON, _ := json.Marshal(points)
	now := s.now()
	if _, err := tx.ExecContext(ctx, `UPDATE contest_problems SET revision_id = ?, pinned_hash = ?, points = ? WHERE contest_id = ? AND label = ?`,
		to.ID, to.ContentHash, string(pointsJSON), contestID, label); err != nil {
		return nil, err
	}
	if err := s.recordPinTx(ctx, tx, contestID, label, cp.ProblemID, to.ID, by, now); err != nil {
		return nil, err
	}
	u := &PinUpdate{Label: label, From: from, To: to, At: now, ProblemID: cp.ProblemID, FromRevisionID: cp.RevisionID,
		PointsChanged: !slices.Equal(points, cp.Points)}
	if from != nil {
		u.StatementChanged = problems.StatementChanged(from, to)
		u.JudgingChanged = from.ContentHash != to.ContentHash
	} else {
		u.StatementChanged, u.JudgingChanged = true, true
	}
	details := map[string]any{"label": label, "problem_id": cp.ProblemID, "from_revision_id": nil, "from_rev": nil,
		"to_revision_id": to.ID, "to_rev": to.Rev, "statement_changed": u.StatementChanged, "judging_changed": u.JudgingChanged}
	if from != nil {
		details["from_revision_id"], details["from_rev"] = from.ID, from.Rev
	}
	if err := s.recordTx(ctx, tx, contestID, by, AuditRevision, details); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	u.AnnouncementMD = announcementFor(label, u)
	if u.PointsChanged || u.JudgingChanged {
		s.changed(contestID)
	}
	if c.Published {
		s.publish(ctx, c, false, EventStatement, map[string]any{"contest": c.Slug, "contest_id": c.ID, "label": label,
			"revision_id": to.ID, "statement_changed": u.StatementChanged, "pinned_at": now.UnixMilli()})
	}
	return u, nil
}

// announcementFor is the prefilled announcement the console offers after
// a pin update. It is never posted by itself.
func announcementFor(label string, u *PinUpdate) string {
	if u.JudgingChanged {
		return fmt.Sprintf("Problem %s was updated: its tests or limits changed, so submissions on it will be judged again. Your score on it may change once judging finishes.", label)
	}
	return fmt.Sprintf("The statement of problem %s was updated. Open the problem and choose \"See what changed\".", label)
}

// StatementEntry is one revision pinned to a contest label, as students
// see the statement history.
type StatementEntry struct {
	RevisionID        int64  `json:"revision_id"`
	Rev               int    `json:"rev"`
	PinnedAt          int64  `json:"pinned_at"`
	Title             string `json:"title"`
	StatementChanged  bool   `json:"statement_changed"`
	StatementRecorded bool   `json:"statement_recorded"`
	revision          *problems.Revision
}

// Revision returns the revision behind an entry (with its statement).
func (e StatementEntry) Revision() *problems.Revision { return e.revision }

// pinRow is one pin history row with its revision.
type pinRow struct {
	label  string
	fromAt int64
	rev    *problems.Revision
}

// pinRows reads a contest's pin history, oldest first, with revisions.
func (s *Service) pinRows(ctx context.Context, contestID int64, label string) ([]pinRow, error) {
	q := `SELECT h.label, h.from_at, h.revision_id FROM contest_pin_history h WHERE h.contest_id = ?`
	args := []any{contestID}
	if label != "" {
		q += " AND h.label = ?"
		args = append(args, label)
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY h.label, h.from_at, h.id", args...)
	if err != nil {
		return nil, err
	}
	type raw struct {
		label  string
		fromAt int64
		revID  int64
	}
	var list []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.label, &r.fromAt, &r.revID); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cache := map[int64]*problems.Revision{}
	out := make([]pinRow, 0, len(list))
	for _, r := range list {
		rev, ok := cache[r.revID]
		if !ok {
			if rev, err = s.Problems.RevisionByID(ctx, r.revID); err != nil {
				return nil, err
			}
			cache[r.revID] = rev
		}
		out = append(out, pinRow{r.label, r.fromAt, rev})
	}
	return out, nil
}

// StatementHistory lists the revisions pinned to a label, newest first:
// for students (all false) the one in force when the contest started and
// every later pin, for organisers every pin. Each revision appears once,
// at the time it was last pinned.
func (s *Service) StatementHistory(ctx context.Context, c *Contest, label string, all bool) ([]StatementEntry, error) {
	rows, err := s.pinRows(ctx, c.ID, label)
	if err != nil {
		return nil, err
	}
	start := c.Schedule.StartsAt.UnixMilli()
	if !all {
		// Keep the last pin made before the start and everything after.
		first := 0
		for i, r := range rows {
			if r.fromAt <= start {
				first = i
			}
		}
		rows = rows[first:]
	}
	var entries []StatementEntry
	for _, r := range rows {
		entries = slices.DeleteFunc(entries, func(e StatementEntry) bool { return e.RevisionID == r.rev.ID })
		entries = append(entries, StatementEntry{RevisionID: r.rev.ID, Rev: r.rev.Rev, PinnedAt: r.fromAt, Title: r.rev.Title,
			StatementRecorded: r.rev.StatementRecorded, revision: r.rev})
	}
	for i := range entries {
		if i > 0 {
			entries[i].StatementChanged = problems.StatementChanged(entries[i-1].revision, entries[i].revision)
		}
	}
	slices.Reverse(entries)
	if entries == nil {
		entries = []StatementEntry{}
	}
	return entries, nil
}

// StatementUpdates returns, per label, the time of the newest pin change
// made after the contest started that changed the title or statement.
// Labels without one are absent.
func (s *Service) StatementUpdates(ctx context.Context, c *Contest) (map[string]int64, error) {
	rows, err := s.pinRows(ctx, c.ID, "")
	if err != nil {
		return nil, err
	}
	start := c.Schedule.StartsAt.UnixMilli()
	out := map[string]int64{}
	for i, r := range rows {
		if i == 0 || rows[i-1].label != r.label || r.fromAt <= start {
			continue
		}
		if problems.StatementChanged(rows[i-1].rev, r.rev) {
			out[r.label] = r.fromAt
		}
	}
	return out, nil
}

// Seen is what a student had been shown of a contest problem at a moment:
// the revision pinned then, and when the student was first served it
// (zero when they had not been).
type Seen struct {
	Label       string
	PinnedRev   *problems.Revision
	PinnedSince time.Time
	FirstSeenAt time.Time
	// Served lists every revision of the label the student was served up
	// to the moment, oldest first.
	Served []ServedRevision
}

// ServedRevision is one served log row.
type ServedRevision struct {
	RevisionID  int64
	Rev         int
	FirstSeenAt time.Time
}

// SeenAt answers "what did the student see of this label at t" from the
// pin history and the served log (B2.3, B2.4).
func (s *Service) SeenAt(ctx context.Context, contestID, userID int64, label string, t time.Time) (*Seen, error) {
	rows, err := s.pinRows(ctx, contestID, label)
	if err != nil {
		return nil, err
	}
	out := &Seen{Label: label}
	for _, r := range rows {
		if r.fromAt <= t.UnixMilli() {
			out.PinnedRev, out.PinnedSince = r.rev, time.UnixMilli(r.fromAt)
		}
	}
	if out.PinnedRev == nil {
		return out, nil
	}
	revs := map[int64]int{}
	for _, r := range rows {
		revs[r.rev.ID] = r.rev.Rev
	}
	q, err := s.DB.QueryContext(ctx, `SELECT revision_id, first_seen_at FROM revision_views WHERE user_id = ? AND contest_id = ? AND first_seen_at <= ?
		ORDER BY first_seen_at`, userID, contestID, t.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer q.Close()
	for q.Next() {
		var id, at int64
		if err := q.Scan(&id, &at); err != nil {
			return nil, err
		}
		if rev, ok := revs[id]; ok {
			out.Served = append(out.Served, ServedRevision{id, rev, time.UnixMilli(at)})
			if id == out.PinnedRev.ID {
				out.FirstSeenAt = time.UnixMilli(at)
			}
		}
	}
	return out, q.Err()
}
