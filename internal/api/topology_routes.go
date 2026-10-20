package api

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The slow route rule: a route is slow at slowRouteFloor or more when it
// is slowRouteFactor times its reference.
const (
	slowRouteFactor = 3
	slowRouteFloor  = time.Millisecond
)

// routeEnd is one end's report of a pair.
type routeEnd struct {
	pool   int32
	pinned []string
}

// topologyRoutes pairs the epoch's routes by server and marks the slow
// ones.
func topologyRoutes(f *TopologyFacts, servers []TopologyServerFact) []*v1.TopologyRoute {
	clusterOf := map[string]string{}
	for _, s := range servers {
		clusterOf[s.Name] = s.Cluster
	}
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointRoutez] {
		answered[n] = true
	}
	byPair := map[[2]string]*v1.TopologyRoute{}
	ends := map[[2]string]map[string]*routeEnd{}
	for _, r := range f.Routes {
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
	markSlowRoutes(out)
	slices.SortFunc(out, func(a, b *v1.TopologyRoute) int {
		return cmp.Or(compareBool(b.GetSlow(), a.GetSlow()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetServerA(), b.GetServerA()), cmp.Compare(a.GetServerB(), b.GetServerB()))
	})
	return out
}

// markSlowRoutes marks, per cluster, the pairs whose round trip is at
// least slowRouteFloor and either
//   - at least slowRouteFactor times the median of the cluster's pairs that
//     touch neither of its servers (a slow link), or
//   - has a slow server at one end: a server whose fastest pair is at least
//     slowRouteFactor times the median of the pairs that do not touch it.
//
// Comparing with the pairs that do not touch the servers keeps one slow
// server from setting the reference, as it would with the cluster's
// median in a cluster of 3 or 4. With 2 servers there is nothing to
// compare with, and nothing is slow.
func markSlowRoutes(routes []*v1.TopologyRoute) {
	byCluster := map[string][]*v1.TopologyRoute{}
	for _, r := range routes {
		if r.Rtt != nil {
			byCluster[r.GetCluster()] = append(byCluster[r.GetCluster()], r)
		}
	}
	for _, rs := range byCluster {
		touches := func(r *v1.TopologyRoute, s string) bool { return r.GetServerA() == s || r.GetServerB() == s }
		// reference is the median round trip of the pairs that touch none
		// of the servers; false when there are none.
		reference := func(servers ...string) (time.Duration, bool) {
			var ds []time.Duration
			for _, r := range rs {
				if !slices.ContainsFunc(servers, func(s string) bool { return touches(r, s) }) {
					ds = append(ds, r.GetRtt().AsDuration())
				}
			}
			if len(ds) == 0 {
				return 0, false
			}
			return medianDuration(ds), true
		}
		slowAgainst := func(d time.Duration, servers ...string) bool {
			ref, ok := reference(servers...)
			return ok && d >= slowRouteFloor && d >= slowRouteFactor*ref
		}
		fastest := map[string]time.Duration{}
		for _, r := range rs {
			for _, s := range []string{r.GetServerA(), r.GetServerB()} {
				if d, ok := fastest[s]; !ok || r.GetRtt().AsDuration() < d {
					fastest[s] = r.GetRtt().AsDuration()
				}
			}
		}
		slowServer := map[string]bool{}
		for _, s := range slices.Sorted(maps.Keys(fastest)) {
			slowServer[s] = slowAgainst(fastest[s], s)
		}
		for _, r := range rs {
			r.Slow = slowServer[r.GetServerA()] || slowServer[r.GetServerB()] ||
				slowAgainst(r.GetRtt().AsDuration(), r.GetServerA(), r.GetServerB())
		}
	}
}
