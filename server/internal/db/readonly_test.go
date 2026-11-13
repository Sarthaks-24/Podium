package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// The statement kinds the write gate and the read-only guard tell apart.
func TestWriteKind(t *testing.T) {
	for q, want := range map[string]string{
		"SELECT 1":                             "",
		"  select x from t":                    "",
		"\n\t SELECT 1":                        "",
		"-- note\nSELECT 1":                    "",
		"/* note */ SELECT 1":                  "",
		"/* a */ -- b\n /* c */ select 1":      "",
		"VALUES (1), (2)":                      "",
		"EXPLAIN INSERT INTO t VALUES (1)":     "",
		"EXPLAIN QUERY PLAN DELETE FROM t":     "",
		"WITH a AS (SELECT 1) SELECT * FROM a": "",
		"WITH RECURSIVE a(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM a WHERE n < 3) SELECT n FROM a": "",
		"WITH a AS (SELECT 'insert into x'), b AS MATERIALIZED (SELECT 2) SELECT * FROM a, b":       "",
		"WITH a AS (SELECT 1 -- delete\n) SELECT * FROM a":                                          "",
		"INSERT INTO t VALUES (1)":                                "INSERT",
		"insert into t values (1)":                                "INSERT",
		"update t set x = 1":                                      "UPDATE",
		"DELETE FROM t":                                           "DELETE",
		"REPLACE INTO t VALUES (1)":                               "REPLACE",
		"-- note\nINSERT INTO t VALUES (1)":                       "INSERT",
		"/* note */INSERT INTO t VALUES (1)":                      "INSERT",
		"CREATE TABLE x (a)":                                      "CREATE",
		"DROP TABLE x":                                            "DROP",
		"ALTER TABLE x ADD COLUMN b":                              "ALTER",
		"VACUUM":                                                  "VACUUM",
		"ATTACH DATABASE 'x' AS y":                                "ATTACH",
		"WITH a AS (SELECT 1) INSERT INTO t SELECT * FROM a":      "INSERT",
		"WITH a AS (SELECT 1) UPDATE t SET x = (SELECT * FROM a)": "UPDATE",
		"WITH a AS (SELECT 1), b AS (SELECT 2) DELETE FROM t":     "DELETE",
		"WITH a AS (SELECT 1) REPLACE INTO t SELECT * FROM a":     "REPLACE",
		"/* c */ WITH RECURSIVE a(n) AS (SELECT 1) INSERT INTO t SELECT n FROM a": "INSERT",
		"PRAGMA integrity_check":          "",
		"PRAGMA journal_mode":             "",
		"PRAGMA table_info(t)":            "",
		"pragma main.index_list(t)":       "",
		"PRAGMA foreign_keys = 0":         "PRAGMA",
		"PRAGMA foreign_keys(0)":          "PRAGMA",
		"PRAGMA main.journal_mode=DELETE": "PRAGMA",
		"PRAGMA query_only = 0":           "PRAGMA",
		"PRAGMA wal_checkpoint(TRUNCATE)": "PRAGMA",
		"PRAGMA optimize":                 "PRAGMA",
		"PRAGMA incremental_vacuum":       "PRAGMA",
		"BEGIN":                           "BEGIN",
		"SAVEPOINT x":                     "SAVEPOINT",
		"":                                "",
		"-- only a comment":               "",
		"/* unterminated":                 "",
	} {
		if got := writeKind(q); got != want {
			t.Errorf("writeKind(%q) = %q, want %q", q, got, want)
		}
	}
}

func openOneConn(t *testing.T) *sql.DB {
	t.Helper()
	d := openTest(t)
	d.SetMaxOpenConns(1) // every transaction reuses the same connection
	return d
}

