package fake

import (
	"context"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The demo keeps the newest maxMadeRecords mutes made through the API and
// drops the oldest, never the scenario's own.
func TestDemoKeepsNewestMutes(t *testing.T) {
	ctx := context.Background()
	p := newProvider(globalScenario(), false)
	seeded := len(p.mutes)
	if seeded == 0 {
		t.Fatal("example-global seeds no mute")
	}
	seededIDs := make([]string, seeded)
	for i, sm := range p.mutes {
		seededIDs[i] = sm.m.GetId()
	}
	var last *v1.Mute
	for i := 0; i < maxMadeRecords+20; i++ {
		m, err := p.MuteFinding(ctx, &v1.MuteFindingRequest{CheckCode: "consumer-009",
			Entity: &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_CONSUMER, Key: "APP/order-jobs/resize-worker"},
			Reason: "load", MutedBy: "op", ExpiresAt: timestamppb.New(FixedNow.Add(time.Hour))})
		if err != nil {
			t.Fatal(err)
		}
		last = m
	}
	if len(p.mutes) != seeded+maxMadeRecords {
		t.Fatalf("mutes kept = %d, want %d", len(p.mutes), seeded+maxMadeRecords)
	}
	for i, id := range seededIDs {
		if p.mutes[i].m.GetId() != id {
			t.Fatalf("seeded mute %d is %s, want %s", i, p.mutes[i].m.GetId(), id)
		}
	}
	firstKept, _ := strconv.Atoi(p.mutes[seeded].m.GetId())
	if want := seeded + 1 + 20; firstKept != want {
		t.Errorf("oldest made mute kept = %d, want %d", firstKept, want)
	}
	if p.mutes[len(p.mutes)-1].m.GetId() != last.GetId() {
		t.Errorf("newest mute = %s, want %s", p.mutes[len(p.mutes)-1].m.GetId(), last.GetId())
	}
}

// The same bound holds for acknowledgements.
func TestDemoKeepsNewestAcknowledgements(t *testing.T) {
	ctx := context.Background()
	p := newProvider(globalScenario(), false)
	seeded := len(p.acks)
	for i := 0; i < maxMadeRecords+20; i++ {
		if _, err := p.AcknowledgeFinding(ctx, &v1.AcknowledgeFindingRequest{FindingId: globalAckFinding, Note: "n",
			QuietFor: durationpb.New(time.Minute), AckedBy: "op"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.acks) != seeded+maxMadeRecords {
		t.Fatalf("acknowledgements kept = %d, want %d", len(p.acks), seeded+maxMadeRecords)
	}
	if a := findingOf(t, p, globalAckFinding).GetAcknowledgement(); a.GetId() != strconv.Itoa(seeded+maxMadeRecords+20) {
		t.Errorf("acknowledgement in force = %v", a)
	}
}
