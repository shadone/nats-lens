package api

import (
	"fmt"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

func TestFinishOnCall(t *testing.T) {
	now := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	ts := timestamppb.New
	f := &v1.Finding{Id: "consumer-010:APP/orders/worker", CheckCode: "consumer-010", Entity: EntityRefOf(v1.EntityType_ENTITY_TYPE_CONSUMER, "APP/orders/worker"), Since: ts(now.Add(-2 * time.Hour)), ConditionSince: ts(now.Add(-3 * time.Hour))}
	facts := &OnCallFacts{At: now, Finding: f, Now: &v1.OnCallNow{}, Windows: []*v1.TimeWindow{{Start: ts(now.Add(-4 * time.Hour)), End: ts(now.Add(time.Minute)), Label: "Upgrade"}}}
	for i := 0; i < 16; i++ {
		facts.Firing = append(facts.Firing, &v1.Finding{Id: fmt.Sprint(i), CheckCode: f.CheckCode, Entity: EntityRefOf(f.Entity.Type, fmt.Sprintf("APP/orders/%02d", i)), Severity: v1.Severity_SEVERITY_WARNING})
	}
	facts.Firing[0].Mute = &v1.Mute{}
	facts.Firing[1].CheckCode = "other"
	a, b := OnCallSpan(f, now)
	if !a.Equal(now.Add(-3*time.Hour-15*time.Minute)) || !b.Equal(now) {
		t.Fatalf("span %v %v", a, b)
	}
	replay := &v1.GetReplayResponse{}
	for i := 0; i < 20; i++ {
		replay.Steps = append(replay.Steps, &v1.ReplayStep{Id: fmt.Sprint(i), Time: ts(now.Add(time.Duration(i-20) * time.Minute)), Severity: v1.Severity_SEVERITY_INFO})
	}
	replay.Steps[0].Severity = v1.Severity_SEVERITY_CRITICAL
	got := FinishOnCall(facts, replay, "")
	if got.SameCheckFiring != 14 || got.AssetsTotal != 14 || len(got.Assets) != 10 || got.StepsTotal != 20 || len(got.Steps) != 12 || got.ContextWindow == nil || got.Now.OpenWindow == nil {
		t.Fatalf("card: %v", got)
	}
	if got.Steps[0].Id != "0" {
		t.Fatal("worst step lost")
	}
}

func TestOnCallResolvedAndAbsent(t *testing.T) {
	now := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	f := &v1.Finding{Id: "x", Since: timestamppb.New(now.Add(-48 * time.Hour)), ResolvedAt: timestamppb.New(now.Add(-time.Hour)), State: v1.FindingState_FINDING_STATE_RESOLVED, Entity: EntityRefOf(v1.EntityType_ENTITY_TYPE_SYSTEM, "system")}
	a, b := OnCallSpan(f, now)
	if b.Sub(a) != 24*time.Hour || !b.Equal(f.ResolvedAt.AsTime()) {
		t.Fatalf("span %v %v", a, b)
	}
	got := FinishOnCall(&OnCallFacts{At: now, Finding: f}, nil, "history unavailable")
	if got.SameCheckFiring != 0 || len(got.Assets) != 1 || got.Now == nil || got.Now.ServersAnswering != nil || got.Assets[0].Online != nil || got.StepsNote != "history unavailable" {
		t.Fatal(got)
	}
}

func TestOnCallSelectionBoundaries(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	f := &v1.Finding{Id: "x", CheckCode: "consumer-010", Entity: EntityRefOf(v1.EntityType_ENTITY_TYPE_CONSUMER, "APP/orders/worker"), Since: timestamppb.New(at.Add(-time.Hour))}
	facts := &OnCallFacts{At: at, Finding: f, Transitions: []*v1.FindingTransition{
		{Time: timestamppb.New(at.Add(-2 * time.Hour))}, {Time: timestamppb.New(at.Add(-time.Hour))}, {Time: timestamppb.New(at.Add(time.Second))},
	}, Windows: []*v1.TimeWindow{
		{Start: timestamppb.New(at.Add(-2 * time.Hour)), End: timestamppb.New(at.Add(-76 * time.Minute)), Label: "too early"},
		{Start: timestamppb.New(at.Add(-75 * time.Minute)), End: timestamppb.New(at.Add(-16 * time.Minute)), Label: "overlaps"},
		{Start: timestamppb.New(at.Add(time.Minute)), Label: "future"},
	}}
	got := FinishOnCall(facts, nil, "")
	if len(got.Transitions) != 1 || got.ContextWindow.GetLabel() != "overlaps" || got.Now.OpenWindow != nil {
		t.Fatal(got)
	}
	OnCallRates(facts, map[string]float64{"APP/orders/other": 0, "APP/elsewhere/worker": 123}, map[string]float64{"APP/orders": 0})
	if facts.Now.DeliveredPerSecond == nil || facts.Now.StreamMsgsPerSecond == nil || *facts.Now.DeliveredPerSecond != 0 {
		t.Fatal("known zero was lost")
	}
	noRates := &OnCallFacts{Finding: f}
	OnCallRates(noRates, nil, nil)
	if noRates.Now.DeliveredPerSecond != nil || noRates.Now.StreamMsgsPerSecond != nil {
		t.Fatal("unknown rate became zero")
	}
	for _, tc := range []struct {
		ps   []*v1.ReplicaPlacement
		want *int32
	}{
		{nil, nil}, {[]*v1.ReplicaPlacement{{State: v1.PlacementState_PLACEMENT_STATE_MISSING}}, nil},
		{[]*v1.ReplicaPlacement{{Server: "nats-0", State: v1.PlacementState_PLACEMENT_STATE_LEADER}, {Server: "nats-1", State: v1.PlacementState_PLACEMENT_STATE_OFFLINE}}, onCallPtr(int32(1))},
		{[]*v1.ReplicaPlacement{{Server: "nats-0", State: v1.PlacementState_PLACEMENT_STATE_OFFLINE}}, onCallPtr(int32(0))},
	} {
		n := OnCallOnline(tc.ps)
		if (n == nil) != (tc.want == nil) || n != nil && *n != *tc.want {
			t.Fatalf("online %v want %v", n, tc.want)
		}
	}
}

func onCallPtr[T any](v T) *T { return &v }

func TestOnCallNearestStepsAndOpenWindow(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)
	f := &v1.Finding{Since: timestamppb.New(at.Add(-time.Hour)), Entity: EntityRefOf(v1.EntityType_ENTITY_TYPE_SYSTEM, "system")}
	facts := &OnCallFacts{At: at, Finding: f, Windows: []*v1.TimeWindow{{Start: timestamppb.New(at.Add(-time.Minute)), End: timestamppb.New(at.Add(-time.Second)), Label: "active"}}}
	r := &v1.GetReplayResponse{}
	for i := 0; i < 20; i++ {
		r.Steps = append(r.Steps, &v1.ReplayStep{Id: fmt.Sprint(i), Time: timestamppb.New(f.Since.AsTime().Add(time.Duration(i) * time.Minute))})
	}
	got := FinishOnCall(facts, r, "")
	if len(got.Steps) != 12 || got.Steps[0].Id != "0" || got.Steps[11].Id != "11" || got.Now.OpenWindow == nil {
		t.Fatal(got)
	}
	facts.At = at.Add(K8sUpgradeGap)
	if FinishOnCall(facts, nil, "").Now.OpenWindow != nil {
		t.Fatal("stale window reported open")
	}
}
