package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

// The injected boundary counts native calls, so a poisoned engine returning
// errors on its own cannot accidentally satisfy the containment assertion.
func TestFatalBoundaryRefusesNativeOperations(t *testing.T) {
	for _, stage := range []string{"execute", "query", "prepare", "begin", "commit", "rollback", "statement execute", "statement query", "read rows"} {
		t.Run(stage, func(t *testing.T) {
			f := &fatalDriver{stage: stage}
			g := newEngineGuard()
			db := sql.OpenDB(&guardedConnector{Connector: f, g: g})
			defer db.Close()
			ctx := context.Background()
			// Prepare a statement before arming the fatal boundary.
			stmt, err := db.PrepareContext(ctx, "SELECT ?")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			f.armed = true
			switch stage {
			case "execute":
				_, err = db.ExecContext(ctx, "write")
			case "query":
				_, err = db.QueryContext(ctx, "read")
			case "prepare":
				_, err = db.PrepareContext(ctx, "another")
			case "begin":
				_, err = db.BeginTx(ctx, nil)
			case "commit", "rollback":
				var tx *sql.Tx
				tx, err = db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				if stage == "commit" {
					err = tx.Commit()
				} else {
					err = tx.Rollback()
				}
			case "statement execute":
				_, err = stmt.ExecContext(ctx, 1)
			case "statement query":
				_, err = stmt.QueryContext(ctx, 1)
			case "read rows":
				var n int
				err = db.QueryRowContext(ctx, "read").Scan(&n)
			}
			if !IsFatal(err) {
				t.Fatalf("expected wrapped typed fatal, got %T", err)
			}
			if strings.Contains(err.Error(), "secret") || len(err.Error()) > 512 {
				t.Error("unbounded diagnostic")
			}
			select {
			case <-g.ctx.Done():
			default:
				t.Fatal("fatal did not signal workers")
			}
			before := f.calls.Load()
			for _, attempt := range []func() error{
				func() error { _, e := db.ExecContext(ctx, "write"); return e },
				func() error { _, e := db.QueryContext(ctx, "read"); return e },
				func() error { _, e := db.PrepareContext(ctx, "prepare"); return e },
				func() error { _, e := db.BeginTx(ctx, nil); return e },
				func() error { _, e := stmt.ExecContext(ctx, 1); return e },
				func() error { _, e := stmt.QueryContext(ctx, 1); return e },
			} {
				if e := attempt(); !IsFatal(e) {
					t.Fatalf("operation returned %v", e)
				}
			}
			if got := f.calls.Load(); got != before {
				t.Errorf("native calls after fatal: %d", got-before)
			}
		})
	}
}

func TestFatalClassificationAndSafeSummary(t *testing.T) {
	for _, err := range []error{nil, errors.New("FATAL Chunk: secret"), &duckdb.Error{Type: duckdb.ErrorTypeInvalidInput, Msg: "FATAL secret"}} {
		if IsFatal(err) || SafeError("query", err) != err {
			t.Error("nonfatal error changed")
		}
	}
	raw := fmt.Errorf("private wrapper: %w", &duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: "Failed to delete all rows from index. Chunk: secret"})
	safe := SafeError("commit", raw)
	if !IsFatal(safe) || strings.Contains(safe.Error(), "private") || strings.Contains(safe.Error(), "secret") || !strings.Contains(safe.Error(), "index deletion failed") {
		t.Errorf("bad safe summary: %v", safe)
	}
	if rewrapped := SafeError("retention sweep", fmt.Errorf("secret: %w", safe)); rewrapped != safe {
		t.Error("sanitizing a wrapper lost the safe original stage")
	}
	if e := openError("synthetic.duckdb", fmt.Errorf("replaying WAL: %w", raw)); errors.Is(e, ErrWALReplay) {
		t.Error("typed fatal must never select automatic WAL rebuild")
	}
}

type fatalDriver struct {
	block chan struct{}
	stage string
	armed bool
	calls atomic.Int64
}

func (f *fatalDriver) hit(stage string) error {
	f.calls.Add(1)
	if f.armed && f.stage == stage {
		return fmt.Errorf("secret wrapper: %w", &duckdb.Error{Type: duckdb.ErrorTypeFatal, Msg: strings.Repeat("secret", 30000)})
	}
	return nil
}
func (f *fatalDriver) Connect(context.Context) (driver.Conn, error) { return f, f.hit("connect") }
func (f *fatalDriver) Driver() driver.Driver                        { return nil }
func (f *fatalDriver) Close() error                                 { return nil }
func (f *fatalDriver) CheckNamedValue(*driver.NamedValue) error     { return nil }
func (f *fatalDriver) Prepare(string) (driver.Stmt, error) {
	return &fatalStatement{f}, f.hit("prepare")
}
func (f *fatalDriver) PrepareContext(context.Context, string) (driver.Stmt, error) {
	return f.Prepare("")
}
func (f *fatalDriver) Begin() (driver.Tx, error)                                    { return f, f.hit("begin") }
func (f *fatalDriver) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) { return f.Begin() }
func (f *fatalDriver) Commit() error                                                { return f.hit("commit") }
func (f *fatalDriver) Rollback() error                                              { return f.hit("rollback") }
func (f *fatalDriver) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), f.hit("execute")
}
func (f *fatalDriver) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if q == "block" {
		close(f.block)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &fatalRows{f: f}, f.hit("query")
}
func (f *fatalDriver) NumInput() int { return -1 }
func (f *fatalDriver) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), f.hit("statement execute")
}
func (f *fatalDriver) Query([]driver.Value) (driver.Rows, error) {
	return &fatalRows{f: f}, f.hit("statement query")
}

// A separate statement adapter preserves the context methods' distinct stages.
type fatalStatement struct{ *fatalDriver }

func (f *fatalStatement) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return f.Exec(nil)
}
func (f *fatalStatement) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return f.Query(nil)
}

type fatalRows struct {
	f    *fatalDriver
	done bool
}

func (r *fatalRows) Columns() []string { return []string{"n"} }
func (r *fatalRows) Close() error      { return nil }
func (r *fatalRows) Next(v []driver.Value) error {
	if err := r.f.hit("read rows"); err != nil {
		return err
	}
	if r.done {
		return io.EOF
	}
	r.done = true
	v[0] = int64(1)
	return nil
}

func TestFatalCancelsInflightQuery(t *testing.T) {
	f := &fatalDriver{stage: "execute", armed: true, block: make(chan struct{})}
	db := sql.OpenDB(&guardedConnector{Connector: f, g: newEngineGuard()})
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := db.QueryContext(ctx, "block"); done <- err }()
	select {
	case <-f.block:
	case <-ctx.Done():
		t.Fatal("query never entered boundary")
	}
	if _, err := db.ExecContext(ctx, "fatal"); !IsFatal(err) {
		t.Fatalf("expected fatal, got %T", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("query not canceled: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("fatal query cancellation hung")
	}
}
