package retention

// The daily retention job (B3.1): it applies the periods of periods.go
// that no other job applies. Sessions (auth), integrity events and case
// evidence (contests and integrity) and backups (podium-backup) have their
// own. Each rule runs in its own transactions, in batches, so one pass
// never holds the write lock for long while a contest runs.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// batch is how many rows one transaction changes.
const batch = 2000

// Job deletes what is older than its period.
type Job struct {
	DB *sql.DB
	// Now is the clock; tests replace it. It must not run ahead of the
	// database's own clock: the append-only triggers compare cutoffs with
	// SQLite's now.
	Now func() time.Time
	Log *slog.Logger
	// ExportStandings returns a contest's final standings as JSON. The job
	// keeps them in contest_results before the contest's submissions go;
	// without it no submission is deleted.
	ExportStandings func(ctx context.Context, contestID int64) ([]byte, error)
	// Expired is told about each contest whose submissions were deleted,
	// so cached boards can be dropped. Optional.
	Expired func(contestID int64)
}

// Result counts what one pass changed.
type Result struct {
	RunsEmptied        int64 // run jobs whose payload and output were cleared
	ContactDeleted     int64
	RevisionsDeleted   int64
	ContestsExpired    int64 // contests whose submissions passed retention
	SubmissionsDeleted int64 // in those contests, and outside contests
	AuditDeleted       int64 // site and contest audit rows
}

func (j *Job) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// Run applies every rule once and returns what it changed. A rule that
// fails stops the pass; the next pass starts over, since every rule is
// idempotent.
func (j *Job) Run(ctx context.Context) (Result, error) {
	var r Result
	now := j.now()
	steps := []struct {
		name string
		do   func(context.Context, time.Time, *Result) error
	}{
		{"run payloads", j.runs},
		{"contact messages", j.contact},
		{"submissions", j.submissions},
		{"draft revisions", j.revisions},
		{"audit", j.audit},
	}
	for _, s := range steps {
		if err := s.do(ctx, now, &r); err != nil {
			return r, fmt.Errorf("retention, %s: %w", s.name, err)
		}
	}
	return r, nil
}

// exec runs one statement in its own transaction and returns the rows it
// changed.
func (j *Job) exec(ctx context.Context, q string, args ...any) (int64, error) {
	res, err := j.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// batches repeats one transaction until it changes fewer than batch rows.
func (j *Job) batches(ctx context.Context, step func(tx *sql.Tx) (int64, error)) (int64, error) {
	var total int64
	for {
		tx, err := j.DB.BeginTx(ctx, nil)
		if err != nil {
			return total, err
		}
		n, err := step(tx)
		if err == nil {
			err = tx.Commit()
		}
		tx.Rollback()
		if err != nil {
			return total, err
		}
		total += n
		if n < batch {
			return total, nil
		}
	}
}

// setCutoff moves a table's retention cutoff forward (never back), which
// lets its append-only trigger allow deleting older rows.
func setCutoff(ctx context.Context, tx *sql.Tx, table string, before int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO retention_cutoffs (tbl, before_ms) VALUES (?, ?)
		ON CONFLICT (tbl) DO UPDATE SET before_ms = excluded.before_ms WHERE excluded.before_ms > retention_cutoffs.before_ms`,
		table, before)
	return err
}

// runs empties run jobs older than RunPayloads: the payload (code and
// custom input references, so their files can be collected) and the
// output excerpts. The job row and its verdicts stay.
func (j *Job) runs(ctx context.Context, now time.Time, r *Result) error {
	cut := now.Add(-RunPayloads).UnixMilli()
	n, err := j.batches(ctx, func(tx *sql.Tx) (int64, error) {
		const pick = `SELECT id FROM jobs WHERE kind = 'run' AND payload <> '{}' AND created_at < ? AND state IN ('done', 'failed')
			ORDER BY created_at LIMIT ?`
		if _, err := tx.ExecContext(ctx, `UPDATE job_results SET output = '' WHERE output <> '' AND job_id IN (`+pick+`)`,
			cut, batch); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE jobs SET payload = '{}' WHERE id IN (`+pick+`)`, cut, batch)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
	r.RunsEmptied = n
	return err
}

