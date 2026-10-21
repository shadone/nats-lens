package api_test

import (
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
)

// Two stores that fill alike, one a hair over 2.5 days and one
// a hair under, straddle the rounding boundary; both read "2.5 days", so they keep their name order.
func TestCapacityServersTieAtRoundingBoundary(t *testing.T) {
	f := &api.CapacityFacts{Epoch: 1, EpochAt: capAt, Servers: []api.ServerStoreFact{
		{Name: "nats-0", Cluster: "c", Reported: true, Used: i64(0), Limit: i64(250 << 20), PerDay: f64(100 << 20)},
		{Name: "nats-1", Cluster: "c", Reported: true, Used: i64(0), Limit: i64(250 << 20), PerDay: f64(100<<20 + 0.001)},
	}}
	var order []string
	for _, s := range api.FinishCapacity(f).GetServers() {
		order = append(order, s.GetServerName())
	}
	if want := []string{"nats-0", "nats-1"}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}

// Cards order by the words they show: within one wording by name, across
// wordings the sooner first, and beyond 5 years as one group.
func TestShownDays(t *testing.T) {
	same := [][2]float64{{2.5, 2.49999}, {13.4, 13.2}, {188, 190}, {1500, 1600}, {2000, 9000}, {0.2, 0.205}}
	for _, p := range same {
		if api.ShownDays(p[0]) != api.ShownDays(p[1]) {
			t.Errorf("%v and %v read alike but differ: %v, %v", p[0], p[1], api.ShownDays(p[0]), api.ShownDays(p[1]))
		}
	}
	apart := [][2]float64{{2.4, 2.5}, {13, 14}, {150, 188}, {1000, 1500}, {1800, 2000}}
	for _, p := range apart {
		if !(api.ShownDays(p[0]) < api.ShownDays(p[1])) {
			t.Errorf("%v reads sooner than %v but sorts %v, %v", p[0], p[1], api.ShownDays(p[0]), api.ShownDays(p[1]))
		}
	}
}
