package fake_test

import (
	"math"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/gen/natslens/v1/natslensv1connect"
)

func lastHours(h int) *v1.TimeRange {
	return &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-time.Duration(h) * time.Hour)), End: timestamppb.New(fake.FixedNow)}
}

func demoSeries(t *testing.T, c natslensv1connect.LensServiceClient, sys, metric string, e *v1.EntityRef, r *v1.TimeRange) *v1.Series {
	t.Helper()
	return must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{System: sys, Metric: metric, Entity: e, Range: r}))).GetSeries()
}

func total(s *v1.Series) float64 {
	n := 0.0
	for _, p := range s.GetPoints() {
		n += p.GetValue()
	}
	return n
}

func lastValue(s *v1.Series) float64 {
	ps := s.GetPoints()
	if len(ps) == 0 {
		return math.NaN()
	}
	return ps[len(ps)-1].GetValue()
}

func server(name string) *v1.EntityRef {
	return &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SERVER, Key: name}
}

// example-global's busiest server is nats-ap-2 at about 70% CPU; the band
// holds it and the median stays low.
func TestGlobalCPUBand(t *testing.T) {
	c := demo(t)
	s := demoSeries(t, c, fake.GlobalSystem, "cluster.cpu_percent", nil, lastHours(6))
	if len(s.GetPoints()) == 0 || s.GetTop().GetKey() != "nats-ap-2" || len(s.GetTopPoints()) == 0 {
		t.Fatalf("band = %d points, top %v with %d points", len(s.GetPoints()), s.GetTop(), len(s.GetTopPoints()))
	}
	for _, p := range s.GetPoints() {
		if p.Low == nil || p.High == nil || p.GetLow() > p.GetValue() || p.GetValue() > p.GetHigh() || p.GetValue() > 15 || p.GetHigh() < 55 {
			t.Fatalf("band point %v: want low <= median (under 15) <= high (about 70)", p)
		}
	}
	if top := lastValue(&v1.Series{Points: s.GetTopPoints()}); top < 55 || top > 85 {
		t.Errorf("busiest now = %v, want about 70", top)
	}
	m := demoSeries(t, c, fake.GlobalSystem, "cluster.mem_bytes", nil, lastHours(1))
	if m.GetTop() == nil || m.GetPoints()[0].High == nil {
		t.Errorf("memory band = %v", m)
	}
	// A plain metric has no band.
	if p := demoSeries(t, c, fake.GlobalSystem, "server.cpu_percent", server("nats-eu-0"), lastHours(1)).GetPoints()[0]; p.Low != nil || p.High != nil {
		t.Errorf("server point with a band: %v", p)
	}
}

func TestProdBand(t *testing.T) {
	s := demoSeries(t, demo(t), fake.Cluster, "cluster.cpu_percent", nil, lastHours(1))
	if len(s.GetPoints()) == 0 || s.GetTop() == nil {
		t.Fatalf("prod band = %v", s)
	}
	for _, p := range s.GetPoints() {
		if p.GetLow() > p.GetValue() || p.GetValue() > p.GetHigh() || p.GetValue() < 4 || p.GetValue() > 11 {
			t.Fatalf("prod band point %v, want about 7.5", p)
		}
	}
}

// Counts follow the scenarios' streams and consumers.
func TestDemoAssetCounts(t *testing.T) {
	c := demo(t)
	for _, x := range []struct {
		sys, metric string
		want        float64
	}{
		{fake.GlobalSystem, "cluster.streams", 11},
		{fake.GlobalSystem, "cluster.kv_buckets", 2},
		{fake.GlobalSystem, "cluster.object_stores", 1},
		{fake.GlobalSystem, "cluster.accounts", 1},
		// 14 streams and 19 durable consumers, all on three replicas.
		{fake.GlobalSystem, "cluster.assets_replicated", 33},
		{fake.GlobalSystem, "cluster.assets_single", 0},
		{fake.Cluster, "cluster.streams", 8},
		{fake.Cluster, "cluster.kv_buckets", 1},
		{fake.Cluster, "cluster.object_stores", 0},
		// 9 streams and 13 durable consumers.
		{fake.Cluster, "cluster.assets_replicated", 22},
	} {
		if got := lastValue(demoSeries(t, c, x.sys, x.metric, nil, lastHours(1))); got != x.want {
			t.Errorf("%s %s = %v, want %v", x.sys, x.metric, got, x.want)
		}
	}
	// analytics-aggregator was deleted at 09:31: one asset fewer since.
	r := &v1.TimeRange{Start: timestamppb.New(at(9, 0, 0)), End: timestamppb.New(at(10, 0, 0))}
	s := demoSeries(t, c, fake.Cluster, "cluster.assets_replicated", nil, r)
	if s.GetMax() != 23 || s.GetMin() != 22 {
		t.Errorf("prod assets 09:00-10:00 = %v..%v, want 22..23", s.GetMin(), s.GetMax())
	}
}

// The upgrade moved example-prod's meta leader twice.
func TestDemoMetaLeaderChanges(t *testing.T) {
	c := demo(t)
	if n := total(demoSeries(t, c, fake.Cluster, "cluster.meta_leader_changes", nil, lastHours(24))); n != 2 {
		t.Errorf("prod leader changes = %v, want 2", n)
	}
	if n := total(demoSeries(t, c, fake.GlobalSystem, "cluster.meta_leader_changes", nil, lastHours(24))); n != 0 {
		t.Errorf("global leader changes = %v, want 0", n)
	}
}

// Every slow consumer disconnect of example-global was on nats-us-3, so
// its servers add up to the cluster's count.
func TestDemoServerSlowConsumers(t *testing.T) {
	c := demo(t)
	r := lastHours(1)
	if n := total(demoSeries(t, c, fake.GlobalSystem, "server.slow_consumers", server("nats-us-3"), r)); n != 11 {
		t.Errorf("nats-us-3 slow consumers in the last hour = %v, want 11", n)
	}
	if n := total(demoSeries(t, c, fake.GlobalSystem, "server.slow_consumers", server("nats-eu-0"), r)); n != 0 {
		t.Errorf("nats-eu-0 slow consumers = %v", n)
	}
	day := lastHours(24)
	sum := 0.0
	for _, name := range []string{"nats-0", "nats-1", "nats-2"} {
		sum += total(demoSeries(t, c, fake.Cluster, "server.slow_consumers", server(name), day))
	}
	if want := total(demoSeries(t, c, fake.Cluster, "cluster.slow_consumers", nil, day)); sum != want || sum == 0 {
		t.Errorf("prod servers' slow consumers add to %v, cluster %v", sum, want)
	}
}

func TestDemoDeliveredRate(t *testing.T) {
	c := demo(t)
	lp := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: fake.Account + "/shipping-updates/label-printer"}
	if v := lastValue(demoSeries(t, c, fake.GlobalSystem, "consumer.delivered_rate", lp, lastHours(1))); math.Abs(v-21) > 3 {
		t.Errorf("label-printer drains %v/s, want about 21", v)
	}
	gone := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: fake.Account + "/order-archive/analytics-aggregator"}
	if s := demoSeries(t, c, fake.Cluster, "consumer.delivered_rate", gone, lastHours(1)); len(s.GetPoints()) != 0 {
		t.Errorf("a deleted consumer has a delivered rate: %v", s.GetPoints())
	}
	ri := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: fake.Account + "/order-jobs/resize-worker"}
	if v := lastValue(demoSeries(t, c, fake.Cluster, "consumer.delivered_rate", ri, lastHours(1))); !(v > 0) {
		t.Errorf("resize-worker delivered rate = %v", v)
	}
}
