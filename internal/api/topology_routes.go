package api

import (
	"cmp"
	"slices"

	"github.com/shadone/nats-lens/internal/routehealth"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topologyRoutes pairs the epoch's routes by server, marks the slow ones
// using the shared rule in this and the earlier collection, adds each
// pair's drops, and adds the pairs that are down (downPairs).
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
	listed := map[[2]string]*v1.TopologyRoute{}
	for _, r := range out {
		listed[[2]string{r.GetServerA(), r.GetServerB()}] = r
	}
	for _, r := range downPairs(f, servers, answered, listed) {
		listed[[2]string{r.GetServerA(), r.GetServerB()}] = r
		out = append(out, r)
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

// downPairs are the pairs of a cluster's full mesh that neither end
// lists. A pair is down when both servers are of one cluster and one
// JetStream domain, answered ROUTEZ in the epoch and in the collection
// before it under the same server IDs, and neither lists the other in
// either collection; and when the pair had a route row in the drop
// window, or both servers list routes in those two collections (routed)
// and are joined through them. So a server that lost its routes, and a
// mesh split into meshed halves, show down while the window holds their
// routes; sites that share a cluster name and never routed to each
// other, single-server ones included, are never paired.
func downPairs(f *TopologyFacts, servers []TopologyServerFact, answered map[string]bool, listed map[[2]string]*v1.TopologyRoute) []*v1.TopologyRoute {
	before := map[string]bool{}
	for _, n := range f.RoutezBefore {
		before[n] = true
	}
	parent := map[string]string{}
	var find func(string) string
	find = func(n string) string {
		p, ok := parent[n]
		if !ok || p == n {
			parent[n] = n
			return n
		}
		r := find(p)
		parent[n] = r
		return r
	}
	routed := map[string]bool{}
	listedBefore := map[[2]string]bool{}
	for _, rows := range [][]TopologyRouteRow{f.Routes, f.RoutesBefore} {
		for _, r := range rows {
			if r.Server == r.Remote || r.Remote == "" {
				continue
			}
			routed[r.Server], routed[r.Remote] = true, true
			parent[find(r.Server)] = find(r.Remote)
		}
	}
	for _, r := range f.RoutesBefore {
		listedBefore[pairOf(r.Server, r.Remote)] = true
	}
	inWindow := map[[2]string]bool{}
	for _, p := range f.Drops.RoutePairs {
		inWindow[p] = true
	}
	type mesh struct{ cluster, domain string }
	steady := map[mesh][]string{}
	for _, sv := range servers {
		if sv.Cluster != "" && !sv.Missing && answered[sv.Name] && before[sv.Name] {
			k := mesh{sv.Cluster, sv.Domain}
			steady[k] = append(steady[k], sv.Name)
		}
	}
	var out []*v1.TopologyRoute
	for k, names := range steady {
		slices.Sort(names)
		names = slices.Compact(names)
		for i, a := range names {
			for _, b := range names[i+1:] {
				p := [2]string{a, b}
				if listed[p] != nil || listedBefore[p] {
					continue
				}
				if !inWindow[p] && !(routed[a] && routed[b] && find(a) == find(b)) {
					continue
				}
				out = append(out, &v1.TopologyRoute{Cluster: k.cluster, ServerA: a, ServerB: b, Down: true})
			}
		}
	}
	return out
}
