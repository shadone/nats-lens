package store

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"

	"github.com/shadone/nats-lens/internal/admit"
	"github.com/shadone/nats-lens/internal/timing"
)

// IsFatal classifies wrapped driver errors without relying on diagnostic text.
func IsFatal(err error) bool {
	var de *duckdb.Error
	return errors.As(err, &de) && de.Type == duckdb.ErrorTypeFatal
}

type fatalDiagnostic struct{ cause *duckdb.Error }

func (e *fatalDiagnostic) Error() string { return e.cause.Error() }
func (e *fatalDiagnostic) Unwrap() error { return e.cause }

type engineFailure struct {
	err error
	at  time.Time
}

// engineGuard belongs to one connector (one DuckDB engine), across its SQL
// connections. Already running calls unwind; subsequent calls never enter it.
// Cleanup still releases native resources, but rollback cannot issue more SQL.
type engineGuard struct {
	failure atomic.Pointer[engineFailure]
	ctx     context.Context
	cancel  context.CancelFunc
	// system labels the engine's query timings; see Store.SetSystem.
	system atomic.Pointer[string]
	// gate is the query budget of the API and /metrics; nil admits every
	// query. See Store.SetQueryBudget.
	gate atomic.Pointer[admit.Gate]
}

func newEngineGuard() *engineGuard {
	ctx, cancel := context.WithCancel(context.Background())
	return &engineGuard{ctx: ctx, cancel: cancel}
}
func (g *engineGuard) err() error {
	if f := g.failure.Load(); f != nil {
		return f.err
	}
	return nil
}
func (g *engineGuard) observe(stage string, err error) error {
	if !IsFatal(err) {
		return err
	}
	safe := SafeError(stage, err)
	g.failure.CompareAndSwap(nil, &engineFailure{err: safe, at: time.Now()})
	g.cancel()
	return g.err()
}

// SafeError retains typed fatal classification while removing all untrusted
// diagnostic text, including wrapping errors. Nonfatal errors are unchanged.
func SafeError(stage string, err error) error {
	if !IsFatal(err) {
		return err
	}
	var safe *fatalDiagnostic
	if errors.As(err, &safe) {
		return safe
	}
	// Never copy arbitrary driver text: even its first line can contain values,
	// SQL or paths. Keep only a known mechanism and the operation stage.
	summary := "engine invalidated; diagnostic details omitted"
	var de *duckdb.Error
	errors.As(err, &de)
	if strings.Contains(de.Msg, "Failed to delete all rows from index") {
		summary = "index deletion failed; diagnostic details omitted"
	}
	return &fatalDiagnostic{cause: &duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: fmt.Sprintf("DuckDB fatal at %s: %s; database work stopped", stage, summary)}}
}
func (g *engineGuard) callContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(g.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

// Failure is the bounded first fatal error, or nil while the engine is usable.
func (s *Store) Failure() error {
	if s.guard == nil {
		return nil
	}
	return s.guard.err()
}

// FailedAt is when the first fatal error was observed, or zero while healthy.
func (s *Store) FailedAt() time.Time {
	if s.guard != nil {
		if f := s.guard.failure.Load(); f != nil {
			return f.at
		}
	}
	return time.Time{}
}

// Failed closes at the first fatal error. It is never reset or reopened.
func (s *Store) Failed() <-chan struct{} {
	if s.guard == nil {
		return nil
	}
	return s.guard.ctx.Done()
}

type guardedConnector struct {
	driver.Connector
	g *engineGuard
}

func (c *guardedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := c.g.err(); err != nil {
		return nil, err
	}
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, c.g.observe("connect", err)
	}
	return &guardedConn{Conn: conn, g: c.g}, nil
}

// database/sql owns and closes the engine connector as well as connections.
func (c *guardedConnector) Close() error {
	return c.g.observe("close", c.Connector.(interface{ Close() error }).Close())
}

type guardedConn struct {
	driver.Conn
	g *engineGuard
	// inTx is set while the connection runs a transaction, whose
	// statements never wait for the query budget: a write holds the
	// store's write lock around its transaction.
	inTx atomic.Bool
}

// enter takes a slot of the query budget for a query of the API or of
// /metrics outside a transaction; every other query passes at once. The
// returned func gives the slot back.
func (g *engineGuard) enter(ctx context.Context, inTx bool) (func(), error) {
	gate := g.gate.Load()
	if gate == nil || inTx {
		return func() {}, nil
	}
	src := timing.Source(ctx)
	if src != timing.SourceAPI && src != timing.SourceMetrics {
		return func() {}, nil
	}
	return gate.Enter(ctx, admit.Request{Source: src, Priority: src == timing.SourceMetrics, Abort: g.ctx.Done(), AbortErr: g.err})
}

