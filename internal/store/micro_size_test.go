package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Sizing, run with MICRO_SIZE_DAYS set: that many days of 30-second epochs
// of 100 instances x 20 endpoints in a file store. It logs bytes per row,
// the reads T2 plans (15-minute rates; the 7-day median of hourly averages
// from the raw rows and from micro_endpoint_hours, also with 10 times the
// services), the write of one epoch's hourly sums with an hour of rows
// behind it, and a day's sweep.
func TestMicroSizeAtScale(t *testing.T) {
	days, _ := strconv.Atoi(os.Getenv("MICRO_SIZE_DAYS"))
	if days == 0 {
		t.Skip("MICRO_SIZE_DAYS not set")
	}
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	path := filepath.Join(dir, FileName)
	size := func() int64 {
		if _, err := db.Exec(`CHECKPOINT`); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return fi.Size()
	}
	before := size()
	epochs := days * 2880
	start := time.Now()
	if _, err := db.Exec(`CREATE TABLE ep AS SELECT range AS epoch,
  TIMESTAMP '2026-10-01 00:00:00' + to_seconds(range * 30) AS started_at FROM range(?)`, epochs); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO micro_endpoints
SELECT e.range, 'ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZ2345', 'svc' || (i.range % 5),
  substr(md5(i.range::VARCHAR), 1, 22), 'ep' || k.range, 'orders.ep' || k.range || '.*', 'q',
  e.range * (10 + k.range), e.range * (k.range % 3), e.range * (10 + k.range) * (1000000 + (hash(e.range, i.range, k.range) % 50000)),
  1000000 + (hash(e.range, i.range, k.range) % 50000),
  CASE WHEN k.range = 0 THEN '500 upstream timeout' END, NULL
FROM range(?) e, range(100) i, range(20) k`, epochs); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO micro_stats SELECT e.range, 'ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZ2345',
  'svc' || (i.range % 5), substr(md5(i.range::VARCHAR), 1, 22), '1.0.0', TIMESTAMP '2026-09-30 00:00:00'
FROM range(?) e, range(100) i`, epochs); err != nil {
		t.Fatal(err)
	}
	rows := int64(epochs) * 2000
	after := size()
	t.Logf("%d epochs, %d endpoint rows, insert %s, file +%d MB, %.1f bytes per endpoint row (stats rows included)",
		epochs, rows, time.Since(start).Round(time.Millisecond), (after-before)>>20, float64(after-before)/float64(rows))

	rates := `WITH w AS (SELECT epoch FROM ep WHERE started_at > (SELECT max(started_at) FROM ep) - INTERVAL 15 MINUTE),
x AS (SELECT service, instance_id, endpoint,
  arg_max(num_requests, epoch) - arg_min(num_requests, epoch) AS dreq,
  arg_max(processing_time_ns, epoch) - arg_min(processing_time_ns, epoch) AS dproc
  FROM micro_endpoints WHERE epoch IN (SELECT epoch FROM w) GROUP BY ALL)
SELECT count(*) FROM (SELECT service, endpoint, sum(dreq) / 900.0, sum(dproc) / nullif(sum(dreq), 0) FROM x GROUP BY ALL)`
	medians := `WITH w AS (SELECT epoch, date_trunc('hour', started_at) AS h FROM ep
  WHERE started_at > (SELECT max(started_at) FROM ep) - INTERVAL 7 DAY - INTERVAL 15 MINUTE
    AND started_at <= (SELECT max(started_at) FROM ep) - INTERVAL 15 MINUTE),
x AS (SELECT m.service, m.instance_id, m.endpoint, w.h,
  max(processing_time_ns) - min(processing_time_ns) AS dproc, max(num_requests) - min(num_requests) AS dreq
  FROM micro_endpoints m JOIN w USING (epoch) GROUP BY ALL),
hourly AS (SELECT service, endpoint, h, sum(dproc) / nullif(sum(dreq), 0) AS avg_ns FROM x GROUP BY ALL)
SELECT count(*) FROM (SELECT service, endpoint, median(avg_ns) FROM hourly GROUP BY ALL)`
	timeQuery := func(name, q string) {
		var n int
		best := time.Hour
		for range 3 {
			st := time.Now()
			if err := db.QueryRow(q).Scan(&n); err != nil {
				t.Fatal(err)
			}
			best = min(best, time.Since(st))
		}
		t.Logf("%s: %d groups, best of 3 %s", name, n, best.Round(time.Millisecond))
	}
	timeQuery("15-minute rates", rates)
	timeQuery("7-day hourly medians from micro_endpoints", medians)
	// micro_endpoint_hours for the same days, with 5 and with 50 services
	// of 20 endpoints, as the writer would leave them.
	hourly := func(services int) {
		if _, err := db.Exec(`DELETE FROM micro_endpoint_hours`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO micro_endpoint_hours
SELECT TIMESTAMP '2026-10-01 00:00:00' + to_hours(h.range), 'ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZ2345',
  'svc' || s.range, 'ep' || k.range, 120 * 20 * (10 + k.range), 120 * (k.range % 3),
  120 * 20 * (10 + k.range) * (1000000 + (hash(h.range, s.range, k.range) % 50000)), 120
FROM range(?) h, range(?) s, range(20) k`, days*24, services); err != nil {
			t.Fatal(err)
		}
		timeQuery(fmt.Sprintf("7-day hourly medians from micro_endpoint_hours, %d services", services),
			`SELECT count(*) FROM (SELECT service, endpoint, median(processing_time_ns / nullif(requests, 0))
FROM micro_endpoint_hours
WHERE hour >= (SELECT max(hour) FROM micro_endpoint_hours) - INTERVAL 7 DAY GROUP BY ALL)`)
	}
	hourly(5)
	hourly(50)
	// One epoch's hourly sums with the last hour of rows behind it.
	if _, err := db.Exec(`INSERT INTO epochs (epoch, started_at, finished_at, duration_ms, expected_servers, observed_servers,
  expected_names, observed_names, missing_names, endpoint_failures, partial, errors)
SELECT epoch, started_at, started_at, 0, 0, 0, [], [], [], 0, false, [] FROM ep
WHERE started_at > (SELECT max(started_at) FROM ep) - INTERVAL 1 HOUR`); err != nil {
		t.Fatal(err)
	}
	var last int64
	var lastAt time.Time
	if err := db.QueryRow(`SELECT max(epoch), max(started_at) FROM ep`).Scan(&last, &lastAt); err != nil {
		t.Fatal(err)
	}
	st := time.Now()
	if _, err := db.Exec(microHoursQuery, last, lastAt.Truncate(time.Hour), last-120, lastAt.Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Logf("hourly sums of one epoch over an hour of rows: %s", time.Since(st).Round(time.Millisecond))
	// Sweep cost: one day's delete as retention does.
	st = time.Now()
	if _, err := db.Exec(`DELETE FROM micro_endpoints WHERE epoch < 2880`); err != nil {
		t.Fatal(err)
	}
	t.Logf("delete one day of endpoint rows: %s", time.Since(st).Round(time.Millisecond))
}
