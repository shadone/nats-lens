package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// cluster is n servers s0..s(n-1) in eu with routes both ways, the last
// server's at slowMS and the others' at 0.4 ms.
func cluster(n int, slowMS float64) *api.TopologyFacts {
	f := topoFacts()
	f.Servers = nil
	var names []string
	for i := range n {
		names = append(names, fmt.Sprintf("s%d", i))
		f.Servers = append(f.Servers, api.TopologyServerFact{Name: names[i], Cluster: "eu"})
	}
	slowOne := names[n-1]
	for i, a := range names {
		for _, b := range names[i+1:] {
			rtt := 0.4
			if b == slowOne {
				rtt = slowMS
			}
			f.Routes = append(f.Routes, route(a, b, rtt, ""), route(b, a, rtt, ""))
		}
	}
	return f
}

func slowRoutes(f *api.TopologyFacts) []string {
	var out []string
	for _, r := range api.FinishTopology(f).GetRoutes() {
		if r.GetSlow() {
			out = append(out, r.GetServerA()+"-"+r.GetServerB())
		}
	}
	slices.Sort(out)
	return out
}

// One slow server makes its routes slow in a cluster of 3, 4 or 5; with
// 2 servers there is nothing to compare with.
func TestTopologySlowServerSmallClusters(t *testing.T) {
	for n := 2; n <= 5; n++ {
		var want []string
		if n > 2 {
			for i := range n - 1 {
				want = append(want, fmt.Sprintf("s%d-s%d", i, n-1))
			}
		}
		if got := slowRoutes(cluster(n, 4)); !slices.Equal(got, want) {
			t.Errorf("%d servers, one at 4 ms: slow %v, want %v", n, got, want)
		}
		// Under 1 ms nothing is slow.
		if got := slowRoutes(cluster(n, 0.9)); len(got) != 0 {
			t.Errorf("%d servers, one at 0.9 ms: slow %v", n, got)
		}
	}
}

// One slow link between two otherwise fast servers is slow in a cluster
// of 4 or more; its servers are not.
func TestTopologySlowLink(t *testing.T) {
	f := cluster(5, 0.4)
	for i := range f.Routes {
		if r := &f.Routes[i]; (r.Server == "s0" && r.Remote == "s1") || (r.Server == "s1" && r.Remote == "s0") {
			r.RTT = ms(4)
		}
	}
	if got := slowRoutes(f); !slices.Equal(got, []string{"s0-s1"}) {
		t.Errorf("slow = %v, want s0-s1", got)
	}
}

// A round trip of 0, sent before the first ping, counts as not reported:
// no RTT, out of the medians and the slow rule.
func TestTopologyZeroRTT(t *testing.T) {
	f := cluster(3, 4)
	f.Routes = append(f.Routes, route("s0", "s1", 0, ""))
	for i := range f.Routes {
		if f.Routes[i].Server == "s0" && f.Routes[i].Remote == "s2" {
			f.Routes[i].RTT = ms(0)
		}
	}
	f.Servers = append(f.Servers, api.TopologyServerFact{Name: "u0", Cluster: "us"})
	f.Gateways = []api.TopologyGatewayRow{outbound("E0", "s0", 1, 0), outbound("E1", "s1", 1, 80)}
	f.Gateways[0].Cluster, f.Gateways[0].Gateway, f.Gateways[1].Cluster, f.Gateways[1].Gateway = "eu", "us", "eu", "us"
	f.Leafnodes = []api.TopologyLeafRow{leaf("E0", "s0", "store-a", 1)}
	f.Leafnodes[0].RTT = ms(0)
	res := api.FinishTopology(f)
	for _, r := range res.GetRoutes() {
		if r.GetServerA() == "s0" && r.GetServerB() == "s1" && r.GetRtt().AsDuration() != 400*time.Microsecond {
			t.Errorf("s0-s1 rtt %v, want 0.4ms from the other reports", r.GetRtt().AsDuration())
		}
	}
	if g := res.GetGateways()[0]; g.GetRttMedian().AsDuration() != 80*time.Millisecond || g.GetConnections() != 2 {
		t.Errorf("gateway = %v, want median 80ms of 2 connections", g)
	}
	if l := res.GetLeafnodes()[0]; l.Rtt != nil {
		t.Errorf("leaf rtt %v, want unset", l.GetRtt())
	}
}

