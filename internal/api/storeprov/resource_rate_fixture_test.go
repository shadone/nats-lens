package storeprov_test

import (
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api/storeprov"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The scenario promises positive and measured-zero sequence rates. Those
// assertions require explicit identity evidence, rather than treating NULL
// creation metadata as an unchanged incarnation.
func TestConformanceRateFixtureHasExplicitIdentityEvidence(t *testing.T) {
	st, set := conformanceStore(t)
	var all, known int
	if err := st.DB().QueryRow(`SELECT count(*),count(created) FROM consumer_stats WHERE consumer IN ('fast','planner')`).Scan(&all, &known); err != nil {
		t.Fatal(err)
	}
	if all != 8 || known != all {
		t.Fatalf("positive rate fixture: %d/%d reports have known creation metadata", known, all)
	}
	p := &storeprov.Provider{St: st, Checks: set, Interval: confStep, Clock: func() time.Time { return confAt(confEpochs).Add(30 * time.Second) }}
	req := &v1.GetConsumerRequest{Account: "APP", Stream: "orders", Range: &v1.TimeRange{Start: timestamppb.New(confStart), End: timestamppb.New(confAt(confEpochs).Add(time.Microsecond))}}
	for name, want := range map[string]float64{"fast": 1, "planner": 0} {
		req.Consumer = name
		got, err := p.GetConsumer(ctx, req)
		if err != nil || got.GetConsumer().DeliveredPerSecond == nil || got.GetConsumer().GetDeliveredPerSecond() != want {
			t.Fatalf("%s rate = %v, %v; want measured %v", name, got, err, want)
		}
	}
	// A provider configured for thirty seconds cannot certify five-minute
	// pairs. This is independent of missing identity metadata.
	shortCadence := &storeprov.Provider{St: st, Checks: set, Interval: 30 * time.Second, Clock: p.Clock}
	req.Consumer = "fast"
	tooFar, err := shortCadence.GetConsumer(ctx, req)
	if err != nil || tooFar.GetConsumer().DeliveredPerSecond != nil {
		t.Fatalf("out-of-policy cadence inferred rate: %v, %v", tooFar, err)
	}
	// Removing identity evidence must remove the rate, not turn it into zero
	// or borrow an earlier known run.
	if _, err := st.DB().Exec(`UPDATE consumer_stats SET created=NULL WHERE consumer='fast'`); err != nil {
		t.Fatal(err)
	}
	req.Consumer = "fast"
	got, err := p.GetConsumer(ctx, req)
	if err != nil || got.GetConsumer().DeliveredPerSecond != nil {
		t.Fatalf("unknown identity inferred rate: %v, %v", got, err)
	}
}
