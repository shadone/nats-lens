package storeprov_test

import (
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/store"
)

// Sizing, run with APPS_SIZE set: the Applications page of an epoch of
// 4,096 connections of 500 applications (8 pods each, some with two
// connections) on 3 servers, with a day of 30-second epochs, 20,000
// client events in the day and 14 days of hourly traffic in two
// accounts. It logs the provider's read and FinishApplications, best of
// 5.
func TestApplicationsSize(t *testing.T) {
	if os.Getenv("APPS_SIZE") == "" {
		t.Skip("APPS_SIZE not set")
	}
	st, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	db := st.DB()
	last := time.Date(2026, 10, 17, 14, 25, 0, 0, time.UTC)
	const epochs = 2880
	first := last.Add(-time.Duration(epochs-1) * 30 * time.Second)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO servers (server_id, server_name, first_epoch, last_epoch, first_seen_at, last_seen_at)
SELECT 'S' || i, 'nats-' || i, 1, $1, make_timestamp($2::BIGINT), make_timestamp($3::BIGINT) FROM range(0, 3) r(i)`,
		epochs, first.UnixMicro(), last.UnixMicro())
	exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
SELECT e, make_timestamp($1::BIGINT + (e - 1) * 30000000), make_timestamp($1::BIGINT + (e - 1) * 30000000), 0, 3, 3, [], [], [], 0, false, []
FROM range(1, $2::BIGINT + 1) r(e)`, first.UnixMicro(), epochs)
	exec(`INSERT INTO connections (epoch, server_id, cid, kind, type, name, lang, version, account, ip, port, start_time,
  rtt_ns, in_msgs, out_msgs, in_bytes, out_bytes, subscriptions, pending_bytes)
SELECT $1, 'S' || (i % 3), i, 'Client', 'nats', 'app-' || (i % 500) || '@pod-' || (i % 4000), CASE WHEN i % 7 = 0 THEN 'nats.js' ELSE 'go' END,
  CASE WHEN i % 7 = 0 THEN '3.0.2' ELSE '1.54.0' END, CASE WHEN i % 500 < 250 THEN 'APP' ELSE 'OPS' END,
  '10.0.' || (i // 256) || '.' || (i % 256), 40000, make_timestamp($2::BIGINT), 1000000 + i * 100, i, i, i, i, 5, i % 1000
FROM range(0, 4096) r(i)`, epochs, first.UnixMicro())
	exec(`INSERT INTO events (id, time, received_at, source, kind, client_name, client_host, account, reason, summary, raw)
SELECT i, make_timestamp($1::BIGINT - i * 4300000), make_timestamp($1::BIGINT - i * 4300000), 'sys',
  ['client.connect', 'client.disconnect', 'client.churn'][i % 3 + 1], 'app-' || (i % 500) || '@pod-' || (i % 4000), '10.0.0.1',
  CASE WHEN i % 500 < 250 THEN 'APP' ELSE 'OPS' END, ['Stale Connection', 'Slow Consumer (Write Deadline)', 'Client Closed'][i % 3 + 1],
  'event', CASE WHEN i % 3 = 2 THEN '{"connects": 4}' END
FROM range(1, 20001) r(i)`, last.UnixMicro())
	exec(`INSERT INTO client_app_hours
SELECT make_timestamp($1::BIGINT) - to_hours(h), acc, 'app-' || a, false, 1000 + a, 2000 + a, 100000 + a, 200000 + a, 120
FROM range(0, 14 * 24) r(h), range(0, 500) x(a), (VALUES ('APP'), ('OPS')) v(acc)`, last.Truncate(time.Hour).UnixMicro())
	exec(`CHECKPOINT`)

	clock := last.Add(30 * time.Second)
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }, Retention: 14 * day}
	req := &v1.GetApplicationsRequest{Range: &v1.TimeRange{Start: timestamppb.New(clock.Add(-24 * time.Hour)), End: timestamppb.New(clock)}}
	readBest, finishBest := time.Hour, time.Hour
	var res *v1.GetApplicationsResponse
	for range 5 {
		start := time.Now()
		f, err := p.Applications(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		read := time.Since(start)
		start = time.Now()
		res = api.FinishApplications(f, "", false)
		readBest, finishBest = min(readBest, read), min(finishBest, time.Since(start))
	}
	if res.GetApplicationsTotal() != 500 || res.GetConnectionsTotal() != 4096 || res.GetApplications()[0].InMsgs_24H == nil {
		t.Fatalf("%d applications, %d connections", res.GetApplicationsTotal(), res.GetConnectionsTotal())
	}
	t.Logf("Applications of 4,096 connections in 500 applications: read best of 5 %s, FinishApplications %s",
		readBest.Round(time.Millisecond), finishBest.Round(time.Millisecond))
}
