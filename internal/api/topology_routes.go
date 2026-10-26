package api

import (
	"cmp"
	"slices"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// routeEnd is one end's report of a pair.
type routeEnd struct {
	pool   int32
	pinned []string
}

// topologyRoutes pairs the epoch's routes by server and marks the slow
// ones: those slow by markSlowRoutes in the epoch and in the earlier
// collection.
func topologyRoutes(f *TopologyFacts, servers []TopologyServerFact) []*v1.TopologyRoute {
	clusterOf := map[string]string{}
	for _, s := range servers {
		clusterOf[s.Name] = s.Cluster
	}
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointRoutez] {
		answered[n] = true
	}
	out := pairRoutes(f.Routes, clusterOf, answered)
	var earlier []*v1.TopologyRoute
	bound, known := TopologyEarliestEarlier(f.EpochAt, f.Interval)
	if e := f.Earlier; e != nil && known && e.At.Before(f.EpochAt) && !e.At.Before(bound) {
		earlier = pairRoutes(e.Routes, clusterOf, nil)
	}
	markSlowTwice(out, earlier)
	slices.SortFunc(out, func(a, b *v1.TopologyRoute) int {
		return cmp.Or(compareBool(b.GetSlow(), a.GetSlow()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetServerA(), b.GetServerA()), cmp.Compare(a.GetServerB(), b.GetServerB()))
	})
	return out
}

// pairRoutes pairs one collection's routes by server; answered names the
// servers whose ROUTEZ answered, for one-sided pairs.
func pairRoutes(rows []TopologyRouteRow, clusterOf map[string]string, answered map[string]bool) []*v1.TopologyRoute {
	byPair := map[[2]string]*v1.TopologyRoute{}
	ends := map[[2]string]map[string]*routeEnd{}
	for _, r := range rows {
		if r.Remote == "" || r.Remote == r.Server {
			continue
		}
		p := pairOf(r.Server, r.Remote)
		t := byPair[p]
		if t == nil {
			t = &v1.TopologyRoute{Cluster: cmp.Or(r.Cluster, clusterOf[r.Server]), ServerA: p[0], ServerB: p[1]}
			byPair[p] = t
			ends[p] = map[string]*routeEnd{}
		}
		e := ends[p][r.Server]
		if e == nil {
			e = &routeEnd{}
			ends[p][r.Server] = e
		}
		if r.Account == "" {
			e.pool++
		} else if !slices.Contains(e.pinned, r.Account) {
			e.pinned = append(e.pinned, r.Account)
		}
		if d := reported(r.RTT); d != nil && (t.Rtt == nil || *d > t.GetRtt().AsDuration()) {
			t.Rtt = durationpb.New(*d)
		}
		if r.Pending != nil {
			t.PendingBytes = ptrTo(t.GetPendingBytes() + *r.Pending)
		}
	}
	out := make([]*v1.TopologyRoute, 0, len(byPair))
	for p, t := range byPair {
		for _, n := range p {
			e := ends[p][n]
			if e == nil {
				// The other end answered ROUTEZ and lists no route to
				// this one.
				t.OneSided = answered[n]
				continue
			}
			t.ReportedBy = append(t.ReportedBy, n)
			if e.pool > 0 {
				t.Pool = ptrTo(max(t.GetPool(), e.pool))
			}
			for _, a := range e.pinned {
				if !slices.Contains(t.PinnedAccounts, a) {
					t.PinnedAccounts = append(t.PinnedAccounts, a)
				}
			}
		}
		slices.Sort(t.PinnedAccounts)
		out = append(out, t)
	}
	return out
}
