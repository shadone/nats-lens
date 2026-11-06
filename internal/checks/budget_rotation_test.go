package checks

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/shadone/nats-lens/internal/config"
	"github.com/shadone/nats-lens/internal/store"
)

func TestBudgetRotates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rotation.duckdb")
	st, err := store.OpenPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	f := &fx{t: t, st: st}
	for epoch := int64(1); epoch <= 6; epoch++ {
		f.epoch(epoch, []string{"nats-0"})
	}
	// Constant fixture SQL only (ASVS 1.2.4); the observer forwards SQL and
	// bound arguments unchanged, except when deliberately exhausting a query.
	files := map[string]string{
		"stream-901": `SELECT 'A/x' AS entity_key, 'first' AS detail`,
		"stream-902": `SELECT entity_key FROM no_such_table`,
		"stream-903": `SELECT 'A/z' AS entity_key, 'last' AS detail`,
	}
	e := engineWith(t, config.Checks{Timeout: config.Duration(10 * time.Second), Disabled: []string{SlowRouteCode, LeafHealthCode}}, files)
	connector, err := duckdb.NewConnector(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connector.Close() })
	observer := &budgetObserver{Connector: connector, queries: files, t: t}
	db := sql.OpenDB(observer)
	t.Cleanup(func() { _ = db.Close() })
	// LoadFS includes both built-in server checks and system-001. The
	// disabled server checks still occupy rotation positions. In particular,
	// epochs 1 and 2 legitimately reach the SAME first SQL check.
	wantOrder := [][]string{
		{"stream-901", "stream-902", "stream-903"},
		{"stream-901", "stream-902", "stream-903"},
		{"stream-902", "stream-903", "stream-901"},
		{"stream-903", "stream-901", "stream-902"},
		{"stream-901", "stream-902", "stream-903"},
		{"stream-901", "stream-902", "stream-903"},
	}
	for _, exhaust := range []bool{false, true} {
		name := "ordinary"
		e.Budget = 0
		if exhaust {
			name = "exhausted"
			e.Budget = 200 * time.Millisecond
		}
		t.Run(name, func(t *testing.T) {
			observer.t = t
			observer.exhaust = exhaust
			for i, order := range wantOrder {
				observer.seen = nil
				runs, err := e.Evaluate(ctx, db, int64(i+1))
				if err != nil {
					t.Fatal(err)
				}
				want := order
				if exhaust {
					want = order[:1]
				}
				if !reflect.DeepEqual(observer.seen, want) {
					t.Fatalf("epoch %d query order = %v, want %v", i+1, observer.seen, want)
				}
				if len(runs) != 6 {
					t.Fatalf("epoch %d: got %d runs, want 6", i+1, len(runs))
				}
				for _, r := range runs {
					switch r.Code {
					case SlowRouteCode, LeafHealthCode:
						if r.Status != store.RunDisabled {
							t.Fatalf("disabled check: %+v", r)
						}
					case CheckFailedCode:
						if r.Status != store.RunOK || len(r.Rows) != 1 || r.Rows[0].Value == nil {
							t.Fatalf("system finding: %+v", r)
						}
						failed := float64(1)
						if exhaust {
							failed = 3
						}
						if *r.Rows[0].Value != failed {
							t.Fatalf("failed count = %v, want %v", *r.Rows[0].Value, failed)
						}
					default:
						if exhaust {
							if r.Status != store.RunError {
								t.Fatalf("budget error: %+v", r)
							}
							skipped := strings.HasPrefix(r.Error, "not run:")
							if skipped != (r.Code != order[0]) {
								t.Fatalf("epoch %d: attempted %s, run %+v", i+1, order[0], r)
							}
						} else if r.Code == "stream-902" {
							if r.Status != store.RunError || !strings.Contains(r.Error, "no_such_table") {
								t.Fatalf("SQL error: %+v", r)
							}
						} else if r.Status != store.RunOK || len(r.Rows) != 1 {
							t.Fatalf("ordinary query: %+v", r)
						}
					}
				}
			}
		})
	}
}

// budgetObserver observes actual driver query entry, rather than the sorted
// output of Evaluate. Waiting for the query context makes budget consumption
// independent of DuckDB query planning, execution speed, and error wording.
// All setup, ordinary queries and result scanning still use real DuckDB.
type budgetObserver struct {
	driver.Connector
	queries map[string]string
	seen    []string
	exhaust bool
	t       *testing.T
}

func (o *budgetObserver) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := o.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &budgetConn{Conn: conn, observer: o}, nil
}

type budgetConn struct {
	driver.Conn
	observer *budgetObserver
}

func (c *budgetConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}

func (c *budgetConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	o := c.observer
	for code, query := range o.queries {
		if q != query {
			continue
		}
		o.seen = append(o.seen, code)
		if o.exhaust {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 200*time.Millisecond {
				o.t.Fatal("query did not inherit epoch budget")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
