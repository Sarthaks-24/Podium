package problems

// Delete turns a problem into a tombstone. Nothing is removed that a
// finished contest needs: the row, its revisions and their tests, the
// contest rows and every submission stay, so boards, results and a
// student's own history still read. What goes is the working copy's
// judging data, which nothing reads once the problem cannot be judged.
// Docs: docs/AUTHORING.md "Deleting a problem".

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/auth"
	"github.com/Sarthaks-24/podium/internal/jobs"
	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// deletedReason is the reason a job of a deleted problem closes with.
const deletedReason = "The problem was deleted."

// NotAllowedError refuses a delete the person's role does not permit
// (403 not_allowed).
type NotAllowedError struct{ Message string }

func (e *NotAllowedError) Error() string { return e.Message }

// UnendedContestsError refuses a delete while contests holding the problem
// have not ended (409 in_unended_contest); nobody, the owner included, may
// delete then, because every Submit and Run in those contests would fail.
type UnendedContestsError struct{ Contests []LiveContest }

func (e *UnendedContestsError) Error() string {
	names := make([]string, 0, len(e.Contests))
	for _, c := range e.Contests {
		names = append(names, c.Title)
	}
	return fmt.Sprintf("This problem is in contests that have not ended: %s. Remove it from them or wait until they end.", strings.Join(names, ", "))
}

// DeleteInfo is what the delete dialog shows before anyone confirms.
type DeleteInfo struct {
	// Used: a contest holds the problem or anyone submitted or ran code.
	Used bool `json:"used"`
	// Contests holds every contest that holds the problem; Unended are those
	// that have not ended (they block the delete); Practice are the ended
	// ones where practice for this problem closes.
	Contests []LiveContest `json:"contests"`
	Unended  []LiveContest `json:"unended"`
	Practice []LiveContest `json:"practice"`
}

// DeletedEntry is one row of the "Deleted" list.
type DeletedEntry struct {
	ID int64
	// Slug is the original slug.
	Slug      string
	Title     string
	DeletedAt time.Time
	// DeletedBy is the email of the person, empty if their account is gone.
	DeletedBy string
	Used      bool
}

const usedSQL = `(EXISTS (SELECT 1 FROM contest_problems cp WHERE cp.problem_id = p.id)
	OR EXISTS (SELECT 1 FROM submissions s WHERE s.problem_id = p.id))`

