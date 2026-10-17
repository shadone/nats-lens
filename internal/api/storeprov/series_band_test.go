package storeprov_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
	"github.com/shadone/nats-lens/internal/store"
)

// rows writes table rows into an empty store, column by column.
type rows struct {
	t  *testing.T
	st *store.Store
}

func (r rows) add(table string, cols map[string]any) {
	r.t.Helper()
	names := make([]string, 0, len(cols))
	for k := range cols {
		names = append(names, k)
	}
	slices.Sort(names)
	args := make([]any, len(names))
	for i, n := range names {
		args[i] = cols[n]
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(names, ", "),
		strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", "))
	if _, err := r.st.DB().Exec(q, args...); err != nil {
		r.t.Fatalf("%s: %v", table, err)
	}
}

// epoch writes epoch n at time at with a JSZ and ACCSTATZ reply from each
// server ID.
func (r rows) epoch(n int64, at time.Time, ids ...string) {
	r.t.Helper()
	if _, err := r.st.DB().Exec(`INSERT INTO epochs VALUES (?, ?, ?, 0, ?, ?, [], [], [], 0, false, [], NULL)`,
		n, at, at, len(ids), len(ids)); err != nil {
		r.t.Fatal(err)
	}
	for _, id := range ids {
		r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "JSZ", "state": "ok"})
		r.add("epoch_endpoints", map[string]any{"epoch": n, "server_id": id, "endpoint": "ACCSTATZ", "state": "ok"})
	}
}

func (r rows) server(id, name string) {
	r.t.Helper()
	r.add("servers", map[string]any{"server_id": id, "server_name": name, "first_epoch": 1, "last_epoch": 1,
		"first_seen_at": t0, "last_seen_at": t0})
}

// serveSeries serves a provider over st whose clock stands at clock.
func serveSeries(t *testing.T, st *store.Store, clock time.Time) natslensv1connect.LensServiceClient {
	t.Helper()
	_, c := apitest.Serve(t, &storeprov.Provider{St: st, Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return clock }})
	return c
}

func seriesOf(t *testing.T, c natslensv1connect.LensServiceClient, metric string, e *v1.EntityRef, r *v1.TimeRange) *v1.Series {
	t.Helper()
	return must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: metric, Entity: e, Range: r, MaxPoints: 2000}))).GetSeries()
}

func values(s *v1.Series) []float64 {
	var out []float64
	for _, p := range s.GetPoints() {
		out = append(out, p.GetValue())
	}
	return out
}

func serverRef(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: name}
}

// Three servers at 2, 5 and 40 percent give the band 2 / 5 / 40, and the
// 40 percent server is the busiest. An epoch where no server reported CPU
// has no point.
func TestCPUBandAndBusiestServer(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	ids := []string{"S0", "S1", "S2"}
	for i, id := range ids {
		r.server(id, fmt.Sprintf("nats-%d", i))
	}
	cpu := []float64{2, 5, 40}
	for e := int64(1); e <= 4; e++ {
		r.epoch(e, t0.Add(time.Duration(e)*30*time.Second), ids...)
		for i, id := range ids {
			row := map[string]any{"epoch": e, "server_id": id, "mem_bytes": int64(100*(i+1)) << 20}
			if e != 3 {
				row["cpu_percent"] = cpu[i]
			}
			r.add("server_stats", row)
		}
	}
	c := serveSeries(t, st, t0.Add(150*time.Second))
	rng := &v1.TimeRange{Start: timestamppb.New(t0)}

	s := seriesOf(t, c, "cluster.cpu_percent", nil, rng)
	if len(s.GetPoints()) != 3 {
		t.Fatalf("points = %v, want 3 (epoch 3 reported no CPU)", s.GetPoints())
	}
	for _, p := range s.GetPoints() {
		if p.GetValue() != 5 || p.Low == nil || p.GetLow() != 2 || p.High == nil || p.GetHigh() != 40 {
			t.Errorf("band point = %v, want 5 within 2..40", p)
		}
	}
	if s.GetTop().GetKey() != "nats-2" || s.GetTop().GetType() != v1.EntityType_ENTITY_TYPE_SERVER {
		t.Errorf("top = %v, want nats-2", s.GetTop())
	}
	if got := values(&v1.Series{Points: s.GetTopPoints()}); !slices.Equal(got, []float64{40, 40, 40}) {
		t.Errorf("top points = %v", got)
	}
	if s.GetMin() != 5 || s.GetMax() != 5 || s.GetUnit() != v1.Unit_UNIT_PERCENT {
		t.Errorf("min/max/unit = %v/%v/%v", s.GetMin(), s.GetMax(), s.GetUnit())
	}

	m := seriesOf(t, c, "cluster.mem_bytes", nil, rng)
	if len(m.GetPoints()) != 4 || m.GetPoints()[0].GetValue() != 200<<20 || m.GetPoints()[0].GetHigh() != 300<<20 || m.GetTop().GetKey() != "nats-2" {
		t.Errorf("memory band = %v", m)
	}

	// A plain metric carries no band.
	for _, p := range seriesOf(t, c, "server.cpu_percent", serverRef("nats-0"), rng).GetPoints() {
		if p.Low != nil || p.High != nil {
			t.Errorf("server.cpu_percent point has a band: %v", p)
		}
	}
}

