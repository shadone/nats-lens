package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/shadone/nats-lens/internal/events"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

func TestDatasetCopyActiveBoundary(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "data.duckdb")
	var captured int64
	err = s.SnapshotCopy(ctx, dest, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT max(epoch) FROM epochs").Scan(&captured); err != nil {
			return err
		}
		// A separate SQL connection must commit while the read snapshot remains open.
		done := make(chan error, 1)
		go func() { _, e := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Minute))); done <- e }()
		select {
		case e := <-done:
			return e
		case <-time.After(15 * time.Second):
			t.Error("collection stalled by export")
			return context.DeadlineExceeded
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := duckdb.NewConnector(dest+"?access_mode=read_only", nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(c)
	defer db.Close()
	var newest int64
	if err = db.QueryRow("SELECT max(epoch) FROM epochs").Scan(&newest); err != nil {
		t.Fatal(err)
	}
	if newest != captured || captured != 1 {
		t.Fatalf("capture %d export %d", captured, newest)
	}
	if err = CheckSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"SELECT count(*) FROM duckdb_indexes()", "SELECT count(*) FROM duckdb_sequences()", "SELECT count(*) FROM duckdb_columns() WHERE comment IS NOT NULL"} {
		var a, b int
		if err = s.db.QueryRow(q).Scan(&a); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(q).Scan(&b); err != nil {
			t.Fatal(err)
		}
		if a != b || a == 0 {
			t.Errorf("catalog count %s: source %d export %d", q, a, b)
		}
	}
}

