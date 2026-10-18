package problems

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Sarthaks-24/podium/internal/judgeapi"
)

// GCGrace is how long a blob is kept after it was last recorded (or, for a
// file with no row, written) even when nothing refers to it: longer than
// any job may run, so an upload or a save in progress is never collected.
const GCGrace = 24 * time.Hour

// GCRecentJobs keeps the files of jobs created in the last week, so a
// recent run still shows its samples' expected output after the problem's
// tests were replaced.
const GCRecentJobs = 7 * 24 * time.Hour

// gcBatch is how many blobs one write transaction removes.
const gcBatch = 200

// GCResult counts what GC removed.
type GCResult struct {
	Rows  int   // blob rows and their files
	Files int   // files on disk with no row (abandoned uploads)
	Bytes int64 // bytes freed
}

// GC removes blobs that nothing refers to: not a test of any problem, not
// the source of any submission or run, and not named in the payload of a
// job that is waiting, running, or less than GCRecentJobs old. Problem
// sources, checkers, generators and custom inputs are stored again
// whenever a job needs them, so only those jobs pin them. Blobs recorded
// in the last GCGrace are kept.
//
// The removal of each batch, rows and files, happens inside one write
// transaction, and Record checks the file inside its own: a blob that a
// save touched after the candidates were read keeps its fresh created_at
// and is skipped, and a save that comes after the removal finds the file
// gone and writes it again (Put) or fails cleanly (ErrBlobMissing).
func (b Blobs) GC(ctx context.Context, db *sql.DB, now time.Time) (GCResult, error) {
	var res GCResult
	cutoff := now.Add(-GCGrace).UnixMilli()
	pinned, err := jobBlobs(ctx, db, now.Add(-GCRecentJobs).UnixMilli())
	if err != nil {
		return res, err
	}
	rows, err := db.QueryContext(ctx, `SELECT sha FROM blobs WHERE created_at < ?
		EXCEPT SELECT input_sha FROM testcases
		EXCEPT SELECT output_sha FROM testcases WHERE output_sha IS NOT NULL
		EXCEPT SELECT source_sha FROM submissions`, cutoff)
	if err != nil {
		return res, fmt.Errorf("blob gc: %w", err)
	}
	var unused []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			rows.Close()
			return res, err
		}
		if !pinned[sha] && judgeapi.ValidSHA(sha) {
			unused = append(unused, sha)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for len(unused) > 0 {
		n := min(gcBatch, len(unused))
		if err := b.removeRows(ctx, db, unused[:n], cutoff, &res); err != nil {
			return res, err
		}
		unused = unused[n:]
	}
	orphans, err := b.orphanFiles(ctx, db, now.Add(-GCGrace), pinned)
	if err != nil {
		return res, err
	}
	for len(orphans) > 0 {
		n := min(gcBatch, len(orphans))
		if err := b.removeOrphans(ctx, db, orphans[:n], &res); err != nil {
			return res, err
		}
		orphans = orphans[n:]
	}
	return res, nil
}

// removeRows deletes one batch of unused blobs, rows and files, in one
// write transaction. A row recorded again since the candidates were read
// has a fresh created_at and stays.
func (b Blobs) removeRows(ctx context.Context, db *sql.DB, shas []string, cutoff int64, res *GCResult) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sha := range shas {
		var size int64
		err := tx.QueryRowContext(ctx, "DELETE FROM blobs WHERE sha = ? AND created_at < ? RETURNING size", sha, cutoff).Scan(&size)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("blob gc: %w", err)
		}
		if err := os.Remove(b.path(sha)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("blob gc: %w", err)
		}
		res.Rows++
		res.Bytes += size
	}
	return tx.Commit()
}

// orphanFile is a file under Dir with no row in the blobs table.
type orphanFile struct {
	sha  string
	size int64
}

// orphanFiles lists blob files older than before that have no row and no
// job names: uploads of generate jobs that were never used (the upload's
// own cleanup missed them, for example on a crash), and leftovers of an
// interrupted write.
func (b Blobs) orphanFiles(ctx context.Context, db *sql.DB, before time.Time, pinned map[string]bool) ([]orphanFile, error) {
	known := map[string]bool{}
	rows, err := db.QueryContext(ctx, "SELECT sha FROM blobs")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			rows.Close()
			return nil, err
		}
		known[sha] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []orphanFile
	err = b.walk(func(name string, info os.FileInfo) {
		if info.ModTime().After(before) || known[name] || pinned[name] || !judgeapi.ValidSHA(name) {
			return
		}
		out = append(out, orphanFile{name, info.Size()})
	})
	return out, err
}

// removeOrphans deletes files that still have no row, under the write
// lock (db.Open makes every transaction begin IMMEDIATE) so a concurrent
// Record either sees the file or recreates it.
func (b Blobs) removeOrphans(ctx context.Context, db *sql.DB, files []orphanFile, res *GCResult) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range files {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM blobs WHERE sha = ?", f.sha).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if err := os.Remove(b.path(f.sha)); err == nil {
			res.Files++
			res.Bytes += f.size
		}
	}
	return tx.Commit()
}

