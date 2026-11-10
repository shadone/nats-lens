package api

import (
	"cmp"
	"slices"

	"github.com/shadone/nats-lens/internal/routehealth"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topologyRoutes pairs the epoch's routes by server, marks the slow ones
// using the shared rule in this and the earlier collection, and adds each
// pair's drops. The servers of a cluster form a full mesh, so two servers
// of one cluster that both answered ROUTEZ in the epoch and in the
// collection before it under the same server IDs, and that do not list
// each other, are added as a pair that is down.
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
	before := map[string]bool{}
	for _, n := range f.RoutezBefore {
		before[n] = true
	}
	steady := map[string][]string{}
	for _, sv := range servers {
		if sv.Cluster != "" && !missing[sv.Name] && answered[sv.Name] && before[sv.Name] {
			steady[sv.Cluster] = append(steady[sv.Cluster], sv.Name)
		}
	}
	for cluster, names := range steady {
		slices.Sort(names)
		names = slices.Compact(names)
		for i, a := range names {
			for _, b := range names[i+1:] {
				if listed[[2]string{a, b}] == nil {
					r := &v1.TopologyRoute{Cluster: cluster, ServerA: a, ServerB: b, Down: true}
					listed[[2]string{a, b}] = r
					out = append(out, r)
				}
			}
		}
	}
	for _, d := range f.Drops.Routes {
		r := listed[[2]string{d.A, d.B}]
		if r == nil {
			continue
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