// contestsHolding lists the contests holding the problem that satisfy
// where (a SQL condition over c and f = contest_final), oldest start first.
func contestsHolding(ctx context.Context, q queryer, problemID int64, where string, args ...any) ([]LiveContest, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.id, c.slug, c.title, cp.label FROM contest_problems cp JOIN contests c ON c.id = cp.contest_id
		JOIN contest_final f ON f.contest_id = c.id WHERE cp.problem_id = ? AND `+where+` ORDER BY c.starts_at, c.id`,
		append([]any{problemID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveContest{}
	for rows.Next() {
		var c LiveContest
		if err := rows.Scan(&c.ContestID, &c.Slug, &c.Title, &c.Label); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func deleteInfoIn(ctx context.Context, q queryer, id int64, now time.Time) (*DeleteInfo, error) {
	var used bool
	if err := q.QueryRowContext(ctx, `SELECT `+usedSQL+` FROM problems p WHERE p.id = ?`, id).Scan(&used); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	ms := now.UnixMilli()
	info := &DeleteInfo{Used: used}
	var err error
	if info.Contests, err = contestsHolding(ctx, q, id, "1 = 1"); err != nil {
		return nil, err
	}
	if info.Unended, err = contestsHolding(ctx, q, id, "c.archived = 0 AND ? < f.final_at", ms); err != nil {
		return nil, err
	}
	info.Practice, err = contestsHolding(ctx, q, id, "c.published = 1 AND c.archived = 0 AND c.practice_after_end = 1 AND f.final_at <= ?", ms)
	return info, err
}

// DeleteInfo says whether the problem was used and which contests hold it.
func (s *Service) DeleteInfo(ctx context.Context, id int64) (*DeleteInfo, error) {
	return deleteInfoIn(ctx, s.DB, id, s.now())
}

// Delete tombstones a problem for the person by. The owner may delete any
// problem; an admin only one that was never used (no contest holds it and
// nobody submitted or ran code); nobody while a contest holding it has not
// ended. It is one transaction: the audit row first, so a failure rolls
// everything back, then the tombstone and the slug move, then the working
// copy's data is cleared and its open setter jobs closed. It writes the
// columns directly, never through save, so it makes no revision, and it
// never touches revisions or revision_tests.
func (s *Service) Delete(ctx context.Context, id int64, by auth.Actor) error {
	if !by.Role.AtLeast(auth.Admin) {
		return &NotAllowedError{"You do not have access to this."}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var slug, archivedSlug string
	var archived, deleted bool
	err = tx.QueryRowContext(ctx, "SELECT slug, archived_slug, archived, deleted FROM problems WHERE id = ?", id).
		Scan(&slug, &archivedSlug, &archived, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if deleted {
		return ErrDeleted
	}
	now := s.now()
	info, err := deleteInfoIn(ctx, tx, id, now)
	if err != nil {
		return err
	}
	if info.Used && by.Role != auth.Owner {
		return &NotAllowedError{"Only the site owner can delete a problem that has been used. Archive it instead."}
	}
	if len(info.Unended) > 0 {
		return &UnendedContestsError{info.Unended}
	}
	original := slug
	if archived && archivedSlug != "" {
		original = archivedSlug
	}
	if err := auth.RecordSite(auth.WithActor(ctx, by), tx, now, auth.AuditProblemDeleted, "",
		map[string]any{"problem_id": id, "slug": original, "used": info.Used, "contests": len(info.Contests)}); err != nil {
		return err
	}
	form, err := freeSlugIn(ctx, tx, slugForm(original, "-deleted-", id))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE problems SET deleted = 1, archived = 1, deleted_at = ?, deleted_by = ?,
		archived_slug = ?, slug = ?, updated_at = ?, checker_source = '', reference_source = '', brute_language = '', brute_source = '',
		validator_language = '', validator_source = '', generator_language = '', generator_source = '', generator_plan = '[]',
		starters = '{}', samples = '[]' WHERE id = ? AND deleted = 0`,
		now.UnixMilli(), by.ID, original, form, now.UnixMilli(), id); err != nil {
		return err
	}
	for _, table := range []string{"testcases", "subtasks"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE problem_id = ?", id); err != nil {
			return err
		}
	}
	if err := s.cancelSetterJobs(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// cancelSetterJobs closes the problem's waiting or running validation and
// generation jobs with the deleted reason. They are not retried.
func (s *Service) cancelSetterJobs(ctx context.Context, tx *sql.Tx, id int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM jobs WHERE state IN ('queued', 'leased') AND kind IN (?, ?, ?)
		AND json_extract(payload, '$.problem_id') = ?`, judgeapi.KindValidate, judgeapi.KindGenerate, judgeapi.KindGenerateInputs, id)
	if err != nil {
		return err
	}
	var open []int64
	for rows.Next() {
		var jid int64
		if err := rows.Scan(&jid); err != nil {
			rows.Close()
			return err
		}
		open = append(open, jid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, jid := range open {
		if err := s.Jobs.Cancel(ctx, tx, jid, deletedReason); err != nil {
			return err
		}
	}
	return nil
}

// ListDeleted returns the tombstones, most recently deleted first.
func (s *Service) ListDeleted(ctx context.Context) ([]DeletedEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.archived_slug, p.title, coalesce(p.deleted_at, 0), coalesce(u.email, ''), `+usedSQL+`
		FROM problems p LEFT JOIN users u ON u.id = p.deleted_by WHERE p.deleted = 1 ORDER BY p.deleted_at DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeletedEntry{}
	for rows.Next() {
		var e DeletedEntry
		var at int64
		if err := rows.Scan(&e.ID, &e.Slug, &e.Title, &at, &e.DeletedBy, &e.Used); err != nil {
			return nil, err
		}
		e.DeletedAt = time.UnixMilli(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// closeIfDeleted ends a setter job whose problem was deleted: the job
// fails with the deleted reason (so it is not retried) and Finish writes
// nothing. It reports whether it did.
func closeIfDeleted(ctx context.Context, tx *sql.Tx, problemID int64, job *jobs.Job) (bool, error) {
	var deleted bool
	err := tx.QueryRowContext(ctx, "SELECT deleted FROM problems WHERE id = ?", problemID).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || !deleted {
		return false, err
	}
	job.State, job.Error = jobs.Failed, deletedReason
	return true, nil
}
