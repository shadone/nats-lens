package api

import (
	"cmp"
	"slices"

	"github.com/shadone/nats-lens/internal/routehealth"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topologyRoutes pairs the epoch's routes by server and marks the slow
// ones using the shared rule in this and the earlier collection.
func topologyRoutes(f *TopologyFacts, servers []TopologyServerFact) []*v1.TopologyRoute {
	clusterOf := map[string]string{}
	for _, s := range servers {
		clusterOf[s.Name] = s.Cluster
	}
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointRoutez] {
		answered[n] = true
	}
	out := routehealth.Routes(f.Routes, f.Earlier, f.EpochAt, f.Interval, clusterOf, answered)
	slices.SortFunc(out, func(a, b *v1.TopologyRoute) int {
		return cmp.Or(compareBool(b.GetSlow(), a.GetSlow()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetServerA(), b.GetServerA()), cmp.Compare(a.GetServerB(), b.GetServerB()))
	})
	return out
}
