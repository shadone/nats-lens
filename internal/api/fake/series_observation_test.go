package fake

import (
	"context"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestDemoObservationSummariesAreResolutionIndependent(t *testing.T) {
	p := New(false)
	rng := &v1.TimeRange{Start: timestamppb.New(FixedNow.Add(-time.Hour)), End: timestamppb.New(FixedNow)}
	for _, metric := range []string{"server.cpu_percent", "server.mem_bytes", "server.connections", "server.in_msgs_rate", "server.slow_consumers", "cluster.cpu_percent", "cluster.mem_bytes"} {
		t.Run(metric, func(t *testing.T) {
			var previous *v1.ObservationSummary
			for _, points := range []int32{1, 200} {
				entity := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
				if metric[:7] == "server." {
					entity = serverRef("nats-0")
				}
				out, err := p.GetTimeSeries(context.Background(), &v1.GetTimeSeriesRequest{Metric: metric, Entity: entity, Range: rng, MaxPoints: points, IncludeObservations: true})
				if err != nil {
					t.Fatal(err)
				}
				s := out.GetSeries().GetObservations()
				if s == nil || s.GetKnownCount() == 0 {
					t.Fatalf("missing observations for %s", metric)
				}
				if previous != nil && !proto.Equal(s, previous) {
					t.Fatalf("resolution changed summary: %v / %v", s, previous)
				}
				previous = s
			}
		})
	}
}
