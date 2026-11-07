package storeprov_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// A count bucket shows its last value and keeps the lowest and highest of
// its epochs, so a server that drops out and returns inside one bucket is
// not lost.
func TestCountBucketKeepsItsLowAndHigh(t *testing.T) {
	st := emptyStore(t)
	r := rows{t, st}
	r.server("S0", "nats-0")
	r.server("S1", "nats-1")
	r.server("S2", "nats-2")
	start := t0.Truncate(time.Hour)
	r.epoch(1, start.Add(time.Minute), "S0", "S1", "S2")
	r.epoch(2, start.Add(2*time.Minute), "S0", "S1")
	r.epoch(3, start.Add(3*time.Minute), "S0", "S1", "S2")
	c := serveSeries(t, st, start.Add(2*time.Hour))
	rng := &v1.TimeRange{Start: timestamppb.New(start), End: timestamppb.New(start.Add(time.Hour))}
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.servers_up", Range: rng, MaxPoints: 1}))).GetSeries()
	if len(s.GetPoints()) != 1 {
		t.Fatalf("points = %v, want one bucket", s.GetPoints())
	}
	p := s.GetPoints()[0]
	if p.GetValue() != 3 || p.Low == nil || p.GetLow() != 2 || p.High == nil || p.GetHigh() != 3 {
		t.Fatalf("bucket = %v, want value 3, low 2, high 3", p)
	}
	// A summed count carries no low and high.
	sum := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.meta_leader_changes", Range: rng, MaxPoints: 1}))).GetSeries()
	for _, p := range sum.GetPoints() {
		if p.Low != nil || p.High != nil {
			t.Fatalf("summed bucket has low or high: %v", p)
		}
	}
}
