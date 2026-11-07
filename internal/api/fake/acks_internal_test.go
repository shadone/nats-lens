package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

const (
	prodAckFinding   = "consumer-010:" + Account + "/order-archive/analytics-aggregator"
	globalAckFinding = "consumer-002:" + Account + "/shipping-updates/label-printer"
)

func findingOf(t *testing.T, p *Provider, id string) *v1.Finding {
	t.Helper()
	res, err := p.GetFinding(context.Background(), &v1.GetFindingRequest{Id: id})
	if err != nil {
		t.Fatal(err)
	}
	return res.GetFinding()
}

// example-prod's consumer-010 is acknowledged by alice@example.com, quiet
// for two hours; example-global's label-printer shows the acknowledgement
// that replaced another, its quiet over.
func TestDemoAcknowledgements(t *testing.T) {
	a := findingOf(t, New(false), prodAckFinding).GetAcknowledgement()
	if a.GetAckedBy() != "alice@example.com" || a.GetNote() != "Recreating the durable; the worker's drain fix is in review" ||
		!a.GetQuiet() || !a.GetQuietUntil().AsTime().Equal(FixedNow.Add(65*time.Minute)) {
		t.Errorf("example-prod's acknowledgement = %v", a)
	}
	g := newProvider(globalScenario(), false)
	if a := findingOf(t, g, globalAckFinding).GetAcknowledgement(); a.GetId() != "2" || a.GetAckedBy() != "carol@example.com" || a.GetQuiet() {
		t.Errorf("example-global's acknowledgement = %v", a)
	}
	if first := g.acks[0].view(g.scen()); first.GetEndReason() != "replaced" || first.GetEndedBy() != "carol@example.com" {
		t.Errorf("the replaced one = %v", first)
	}
}

// In live mode the seeded acknowledgement moves with the scenario, and
// one made through the API stays on its finding as the clock moves.
func TestDemoAcknowledgementsInLiveMode(t *testing.T) {
	ctx := context.Background()
	clock := FixedNow.Add(26 * time.Hour)
	p := newProvider(prodScenario(), true)
	p.clock = func() time.Time { return clock }
	a := findingOf(t, p, prodAckFinding).GetAcknowledgement()
	if a == nil || !a.GetCreatedAt().AsTime().Equal(clock.Add(-55*time.Minute)) || !a.GetQuiet() {
		t.Fatalf("seeded acknowledgement in live mode = %v", a)
	}
	made, err := p.AcknowledgeFinding(ctx, &v1.AcknowledgeFindingRequest{FindingId: prodAckFinding, Note: "taking over",
		QuietFor: durationpb.New(30 * time.Minute), AckedBy: "bob@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(10 * time.Minute)
	if got := findingOf(t, p, prodAckFinding).GetAcknowledgement(); got.GetId() != made.GetId() || !got.GetQuiet() {
		t.Errorf("10 minutes later = %v, want %s", got, made.GetId())
	}
	clock = clock.Add(25 * time.Minute)
	if got := findingOf(t, p, prodAckFinding).GetAcknowledgement(); got.GetId() != made.GetId() || got.GetQuiet() {
		t.Errorf("35 minutes later = %v, want %s not quiet", got, made.GetId())
	}
	seeded := p.acks[0].view(p.scen())
	if seeded.GetEndReason() != "replaced" || seeded.GetEndedBy() != "bob@example.com" || !seeded.GetEndedAt().AsTime().Equal(clock.Add(-35*time.Minute)) {
		t.Errorf("the seeded one after the replacement = %v", seeded)
	}
	if _, err := p.EndAcknowledgement(ctx, &v1.EndAcknowledgementRequest{Id: "1", EndedBy: "x"}); !errors.Is(err, api.ErrFailedPrecondition) {
		t.Errorf("ending the replaced one: %v", err)
	}
}
