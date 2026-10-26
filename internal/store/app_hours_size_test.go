package store

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"

	"github.com/shadone/nats-lens/internal/collect"
)

// Sizing, run with APP_HOURS_SIZE set to a number of hours: that many
// hours of 30-second epochs of 4,096 connections of 512 applications on 3
// servers in a file store. It logs the write of one epoch of 4,096
// connections, the hourly traffic's query alone (its share of the write), and the read of 24 hours for 500 applications from 14 days
// of client_app_hours.
func TestClientAppHoursSize(t *testing.T) {
	hours, _ := strconv.Atoi(os.Getenv("APP_HOURS_SIZE"))
	if hours == 0 {
		t.Skip("APP_HOURS_SIZE not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	const conns = 4096
	ids := []string{"NFAKESERVER0ID", "NFAKESERVER1ID", "NFAKESERVER2ID"}
	epochs := hours * 120
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	first := last.Add(-time.Duration(epochs) * 30 * time.Second)
	connStart := first.Add(-time.Hour)
	exec := func(q string, args ...any) {
		t.Helper()
		st := time.Now()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %.70s", time.Since(st).Round(time.Millisecond), q)
	}
	// Epoch e (1 to epochs) starts at first + 30 s * (e - 1); connection i
	// is on server i % 3 with cid i, application i % 512.
	exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
SELECT e, make_timestamp($1::BIGINT + (e - 1) * 30000000), make_timestamp($1::BIGINT + (e - 1) * 30000000), 0, 3, 3, [], [], [], 0, false, []
FROM range(1, $2::BIGINT + 1) r(e)`, first.UnixMicro(), epochs)
	exec(`INSERT INTO connections (epoch, server_id, cid, kind, type, name, lang, version, account, ip, port, start_time,
  in_msgs, out_msgs, in_bytes, out_bytes, subscriptions)
SELECT e, ['NFAKESERVER0ID', 'NFAKESERVER1ID', 'NFAKESERVER2ID'][i % 3 + 1], i, 'Client', 'nats',
  'app-' || (i % 512) || '@pod-' || i, 'go', '1.54.0', 'APP', '10.0.' || (i // 256) || '.' || (i % 256), 40000, make_timestamp($1::BIGINT),
  e * 10, e * 20, e * 1000, e * 2000, 5
FROM range(1, $2::BIGINT + 1) r(e), range(0, $3::BIGINT) c(i)`, connStart.UnixMicro(), epochs, conns)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartSequence(ctx, tx, "epoch_seq", fmt.Sprintf("SELECT %d", epochs+1)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CHECKPOINT`); err != nil {
		t.Fatal(err)
	}

	// The hourly traffic's query alone for the last stored epoch, rolled
	// back, best of 3.
	best := time.Hour
	for range 3 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		st := time.Now()
		if _, err := tx.Exec(appHoursQuery, epochs, last.Add(-30*time.Second).Truncate(time.Hour).UnixMicro(), max(1, epochs-120),
			last.Add(-time.Minute).UnixMicro()); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(st))
		_ = tx.Rollback()
	}
	t.Logf("hourly traffic of one epoch of %d connections over %d hours of rows: best of 3 %s", conns, hours, best.Round(time.Millisecond))

	// Whole epochs of 4,096 connections through WriteSnapshot.
	snapAt := func(i int) *collect.Snapshot {
		at := last.Add(time.Duration(i) * 30 * time.Second)
		snap := fixtureSnapshot(t, at)
		snap.Micro = nil
		mustApply(t, snap, collect.Connz, envelope(t, "nats-2", ids[2], server.Connz{ID: ids[2]}))
		for _, id := range ids {
			snap.Servers[id].Connz.Conns = nil
		}
		e := int64(epochs + 1 + i)
		for c := range conns {
			v := snap.Servers[ids[c%3]]
			v.Connz.Conns = append(v.Connz.Conns, &server.ConnInfo{Cid: uint64(c), Kind: "Client", Type: "nats",
				Name: fmt.Sprintf("app-%d@pod-%d", c%512, c), Lang: "go", Version: "1.54.0", Account: "APP",
				IP: fmt.Sprintf("10.0.%d.%d", c/256, c%256), Port: 40000, Start: connStart, NumSubs: 5,
				InMsgs: e * 10, OutMsgs: e * 20, InBytes: e * 1000, OutBytes: e * 2000})
		}
		return snap
	}
	timeWrites := func(name string) {
		var total time.Duration
		for i := range 3 {
			snap := snapAt(i)
			st := time.Now()
			if _, err := s.WriteSnapshot(ctx, snap); err != nil {
				t.Fatal(err)
			}
			total += time.Since(st)
		}
		t.Logf("%s: mean of 3 epoch writes %s", name, (total / 3).Round(time.Millisecond))
	}
	timeWrites("epoch of 4,096 connections with the hourly traffic")
	if n := count(t, db, `SELECT count(*) FROM client_app_hours`); n != 512 {
		t.Errorf("client_app_hours = %d rows, want 512", n)
	}

	// 14 days of hourly rows of 500 applications in two accounts, with a
	// loopback row for one in ten; the read of 24 hours as the
	// Applications page plans it.
	exec(`DELETE FROM client_app_hours`)
	exec(`INSERT INTO client_app_hours
SELECT make_timestamp($1::BIGINT) - to_hours(h), acc, 'app-' || a, false, 1000 + a, 2000 + a, 100000 + a, 200000 + a, 120
FROM range(0, 14 * 24) r(h), range(0, 500) x(a), (VALUES ('APP'), ('OPS')) v(acc)
UNION ALL
SELECT make_timestamp($1::BIGINT) - to_hours(h), 'APP', 'app-' || a, true, 1, 1, 1, 1, 120
FROM range(0, 14 * 24) r(h), range(0, 500, 10) x(a)`, last.Truncate(time.Hour).UnixMicro())
	read := `SELECT count(*) FROM (SELECT app, hour, sum(in_msgs), sum(out_msgs), sum(in_bytes), sum(out_bytes)
FROM client_app_hours
WHERE hour >= make_timestamp($1::BIGINT) AND hour < make_timestamp($2::BIGINT)
  AND NOT (list_contains($3::VARCHAR[], app) OR ($4 AND loopback))
GROUP BY app, hour)`
	best = time.Hour
	var n int
	for range 3 {
		st := time.Now()
		if err := db.QueryRow(read, last.Add(-23*time.Hour).Truncate(time.Hour).UnixMicro(), last.Add(time.Hour).Truncate(time.Hour).UnixMicro(),
			[]string{"natslens", "natslens-events"}, true).Scan(&n); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(st))
	}
	t.Logf("24 hours of 500 applications from 14 days of client_app_hours: %d points, best of 3 %s", n, best.Round(time.Millisecond))
}