func (c *guardedConn) CheckNamedValue(v *driver.NamedValue) error {
	return c.Conn.(driver.NamedValueChecker).CheckNamedValue(v)
}
func (c *guardedConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.g.err(); err != nil {
		return nil, err
	}
	defer c.g.timed(ctx, q, args, time.Now())
	ctx, done := c.g.callContext(ctx)
	defer done()
	r, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	return r, c.g.observe("execute", err)
}
func (c *guardedConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.g.err(); err != nil {
		return nil, err
	}
	release, err := c.g.enter(ctx, c.inTx.Load())
	if err != nil {
		return nil, err
	}
	done := joined(c.g.timedQuery(ctx, q, args), release)
	ctx, stop := c.g.callContext(ctx)
	done = joined(stop, done)
	r, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil {
		done()
		return nil, c.g.observe("query", err)
	}
	return &guardedRows{Rows: r, g: c.g, done: done}, nil
}
func (c *guardedConn) Prepare(q string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), q)
}
func (c *guardedConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if err := c.g.err(); err != nil {
		return nil, err
	}
	ctx, done := c.g.callContext(ctx)
	defer done()
	s, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if err != nil {
		return nil, c.g.observe("prepare", err)
	}
	return &guardedStmt{Stmt: s, g: c.g, q: q, conn: c}, nil
}
func (c *guardedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *guardedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := c.g.err(); err != nil {
		return nil, err
	}
	ctx, done := c.g.callContext(ctx)
	defer done()
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, c.g.observe("begin", err)
	}
	c.inTx.Store(true)
	return &guardedTx{Tx: tx, g: c.g, conn: c}, nil
}
func (c *guardedConn) Close() error { return c.g.observe("close connection", c.Conn.Close()) }

type guardedTx struct {
	driver.Tx
	g    *engineGuard
	conn *guardedConn
}

func (t *guardedTx) Commit() error {
	defer t.conn.inTx.Store(false)
	if err := t.g.err(); err != nil {
		return err
	}
	return t.g.observe("commit", t.Tx.Commit())
}
func (t *guardedTx) Rollback() error {
	defer t.conn.inTx.Store(false)
	if err := t.g.err(); err != nil {
		return err
	}
	return t.g.observe("rollback", t.Tx.Rollback())
}

type guardedStmt struct {
	driver.Stmt
	g    *engineGuard
	q    string
	conn *guardedConn
}

func (s *guardedStmt) Exec(args []driver.Value) (driver.Result, error) {
	if err := s.g.err(); err != nil {
		return nil, err
	}
	r, err := s.Stmt.Exec(args)
	return r, s.g.observe("statement execute", err)
}
func (s *guardedStmt) Query(args []driver.Value) (driver.Rows, error) {
	if err := s.g.err(); err != nil {
		return nil, err
	}
	r, err := s.Stmt.Query(args)
	if err != nil {
		return nil, s.g.observe("statement query", err)
	}
	return &guardedRows{Rows: r, g: s.g}, nil
}
func (s *guardedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.g.err(); err != nil {
		return nil, err
	}
	defer s.g.timed(ctx, s.q, args, time.Now())
	ctx, done := s.g.callContext(ctx)
	defer done()
	r, err := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
	return r, s.g.observe("statement execute", err)
}
func (s *guardedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.g.err(); err != nil {
		return nil, err
	}
	release, err := s.g.enter(ctx, s.conn.inTx.Load())
	if err != nil {
		return nil, err
	}
	done := joined(s.g.timedQuery(ctx, s.q, args), release)
	ctx, stop := s.g.callContext(ctx)
	done = joined(stop, done)
	r, err := s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
	if err != nil {
		done()
		return nil, s.g.observe("statement query", err)
	}
	return &guardedRows{Rows: r, g: s.g, done: done}, nil
}
func (s *guardedStmt) Close() error { return s.g.observe("close statement", s.Stmt.Close()) }

type guardedRows struct {
	done func()
	driver.Rows
	g *engineGuard
}

func (r *guardedRows) Next(v []driver.Value) error {
	if err := r.g.err(); err != nil {
		return err
	}
	return r.g.observe("read rows", r.Rows.Next(v))
}
func (r *guardedRows) Close() error {
	if r.done != nil {
		defer r.done()
	}
	return r.g.observe("close rows", r.Rows.Close())
}
func (r *guardedRows) ColumnTypeScanType(i int) reflect.Type {
	return r.Rows.(driver.RowsColumnTypeScanType).ColumnTypeScanType(i)
}
func (r *guardedRows) ColumnTypeDatabaseTypeName(i int) string {
	return r.Rows.(driver.RowsColumnTypeDatabaseTypeName).ColumnTypeDatabaseTypeName(i)
}
