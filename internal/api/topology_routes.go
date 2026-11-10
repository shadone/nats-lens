package api

import (
	"cmp"
	"slices"

	"github.com/shadone/nats-lens/internal/routehealth"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topologyRoutes pairs the epoch's routes by server, marks the slow ones
// using the shared rule in this and the earlier collection, and adds each
// pair's drops. A pair whose route dropped in the window and that neither
// end lists now, while both answered ROUTEZ, is added as down.
func topologyRoutes(f *TopologyFacts, servers []TopologyServerFact) []*v1.TopologyRoute {
	clusterOf := map[string]string{}
	missing := map[string]bool{}
	for _, s := range servers {
		clusterOf[s.Name] = s.Cluster
		missing[s.Name] = s.Missing
	}
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointRoutez] {
		answered[n] = true
	}
	out := routehealth.Routes(f.Routes, f.Earlier, f.EpochAt, f.Interval, clusterOf, answered)
	listed := map[[2]string]*v1.TopologyRoute{}
	for _, r := range out {
		listed[[2]string{r.GetServerA(), r.GetServerB()}] = r
	}
	for _, d := range f.Drops.Routes {
		r := listed[[2]string{d.A, d.B}]
		if r == nil {
			cluster, known := clusterOf[d.A]
			if d.Day == 0 || !known || cluster != clusterOf[d.B] || missing[d.A] || missing[d.B] || !answered[d.A] || !answered[d.B] {
				continue
			}
			r = &v1.TopologyRoute{Cluster: cluster, ServerA: d.A, ServerB: d.B, Down: true}
			out = append(out, r)
		}
		if d.ObservedRecent {
			r.Drops_1H = ptrTo(d.Recent)
		}
		if d.ObservedDay {
			r.Drops_24H = ptrTo(d.Day)
		}
	}
	slices.SortFunc(out, func(a, b *v1.TopologyRoute) int {
		return cmp.Or(compareBool(b.GetDown(), a.GetDown()), compareBool(b.GetSlow(), a.GetSlow()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetServerA(), b.GetServerA()), cmp.Compare(a.GetServerB(), b.GetServerB()))
	})
	return out
}
