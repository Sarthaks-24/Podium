package db

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
)

// CheckReport is what Check found in a database.
type CheckReport struct {
	// Integrity is the output of PRAGMA integrity_check: "ok", or the
	// problems SQLite found.
	Integrity []string
	// ForeignKeys lists rows whose foreign key points nowhere, as
	// "table rowid -> parent".
	ForeignKeys []string
	// Applied and Pending are the migrations the database has and those
	// this binary would still apply; Unknown are migrations the database
	// has that this binary does not know (a newer server wrote it).
	Applied, Pending, Unknown []string
	// Counts holds the number of rows in the main tables.
	Counts map[string]int64
}

// OK reports whether the database is sound and this binary can run on it.
func (r CheckReport) OK() bool {
	return len(r.Integrity) == 1 && r.Integrity[0] == "ok" && len(r.ForeignKeys) == 0 && len(r.Unknown) == 0
}

// countedTables are reported by Check, in this order.
var countedTables = []string{"users", "problems", "testcases", "contests", "submissions", "jobs", "blobs", "integrity_cases"}

// Check runs SQLite's integrity and foreign key checks and compares the
// recorded migrations with this binary's. It only reads: a restored copy
// is checked as it is, without migrating it.
func Check(ctx context.Context, d *sql.DB) (CheckReport, error) {
	r := CheckReport{Counts: map[string]int64{}}
	rows, err := d.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return r, err
		}
		r.Integrity = append(r.Integrity, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return r, err
	}

	fk, err := d.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return r, err
	}
	for fk.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var idx int
		if err := fk.Scan(&table, &rowid, &parent, &idx); err != nil {
			fk.Close()
			return r, err
		}
		r.ForeignKeys = append(r.ForeignKeys, fmt.Sprintf("%s %d -> %s", table, rowid.Int64, parent))
	}
	fk.Close()
	if err := fk.Err(); err != nil {
		return r, err
	}

	names, err := migrationNames()
	if err != nil {
		return r, err
	}
	known := map[string]bool{}
	for _, n := range names {
		known[strings.TrimSuffix(path.Base(n), ".sql")] = true
	}
	have := map[string]bool{}
	var tableExists int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'").Scan(&tableExists); err != nil {
		return r, err
	}
	if tableExists > 0 {
		mig, err := d.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
		if err != nil {
			return r, err
		}
		for mig.Next() {
			var v string
			if err := mig.Scan(&v); err != nil {
				mig.Close()
				return r, err
			}
			have[v] = true
			if known[v] {
				r.Applied = append(r.Applied, v)
			} else {
				r.Unknown = append(r.Unknown, v)
			}
		}
		mig.Close()
		if err := mig.Err(); err != nil {
			return r, err
		}
	}
	for _, n := range names {
		if v := strings.TrimSuffix(path.Base(n), ".sql"); !have[v] {
			r.Pending = append(r.Pending, v)
		}
	}

	for _, t := range countedTables {
		var n int64
		// Table names come from the fixed list above, never from input.
		if err := d.QueryRowContext(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			continue // not created yet on an old database
		}
		r.Counts[t] = n
	}
	return r, nil
}

// CountedTables lists the tables whose row counts Check reports, in order.
func CountedTables() []string { return append([]string(nil), countedTables...) }
