package api

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topologyGateways lists every pair of clusters with a gateway row in the
// epoch or an outbound gateway in the window, so a pair down in the epoch
// shows 0 connections. Connections and round trips come from the outbound
// rows: every gateway connection is one server's outbound and another's
// inbound, so reading both would count it twice.
func topologyGateways(f *TopologyFacts, servers []TopologyServerFact) []*v1.TopologyGateway {
	byPair := map[[2]string]*v1.TopologyGateway{}
	rtts := map[[2]string][]time.Duration{}
	get := func(cluster, gateway string) *v1.TopologyGateway {
		if cluster == gateway || cluster == "" || gateway == "" {
			return nil
		}
		p := pairOf(cluster, gateway)
		g := byPair[p]
		if g == nil {
			g = &v1.TopologyGateway{ClusterA: p[0], ClusterB: p[1]}
			byPair[p] = g
		}
		return g
	}
	for _, p := range f.Drops.GatewayPairs {
		get(p[0], p[1])
	}
	for _, r := range f.Gateways {
		g := get(r.Cluster, r.Gateway)
		if g == nil {
			continue
		}
		if r.CID != nil && r.Pending != nil {
			g.PendingBytes = ptrTo(g.GetPendingBytes() + *r.Pending)
		}
		if !r.Outbound || r.CID == nil {
			continue
		}
		g.Connections++
		if d := reported(r.RTT); d != nil {
			p := pairOf(r.Cluster, r.Gateway)
			rtts[p] = append(rtts[p], *d)
		}
	}
	// Expected: one outbound from every server of either cluster that
	// answered GATEWAYZ.
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointGatewayz] {
		answered[n] = true
	}
	for p, g := range byPair {
		for _, s := range servers {
			if answered[s.Name] && !s.Missing && (s.Cluster == p[0] || s.Cluster == p[1]) {
				g.Expected++
			}
		}
		if ds := rtts[p]; len(ds) > 0 {
			g.RttMax = durationpb.New(slices.Max(ds))
			g.RttMedian = durationpb.New(medianDuration(ds))
		}
	}
	dropping := map[[2]string]map[string]bool{}
	for _, d := range f.Drops.Gateways {
		g := byPair[pairOf(d.Cluster, d.Gateway)]
		if g == nil {
			continue
		}
		if d.ObservedDay {
			g.Drops_24H = ptrTo(g.GetDrops_24H() + d.Day)
		}
		if d.ObservedRecent {
			g.Drops_1H = ptrTo(g.GetDrops_1H() + d.Recent)
		}
		if d.Recent > 0 {
			p := pairOf(d.Cluster, d.Gateway)
			if dropping[p] == nil {
				dropping[p] = map[string]bool{}
			}
			dropping[p][d.Server] = true
		}
	}
	for p, names := range dropping {
		byPair[p].DroppingServers = slices.Sorted(maps.Keys(names))
	}
	out := slices.Collect(maps.Values(byPair))
	down := func(g *v1.TopologyGateway) bool { return g.GetConnections() == 0 && g.GetExpected() > 0 }
	slices.SortFunc(out, func(a, b *v1.TopologyGateway) int {
		return cmp.Or(compareBool(down(b), down(a)),
			cmp.Compare(b.GetDrops_1H(), a.GetDrops_1H()),
			cmp.Compare(b.GetExpected()-b.GetConnections(), a.GetExpected()-a.GetConnections()),
			cmp.Compare(b.GetRttMax().AsDuration(), a.GetRttMax().AsDuration()),
			cmp.Compare(a.GetClusterA(), b.GetClusterA()), cmp.Compare(a.GetClusterB(), b.GetClusterB()))
	})
	return out
}
