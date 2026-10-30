package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"time"
)

// The write gate lets one write at a time into SQLite from this process,
// queued in Go, so writers never meet SQLite's busy handler. Without it,
// writers that find the database locked sleep and poll (1 ms rising to
// 100 ms between tries) while holding a pooled connection; under the
// 700-student load test that sleeping hand-off became the bottleneck: the
// four connections all sat in waiting writers, reads queued behind them for
// seconds, and judging stalled for 90 s (measured on a desktop). Queued in
// Go, the next writer starts the moment the last one commits.
//
// What counts as a write: every transaction that is not read-only (they
// begin IMMEDIATE anyway), and outside a transaction any statement that does
// not start with SELECT, WITH, PRAGMA, EXPLAIN or VALUES. Other processes
// (the sqlite3 shell, Litestream) still meet the busy timeout as before.

// gateTimeout is how long a writer waits for its turn before failing with
// ErrBusy, like SQLite's busy timeout did before.
var gateTimeout = 10 * time.Second

// ErrBusy means a write waited gateTimeout for its turn. It usually means a
// write was started while the same goroutine held a write transaction.
var ErrBusy = errors.New("database is busy: a write waited too long for its turn")

type writeGate struct{ ch chan struct{} }

func newWriteGate() *writeGate { return &writeGate{ch: make(chan struct{}, 1)} }

func (g *writeGate) acquire(ctx context.Context) error {
	select {
	case g.ch <- struct{}{}:
		return nil
	default:
	}
	t := time.NewTimer(gateTimeout)
	defer t.Stop()
	select {
	case g.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ErrBusy
	}
}

func (g *writeGate) release() { <-g.ch }

// once returns a release function that is safe to call more than once.
func (g *writeGate) once() func() {
	var o sync.Once
	return func() { o.Do(g.release) }
}

// gatedConnector wraps the SQLite connector so every connection shares one
// gate.
type gatedConnector struct {
	driver.Connector
	gate *writeGate
}

func (c gatedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &gatedConn{inner: conn, gate: c.gate}, nil
}

// isWrite reports whether a statement run outside a transaction may write.
func isWrite(query string) bool {
	q := strings.TrimSpace(query)
	for strings.HasPrefix(q, "--") {
		if i := strings.IndexByte(q, '\n'); i >= 0 {
			q = strings.TrimSpace(q[i+1:])
		} else {
			return false
		}
	}
	end := strings.IndexFunc(q, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') })
	if end < 0 {
		end = len(q)
	}
	switch strings.ToUpper(q[:end]) {
	case "SELECT", "WITH", "PRAGMA", "EXPLAIN", "VALUES":
		return false
	}
	return true
}

type gatedConn struct {
	inner driver.Conn
	gate  *writeGate
	// inTx is set while a transaction holds the gate; statements inside it
	// need no turn of their own. database/sql uses a connection from one
	// goroutine at a time, so it needs no lock.
	inTx bool
}

func (c *gatedConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *gatedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var s driver.Stmt
	var err error
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		s, err = p.PrepareContext(ctx, query)
	} else {
		s, err = c.inner.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &gatedStmt{inner: s, conn: c, write: isWrite(query)}, nil
}

func (c *gatedConn) Close() error { return c.inner.Close() }

func (c *gatedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *gatedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	b, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.New("sqlite connection cannot begin a transaction with options")
	}
	if opts.ReadOnly {
		return b.BeginTx(ctx, opts)
	}
	if err := c.gate.acquire(ctx); err != nil {
		return nil, err
	}
	release := c.gate.once()
	tx, err := b.BeginTx(ctx, opts)
	if err != nil {
		release()
		return nil, err
	}
	c.inTx = true
	return &gatedTx{inner: tx, done: func() { c.inTx = false; release() }}, nil
}

// turn takes the gate for a statement outside a transaction that may write,
// and returns the function that gives it back (a no-op otherwise).
func (c *gatedConn) turn(ctx context.Context, write bool) (func(), error) {
	if c.inTx || !write {
		return func() {}, nil
	}
	if err := c.gate.acquire(ctx); err != nil {
		return nil, err
	}
	return c.gate.once(), nil
}

func (c *gatedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	release, err := c.turn(ctx, isWrite(query))
	if err != nil {
		return nil, err
	}
	defer release()
	return e.ExecContext(ctx, query, args)
}

func (c *gatedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	release, err := c.turn(ctx, isWrite(query))
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, query, args)
	if err != nil {
		release()
		return nil, err
	}
	// INSERT ... RETURNING writes as its rows are read: keep the turn
	// until they are closed.
	return &gatedRows{Rows: rows, release: release}, nil
}

func (c *gatedConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *gatedConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *gatedConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

type gatedTx struct {
	inner driver.Tx
	done  func()
}

func (t *gatedTx) Commit() error {
	defer t.done()
	return t.inner.Commit()
}

func (t *gatedTx) Rollback() error {
	defer t.done()
	return t.inner.Rollback()
}

type gatedStmt struct {
	inner driver.Stmt
	conn  *gatedConn
	write bool
}

func (s *gatedStmt) Close() error  { return s.inner.Close() }
func (s *gatedStmt) NumInput() int { return s.inner.NumInput() }

func (s *gatedStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, errors.New("sqlite statement: use ExecContext")
}

func (s *gatedStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, errors.New("sqlite statement: use QueryContext")
}

func (s *gatedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	e, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, errors.New("sqlite statement cannot execute with a context")
	}
	release, err := s.conn.turn(ctx, s.write)
	if err != nil {
		return nil, err
	}
	defer release()
	return e.ExecContext(ctx, args)
}

func (s *gatedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, errors.New("sqlite statement cannot query with a context")
	}
	release, err := s.conn.turn(ctx, s.write)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, args)
	if err != nil {
		release()
		return nil, err
	}
	return &gatedRows{Rows: rows, release: release}, nil
}

type gatedRows struct {
	driver.Rows
	release func()
}

func (r *gatedRows) Close() error {
	defer r.release()
	return r.Rows.Close()
}
