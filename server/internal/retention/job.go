package retention

// The run payload step of the server's retention pass: it empties run
// payloads older than RunPayloads in batches, so one pass never holds the
// write lock for long while a contest runs. podium-server runs it with the
// integrity retention of contests and integrity and the collection of
// unused test files; the session sweep and backups (podium-backup) run on
// their own.

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// batch is how many rows one transaction changes.
const batch = 2000

// Job empties what is older than its period.
type Job struct {
	DB *sql.DB
	// Now is the clock; tests replace it.
	Now func() time.Time
	Log *slog.Logger
	// Hold, when set, runs inside each batch's transaction before it
	// commits; tests use it to stop the server mid-batch.
	Hold func()
}

// Result counts what one pass changed.
type Result struct {
	RunsEmptied int64 // run jobs whose payload and output were cleared
}

func (j *Job) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

// Run applies the rule once and returns what it changed. It is
// idempotent, so a failed pass is retried by the next.
func (j *Job) Run(ctx context.Context) (Result, error) {
	var r Result
	err := j.runs(ctx, j.now(), &r)
	return r, err
}

// batches repeats one transaction until it changes fewer than batch rows.
// When ctx ends (the server is stopping) the batch in progress still
// commits, and no further batch starts.
func (j *Job) batches(ctx context.Context, step func(ctx context.Context, tx *sql.Tx) (int64, error)) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		txCtx := context.WithoutCancel(ctx)
		tx, err := j.DB.BeginTx(txCtx, nil)
		if err != nil {
			return total, err
		}
		n, err := step(txCtx, tx)
		if err == nil && j.Hold != nil {
			j.Hold()
		}
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

// runs empties run jobs older than RunPayloads: the payload (code and
// custom input references, so their files can be collected) and the
// output and stderr excerpts. The job row and its verdicts stay.
func (j *Job) runs(ctx context.Context, now time.Time, r *Result) error {
	cut := now.Add(-RunPayloads).UnixMilli()
	n, err := j.batches(ctx, func(ctx context.Context, tx *sql.Tx) (int64, error) {
		const pick = `SELECT id FROM jobs WHERE kind = 'run' AND payload <> '{}' AND created_at < ? AND state IN ('done', 'failed')
			ORDER BY created_at LIMIT ?`
		if _, err := tx.ExecContext(ctx, `UPDATE job_results SET output = '', stderr = '' WHERE (output <> '' OR stderr <> '') AND job_id IN (`+pick+`)`,
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
