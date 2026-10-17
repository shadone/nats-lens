package storeprov_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/shadone/nats-lens/internal/api/apitest"
	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A minute of observations gives no growth per day: one write would
// otherwise be extrapolated to a day.
func TestStreamGrowthNeedsHoursOfData(t *testing.T) {
	c := client(t)
	for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour(), IncludeKv: true}))).GetStreams() {
		if s.BytesPerDay != nil {
			t.Errorf("%s: bytes per day %v from a minute of data", s.GetEntity().GetKey(), s.GetBytesPerDay())
		}
	}
}

// A consumer whose configuration says 0 replicas has its stream's.
func TestConsumerEffectiveReplicas(t *testing.T) {
	c := client(t)
	for _, x := range must(c.ListConsumers(ctx, connect.NewRequest(&v1.ListConsumersRequest{Range: hour(), Stream: "order-jobs"}))).GetConsumers() {
		if x.GetReplicas() != 3 {
			t.Errorf("%s: replicas %d, want the stream's 3", x.GetEntity().GetKey(), x.GetReplicas())
		}
	}
}

// The last delivery is the leader's; a follower may report a later time.
func TestConsumerLastDeliveredFromLeader(t *testing.T) {
	c := client(t)
	gc := must(c.GetConsumer(ctx, connect.NewRequest(&v1.GetConsumerRequest{Account: account, Stream: "order-archive",
		Consumer: "analytics-aggregator", Range: hour()}))).GetConsumer()
	want := time.Date(2026, 1, 16, 22, 28, 59, 336005000, time.UTC)
	if got := gc.GetLastDelivered().AsTime(); !got.Equal(want) {
		t.Errorf("last delivered %v, want the leader nats-0's %v", got, want)
	}
}

// A present stream always has a consumer count, 0 included, so that the
// JSON form carries it.
func TestStreamConsumerCountSet(t *testing.T) {
	c := client(t)
	for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour(), IncludeKv: true}))).GetStreams() {
		if s.Consumers == nil {
			t.Errorf("%s: consumers unset", s.GetEntity().GetKey())
		}
	}
}

// Byte rates come from the servers' byte counters; a stream's message rate
// from its last sequence.
func TestByteAndStreamRates(t *testing.T) {
	c := client(t)
	nats0 := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: "nats-0"}
	for metric, want := range map[string]float64{"server.in_bytes_rate": 10000, "server.out_bytes_rate": 20000} {
		s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: metric, Range: hour(), MaxPoints: 120,
			Entity: nats0}))).GetSeries()
		if len(s.GetPoints()) != 2 || s.GetPoints()[0].GetValue() != want || s.GetUnit() != v1.Unit_UNIT_BYTES_PER_SECOND {
			t.Errorf("%s = %v, want %v B/s", metric, s, want)
		}
	}
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	for _, metric := range []string{"cluster.in_bytes_rate", "cluster.out_bytes_rate"} {
		s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: metric, Range: hour(), Entity: sys}))).GetSeries()
		if len(s.GetPoints()) == 0 {
			t.Errorf("%s has no points", metric)
		}
	}
	jobs := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_STREAM, Key: account + "/order-jobs"}
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "stream.msgs_rate", Range: hour(), MaxPoints: 120,
		Entity: jobs}))).GetSeries()
	if len(s.GetPoints()) != 2 || s.GetPoints()[0].GetValue() != 0 || s.GetUnit() != v1.Unit_UNIT_PER_SECOND {
		t.Errorf("stream.msgs_rate = %v", s)
	}
}

// Every entity, finding, event and connection that names an account by
// its ID carries the readable name as well.
func TestAccountNameExposed(t *testing.T) {
	c := client(t)
	for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{Range: hour()}))).GetStreams() {
		if s.GetEntity().GetAccountName() != "APP" || s.GetEntity().GetAccount() != account {
			t.Errorf("stream entity = %v", s.GetEntity())
		}
	}
	gs := must(c.GetStream(ctx, connect.NewRequest(&v1.GetStreamRequest{Account: account, Stream: "order-jobs", Range: hour()})))
	for _, x := range gs.GetConsumers() {
		if x.GetEntity().GetAccountName() != "APP" {
			t.Errorf("consumer entity = %v", x.GetEntity())
		}
	}
}

// GetLensInfo says where the stored history starts: the oldest epoch.
func TestLensInfoHistoryStart(t *testing.T) {
	m := must(client(t).GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{})))
	if got := m.GetHistoryStart(); got == nil || !got.AsTime().Equal(t0) {
		t.Errorf("history start = %v, want %v", got, t0)
	}
	if m := must(func() (*connect.Response[v1.GetLensInfoResponse], error) {
		_, c := serve(t, emptyStore(t))
		return c.GetLensInfo(ctx, connect.NewRequest(&v1.GetLensInfoRequest{}))
	}()); m.GetHistoryStart() != nil {
		t.Errorf("empty store history start = %v", m.GetHistoryStart())
	}
}

// nats-lens's own connections and loopback clients are marked as tooling.
func TestConnectionsMarkTooling(t *testing.T) {
	p := &storeprov.Provider{St: fixtureStore(t), Interval: 30 * time.Second, Live: true, Clock: func() time.Time { return now },
		ToolingNames: []string{"ledger-service"}}
	_, c := apitest.Serve(t, p)
	res := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour(), IncludeTooling: true})))
	if len(res.GetConnections()) != 4 || res.GetToolingHidden() != 0 {
		t.Fatalf("with tooling: %d connections, %d hidden", len(res.GetConnections()), res.GetToolingHidden())
	}
	hidden := must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour()})))
	if len(hidden.GetConnections()) != 2 || hidden.GetToolingHidden() != 2 || hidden.GetTotalCount() != 2 {
		t.Fatalf("without tooling: %d connections, %d hidden, total %d", len(hidden.GetConnections()),
			hidden.GetToolingHidden(), hidden.GetTotalCount())
	}
	for _, x := range res.GetConnections() {
		if x.GetTooling() != (x.GetName() == "ledger-service") {
			t.Errorf("%s tooling = %v", x.GetName(), x.GetTooling())
		}
	}
}

// ListConnections judges loopback clients by the epoch's stored guard
// decision; an epoch without one counts as on.
func TestConnectionsUseTheStoredLoopbackDecision(t *testing.T) {
	st := fixtureStore(t)
	if _, err := st.DB().Exec(`INSERT INTO connections (epoch, server_id, cid, name, ip) VALUES (3, 'NFAKESERVER0ID', 99, 'nats-cli', '127.0.0.1')`); err != nil {
		t.Fatal(err)
	}
	_, c := serve(t, st)
	list := func() *v1.ListConnectionsResponse {
		return must(c.ListConnections(ctx, connect.NewRequest(&v1.ListConnectionsRequest{Range: hour()})))
	}
	if r := list(); r.GetToolingHidden() != 1 {
		t.Fatalf("without a stored decision: %d hidden, want the CLI", r.GetToolingHidden())
	}
	if err := st.SetLoopbackTooling(ctx, 3, false); err != nil {
		t.Fatal(err)
	}
	if r := list(); r.GetToolingHidden() != 0 {
		t.Fatalf("with the rule off for the epoch: %d hidden, want 0", r.GetToolingHidden())
	}
}