func TestBandOnAnEmptyStore(t *testing.T) {
	c := serveSeries(t, emptyStore(t), now)
	s := seriesOf(t, c, "cluster.cpu_percent", nil, hour())
	if len(s.GetPoints()) != 0 || s.GetTop() != nil || len(s.GetTopPoints()) != 0 {
		t.Fatalf("empty store band = %v", s)
	}
}

func TestClusterMetricWithAnEntityIsInvalid(t *testing.T) {
	c := serveSeries(t, emptyStore(t), now)
	for _, metric := range []string{"cluster.cpu_percent", "cluster.connections"} {
		_, err := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: metric, Entity: serverRef("nats-0")}))
		if codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s with a server: %v", metric, err)
		}
	}
}

// Streams, KV buckets and object stores are told apart by name; replicas
// 0 on a consumer inherits its stream's count.
func TestAssetCounts(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	ids := []string{"S0", "S1", "S2"}
	for i, id := range ids {
		r.server(id, fmt.Sprintf("nats-%d", i))
	}
	streams := map[string]int{"order-jobs": 3, "audit-log": 1, "KV_customer-config": 3, "OBJ_product-images": 1, "KVX_foo": 1}
	type cons struct {
		stream   string
		replicas int
		durable  string
	}
	consumers := map[string]cons{
		"resize-worker": {"order-jobs", 0, "resize-worker"}, // inherits R3
		"index-worker":  {"order-jobs", 1, "index-worker"},  // R1
		"audit-reader":  {"audit-log", 0, "audit-reader"},   // inherits R1
		"oc_watch":      {"KV_customer-config", 1, ""},      // ephemeral: not counted
	}
	for e := int64(1); e <= 2; e++ {
		r.epoch(e, t0.Add(time.Duration(e)*30*time.Second), ids...)
	}
	for name, rep := range streams {
		for _, id := range ids[:rep] {
			r.add("stream_config", map[string]any{"server_id": id, "account": "APP", "stream": name, "first_epoch": 1, "last_epoch": 2,
				"config_hash": "h", "replicas": rep})
			for e := int64(1); e <= 2; e++ {
				r.add("stream_stats", map[string]any{"epoch": e, "server_id": id, "account": "APP", "stream": name, "is_leader": id == "S0"})
			}
		}
	}
	for name, cn := range consumers {
		r.add("consumer_config", map[string]any{"server_id": "S0", "account": "APP", "stream": cn.stream, "consumer": name,
			"first_epoch": 1, "last_epoch": 2, "config_hash": "h", "replicas": cn.replicas, "durable_name": cn.durable})
		for e := int64(1); e <= 2; e++ {
			r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": cn.stream, "consumer": name, "is_leader": true})
		}
	}
	c := serveSeries(t, st, t0.Add(90*time.Second))
	rng := &v1.TimeRange{Start: timestamppb.New(t0)}
	for metric, want := range map[string]float64{
		"cluster.streams":       3, // order-jobs, audit-log, KVX_foo
		"cluster.kv_buckets":    1,
		"cluster.object_stores": 1,
		// order-jobs, KV_customer-config, resize-worker.
		"cluster.assets_replicated": 3,
		// audit-log, OBJ_product-images, KVX_foo, index-worker, audit-reader.
		"cluster.assets_single": 5,
	} {
		if got := values(seriesOf(t, c, metric, nil, rng)); !slices.Equal(got, []float64{want, want}) {
			t.Errorf("%s = %v, want %v twice", metric, got, want)
		}
	}
}

