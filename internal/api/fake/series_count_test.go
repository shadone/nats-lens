package fake_test

import (
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// As in the store, a count bucket keeps the lowest and highest sample, so
// the demo's server dip survives buckets of six hours.
func TestCountBucketKeepsItsLowAndHigh(t *testing.T) {
	c := client(t)
	s := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.servers_up", MaxPoints: 4}))).GetSeries()
	dip := false
	for _, p := range s.GetPoints() {
		if p.Low == nil || p.High == nil || p.GetLow() > p.GetValue() || p.GetHigh() < p.GetValue() {
			t.Fatalf("bucket %v has no low and high around its value", p)
		}
		dip = dip || (p.GetLow() == 2 && p.GetValue() == 3)
	}
	if !dip {
		t.Fatalf("no bucket holds the dip to 2: %v", s.GetPoints())
	}
	sum := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.slow_consumers", MaxPoints: 4}))).GetSeries()
	for _, p := range sum.GetPoints() {
		if p.Low != nil || p.High != nil {
			t.Fatalf("summed bucket has low or high: %v", p)
		}
	}
}
