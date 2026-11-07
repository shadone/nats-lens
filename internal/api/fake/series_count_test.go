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
	// The series' min and max stay those of the bucket values, which the
	// charts draw; a reader that wants the edges reads low and high.
	lo, hi := s.GetPoints()[0].GetValue(), s.GetPoints()[0].GetValue()
	for _, p := range s.GetPoints() {
		lo, hi = min(lo, p.GetValue()), max(hi, p.GetValue())
	}
	if s.GetMin() != lo || s.GetMax() != hi || lo == 2 {
		t.Fatalf("min/max = %v/%v, want the bucket values' %v/%v", s.GetMin(), s.GetMax(), lo, hi)
	}
	sum := must(c.GetTimeSeries(ctx, connect.NewRequest(&v1.GetTimeSeriesRequest{Metric: "cluster.slow_consumers", MaxPoints: 4}))).GetSeries()
	for _, p := range sum.GetPoints() {
		if p.Low != nil || p.High != nil {
			t.Fatalf("summed bucket has low or high: %v", p)
		}
	}
}
