package contests

// Pins by revision. A contest pins a problem revision per
// label; the workspace serves that revision's title, statement and
// samples. "Update students now" moves a pin to the problem's newest
// validated revision, writes an audit row, and offers the organiser an
// announcement to post (the DOMjudge way: edit, validate, update,
// announce, rejudge).

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

// ErrPinMoved refuses an update when another one moved the same label's
// pin after this one read it: the "from" side of the record would be
// wrong.
var ErrPinMoved = errors.New("the pin changed since it was read")

// EventStatement tells a contest's members that a label now pins another
// revision.
const EventStatement = "statement"

// AuditRevision is the audit action of "Update students now".
const AuditRevision = "revision"

// PinUpdate is what "Update students now" changed.
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
// validation covers it). The pin and the subtask points
// change in one transaction with an audit row.
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
	return s.applyPin(ctx, c, *cp, label, from, to, by)
}

// applyPin writes the update UpdatePin read and checked, in one
// transaction that first re-reads the label's pin: one moved since cp was
// read is ErrPinMoved, so two organisers never both update from the same
// revision.
func (s *Service) applyPin(ctx context.Context, c *Contest, cp ContestProblem, label string, from, to *problems.Revision, by Actor) (*PinUpdate, error) {
	contestID := c.ID
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT revision_id FROM contest_problems WHERE contest_id = ? AND label = ?",
		contestID, label).Scan(&current); err != nil {
		return nil, err
	}
	if current.Int64 != cp.RevisionID {
		return nil, ErrPinMoved
	}
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
	if _, err := tx.ExecContext(ctx, `UPDATE contest_problems SET revision_id = ?, points = ? WHERE contest_id = ? AND label = ?`,
		to.ID, string(pointsJSON), contestID, label); err != nil {
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
	return fmt.Sprintf("The statement of problem %s was updated. Open the problem again to read the new version.", label)
}
