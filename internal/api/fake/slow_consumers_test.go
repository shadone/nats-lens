package fake_test

import (
	"connectrpc.com/connect"
	"github.com/shadone/nats-lens/internal/api/fake"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestDemoSlowConsumerInvestigation(t *testing.T) {
	c := demo(t)
	req := &v1.GetSlowConsumerInvestigationRequest{System: fake.GlobalSystem, Range: &v1.TimeRange{Start: timestamppb.New(fake.FixedNow.Add(-time.Hour)), End: timestamppb.New(fake.FixedNow)}, Limit: 1}
	a := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	b := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	if a.Detections != 11 || a.RecordedDisconnects != 11 || !a.EventsTruncated || !a.ObservationsTruncated || len(a.Events) != 1 || len(a.Observations) != 1 || a.IncompleteCollections == 0 {
		t.Fatalf("demo evidence %v", a)
	}
	if a.Events[0].Event.Id != b.Events[0].Event.Id || a.Observations[0].ServerId != b.Observations[0].ServerId || a.Observations[0].Epoch != b.Observations[0].Epoch {
		t.Fatalf("unstable identities %v / %v", a, b)
	}
	if a.Observations[0].IntervalStart == nil || !a.Observations[0].IntervalStart.AsTime().Before(a.Observations[0].IntervalEnd.AsTime()) {
		t.Fatal("no observed interval")
	}
	req.Account = "OTHER"
	a = must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	if a.Detections != 11 || a.RecordedDisconnects != 0 {
		t.Fatalf("account evidence %v", a)
	}
	req.System = fake.StagingSystem
	req.Account = ""
	a = must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(req)))
	if !a.CaptureGaps {
		t.Fatalf("stale history %v", a)
	}
}

func TestDemoSlowConsumerInvestigationRangeAgreement(t *testing.T) {
	c := demo(t)
	for _, bounds := range [][2]time.Time{{at(13, 41, 0), at(13, 41, 1)}, {at(13, 41, 15), at(13, 42, 0)}, {at(13, 40, 0), at(13, 42, 30)}} {
		rng := &v1.TimeRange{Start: timestamppb.New(bounds[0]), End: timestamppb.New(bounds[1])}
		inv := must(c.GetSlowConsumerInvestigation(ctx, connect.NewRequest(&v1.GetSlowConsumerInvestigationRequest{System: fake.GlobalSystem, Range: rng})))
		list := must(c.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: fake.GlobalSystem, Range: rng})))
		var headline int64
		for _, s := range list.Servers {
			headline += s.GetSlowConsumersInRange()
		}
		chart := total(demoSeries(t, c, fake.GlobalSystem, "cluster.slow_consumers", nil, rng))
		detail := total(demoSeries(t, c, fake.GlobalSystem, "server.slow_consumers", server("nats-us-3"), rng))
		if int64(chart) != inv.Detections || headline != inv.Detections || int64(detail) != inv.Detections {
			t.Errorf("%v: chart=%v detail=%v headline=%d investigation=%d", bounds, chart, detail, headline, inv.Detections)
		}
	}
}