// An epoch whose JSZ replies all failed did not observe the streams, so it
// has no point rather than a 0.
func TestCountsNeedAnAnsweredJSZ(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.epoch(1, t0.Add(30*time.Second), "S0")
	if _, err := st.DB().Exec(`INSERT INTO epochs VALUES (2, ?, ?, 0, 1, 1, [], [], [], 1, true, [], NULL)`, t0.Add(60*time.Second), t0.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}
	r.add("epoch_endpoints", map[string]any{"epoch": 2, "server_id": "S0", "endpoint": "JSZ", "state": "missing"})
	c := serveSeries(t, st, t0.Add(90*time.Second))
	got := values(seriesOf(t, c, "cluster.streams", nil, &v1.TimeRange{Start: timestamppb.New(t0)}))
	if !slices.Equal(got, []float64{0}) {
		t.Fatalf("streams = %v, want one observed 0", got)
	}
}

// An account counts while it has a connection or a stream; the system
// account does not count.
func TestActiveAccounts(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	r.epoch(1, t0.Add(30*time.Second), "S0", "S1")
	for acc, sys := range map[string]bool{"APP": false, "ORDERS": false, "IDLE": false, "SYS": true} {
		r.add("accounts", map[string]any{"account": acc, "is_system": sys, "first_epoch": 1, "last_epoch": 1})
	}
	for _, x := range []struct {
		id, acc        string
		conns, streams int
	}{{"S0", "APP", 2, 0}, {"S1", "APP", 1, 0}, {"S0", "ORDERS", 0, 1}, {"S0", "IDLE", 0, 0}, {"S0", "SYS", 5, 0}} {
		r.add("account_stats", map[string]any{"epoch": 1, "server_id": x.id, "account": x.acc, "conns": x.conns, "js_streams": x.streams})
	}
	c := serveSeries(t, st, t0.Add(60*time.Second))
	if got := values(seriesOf(t, c, "cluster.accounts", nil, &v1.TimeRange{Start: timestamppb.New(t0)})); !slices.Equal(got, []float64{2}) {
		t.Fatalf("accounts = %v, want 2", got)
	}
}

// The meta leader is the majority's view; an epoch without a leader is
// not a change.
func TestMetaLeaderChanges(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	ids := []string{"S0", "S1", "S2"}
	for i, id := range ids {
		r.server(id, fmt.Sprintf("nats-%d", i))
	}
	leaders := [][]string{
		{"nats-0", "nats-0", "nats-0"},
		{"nats-0", "nats-0", "nats-0"},
		{"nats-1", "nats-1", "nats-0"}, // change
		{"nats-1", "nats-1", "nats-1"},
		{"", "", ""},
		{"nats-1", "nats-1", "nats-1"}, // the same leader after none
		{"", "", ""},
		{"nats-2", "nats-2", "nats-2"}, // change across an epoch without one
	}
	for i, ls := range leaders {
		e := int64(i + 1)
		r.epoch(e, t0.Add(time.Duration(e)*30*time.Second), ids...)
		for j, id := range ids {
			r.add("server_stats", map[string]any{"epoch": e, "server_id": id, "meta_leader": ls[j]})
		}
	}
	c := serveSeries(t, st, t0.Add(9*30*time.Second))
	s := seriesOf(t, c, "cluster.meta_leader_changes", nil, &v1.TimeRange{Start: timestamppb.New(t0)})
	if got := values(s); !slices.Equal(got, []float64{0, 0, 1, 0, 0, 0, 0, 1}) || s.GetUnit() != v1.Unit_UNIT_COUNT {
		t.Fatalf("leader changes = %v, want changes at epochs 3 and 8", got)
	}
}