func (j *Job) contact(ctx context.Context, now time.Time, r *Result) error {
	n, err := j.exec(ctx, "DELETE FROM contact_messages WHERE created_at < ?", now.Add(-ContactMessages).UnixMilli())
	r.ContactDeleted = n
	return err
}

// revisions deletes problem revisions older than DraftRevisions that
// nothing names: not the problem's newest, no contest pins or pinned it,
// no submission or job was judged against it, no student was served it.
func (j *Job) revisions(ctx context.Context, now time.Time, r *Result) error {
	cut := now.Add(-DraftRevisions).UnixMilli()
	n, err := j.batches(ctx, func(tx *sql.Tx) (int64, error) {
		if err := setCutoff(ctx, tx, "problem_revisions", cut); err != nil {
			return 0, err
		}
		const unused = `SELECT r.id FROM problem_revisions r WHERE r.created_at < ?
			AND r.rev < (SELECT max(m.rev) FROM problem_revisions m WHERE m.problem_id = r.problem_id)
			AND NOT EXISTS (SELECT 1 FROM contest_problems WHERE revision_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM contest_pin_history WHERE revision_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM submissions WHERE revision_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM jobs WHERE revision_id = r.id)
			AND NOT EXISTS (SELECT 1 FROM revision_views WHERE revision_id = r.id)
			ORDER BY r.id LIMIT ?`
		if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE IF NOT EXISTS retention_ids (id INTEGER PRIMARY KEY)"); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM temp.retention_ids"); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO temp.retention_ids "+unused, cut, batch); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM revision_tests WHERE revision_id IN (SELECT id FROM temp.retention_ids)"); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM problem_revisions WHERE id IN (SELECT id FROM temp.retention_ids)")
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	})
	r.RevisionsDeleted = n
	return err
}

// submissions deletes, Submissions after a contest ended for everyone, its
// submissions and their jobs, its pin history, what each student was
// served, and its pins' references to revisions (so the revision rule can
// collect them), after keeping the final standings in contest_results.
// Submissions outside contests go Submissions after they were made.
func (j *Job) submissions(ctx context.Context, now time.Time, r *Result) error {
	if j.ExportStandings == nil {
		return nil
	}
	cut := now.Add(-Submissions).UnixMilli()
	// A contest's final moment: its end plus the contest-wide and the
	// longest personal extension, as contests.Retain reads it.
	rows, err := j.DB.QueryContext(ctx, `SELECT c.id FROM contests c WHERE c.ends_at + 60000 * (c.extended_minutes +
		coalesce((SELECT max(minutes) FROM contest_extensions e WHERE e.contest_id = c.id), 0)) < ?
		AND (EXISTS (SELECT 1 FROM submissions s WHERE s.contest_id = c.id)
		  OR EXISTS (SELECT 1 FROM contest_problems p WHERE p.contest_id = c.id AND p.revision_id IS NOT NULL)
		  OR EXISTS (SELECT 1 FROM revision_views v WHERE v.contest_id = c.id AND v.first_seen_at < ?)
		  OR EXISTS (SELECT 1 FROM contest_pin_history h WHERE h.contest_id = c.id AND h.from_at < ?))
		ORDER BY c.id`, cut, cut, cut)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := j.expireContest(ctx, id, cut, r); err != nil {
			return fmt.Errorf("contest %d: %w", id, err)
		}
	}
	// Outside contests (an organiser trying a problem): by their own age.
	n, err := j.deleteSubmissions(ctx, "contest_id IS NULL AND created_at < ?", cut)
	r.SubmissionsDeleted += n
	return err
}