func TestDatasetCatalogAndContentsConserved(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`CREATE VIEW snapshot_epochs AS SELECT epoch FROM epochs; COMMENT ON VIEW snapshot_epochs IS 'Synthetic collection view';`); err != nil {
		t.Fatal(err)
	}
	qs := []string{
		`SELECT table_name,sql,comment FROM duckdb_tables() WHERE NOT internal ORDER BY table_name`,
		`SELECT table_name,column_name,data_type,column_default,is_nullable,comment FROM duckdb_columns() WHERE NOT internal ORDER BY table_name,column_index`,
		`SELECT view_name,sql,comment FROM duckdb_views() WHERE NOT internal ORDER BY view_name`,
		`SELECT table_name,constraint_type,constraint_text FROM duckdb_constraints() ORDER BY table_name,constraint_index`,
		`SELECT index_name,table_name,sql FROM duckdb_indexes() ORDER BY index_name`,
		`SELECT sequence_name,min_value,max_value,increment_by,cycle FROM duckdb_sequences() ORDER BY sequence_name`,
	}
	var before []string
	dest := filepath.Join(t.TempDir(), "captured.duckdb")
	err = s.SnapshotCopy(ctx, dest, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT table_name FROM duckdb_tables() WHERE NOT internal ORDER BY table_name`)
		if e != nil {
			return e
		}
		var names []string
		for rows.Next() {
			var n string
			if e = rows.Scan(&n); e != nil {
				rows.Close()
				return e
			}
			names = append(names, n)
		}
		rows.Close()
		for _, n := range names {
			qs = append(qs, `SELECT * FROM "`+n+`"`)
		}
		for _, q := range qs {
			x, e := datasetResult(ctx, tx, q)
			if e != nil {
				return e
			}
			before = append(before, x)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := duckdb.NewConnector(dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(c)
	defer db.Close()
	for i, q := range qs {
		after, e := datasetResult(ctx, db, q)
		if e != nil {
			t.Fatal(e)
		}
		if before[i] != after {
			t.Errorf("conservation mismatch %s\nsource %s\nexport %s", q, before[i], after)
		}
	}
	var next int64
	if err = db.QueryRow(`SELECT nextval('epoch_seq')`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next <= 1 {
		t.Fatalf("sequence reused epoch %d", next)
	}
}

type datasetQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func datasetResult(ctx context.Context, q datasetQueryer, query string) (string, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return "", err
	}
	var all []string
	for rows.Next() {
		v := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range v {
			ptr[i] = &v[i]
		}
		if err = rows.Scan(ptr...); err != nil {
			return "", err
		}
		row, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		all = append(all, string(row))
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	sort.Strings(all)
	b, err := json.Marshal(all)
	return string(b), err
}

func TestDatasetConcurrentPublishedStagesAndCancellation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.WriteSnapshot(ctx, fixtureSnapshot(t, t0)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyCheckRuns(ctx, 1, []CheckRun{okRun("stream-001", frow("APP/jobs", "critical", "Synthetic finding."))}); err != nil {
		t.Fatal(err)
	}
	if err = s.WriteEvents(ctx, []events.Event{{Time: t0, ReceivedAt: t0, Source: events.SourceCollector, Kind: "collector.gap", Summary: "Synthetic event"}}); err != nil {
		t.Fatal(err)
	}
	mute, err := s.CreateMute(ctx, Mute{CheckCode: "stream-001", Reason: "Synthetic review", MutedBy: "operator", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Acknowledge(ctx, Ack{FindingID: FindingID("stream-001", "APP/jobs"), AckedBy: "operator", CreatedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if i, err := os.Stat(filepath.Join(dir, FileName) + ".wal"); err != nil || i.Size() == 0 {
		t.Fatalf("synthetic WAL activity missing: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "active.duckdb")
	var before []string
	qs := []string{`SELECT count(*),bit_xor(hash(t))::VARCHAR FROM epochs t`, `SELECT count(*),bit_xor(hash(t))::VARCHAR FROM events t`, `SELECT count(*),bit_xor(hash(t))::VARCHAR FROM check_runs t`, `SELECT count(*),bit_xor(hash(t))::VARCHAR FROM findings t`, `SELECT count(*),bit_xor(hash(t))::VARCHAR FROM mutes t`, `SELECT count(*),bit_xor(hash(t))::VARCHAR FROM acknowledgements t`}
	err = s.SnapshotCopy(ctx, dest, func(tx *sql.Tx) error {
		for _, q := range qs {
			x, e := datasetResult(ctx, tx, q)
			if e != nil {
				return e
			}
			before = append(before, x)
		}
		// All separately published stages can progress under the pinned source read.
		if _, e := s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(time.Minute))); e != nil {
			return e
		}
		if e := s.WriteEvents(ctx, []events.Event{{Time: t0.Add(time.Minute), ReceivedAt: t0.Add(time.Minute), Source: events.SourceCollector, Kind: "collector.gap", Summary: "Later synthetic event"}}); e != nil {
			return e
		}
		if _, e := s.ApplyCheckRuns(ctx, 2, []CheckRun{okRun("stream-001", frow("APP/jobs", "warning", "Changed finding."))}); e != nil {
			return e
		}
		if _, e := s.UnmuteMute(ctx, mute.ID, "operator", t0.Add(time.Minute)); e != nil {
			return e
		}
		_, e := s.Acknowledge(ctx, Ack{FindingID: FindingID("stream-001", "APP/jobs"), AckedBy: "second-operator", CreatedAt: t0.Add(time.Minute)})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := ReadOnlyDB(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, q := range qs {
		after, e := datasetResult(ctx, db, q)
		if e != nil {
			t.Fatal(e)
		}
		if before[i] != after {
			t.Errorf("published stage crossed boundary: %s", q)
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	aborted := filepath.Join(t.TempDir(), "aborted.duckdb")
	err = s.SnapshotCopy(cancelCtx, aborted, func(*sql.Tx) error { cancel(); return cancelCtx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	// Cleanup must detach and unwind without invalidating the collector engine.
	if _, err = s.WriteSnapshot(ctx, fixtureSnapshot(t, t0.Add(2*time.Minute))); err != nil {
		t.Fatalf("collection after cancel: %v", err)
	}
	if s.Failure() != nil {
		t.Fatalf("source engine invalidated: %v", s.Failure())
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("source restart: %v", err)
	}
	defer reopened.Close()
	var latest int
	if err = reopened.DB().QueryRow("SELECT max(epoch) FROM epochs").Scan(&latest); err != nil || latest != 3 {
		t.Fatalf("source restart epoch=%d err=%v", latest, err)
	}
}