func TestReadOnlyTxRefusesWrites(t *testing.T) {
	d := openOneConn(t)
	ctx := context.Background()
	if _, err := d.Exec("INSERT INTO t (who) VALUES ('seed')"); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"INSERT":  "INSERT INTO t (who) VALUES ('x')",
		"UPDATE":  "UPDATE t SET who = 'x'",
		"DELETE":  "DELETE FROM t",
		"REPLACE": "REPLACE INTO t (id, who) VALUES (1, 'x')",
		"CREATE":  "CREATE TABLE u (a)",
		"PRAGMA":  "PRAGMA foreign_keys = 0",
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, q)
			if !errors.Is(err, ErrWriteInReadOnly) || !strings.Contains(err.Error(), name) {
				t.Errorf("exec: %v, want ErrWriteInReadOnly naming %s", err, name)
			}
			if _, err := tx.PrepareContext(ctx, q); !errors.Is(err, ErrWriteInReadOnly) {
				t.Errorf("prepare: %v, want ErrWriteInReadOnly", err)
			}
		})
	}
	// A statement prepared outside the transaction is checked when used in it.
	st, err := d.PrepareContext(ctx, "INSERT INTO t (who) VALUES ('x')")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tx, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// A write that returns rows, and one hidden behind a WITH, are caught.
	if _, err := tx.QueryContext(ctx, "INSERT INTO t (who) VALUES ('x') RETURNING id"); !errors.Is(err, ErrWriteInReadOnly) {
		t.Errorf("INSERT ... RETURNING: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "WITH a AS (SELECT 1) INSERT INTO t (who) VALUES ('x')"); !errors.Is(err, ErrWriteInReadOnly) {
		t.Errorf("WITH ... INSERT: %v", err)
	}
	if _, err := tx.StmtContext(ctx, st).ExecContext(ctx); !errors.Is(err, ErrWriteInReadOnly) {
		t.Errorf("prepared outside, used inside: %v", err)
	}
}

// Reads in a read-only transaction, and every statement of an ordinary
// transaction, are left alone. The guard also lets go when the read-only
// transaction ends.
func TestReadOnlyGuardLeavesOtherWorkAlone(t *testing.T) {
	d := openOneConn(t)
	ctx := context.Background()
	ro, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := ro.QueryRowContext(ctx, "WITH a AS (SELECT 1 AS v) SELECT v FROM a").Scan(&n); err != nil || n != 1 {
		t.Fatalf("read in read-only tx: %v %d", err, n)
	}
	if err := ro.QueryRowContext(ctx, "PRAGMA table_info(t)").Scan(new(any), new(any), new(any), new(any), new(any), new(any)); err != nil {
		t.Fatalf("read pragma in read-only tx: %v", err)
	}
	if err := ro.Commit(); err != nil {
		t.Fatal(err)
	}
	rw, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.ExecContext(ctx, "INSERT INTO t (who) VALUES ('rw')"); err != nil {
		t.Fatalf("write in ordinary tx after a read-only one: %v", err)
	}
	if err := rw.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO t (who) VALUES ('plain')"); err != nil {
		t.Fatalf("write outside a tx: %v", err)
	}
	// A refused write rolls back; the connection serves writes afterwards.
	ro2, _ := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	ro2.ExecContext(ctx, "DELETE FROM t")
	ro2.Rollback()
	if _, err := d.ExecContext(ctx, "INSERT INTO t (who) VALUES ('after')"); err != nil {
		t.Fatalf("write after a refused one: %v", err)
	}
	var rows int
	d.QueryRow("SELECT count(*) FROM t").Scan(&rows)
	if rows != 3 {
		t.Errorf("rows = %d, want 3", rows)
	}
}

// BenchmarkWriteKind is what recognising a statement costs: it runs once
// per statement prepared or executed.
func BenchmarkWriteKind(b *testing.B) {
	for name, q := range map[string]string{
		"select":      "SELECT id, name FROM users WHERE id = ?",
		"insert":      "INSERT INTO submissions (user_id, problem_id, code) VALUES (?, ?, ?)",
		"with-select": "WITH a AS (SELECT user_id FROM contest_starts WHERE contest_id = ?), b AS (SELECT 1) SELECT * FROM a, b",
		"with-insert": "WITH a AS (SELECT user_id FROM contest_starts WHERE contest_id = ?) INSERT INTO t SELECT * FROM a",
		"pragma":      "PRAGMA table_info(t)",
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = writeKind(q)
			}
		})
	}
}
