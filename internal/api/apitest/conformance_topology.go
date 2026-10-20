package apitest

import (
	"fmt"
	"slices"

	"connectrpc.com/connect"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topology reads the Topology of the system's last day.
func (q Q) topology() *v1.GetTopologyResponse {
	q.t.Helper()
	return must(q.env.Client.GetTopology(ctx, connect.NewRequest(&v1.GetTopologyRequest{System: q.System, Range: q.day()}))).of(q.t)
}

// topologyGateway finds a gateway pair of the system's Topology that ok
// accepts.
func topologyGateway(ok func(*v1.TopologyGateway) bool) func(Q) string {
	return func(q Q) string {
		for _, g := range q.topology().GetGateways() {
			if ok(g) {
				return g.GetClusterA() + "-" + g.GetClusterB()
			}
		}
		return ""
	}
}

// topologyLeaf finds a leaf node site of the system's Topology that ok
// accepts.
func topologyLeaf(ok func(*v1.TopologyLeafnode) bool) func(Q) string {
	return func(q Q) string {
		for _, l := range q.topology().GetLeafnodes() {
			if ok(l) {
				return l.GetName() + " on " + l.GetHubServer()
			}
		}
		return ""
	}
}

// topologyRows are the Topology states both providers must show.
func topologyRows() []State {
	return []State{
		{"a Topology gateway pair with its highest and median round trip", topologyGateway(func(g *v1.TopologyGateway) bool {
			return g.GetConnections() > 0 && g.GetRttMax() != nil && g.GetRttMedian() != nil &&
				g.GetRttMedian().AsDuration() <= g.GetRttMax().AsDuration()
		})},
		{"a Topology gateway that dropped in the last hour, with its servers", topologyGateway(func(g *v1.TopologyGateway) bool {
			return g.GetDrops_1H() > 0 && len(g.GetDroppingServers()) > 0 && g.GetDrops_24H() >= g.GetDrops_1H()
		})},
		{"a slow Topology route, listed first", func(q Q) string {
			rs := q.topology().GetRoutes()
			if len(rs) > 0 && rs[0].GetSlow() && rs[0].GetRtt() != nil {
				return rs[0].GetServerA() + "-" + rs[0].GetServerB()
			}
			return ""
		}},
		{"a Topology leaf node not connected, with drops, its last hub and when it was last seen",
			topologyLeaf(func(l *v1.TopologyLeafnode) bool {
				return !l.GetConnected() && l.GetDrops_24H() > 0 && l.GetHubServer() != "" && l.GetHubCluster() != "" &&
					l.GetLastSeen() != nil && l.Rtt == nil && l.Subscriptions == nil
			})},
		{"a Topology leaf node whose drops are unset: no two consecutive collections cover it",
			topologyLeaf(func(l *v1.TopologyLeafnode) bool { return l.GetConnected() && l.Drops_24H == nil })},
		{"a single-cluster Topology with routes only", func(q Q) string {
			res := q.topology()
			if len(res.GetClusters()) == 1 && len(res.GetRoutes()) > 0 && len(res.GetGateways()) == 0 && len(res.GetLeafnodes()) == 0 {
				return res.GetClusters()[0].GetName()
			}
			return ""
		}},
		{"the Topology's servers are ListServers' expected ones, and its missing ones its missing, in every system", everySystem(func(q Q) string {
			res := q.topology()
			ls := must(q.env.Client.ListServers(ctx, connect.NewRequest(&v1.ListServersRequest{System: q.System, Range: q.day()}))).of(q.t)
			var names, missing []string
			for _, c := range res.GetClusters() {
				for _, s := range c.GetServers() {
					names = append(names, s.GetName())
					if s.GetMissing() {
						missing = append(missing, s.GetName())
					}
				}
			}
			want := append(slices.Clone(ls.GetExpected()), ls.GetMissing()...)
			slices.Sort(want)
			want = slices.Compact(want)
			slices.Sort(names)
			wantMissing := slices.Sorted(slices.Values(ls.GetMissing()))
			slices.Sort(missing)
			if !slices.Equal(names, want) || !slices.Equal(missing, wantMissing) {
				q.t.Errorf("system %q: topology servers %v missing %v; ListServers %v missing %v", q.System, names, missing, want, wantMissing)
				return ""
			}
			if len(names) == 0 {
				return ""
			}
			return fmt.Sprintf("%d servers, %d missing", len(names), len(missing))
		})},
	}
}