// A server's slow consumer counter is read per process: a restart starts
// from zero and is not a negative increase.
func TestServerSlowConsumers(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S0b", "nats-0")
	r.server("S1", "nats-1")
	for e, x := range []struct {
		id   string
		slow int64
	}{{"S0", 3}, {"S0", 5}, {"S0b", 1}, {"S0b", 4}} {
		ep := int64(e + 1)
		r.epoch(ep, t0.Add(time.Duration(ep)*30*time.Second), x.id, "S1")
		r.add("server_stats", map[string]any{"epoch": ep, "server_id": x.id, "slow_consumers": x.slow})
		r.add("server_stats", map[string]any{"epoch": ep, "server_id": "S1", "slow_consumers": 100 * ep})
	}
	c := serveSeries(t, st, t0.Add(5*30*time.Second))
	got := values(seriesOf(t, c, "server.slow_consumers", serverRef("nats-0"), &v1.TimeRange{Start: timestamppb.New(t0)}))
	if !slices.Equal(got, []float64{2, 3}) {
		t.Fatalf("nats-0 slow consumers = %v, want [2 3]", got)
	}
}

// The delivered rate comes from the leader's row and has no value across
// a collection gap longer than three intervals.
func TestDeliveredRateSkipsGaps(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	r.add("consumers", map[string]any{"account": "APP", "stream": "order-jobs", "consumer": "resize-worker", "first_epoch": 1, "last_epoch": 4})
	at := []time.Duration{0, 30 * time.Second, 30*time.Second + 5*time.Minute, 60*time.Second + 5*time.Minute}
	seq := []int64{100, 130, 1000, 1060}
	for i := range at {
		e := int64(i + 1)
		r.epoch(e, t0.Add(at[i]), "S0", "S1")
		r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S0", "account": "APP", "stream": "order-jobs",
			"consumer": "resize-worker", "is_leader": true, "delivered_consumer_seq": seq[i]})
		// A follower that lags must not be read.
		r.add("consumer_stats", map[string]any{"epoch": e, "server_id": "S1", "account": "APP", "stream": "order-jobs",
			"consumer": "resize-worker", "is_leader": false, "delivered_consumer_seq": seq[i] + 5000*e})
	}
	c := serveSeries(t, st, t0.Add(at[3]+30*time.Second))
	e := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/order-jobs/resize-worker"}
	s := seriesOf(t, c, "consumer.delivered_rate", e, &v1.TimeRange{Start: timestamppb.New(t0.Add(-time.Minute))})
	if got := values(s); !slices.Equal(got, []float64{1, 2}) || s.GetUnit() != v1.Unit_UNIT_PER_SECOND {
		t.Fatalf("delivered rate = %v, want [1 2] with no value across the gap", got)
	}
}

// Every metric ListMetrics names has a query that runs.
func TestEveryMetricHasAQuery(t *testing.T) {
	c := client(t)
	for _, m := range api.MetricList() {
		var e *v1.EntityRef
		switch m.GetEntityType() {
		case v1.EntityType_ENTITY_TYPE_SERVER:
			e = serverRef("nats-0")
		case v1.EntityType_ENTITY_TYPE_STREAM:
			e = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}
		case v1.EntityType_ENTITY_TYPE_CONSUMER:
			e = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: account + "/order-jobs/index-worker"}
		}
		if _, err := c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: m.GetId(), Entity: e, Range: hour()})); err != nil {
			t.Errorf("%s: %v", m.GetId(), err)
		}
	}
}
