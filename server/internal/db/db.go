// Package db opens podium's SQLite database and applies its numbered
// migrations. The database is opened once per process, in WAL mode with a
// busy timeout, through the pure-Go modernc.org/sqlite driver so the server
// cross-compiles without cgo. Schema changes are only ever made by adding a
// file to migrations/; nothing alters tables at run time.
package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sarthaks-24/podium/internal/db/migrations"
	"modernc.org/sqlite" // also registers the "sqlite" driver, used read-only by "podium-server check"
)

// migrationFS holds the schema files (package migrations).
var migrationFS = migrations.FS

// init registers podium_sha256(text), the lowercase hex SHA-256 of a
// text or blob value (problems.SHA computes the same in Go), on every
// connection of the driver. Migration 0010 uses it to fill the revision
// rows' source hashes, so the backfill stays in its SQL file.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("podium_sha256", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		var b []byte
		switch v := args[0].(type) {
		case nil:
			return nil, nil
		case string:
			b = []byte(v)
		case []byte:
			b = v
		default:
			b = []byte(fmt.Sprint(v))
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:]), nil
	})
}

// MaxConns is the size of the connection pool.
const MaxConns = 8

// pragmas are applied to every pooled connection.
var pragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(1)",
}

// Open opens (creating if needed) the database at path. Write transactions
// begin IMMEDIATE so concurrent writers queue on the busy timeout instead of
// failing with SQLITE_BUSY halfway through.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	groupWritable(path)
	q := make([]string, 0, len(pragmas)+1)
	for _, p := range pragmas {
		q = append(q, "_pragma="+p)
	}
	q = append(q, "_txlock=immediate")
	connector, err := sqlite.NewConnector(path + "?" + strings.Join(q, "&"))
	if err != nil {
		return nil, err
	}
	// Writes take turns in Go (writegate.go); reads run in parallel on the
	// other connections. Eight connections let reads go on while writers
	// queue; each carries its own page cache (2 MB at most).
	d := sql.OpenDB(gatedConnector{Connector: connector, gate: newWriteGate()})
	d.SetMaxOpenConns(MaxConns)
	d.SetMaxIdleConns(MaxConns)
	d.SetConnMaxIdleTime(5 * time.Minute)
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	var mode string
	if err := d.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		d.Close()
		return nil, fmt.Errorf("open database: journal_mode is %q, want wal (%v)", mode, err)
	}
	return d, nil
}

// groupWritable makes the database file mode 0660, creating it empty
// when it does not exist (SQLite treats an empty file as a new database).
// Litestream runs as its own user in the server's group and must write
// the database for its checkpoints (D116); SQLite gives the -wal and -shm
// files the database file's mode, so they follow. Best effort: on a
// filesystem without Unix modes nothing changes.
func groupWritable(path string) {
	if f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o660); err == nil {
		f.Close()
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o060 != 0o060 {
		os.Chmod(path, fi.Mode().Perm()|0o060)
	}
}

// Migrate applies every migration in migrations/ that has not run yet, in
// file-name order, each in its own transaction. It returns the names applied.
func Migrate(ctx context.Context, d *sql.DB) ([]string, error) {
	return MigrateTo(ctx, d, "")
}

// MigrateTo is Migrate stopping after the migration named last (without
// ".sql"); an empty last applies them all. Tests use it to build the
// schema of an older release and migrate it with fixtures in place.
func MigrateTo(ctx context.Context, d *sql.DB, last string) ([]string, error) {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	) STRICT`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	names, err := migrationNames()
	if err != nil {
		return nil, err
	}
	if err := refuseUnknown(ctx, d, names); err != nil {
		return nil, err
	}
	var applied []string
	for _, name := range names {
		version := strings.TrimSuffix(path.Base(name), ".sql")
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return applied, err
		}
		ran, err := apply(ctx, d, version, string(body))
		if err != nil {
			return applied, fmt.Errorf("migration %s: %w", version, err)
		}
		if ran {
			applied = append(applied, version)
		}
		if version == last {
			break
		}
	}
	return applied, nil
}

func migrationNames() ([]string, error) {
	return migrations.Files(), nil
}

// refuseUnknown fails when the database records a migration this binary
// does not have, which means an older binary is running against a newer
// schema.
func refuseUnknown(ctx context.Context, d *sql.DB, names []string) error {
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[strings.TrimSuffix(path.Base(n), ".sql")] = true
	}
	rows, err := d.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		if !known[v] {
			return fmt.Errorf("database has migration %s, which this binary does not know; it was written by a newer podium-server, so upgrade instead of rolling back", v)
		}
	}
	return rows.Err()
}

// apply runs one migration unless it already ran. The check and the change
// share one IMMEDIATE transaction, so two processes starting together
// cannot both apply it. PRAGMA foreign_keys cannot change inside a
// transaction, so migrations must not rely on switching it off.
func apply(ctx context.Context, d *sql.DB, version, body string) (bool, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version = ?", version).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
		version, time.Now().UnixMilli()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