// walk calls fn for every file in the blob directory's two-letter
// folders. Temporary files (.tmp-*) are passed too; callers filter.
func (b Blobs) walk(fn func(name string, info os.FileInfo)) error {
	dirs, err := os.ReadDir(b.Dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if !d.IsDir() || len(d.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(b.Dir, d.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			if info, err := f.Info(); err == nil && info.Mode().IsRegular() {
				fn(f.Name(), info)
			}
		}
	}
	return nil
}

// jobBlobs collects every SHA-256 named in the payload of a job that is
// queued or leased, or was created at or after since. Payloads are read
// as text, so every hash a job kind could fetch is seen whatever its
// shape; anything that merely looks like a hash only keeps more.
func jobBlobs(ctx context.Context, db *sql.DB, since int64) (map[string]bool, error) {
	pinned := map[string]bool{}
	scan := func(q string, since int64) error {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var payload string
			var created int64
			if err := rows.Scan(&payload, &created); err != nil {
				return err
			}
			if created < since {
				// Newest first: everything after this is older still,
				// apart from the odd out-of-order row.
				return nil
			}
			hexRuns(payload, pinned)
		}
		return rows.Err()
	}
	if err := scan("SELECT payload, 0 FROM jobs WHERE state IN ('queued', 'leased')", 0); err != nil {
		return nil, fmt.Errorf("blob gc: open jobs: %w", err)
	}
	if err := scan("SELECT payload, created_at FROM jobs ORDER BY id DESC", since); err != nil {
		return nil, fmt.Errorf("blob gc: recent jobs: %w", err)
	}
	return pinned, nil
}

// hexRuns adds every run of exactly 64 lowercase hex digits in s to set.
func hexRuns(s string, set map[string]bool) {
	start := -1
	for i := 0; i <= len(s); i++ {
		hex := i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f')
		switch {
		case hex && start < 0:
			start = i
		case !hex && start >= 0:
			if i-start == 64 {
				set[s[start:i]] = true
			}
			start = -1
		}
	}
}

// VerifyReport is what Verify found.
type VerifyReport struct {
	Blobs int   // rows in the blobs table
	Bytes int64 // their recorded sizes
	// Missing lists rows whose file is absent; WrongSize, files whose size
	// differs from the row; BadHash, files whose content does not hash to
	// their name (only with hash set).
	Missing, WrongSize, BadHash []string
	// Orphans counts files with no row (GC removes them after GCGrace).
	Orphans int
	// Dangling lists hashes a test or a submission names that have no row.
	Dangling []string
}

// OK reports whether nothing is missing or damaged.
func (r VerifyReport) OK() bool {
	return len(r.Missing)+len(r.WrongSize)+len(r.BadHash)+len(r.Dangling) == 0
}

// Verify checks that every blob row has its file with the recorded size
// (and, with hash, the right content), and that every test and submission
// names a recorded blob. Used by "podium-server check" after a restore.
func (b Blobs) Verify(ctx context.Context, db *sql.DB, hash bool) (VerifyReport, error) {
	var r VerifyReport
	rows, err := db.QueryContext(ctx, "SELECT sha, size FROM blobs ORDER BY sha")
	if err != nil {
		return r, err
	}
	known := map[string]bool{}
	for rows.Next() {
		var sha string
		var size int64
		if err := rows.Scan(&sha, &size); err != nil {
			rows.Close()
			return r, err
		}
		known[sha] = true
		r.Blobs++
		r.Bytes += size
		if !judgeapi.ValidSHA(sha) {
			r.BadHash = append(r.BadHash, sha)
			continue
		}
		info, err := os.Stat(b.path(sha))
		switch {
		case err != nil:
			r.Missing = append(r.Missing, sha)
		case info.Size() != size:
			r.WrongSize = append(r.WrongSize, sha)
		case hash:
			if ok, err := hashes(b.path(sha), sha); err != nil || !ok {
				r.BadHash = append(r.BadHash, sha)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return r, err
	}
	refs, err := db.QueryContext(ctx, `SELECT input_sha FROM testcases UNION SELECT output_sha FROM testcases WHERE output_sha IS NOT NULL
		UNION SELECT source_sha FROM submissions`)
	if err != nil {
		return r, err
	}
	for refs.Next() {
		var sha string
		if err := refs.Scan(&sha); err != nil {
			refs.Close()
			return r, err
		}
		if !known[sha] {
			r.Dangling = append(r.Dangling, sha)
		}
	}
	refs.Close()
	if err := refs.Err(); err != nil {
		return r, err
	}
	err = b.walk(func(name string, _ os.FileInfo) {
		if judgeapi.ValidSHA(name) && !known[name] {
			r.Orphans++
		}
	})
	return r, err
}

func hashes(path, sha string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == sha, nil
}
