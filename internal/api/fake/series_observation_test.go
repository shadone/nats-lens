package fake

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
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

func TestDemoObservationMissingCaptureIsNotZero(t *testing.T) {
	sc := prodScenario()
	sc.noEpochs = []hole{{from: FixedNow.Add(-3 * time.Minute), to: FixedNow}}
	p := newProvider(sc, false)
	rng := &v1.TimeRange{Start: timestamppb.New(FixedNow.Add(-2 * time.Minute)), End: timestamppb.New(FixedNow.Add(-time.Minute))}
	for _, metric := range []string{"server.cpu_percent", "server.slow_consumers", "cluster.slow_consumers"} {
		var e *v1.EntityRef
		e = &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
		if metric[:7] == "server." {
			e = serverRef("nats-0")
		}
		out, err := p.GetTimeSeries(context.Background(), &v1.GetTimeSeriesRequest{Metric: metric, Entity: e, Range: rng, MaxPoints: 1, IncludeObservations: true})
		if err != nil {
			t.Fatal(err)
		}
		s := out.Series.Observations
		if len(out.Series.Points) != 0 {
			t.Errorf("%s curve invented zero during absent capture: %v", metric, out.Series.Points)
		}
		if s == nil || s.KnownCount != 0 || s.Latest != nil || s.ObservedTotal != nil {
			t.Errorf("%s invented samples during absent capture: %v", metric, s)
		}
	}
}

func TestDemoObservationOptionValidationAndLegacy(t *testing.T) {
	p := New(false)
	req := &v1.GetTimeSeriesRequest{Metric: "server.cpu_percent", Entity: serverRef("nats-0"), Range: &v1.TimeRange{Start: timestamppb.New(FixedNow.Add(-time.Hour)), End: timestamppb.New(FixedNow)}, MaxPoints: 1}
	out, err := p.GetTimeSeries(context.Background(), req)
	if err != nil || out.Series.Observations != nil {
		t.Fatalf("legacy response changed: %v %v", out, err)
	}
	req.IncludeObservations = true
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = p.GetTimeSeries(canceled, req); err != context.Canceled {
		t.Fatalf("cancelled summary: %v", err)
	}
	req.Range.Start = timestamppb.New(FixedNow.Add(-31 * 24 * time.Hour))
	if _, err = p.GetTimeSeries(context.Background(), req); err == nil {
		t.Fatal("wide summary accepted")
	}
	req.Range.Start = timestamppb.New(FixedNow.Add(-time.Hour))
	req.Entity = serverRef("absent-server")
	if _, err = p.GetTimeSeries(context.Background(), req); err == nil {
		t.Fatal("unknown entity accepted")
	}
}

func TestDemoObservationCurveUsesSameNativeGrid(t *testing.T) {
	p := New(false)
	rng := &v1.TimeRange{Start: timestamppb.New(FixedNow.Add(-45 * time.Second)), End: timestamppb.New(FixedNow.Add(-35 * time.Second))}
	for _, metric := range []string{"server.cpu_percent", "server.mem_bytes", "server.connections", "server.in_msgs_rate", "cluster.cpu_percent", "server.slow_consumers"} {
		e := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
		if metric[:7] == "server." {
			e = serverRef("nats-0")
		}
		out, err := p.GetTimeSeries(context.Background(), &v1.GetTimeSeriesRequest{Metric: metric, Entity: e, Range: rng, MaxPoints: 1, IncludeObservations: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Series.Points) != 0 || out.Series.GetObservations().KnownCount != 0 {
			t.Errorf("%s created a point between native sample times: %v", metric, out.Series)
		}
	}
}