// A gateway pair whose connections are all gone in the epoch is listed
// first, with 0 connections and its drops.
func TestTopologyGatewayPairDownInEpoch(t *testing.T) {
	f := topoFacts()
	f.Servers = append(f.Servers, api.TopologyServerFact{Name: "a0", Cluster: "ap"})
	up := []api.TopologyGatewayRow{outbound("E0", "e0", 1, 80), outbound("E1", "e1", 1, 80)}
	ap := api.TopologyGatewayRow{ServerID: "A0", Server: "a0", Cluster: "ap", Gateway: "eu", Outbound: true, CID: i64(3), RTT: ms(160)}
	f.Gateways = []api.TopologyGatewayRow{ap}
	history(f, []api.TopologyCollection{
		collection(1, time.Minute, append(slices.Clone(up), ap), nil, "E0", "E1", "A0"),
		collection(2, 30*time.Second, append(slices.Clone(up), ap), nil, "E0", "E1", "A0"),
		collection(3, 0, []api.TopologyGatewayRow{ap}, nil, "E0", "E1", "A0"),
	})
	gws := api.FinishTopology(f).GetGateways()
	if len(gws) != 2 {
		t.Fatalf("gateways = %v, want ap-eu and eu-us", gws)
	}
	if g := gws[0]; g.GetClusterA() != "eu" || g.GetConnections() != 0 || g.GetExpected() != 6 || g.GetDrops_1H() != 2 ||
		!slices.Equal(g.GetDroppingServers(), []string{"e0", "e1"}) {
		t.Errorf("first pair = %v, want eu-us down with 2 drops", g)
	}
}

// A server without a cluster is placed in the cluster of a server whose
// route names it; without one it stays in the cluster of unknown name.
func TestTopologyUnknownCluster(t *testing.T) {
	f := topoFacts()
	f.Servers = append(f.Servers, api.TopologyServerFact{Name: "e9", Missing: true}, api.TopologyServerFact{Name: "x1", Missing: true})
	f.Routes = []api.TopologyRouteRow{route("e0", "e9", 0.4, "")}
	var got []string
	for _, c := range api.FinishTopology(f).GetClusters() {
		for _, s := range c.GetServers() {
			if s.GetName() == "e9" || s.GetName() == "x1" {
				got = append(got, s.GetName()+" in "+c.GetName())
			}
		}
	}
	if want := []string{"x1 in ", "e9 in eu"}; !slices.Equal(got, want) {
		t.Errorf("placed %v, want %v", got, want)
	}
}

// A site bound to two accounts counts once in its cluster; a site on two
// hubs at once is one row naming both.
func TestTopologySiteAccountsAndHubs(t *testing.T) {
	f := topoFacts()
	other := leaf("E0", "e0", "store-a", 2)
	other.Account = "SYS"
	f.Leafnodes = []api.TopologyLeafRow{leaf("E0", "e0", "store-a", 1), other, leaf("E1", "e1", "store-b", 1), leaf("E0", "e0", "store-b", 3)}
	res := api.FinishTopology(f)
	if n := res.GetClusters()[0].GetLeafnodesConnected(); n != 2 {
		t.Errorf("eu counts %d sites, want 2", n)
	}
	for _, l := range res.GetLeafnodes() {
		if l.GetName() == "store-b" && (l.GetHubServer() != "e0" || !slices.Equal(l.GetHubServers(), []string{"e0", "e1"})) {
			t.Errorf("store-b = %v, want hubs e0 and e1", l)
		}
	}
	if res.GetLeafnodesTotal() != 3 {
		t.Errorf("%d rows, want 3 (store-a twice, store-b once)", res.GetLeafnodesTotal())
	}
}

