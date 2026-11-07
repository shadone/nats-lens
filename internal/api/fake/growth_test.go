package fake_test

import (
	"math"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Growth is the Theil-Sen slope of the stream's own bytes: over a day the
// steady growth of the scenario, negative for the shrinking session
// bucket, and unset over an hour or for a system with less than six hours
// of data in the range.
func TestFakeGrowthFromTheSeries(t *testing.T) {
	c := demo(t)
	growth := func(sys string, d time.Duration) map[string]*float64 {
		t.Helper()
		r := &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-d)), End: timestamppb.New(fake.FixedNow)}
		out := map[string]*float64{}
		for _, s := range must(c.ListStreams(ctx, connect.NewRequest(&v1.ListStreamsRequest{System: sys, Range: r, IncludeKv: true}))).GetStreams() {
			out[s.GetEntity().GetStream()] = s.BytesPerDay
		}
		return out
	}
	near := func(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) <= math.Abs(want)*0.01 }
	if g := growth(fake.Cluster, 6*time.Hour)["order-archive"]; !near(g, 96*(1<<20)) {
		t.Errorf("six-hour order-archive growth = %v, want 96 MiB/day", g)
	}
	if g := growth(fake.Cluster, 24*time.Hour)["order-archive"]; !near(g, 96*(1<<20)) {
		t.Errorf("order-archive grows %v a day, want 96 MiB", g)
	}
	if g := growth(fake.GlobalSystem, 24*time.Hour)["KV_session-cache"]; !near(g, -(1 << 20)) {
		t.Errorf("KV_session-cache grows %v a day, want -1 MiB", g)
	}
	for name, g := range growth(fake.Cluster, time.Hour) {
		if g != nil {
			t.Errorf("%s grows %v over an hour, want unset", name, *g)
		}
	}
	// example-staging's six hours of data end three hours ago: the epochs
	// in the default range span just under six hours.
	for name, g := range growth(fake.StagingSystem, 24*time.Hour) {
		if g != nil {
			t.Errorf("staging %s grows %v, want unset", name, *g)
		}
	}
}
