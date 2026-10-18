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

// Blobs stores test files and program sources by the SHA-256 of their
// content, under Dir/<first two hex digits>/<sha>. A file is written once
// and never changed, so the judge can cache it by name forever.
type Blobs struct {
	Dir string
}

// SHA returns the hex SHA-256 of data.
func SHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (b Blobs) path(sha string) string {
	return filepath.Join(b.Dir, sha[:2], sha)
}

// ErrBlobMissing means a blob's file was not on disk when it was recorded:
// the unused-blob collection (GC) removed it after Write had found it.
var ErrBlobMissing = errors.New("the file was removed while it was being saved; try again")

// Put writes data and records it inside tx. Callers storing large files
// use Write before opening the transaction and Record inside it, so the
// database's write lock is never held while megabytes go to disk.
func (b Blobs) Put(ctx context.Context, tx *sql.Tx, data []byte) (string, error) {
	sha, err := b.Write(data)
	if err != nil {
		return "", err
	}
	err = b.Record(ctx, tx, sha, int64(len(data)))
	if errors.Is(err, ErrBlobMissing) {
		// GC removed the file between Write and Record. It cannot remove
		// it again while tx holds the write lock, so write it back.
		_, err = b.Write(data)
	}
	return sha, err
}

// Record adds a written blob to the blobs table, or marks an existing row
// as just used: created_at is the last time anything recorded the blob,
// and GC never removes a row recorded in the last GCGrace. It checks the
// file is still there, inside tx, so a file GC removed just before is
// noticed (ErrBlobMissing) instead of leaving a row without a file.
func (b Blobs) Record(ctx context.Context, tx *sql.Tx, sha string, size int64) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha, size, created_at) VALUES (?, ?, ?)
		ON CONFLICT (sha) DO UPDATE SET created_at = excluded.created_at`, sha, size, time.Now().UnixMilli()); err != nil {
		return err
	}
	if _, err := os.Stat(b.path(sha)); err != nil {
		return fmt.Errorf("blob %.12s: %w", sha, ErrBlobMissing)
	}
	return nil
}

// Write stores data under its hash if it is not stored already, through a
// temporary name, so a file is never seen partial. It returns the hash.
func (b Blobs) Write(data []byte) (string, error) {
	sha := SHA(data)
	p := b.path(sha)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return "", err
		}
		tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
		if err != nil {
			return "", err
		}
		_, werr := tmp.Write(data)
		cerr := tmp.Close()
		if werr != nil || cerr != nil {
			os.Remove(tmp.Name())
			return "", errors.Join(werr, cerr)
		}
		if err := os.Rename(tmp.Name(), p); err != nil {
			os.Remove(tmp.Name())
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return sha, nil
}

// Open opens a stored blob for reading.
func (b Blobs) Open(sha string) (*os.File, error) {
	if !judgeapi.ValidSHA(sha) {
		return nil, fmt.Errorf("blob %q: %w", sha, os.ErrNotExist)
	}
	return os.Open(b.path(sha))
}

// Read returns at most n bytes from the start of a blob.
func (b Blobs) Read(sha string, n int64) ([]byte, error) {
	f, err := b.Open(sha)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}
