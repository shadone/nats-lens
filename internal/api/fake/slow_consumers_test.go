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
