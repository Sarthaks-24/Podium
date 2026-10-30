package contests

// The contest audit log (migration 0008): one
// append-only row for every organiser action that can change a contest's
// results, so a later dispute can be settled from the database rather than
// from journald.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Audit actions.
const (
	AuditSettings  = "settings"   // the contest form: {"changes": {field: {"from", "to"}}}
	AuditProblems  = "problems"   // problems and pins: {"from": [...], "to": [...]}
	AuditAIProblem = "ai_problem" // an admin's reason to grade an AI problem
	AuditExtension = "extension"  // {"user_id", "minutes", "from_minutes", "reason_given"} ("email" and "reason" in older rows)
	AuditExtend    = "extend"     // {"minutes", "extended_minutes"}
	AuditPause     = "pause"
	AuditResume    = "resume"
	AuditRejudge   = "rejudge" // {"problem_label" or "submission_id", "queued"}
	AuditReveal    = "reveal"
	AuditEditorial = "editorial" // {"published", "was_published", "changed", "bytes"}
	AuditArchive   = "archive"
	// AuditDecision is an integrity decision about one student:
	// {"user_id", "decision"}; the reason stays in integrity_cases.
	AuditDecision = "integrity_decision"
)

// Actor is the person an audit row names.
type Actor struct {
	ID    int64  `json:"user_id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// AuditEntry is one row of the audit log.
type AuditEntry struct {
	ID        int64           `json:"id"`
	Action    string          `json:"action"`
	Actor     Actor           `json:"actor"`
	Details   json.RawMessage `json:"details"`
	CreatedAt int64           `json:"created_at"`
}

// execer is a database or a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RecordTx is Record inside the caller's transaction, stamped now (Unix
// milliseconds), so the audit row commits or rolls back with the change.
func RecordTx(ctx context.Context, tx *sql.Tx, contestID int64, a Actor, action string, details any, now int64) error {
	return recordAt(ctx, tx, contestID, a, action, details, now)
}

// recordAt writes one audit row through ex, stamped now.
func recordAt(ctx context.Context, ex execer, contestID int64, a Actor, action string, details any, now int64) error {
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("audit details: %w", err)
	}
	_, err = ex.ExecContext(ctx, `INSERT INTO contest_audit (contest_id, actor_id, actor_email, actor_role, action, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, contestID, a.ID, a.Email, a.Role, action, string(b), now)
	if err != nil {
		return fmt.Errorf("record %s in the audit log: %w", action, err)
	}
	return nil
}

// audited runs change and its audit row in one transaction, so the row
// commits or rolls back with the change. change returns the row's
// details and whether to write one at all.
func (s *Service) audited(ctx context.Context, contestID int64, by Actor, action string, change func(tx *sql.Tx) (details any, record bool, err error)) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	details, record, err := change(tx)
	if err != nil {
		return err
	}
	if record {
		if err := s.recordTx(ctx, tx, contestID, by, action, details); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) recordTx(ctx context.Context, ex execer, contestID int64, a Actor, action string, details any) error {
	return recordAt(ctx, ex, contestID, a, action, details, s.now().UnixMilli())
}

// change is one setting's old and new value in a settings row.
type change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// settingChanges lists the form fields an edit changes, by their API names,
// with times in Unix milliseconds. Long text (description, honour code) is
// recorded as changed without its text.
func settingChanges(c *Contest, e Edit) map[string]change {
	out := map[string]change{}
	add := func(field string, from, to any) {
		if !reflect.DeepEqual(from, to) {
			out[field] = change{from, to}
		}
	}
	add("slug", c.Slug, e.Slug)
	add("title", c.Title, e.Title)
	if c.Description != e.Description {
		out["description_md"] = change{fmt.Sprintf("%d bytes", len(c.Description)), fmt.Sprintf("%d bytes", len(e.Description))}
	}
	if c.HonourText != e.HonourText {
		out["honour_text"] = change{fmt.Sprintf("%d bytes", len(c.HonourText)), fmt.Sprintf("%d bytes", len(e.HonourText))}
	}
	add("starts_at", c.Schedule.StartsAt.UnixMilli(), e.StartsAt.UnixMilli())
	add("ends_at", c.Schedule.EndsAt.UnixMilli(), e.EndsAt.UnixMilli())
	add("freeze_minutes", int(c.Schedule.Freeze/time.Minute), e.FreezeMinutes)
	add("published", c.Published, e.Published)
	add("practice_after_end", c.PracticeAfterEnd, e.PracticeAfterEnd)
	add("min_gap_s", int(c.MinGap/time.Second), e.MinGapS)
	add("max_per_problem", c.MaxPerProblem, e.MaxPerProblem)
	add("languages", nonNilStrings(c.Languages), nonNilStrings(e.Languages))
	add("hold_reveal", c.HoldReveal, e.HoldReveal)
	add("full_screen", c.Exam.FullScreen, e.Exam.FullScreen)
	add("count_focus", c.Exam.CountFocus, e.Exam.CountFocus)
	add("block_paste", c.Exam.BlockPaste, e.Exam.BlockPaste)
	add("block_copy", c.Exam.BlockCopy, e.Exam.BlockCopy)
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// AuditAll lists every audit row of the contest, oldest first, for the
// JSON export. noIntegrity leaves out the integrity rows, as AuditFilter.
func (s *Service) AuditAll(ctx context.Context, contestID int64) ([]AuditEntry, error) {
	var out []AuditEntry
	for page := 1; ; page++ {
		list, more, err := s.AuditLog(ctx, contestID, page, AuditFilter{})
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
		if !more {
			break
		}
	}
	slices.Reverse(out)
	if out == nil {
		out = []AuditEntry{}
	}
	return out, nil
}

// AuditPage is how many rows one page of the audit log holds.
const AuditPage = 200

// AuditFilter narrows the console's Changes feed: one action, and
// actors whose email contains Actor (case insensitive).
type AuditFilter struct {
	Action, Actor string
}

// AuditLog lists the contest's audit rows, newest first, a page at a time
// (from 1), and reports whether there are more.
func (s *Service) AuditLog(ctx context.Context, contestID int64, page int, f AuditFilter) ([]AuditEntry, bool, error) {
	if page < 1 {
		page = 1
	}
	q := `SELECT id, action, actor_id, actor_email, actor_role, details, created_at FROM contest_audit WHERE contest_id = ?`
	args := []any{contestID}
	if f.Action != "" {
		q += " AND action = ?"
		args = append(args, f.Action)
	}
	if f.Actor != "" {
		q += " AND instr(lower(actor_email), ?) > 0"
		args = append(args, strings.ToLower(f.Actor))
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, AuditPage+1, (page-1)*AuditPage)...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var details string
		if err := rows.Scan(&e.ID, &e.Action, &e.Actor.ID, &e.Actor.Email, &e.Actor.Role, &details, &e.CreatedAt); err != nil {
			return nil, false, err
		}
		e.Details = json.RawMessage(details)
		out = append(out, e)
	}
	more := len(out) > AuditPage
	if more {
		out = out[:AuditPage]
	}
	return out, more, rows.Err()
}
