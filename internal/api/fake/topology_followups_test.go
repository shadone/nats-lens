package fake_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/api/fake"
)

// example-global's route between nats-eu-3 and nats-eu-5 was replaced
// three times today, twice in the last hour; no other route dropped, and
// the restarts of the us servers count none.
func TestGlobalTopologyRouteDrops(t *testing.T) {
	res := topology(t, demo(t), fake.GlobalSystem, nil)
	var dropping []string
	observed := 0
	for _, r := range res.GetRoutes() {
		if r.Drops_24H != nil {
			observed++
		}
		if r.GetDrops_24H() > 0 || r.GetDown() {
			dropping = append(dropping, fmt.Sprintf("%s-%s %s %s %v", r.GetServerA(), r.GetServerB(), optCount(r.Drops_1H), optCount(r.Drops_24H), r.GetDown()))
		}
	}
	if want := []string{"nats-eu-3-nats-eu-5 2 3 false"}; !slices.Equal(dropping, want) {
		t.Errorf("dropping routes = %v, want %v", dropping, want)
	}
	if observed == 0 {
		t.Error("no route has observed drops")
	}
}

// example-global's order-archive in ap mirrors order-events in eu, and
// search-index in ap sources catalog-changes and price-changes in us; all
// keep up.
func TestGlobalTopologyReplication(t *testing.T) {
	res := topology(t, demo(t), fake.GlobalSystem, nil)
	var got []string
	for _, r := range res.GetReplication() {
		if r.Lag == nil || r.LagBefore == nil || r.GetLastActive() == nil || r.GetNotActive() || r.GetFallingBehind() || r.GetLeader() == "" {
			t.Errorf("%s: %v", r.GetStream(), r)
		}
		got = append(got, fmt.Sprintf("%s %s@%s <- %s@%s", r.GetKind(), r.GetStream(), r.GetCluster(), r.GetOriginStream(), r.GetOriginCluster()))
	}
	want := []string{"mirror order-archive@ap <- order-events@eu", "source search-index@ap <- catalog-changes@us", "source search-index@ap <- price-changes@us"}
	if !slices.Equal(got, want) {
		t.Errorf("replication = %v, want %v", got, want)
	}
}

// Every answering server of example-global has its process memory and
// JetStream memory store; a missing one has none.
func TestGlobalTopologyMemory(t *testing.T) {
	res := topology(t, demo(t), fake.GlobalSystem, nil)
	for _, c := range res.GetClusters() {
		for _, s := range c.GetServers() {
			if s.GetMissing() != (s.MemBytes == nil) || s.GetMissing() != (s.JsMaxMemoryBytes == nil) || s.GetMemHigh() {
				t.Errorf("%s: missing %v mem %v js max %v high %v", s.GetName(), s.GetMissing(), s.MemBytes, s.JsMaxMemoryBytes, s.GetMemHigh())
			}
		}
	}
}
