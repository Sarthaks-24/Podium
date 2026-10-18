package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(context.Background(), filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, who TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	return d
}

// Many concurrent writers, in and out of transactions, all succeed: none
// meets SQLite's busy timeout.
func TestConcurrentWritersAllSucceed(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	const workers, each = 24, 40
	var wg sync.WaitGroup
	errs := make(chan error, workers*each)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				var err error
				switch i % 3 {
				case 0:
					_, err = d.ExecContext(ctx, "INSERT INTO t (who) VALUES ('exec')")
				case 1:
					var id int64
					err = d.QueryRowContext(ctx, "INSERT INTO t (who) VALUES ('returning') RETURNING id").Scan(&id)
				default:
					tx, e := d.BeginTx(ctx, nil)
					if e != nil {
						err = e
						break
					}
					if _, e := tx.ExecContext(ctx, "INSERT INTO t (who) VALUES ('tx')"); e != nil {
						tx.Rollback()
						err = e
						break
					}
					err = tx.Commit()
				}
				if err != nil {
					errs <- err
				}
				// Reads interleave without taking a turn.
				var n int
				if err := d.QueryRowContext(ctx, "SELECT count(*) FROM t").Scan(&n); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("write failed: %v", err)
	}
	var n int
	d.QueryRow("SELECT count(*) FROM t").Scan(&n)
	if n != workers*each {
		t.Errorf("%d rows, want %d", n, workers*each)
	}
}

// A read-only transaction and plain reads run while a write transaction is
// open; a second writer waits for its turn and then succeeds.
func TestReadsDoNotWaitForWriters(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO t (who) VALUES ('first')"); err != nil {
		t.Fatal(err)
	}
	ro, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("read-only transaction waited for the writer: %v", err)
	}
	var n int
	if err := ro.QueryRow("SELECT count(*) FROM t").Scan(&n); err != nil || n != 0 {
		t.Errorf("read-only transaction: %d %v", n, err)
	}
	ro.Rollback()
	if err := d.QueryRow("WITH x AS (SELECT 1) SELECT count(*) FROM t").Scan(&n); err != nil {
		t.Errorf("plain read waited: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := d.Exec("INSERT INTO t (who) VALUES ('second')")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second writer did not wait for the first: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Errorf("second writer after the first committed: %v", err)
	}
}

// A write started while the same goroutine holds a write transaction fails
// after the gate timeout instead of hanging, as SQLITE_BUSY did before.
func TestNestedWriteFailsInsteadOfHanging(t *testing.T) {
	d := openTest(t)
	old := gateTimeout
	gateTimeout = 200 * time.Millisecond
	defer func() { gateTimeout = old }()
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	start := time.Now()
	_, err = d.Exec("INSERT INTO t (who) VALUES ('nested')")
	if !errors.Is(err, ErrBusy) || time.Since(start) > 2*time.Second {
		t.Errorf("nested write: %v after %v, want ErrBusy after about 200 ms", err, time.Since(start))
	}
	// A cancelled context gives up at once.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.ExecContext(ctx, "INSERT INTO t (who) VALUES ('cancelled')"); err == nil {
		t.Error("write with a cancelled context succeeded while the gate was held")
	}
}

// A failed write gives its turn back.
func TestFailedWriteReleasesTheGate(t *testing.T) {
	d := openTest(t)
	if _, err := d.Exec("INSERT INTO nope (x) VALUES (1)"); err == nil {
		t.Fatal("insert into a missing table succeeded")
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatalf("gate still held after a failed statement: %v", err)
	}
	tx.Rollback()
}

func TestIsWrite(t *testing.T) {
	for q, want := range map[string]bool{
		"SELECT 1":                             false,
		"  select x from t":                    false,
		"WITH a AS (SELECT 1) SELECT * FROM a": false,
		"PRAGMA integrity_check":               false,
		"-- note\nSELECT 1":                    false,
		"INSERT INTO t VALUES (1)":             true,
		"update t set x = 1":                   true,
		"DELETE FROM t":                        true,
		"-- note\nINSERT INTO t VALUES (1)":    true,
		"CREATE TABLE x (a)":                   true,
	} {
		if got := isWrite(q); got != want {
			t.Errorf("isWrite(%q) = %v, want %v", q, got, want)
		}
	}
}
