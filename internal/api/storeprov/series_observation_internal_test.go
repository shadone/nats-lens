package storeprov

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

type observationWriter struct {
	*sql.DB
	write   func()
	written bool
}

func (r *observationWriter) mutate() {
	if !r.written {
		r.written = true
		r.write()
	}
}
func (r *observationWriter) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	if strings.Contains(q, "/* observation_summary */") {
		r.mutate()
	}
	return r.DB.QueryRowContext(ctx, q, args...)
}
func (r *observationWriter) BeginTx(ctx context.Context, o *sql.TxOptions) (*sql.Tx, error) {
	tx, err := r.DB.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM epochs").Scan(&n); err != nil {
		tx.Rollback()
		return nil, err
	}
	r.mutate()
	return tx, nil
}
func TestSeriesObservationSharesReadBoundary(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, err = st.DB().Exec("INSERT INTO servers(server_id,server_name,first_epoch,last_epoch,first_seen_at,last_seen_at) VALUES ('S0','nats-0',1,1,TIMESTAMP '2026-10-17 12:00:00',TIMESTAMP '2026-10-17 12:00:00')"); err != nil {
		t.Fatal(err)
	}
	add := func(epoch int, at time.Time, cpu int) {
		t.Helper()
		tx, e := st.DB().BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if _, e = tx.Exec("INSERT INTO epochs(epoch,started_at,finished_at,duration_ms,expected_servers,observed_servers,expected_names,observed_names,missing_names,endpoint_failures,partial,errors) VALUES (?,?,?,0,1,1,[],[],[],0,false,[])", epoch, at, at); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec("INSERT INTO server_stats(epoch,server_id,cpu_percent) VALUES (?,'S0',?)", epoch, cpu); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	add(1, at, 10)
	spy := &observationWriter{DB: st.DB()}
	spy.write = func() { add(2, at.Add(30*time.Second), 1000) }
	p := &Provider{St: st, Interval: 30 * time.Second, readDB: spy}
	out, err := p.GetTimeSeries(ctx, &v1.GetTimeSeriesRequest{Metric: "server.cpu_percent", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}, Range: &v1.TimeRange{Start: ts(at), End: ts(at.Add(2 * time.Minute))}, MaxPoints: 1, IncludeObservations: true})
	if err != nil {
		t.Fatal(err)
	}
	if !spy.written {
		t.Fatal("concurrent append did not execute")
	}
	if out.Series.GetObservations().GetMaximum() != 10 || out.Series.GetPoints()[0].Value != 10 {
		t.Fatalf("summary crossed plot boundary: %v", out.Series)
	}
}

func TestSeriesObservationOptInBoundaryAndCancellation(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	spy := &observationWriter{DB: st.DB(), write: func() {}}
	p := &Provider{St: st, Interval: 30 * time.Second, readDB: spy}
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	req := &v1.GetTimeSeriesRequest{Metric: "cluster.connections", Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}, Range: &v1.TimeRange{Start: ts(at), End: ts(at.Add(time.Hour))}, MaxPoints: 1}
	if out, e := p.GetTimeSeries(ctx, req); e != nil || out.Series.Observations != nil || spy.written {
		t.Fatalf("legacy query changed transaction/work: %v %v", out, e)
	}
	req.IncludeObservations = true
	if _, e := p.GetTimeSeries(ctx, req); e != nil || !spy.written {
		t.Fatalf("opt-in boundary absent: %v", e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := p.GetTimeSeries(canceled, req); e == nil {
		t.Fatal("cancelled request succeeded")
	}
	req.Range.Start = ts(at.Add(-31 * 24 * time.Hour))
	if _, e := p.GetTimeSeries(ctx, req); e == nil {
		t.Fatal("wide raw summary succeeded")
	}
}