// A failed LEAFZ makes a site's state unknown only in the cluster the site
// was last seen on.
func TestTopologyUnknownScope(t *testing.T) {
	f := topoFacts()
	gone := leaf("E0", "e0", "gone", 2)
	f.Drops = api.TopologyDrops{LastSeen: []api.TopologyLeafSighting{{Row: gone, At: topoAt.Add(-time.Hour)}},
		Leafs: []api.TopologyLeafDrops{{Name: "gone", Account: "APP", Drops: 1}}}
	f.Answered[api.EndpointLeafz] = []string{"e0", "e1", "e2", "u1", "u2"}
	f.Unanswered[api.EndpointLeafz] = []string{"u0"}
	res := api.FinishTopology(f)
	if l := res.GetLeafnodes()[0]; l.GetConnectionUnknown() || res.GetClusters()[0].GetLeafnodesNotConnected() != 1 {
		t.Errorf("a failure on us: %v, eu not connected %d; want known not connected", l, res.GetClusters()[0].GetLeafnodesNotConnected())
	}
	f.Answered[api.EndpointLeafz] = []string{"e0", "e1", "u0", "u1", "u2"}
	f.Unanswered[api.EndpointLeafz] = []string{"e2"}
	if l := api.FinishTopology(f).GetLeafnodes()[0]; !l.GetConnectionUnknown() {
		t.Errorf("a failure on eu: %v, want unknown", l)
	}
}

// The rows of a server placed in a cluster by the routes count there: its
// gateways, its sites and their counts.
func TestTopologyPlacedRows(t *testing.T) {
	f := topoFacts()
	f.Servers = []api.TopologyServerFact{{Name: "e0", Cluster: "eu"}, {Name: "x"}, {Name: "u0", Cluster: "us"}}
	f.Answered = map[string][]string{api.EndpointLeafz: {"e0", "x", "u0"}, api.EndpointGatewayz: {"e0", "x", "u0"}, api.EndpointRoutez: {"e0", "x"}}
	f.Routes = []api.TopologyRouteRow{route("e0", "x", 0.4, ""), {Server: "x", Remote: "e0", RTT: ms(0.4)}}
	xGateway := api.TopologyGatewayRow{ServerID: "X", Server: "x", Gateway: "us", Outbound: true, CID: i64(1)}
	f.Gateways = []api.TopologyGatewayRow{outbound("E0", "e0", 1, 80), xGateway, outbound("U0", "u0", 1, 80)}
	f.Leafnodes = []api.TopologyLeafRow{{ServerID: "X", Server: "x", LID: 1, Name: "site-a", Account: "APP"}}
	f.Drops = api.TopologyDrops{LastSeen: []api.TopologyLeafSighting{{Row: api.TopologyLeafRow{ServerID: "X", Server: "x", LID: 2,
		Name: "site-b", Account: "APP"}, At: topoAt.Add(-time.Minute)}},
		Gateways: []api.TopologyGatewayDrops{{Server: "x", Gateway: "us", Recent: 1, Day: 1, ObservedRecent: true, ObservedDay: true}}}
	res := api.FinishTopology(f)
	if len(res.GetClusters()) != 2 || res.GetClusters()[0].GetName() != "eu" {
		t.Fatalf("clusters = %v", res.GetClusters())
	}
	if eu := res.GetClusters()[0]; eu.GetLeafnodesConnected() != 1 || eu.GetLeafnodesNotConnected() != 1 {
		t.Errorf("eu = %v, want site-a connected and site-b not", eu)
	}
	for _, l := range res.GetLeafnodes() {
		if l.GetHubCluster() != "eu" {
			t.Errorf("site %s on cluster %q, want eu", l.GetName(), l.GetHubCluster())
		}
	}
	if g := res.GetGateways(); len(g) != 1 || g[0].GetConnections() != 3 || g[0].GetExpected() != 3 || g[0].GetDrops_1H() != 1 {
		t.Errorf("gateways = %v, want eu-us 3 of 3 with x's drop", g)
	}
}