func (j *Job) expireContest(ctx context.Context, id, cut int64, r *Result) error {
	var kept int
	if err := j.DB.QueryRowContext(ctx, "SELECT count(*) FROM contest_results WHERE contest_id = ?", id).Scan(&kept); err != nil {
		return err
	}
	if kept == 0 {
		st, err := j.ExportStandings(ctx, id)
		if err != nil {
			return fmt.Errorf("export standings: %w", err)
		}
		if _, err := j.DB.ExecContext(ctx, "INSERT INTO contest_results (contest_id, exported_at, standings) VALUES (?, ?, ?)",
			id, j.now().UnixMilli(), string(st)); err != nil {
			return err
		}
	}
	n, err := j.deleteSubmissions(ctx, "contest_id = ?", id)
	if err != nil {
		return err
	}
	r.SubmissionsDeleted += n
	tx, err := j.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"contest_pin_history", "revision_views"} {
		if err := setCutoff(ctx, tx, t, cut); err != nil {
			return err
		}
	}
	// Rows newer than the cutoff (a pin moved, or a practice view, long
	// after the end) wait for a later pass.
	for _, q := range []string{
		"DELETE FROM contest_pin_history WHERE contest_id = ? AND from_at < ?",
		"DELETE FROM revision_views WHERE contest_id = ? AND first_seen_at < ?",
	} {
		if _, err := tx.ExecContext(ctx, q, id, cut); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE contest_problems SET revision_id = NULL WHERE contest_id = ?", id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// A contest seen again only for a late pin or view is not counted.
	if kept == 0 || n > 0 {
		r.ContestsExpired++
		if j.Expired != nil {
			j.Expired(id)
		}
		if j.Log != nil {
			j.Log.Info("contest submissions past retention removed; standings kept", "contest_id", id, "submissions", n)
		}
	}
	return nil
}

// deleteSubmissions deletes submissions matching where, in batches, with
// the jobs that judged them (their results go by cascade).
func (j *Job) deleteSubmissions(ctx context.Context, where string, arg any) (int64, error) {
	return j.batches(ctx, func(tx *sql.Tx) (int64, error) {
		if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE IF NOT EXISTS retention_subs (id INTEGER PRIMARY KEY, job_id INTEGER)"); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM temp.retention_subs"); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO temp.retention_subs SELECT id, job_id FROM submissions WHERE "+where+
			" AND state IN ('done', 'error') ORDER BY id LIMIT ?", arg, batch); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM submissions WHERE id IN (SELECT id FROM temp.retention_subs)")
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		// A job no remaining submission names (a rejudge leaves the old one).
		if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE state IN ('done', 'failed')
			AND id IN (SELECT job_id FROM temp.retention_subs WHERE job_id IS NOT NULL)
			AND NOT EXISTS (SELECT 1 FROM submissions s WHERE s.job_id = jobs.id)`); err != nil {
			return 0, err
		}
		return n, nil
	})
}

// audit deletes site and contest audit rows older than Audit.
func (j *Job) audit(ctx context.Context, now time.Time, r *Result) error {
	cut := now.Add(-Audit).UnixMilli()
	tx, err := j.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var total int64
	for _, t := range []struct{ table, col string }{{"site_audit", "at"}, {"contest_audit", "created_at"}} {
		if err := setCutoff(ctx, tx, t.table, cut); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM "+t.table+" WHERE "+t.col+" < ?", cut)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.AuditDeleted = total
	return nil
}

// ErrFloor is returned by CheckFloors for a period shorter than the
// trigger floor of a table it opens.
var ErrFloor = errors.New("retention period below the trigger floor")

// CheckFloors reports a period that the append-only triggers would refuse
// as a cutoff. The tests call it; it documents the rule.
func CheckFloors() error {
	if DraftRevisions < FloorDraftRevisions {
		return fmt.Errorf("%w: draft revisions %v < %v", ErrFloor, DraftRevisions, FloorDraftRevisions)
	}
	for name, p := range map[string]time.Duration{"submissions": Submissions, "audit": Audit} {
		if p < FloorOther {
			return fmt.Errorf("%w: %s %v < %v", ErrFloor, name, p, FloorOther)
		}
	}
	return nil
}
