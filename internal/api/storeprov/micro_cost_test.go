package storeprov_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// 100 instances x 20 endpoints of one service, a report every 30 s for
// MICRO_COST_DAYS days (MICRO_COST_SLOW makes every endpoint slow in the
// last hour; MICRO_COST_IDLE makes them active for the first 30 hours,
// idle until the last hour and slow in it); times GetMicroServices. Run with TZ=UTC: the setup casts
// times to TIMESTAMP in the session's zone.
func TestMicroServicesCost(t *testing.T) {
	days, _ := strconv.Atoi(os.Getenv("MICRO_COST_DAYS"))
	if days == 0 {
		t.Skip("MICRO_COST_DAYS not set")
	}
	st := emptyStore(t)
	db := st.DB()
	last := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	n := days * 24 * 120
	first := last.Add(-time.Duration(n-1) * 30 * time.Second)
	exec := func(q string, args ...any) {
		t.Helper()
		t0 := time.Now()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
		t.Logf("%.1fs %.60s", time.Since(t0).Seconds(), q)
	}
	exec(`INSERT INTO epochs SELECT e, ?::TIMESTAMP + INTERVAL 30 SECOND * (e - 1), ?::TIMESTAMP + INTERVAL 30 SECOND * (e - 1) + INTERVAL 1 SECOND,
		0, 2, 2, [], [], [], 0, false, [], NULL FROM range(1, ?) r(e)`, first, first, n+1)
	exec(`INSERT INTO micro_requests (epoch, label, account, request, state, replies) SELECT e, 'app', 'APP', 'STATS', 'ok', 100 FROM range(1, ?) r(e)`, n+1)
	exec(`INSERT INTO micro_stats (epoch, account, service, instance_id, version, started)
		SELECT e, 'APP', 'orders', 'I' || i, '1.0.0', ?::TIMESTAMP FROM range(1, ?) r(e), range(0, 100) s(i)`, first.Add(-time.Hour), n+1)
	exec(`INSERT INTO micro_endpoints (epoch, account, service, instance_id, endpoint, subject, queue_group, num_requests, num_errors,
		processing_time_ns, average_processing_time_ns)
		SELECT e, 'APP', 'orders', 'I' || i, 'ep' || k, 'orders.ep' || k, 'q', e * 30, e, e * 30 * 4000000, 4000000
		FROM range(1, ?) r(e), range(0, 100) s(i), range(0, 20) x(k)`, n+1)
	if os.Getenv("MICRO_COST_IDLE") != "" {
		// Active for the first 30 hours, idle (counters unchanged) until the
		// last hour, then 10 times slower: the slow run's span crosses
		// days of idle reports.
		active, resume := int64(30*120), int64(n-120)
		exec(`UPDATE micro_endpoints SET num_requests = (least(epoch, $1) + greatest(epoch - $2, 0)) * 30,
			num_errors = least(epoch, $1) + greatest(epoch - $2, 0),
			processing_time_ns = least(epoch, $1) * 30 * 4000000 + greatest(epoch - $2, 0) * 30 * 40000000`, active, resume)
	} else if os.Getenv("MICRO_COST_SLOW") != "" {
		r, err := db.Exec(`UPDATE micro_endpoints SET processing_time_ns = processing_time_ns + (epoch - $1) * 30 * 36000000 WHERE epoch > $1`, int64(n-120))
		if err != nil {
			t.Fatal(err)
		}
		k, _ := r.RowsAffected()
		t.Logf("updated %d", k)
		var a, b int64
		_ = db.QueryRow(`SELECT min(processing_time_ns), max(processing_time_ns) FROM micro_endpoints WHERE epoch IN ($1, $2) AND instance_id = 'I0' AND endpoint = 'ep0'`, int64(n-29), int64(n)).Scan(&a, &b)
		var dmin, dmax float64
		if err := db.QueryRow(`SELECT min(dn/dr), max(dn/dr) FROM (SELECT instance_id, endpoint, arg_max(processing_time_ns, ep.started_at) - arg_min(processing_time_ns, ep.started_at) AS dn, arg_max(num_requests, ep.started_at) - arg_min(num_requests, ep.started_at) AS dr FROM micro_endpoints x JOIN epochs ep USING (epoch) WHERE ep.started_at >= $1 GROUP BY ALL)`, last.Add(-14*time.Minute-30*time.Second)).Scan(&dmin, &dmax); err != nil {
			t.Fatal(err)
		}
		t.Logf("window per instance %.0f %.0f", dmin, dmax)
		t.Logf("ns %d %d avg %.2f", a, b, float64(b-a)/(29*30)/1e6)
	}
	// The hourly rollup, as the writer fills it epoch by epoch.
	exec(`INSERT INTO micro_endpoint_hours
		SELECT date_trunc('hour', t), account, service, endpoint, sum(dr), sum(de), sum(dn), count(DISTINCT t) FROM (
		  SELECT ep.started_at AS t, x.account, x.service, x.endpoint,
		    x.num_requests - lag(x.num_requests, 1, x.num_requests) OVER w AS dr, x.num_errors - lag(x.num_errors, 1, x.num_errors) OVER w AS de,
		    x.processing_time_ns - lag(x.processing_time_ns, 1, x.processing_time_ns) OVER w AS dn
		  FROM micro_endpoints x JOIN epochs ep USING (epoch)
		  WINDOW w AS (PARTITION BY x.account, x.service, x.endpoint, x.instance_id ORDER BY x.epoch))
		GROUP BY ALL`)
	p := &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return last.Add(30 * time.Second) }, Retention: 14 * day}
	_, c := apitest.Serve(t, p)
	for i := 0; i < 3; i++ {
		t0 := time.Now()
		res, err := c.GetMicroServices(ctx, connect.NewRequest(&v1.GetMicroServicesRequest{
			Range: &v1.TimeRange{Start: timestamppb.New(last.Add(-time.Hour)), End: timestamppb.New(last.Add(30 * time.Second))}}))
		if err != nil {
			t.Fatal(err)
		}
		e := res.Msg.GetServices()[0].GetEndpoints()[0]
		t.Logf("GetMicroServices %v: %d endpoints, rate %.1f avg %.2f level %.2f slow %v since %v", time.Since(t0),
			len(res.Msg.GetServices()[0].GetEndpoints()), e.GetRequestsPerSecond(), e.GetAverageMs(), e.GetLevelMs(), e.GetSlow(),
			e.GetSlowSince().AsTime())
	}
}
