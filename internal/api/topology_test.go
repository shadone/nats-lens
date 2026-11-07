package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var topoAt = time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)

func ms(f float64) *time.Duration {
	d := time.Duration(f * float64(time.Millisecond))
	return &d
}

func i64(v int64) *int64 { return &v }

// topoServers are e0 to e2 in eu and u0 to u2 in us; every server
// answered every endpoint.
func topoFacts() *api.TopologyFacts {
	f := &api.TopologyFacts{Epoch: 100, EpochAt: topoAt, Interval: 30 * time.Second,
		Answered: map[string][]string{}, Unanswered: map[string][]string{}}
	for _, c := range []string{"eu", "us"} {
		for i := range 3 {
			n := fmt.Sprintf("%c%d", c[0], i)
			f.Servers = append(f.Servers, api.TopologyServerFact{Name: n, Cluster: c, Tags: []string{"region:" + c}})
			for _, e := range []string{api.EndpointRoutez, api.EndpointGatewayz, api.EndpointLeafz} {
				f.Answered[e] = append(f.Answered[e], n)
			}
		}
	}
	return f
}

func clusterOf(name string) string { return map[byte]string{'e': "eu", 'u': "us"}[name[0]] }

// outbound is name's outbound gateway connection to the other cluster.
func outbound(id, name string, cid int64, rtt float64) api.TopologyGatewayRow {
	other := map[string]string{"eu": "us", "us": "eu"}[clusterOf(name)]
	return api.TopologyGatewayRow{ServerID: id, Server: name, Cluster: clusterOf(name), Gateway: other, Outbound: true,
		CID: i64(cid), RTT: ms(rtt), Pending: i64(10)}
}

// answering is a set of server IDs.
func answering(ids ...string) map[string]bool {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func TestTopologyEmpty(t *testing.T) {
	res := api.FinishTopology(&api.TopologyFacts{})
	if res.GetEpoch() != 0 || len(res.GetClusters()) != 0 || res.GetEpochAt() != nil {
		t.Errorf("empty facts gave %v", res)
	}
}

// One gateway pair per two clusters: connections and RTTs from the
// outbound rows, pending from every row, expected from the servers that
// answered GATEWAYZ.
func TestTopologyGatewayPairs(t *testing.T) {
	f := topoFacts()
	f.Gateways = []api.TopologyGatewayRow{
		outbound("E0", "e0", 1, 80), outbound("E1", "e1", 1, 84), outbound("U0", "u0", 1, 86), outbound("U1", "u1", 1, 90),
		// e2's outbound is configured and not connected.
		{ServerID: "E2", Server: "e2", Cluster: "eu", Gateway: "us", Outbound: true},
		{ServerID: "U0", Server: "u0", Cluster: "us", Gateway: "eu", CID: i64(7), RTT: ms(500), Pending: i64(5)},
	}
	// u2 did not answer GATEWAYZ.
	f.Answered[api.EndpointGatewayz] = []string{"e0", "e1", "e2", "u0", "u1"}
	f.Unanswered[api.EndpointGatewayz] = []string{"u2"}
	res := api.FinishTopology(f)
	if len(res.GetGateways()) != 1 {
		t.Fatalf("gateways = %v", res.GetGateways())
	}
	g := res.GetGateways()[0]
	if g.GetClusterA() != "eu" || g.GetClusterB() != "us" || g.GetConnections() != 4 || g.GetExpected() != 5 {
		t.Errorf("gateway = %v, want eu-us 4 of 5", g)
	}
	if g.GetRttMax().AsDuration() != 90*time.Millisecond || g.GetRttMedian().AsDuration() != 85*time.Millisecond {
		t.Errorf("rtt max %v median %v, want 90ms and 85ms", g.GetRttMax().AsDuration(), g.GetRttMedian().AsDuration())
	}
	if g.GetPendingBytes() != 45 {
		t.Errorf("pending = %d, want 45", g.GetPendingBytes())
	}
	// A single collection: no drops counted.
	if g.Drops_1H != nil || g.Drops_24H != nil {
		t.Errorf("drops %s %s without history", opt(g.Drops_1H), opt(g.Drops_24H))
	}
	if u := res.GetUnobserved(); len(u) != 1 || u[0].GetEndpoint() != "GATEWAYZ" || !slices.Equal(u[0].GetServers(), []string{"u2"}) {
		t.Errorf("unobserved = %v", u)
	}
}

// collection is one collection of the drop window at topoAt minus back.
func collection(epoch int64, back time.Duration, gws []api.TopologyGatewayRow, leafs []api.TopologyLeafRow, ids ...string) api.TopologyCollection {
	return api.TopologyCollection{Epoch: epoch, At: topoAt.Add(-back), Gatewayz: answering(ids...), Leafz: answering(ids...),
		Gateways: gws, Leafnodes: leafs}
}

// history counts the drops of h into f, as the demo does, its last
// collection the epoch.
func history(f *api.TopologyFacts, h []api.TopologyCollection) {
	last := h[len(h)-1]
	f.Drops = api.CountTopologyDrops(h, f.Interval, last.Epoch, last.At)
}

// Drops: a changed connection ID or a connection gone counts between two
// consecutive collections in which the server answered GATEWAYZ; a gap, a
// failed GATEWAYZ and a restart count none.
func TestTopologyGatewayDrops(t *testing.T) {
	f := topoFacts()
	both := []string{"E0", "U0", "U0B"}
	h := []api.TopologyCollection{
		collection(1, 5*time.Hour, []api.TopologyGatewayRow{outbound("E0", "e0", 1, 80), outbound("U0", "u0", 1, 80)}, nil, both...),
		// e0 reconnected: a drop, 5 hours back.
		collection(2, 5*time.Hour-30*time.Second, []api.TopologyGatewayRow{outbound("E0", "e0", 2, 80), outbound("U0", "u0", 1, 80)}, nil, both...),
		// 10 minutes without collections: e0's new ID counts no drop.
		collection(3, 4*time.Hour, []api.TopologyGatewayRow{outbound("E0", "e0", 3, 80), outbound("U0", "u0", 1, 80)}, nil, both...),
		// e0's GATEWAYZ failed.
		collection(4, 4*time.Hour-30*time.Second, []api.TopologyGatewayRow{outbound("U0", "u0", 1, 80)}, nil, "U0", "U0B"),
		collection(5, 4*time.Hour-60*time.Second, []api.TopologyGatewayRow{outbound("E0", "e0", 4, 80), outbound("U0", "u0", 1, 80)}, nil, both...),
		// u0 restarted with a new server ID: no drop of its old one.
		collection(6, 50*time.Minute, []api.TopologyGatewayRow{outbound("E0", "e0", 4, 80), outbound("U0", "u0", 1, 80)}, nil, both...),
		collection(7, 50*time.Minute-30*time.Second, []api.TopologyGatewayRow{outbound("E0", "e0", 4, 80), outbound("U0B", "u0", 1, 80)}, nil, "E0", "U0B"),
		// e0's connection went away, then came back: one drop in the hour.
		collection(8, 50*time.Minute-60*time.Second, []api.TopologyGatewayRow{outbound("U0B", "u0", 1, 80)}, nil, both...),
		collection(9, 50*time.Minute-90*time.Second, []api.TopologyGatewayRow{outbound("E0", "e0", 5, 80), outbound("U0B", "u0", 1, 80)}, nil, both...),
	}
	f.Gateways = h[len(h)-1].Gateways
	f.Epoch, f.EpochAt = 9, h[len(h)-1].At
	history(f, h)
	g := api.FinishTopology(f).GetGateways()[0]
	if g.Drops_24H == nil || g.GetDrops_24H() != 2 || g.Drops_1H == nil || g.GetDrops_1H() != 1 {
		t.Errorf("drops 24h %s, 1h %s; want 2 and 1", opt(g.Drops_24H), opt(g.Drops_1H))
	}
	if !slices.Equal(g.GetDroppingServers(), []string{"e0"}) {
		t.Errorf("dropping servers %v, want e0", g.GetDroppingServers())
	}
	res := api.FinishTopology(f)
	if !res.GetDropsWindowStart().AsTime().Equal(h[0].At) {
		t.Errorf("window start %v, want %v", res.GetDropsWindowStart().AsTime(), h[0].At)
	}
}

// Drop counts are unset when no two consecutive collections were
// answered by a server of the pair, and 0 when they were and nothing
// changed.
func TestTopologyGatewayDropsUnset(t *testing.T) {
	f := topoFacts()
	rows := []api.TopologyGatewayRow{outbound("E0", "e0", 1, 80)}
	f.Gateways = rows
	history(f, []api.TopologyCollection{
		collection(1, 2*time.Hour, rows, nil, "E0"),
		// Too long after the first to be consecutive.
		collection(2, 0, rows, nil, "E0"),
	})
	g := api.FinishTopology(f).GetGateways()[0]
	if g.Drops_24H != nil || g.Drops_1H != nil {
		t.Errorf("drops %s %s across a gap, want unset", opt(g.Drops_24H), opt(g.Drops_1H))
	}
	history(f, []api.TopologyCollection{
		collection(1, 2*time.Hour, rows, nil, "E0"),
		collection(2, 2*time.Hour-30*time.Second, rows, nil, "E0"),
		collection(3, 0, rows, nil, "E0"),
	})
	g = api.FinishTopology(f).GetGateways()[0]
	if g.Drops_24H == nil || g.GetDrops_24H() != 0 || g.Drops_1H != nil {
		t.Errorf("drops 24h %s 1h %s, want 0 and unset", opt(g.Drops_24H), opt(g.Drops_1H))
	}
}

// route is one route as server reports it.
func route(server, remote string, rtt float64, account string) api.TopologyRouteRow {
	return api.TopologyRouteRow{Server: server, Remote: remote, Cluster: "eu", RTT: ms(rtt), Pending: i64(1), Account: account}
}

// measuredTwice gives f an earlier collection one interval before with
// the same routes, so the slow rule's two collections agree.
func measuredTwice(f *api.TopologyFacts) *api.TopologyFacts {
	f.Earlier = &api.TopologyEarlier{Epoch: f.Epoch - 1, At: f.EpochAt.Add(-f.Interval), Routes: slices.Clone(f.Routes)}
	return f
}

// Routes pair by server: the highest RTT, pending summed, the pool and
// the pinned accounts; a pair the other end does not list is one-sided
// only when that end answered ROUTEZ.
func TestTopologyRoutePairs(t *testing.T) {
	f := topoFacts()
	f.Routes = []api.TopologyRouteRow{
		route("e0", "e1", 0.3, ""), route("e0", "e1", 0.5, ""), route("e0", "e1", 0.4, "APP"),
		route("e1", "e0", 0.6, ""), route("e1", "e0", 0.4, ""), route("e1", "e0", 0.4, ""), route("e1", "e0", 0.4, "SYS"),
		route("e0", "e2", 0.4, ""),
		route("e1", "e2", 0.4, ""),
	}
	f.Answered[api.EndpointRoutez] = []string{"e0", "e1"}
	f.Unanswered[api.EndpointRoutez] = []string{"e2"}
	f.Servers[2].Missing = false
	res := api.FinishTopology(f)
	by := map[string]*v1.TopologyRoute{}
	for _, r := range res.GetRoutes() {
		by[r.GetServerA()+"-"+r.GetServerB()] = r
	}
	r := by["e0-e1"]
	if r.GetRtt().AsDuration() != 600*time.Microsecond || r.GetPendingBytes() != 7 || r.GetPool() != 3 ||
		!slices.Equal(r.GetPinnedAccounts(), []string{"APP", "SYS"}) || r.GetOneSided() || len(r.GetReportedBy()) != 2 {
		t.Errorf("e0-e1 = %v", r)
	}
	// e2 did not answer ROUTEZ: its pairs are not one-sided.
	if r := by["e0-e2"]; r.GetOneSided() || !slices.Equal(r.GetReportedBy(), []string{"e0"}) {
		t.Errorf("e0-e2 = %v", r)
	}
	// Once e2 answers and lists no route to e0, the pair is one-sided.
	f.Answered[api.EndpointRoutez] = append(f.Answered[api.EndpointRoutez], "e2")
	f.Unanswered[api.EndpointRoutez] = nil
	f.Routes = append(f.Routes, route("e2", "e1", 0.4, ""))
	for _, r := range api.FinishTopology(f).GetRoutes() {
		if want := r.GetServerA() == "e0" && r.GetServerB() == "e2"; r.GetOneSided() != want {
			t.Errorf("%s-%s one-sided %v", r.GetServerA(), r.GetServerB(), r.GetOneSided())
		}
	}
}

// One server far from the other four makes its routes slow (the rule is
// in topology_slow.go); slow routes come first, then by RTT.
func TestTopologySlowRoute(t *testing.T) {
	f := topoFacts()
	f.Servers = nil
	names := []string{"n0", "n1", "n2", "n3", "n4"}
	for _, n := range names {
		f.Servers = append(f.Servers, api.TopologyServerFact{Name: n, Cluster: "eu"})
	}
	for i, a := range names {
		for _, b := range names[i+1:] {
			rtt := 0.4
			if b == "n4" {
				rtt = 4
			}
			f.Routes = append(f.Routes, route(a, b, rtt, ""))
		}
	}
	res := api.FinishTopology(measuredTwice(f))
	var slow []string
	for i, r := range res.GetRoutes() {
		if r.GetSlow() {
			slow = append(slow, r.GetServerA()+"-"+r.GetServerB())
			if i >= 4 {
				t.Errorf("slow route at %d", i)
			}
		}
	}
	if !slices.Equal(slow, []string{"n0-n4", "n1-n4", "n2-n4", "n3-n4"}) {
		t.Errorf("slow = %v", slow)
	}
	// Under 2 ms nothing is slow, however far above the rest.
	for i := range f.Routes {
		if *f.Routes[i].RTT == 4*time.Millisecond {
			f.Routes[i].RTT = ms(0.9)
		} else {
			f.Routes[i].RTT = ms(0.1)
		}
	}
	for _, r := range api.FinishTopology(measuredTwice(f)).GetRoutes() {
		if r.GetSlow() {
			t.Errorf("%v slow under 1 ms", r)
		}
	}
}

// leaf is one leaf node connection of site on hub.
func leaf(hubID, hub, site string, lid int64) api.TopologyLeafRow {
	return api.TopologyLeafRow{ServerID: hubID, Server: hub, Cluster: clusterOf(hub), LID: lid, Name: site, Account: "APP",
		RTT: ms(20), Subscriptions: i64(40)}
}

// Leaf drops: a connection gone or changed between consecutive collections
// in which the old hub answered LEAFZ; a move to another hub counts, a
// gap, a failed LEAFZ and a hub's restart do not.
func TestTopologyLeafDrops(t *testing.T) {
	f := topoFacts()
	all := []string{"E0", "E1", "E0B"}
	step := func(i int) time.Duration { return 2*time.Hour - time.Duration(i)*30*time.Second }
	h := []api.TopologyCollection{
		collection(1, step(0), nil, []api.TopologyLeafRow{leaf("E0", "e0", "store-a", 1), leaf("E0", "e0", "store-b", 2)}, all...),
		// store-a reconnected (new ID): a drop. store-b moved to e1: a drop.
		collection(2, step(1), nil, []api.TopologyLeafRow{leaf("E0", "e0", "store-a", 3), leaf("E1", "e1", "store-b", 1)}, all...),
		// e0's LEAFZ failed: store-a's absence counts nothing.
		collection(3, step(2), nil, []api.TopologyLeafRow{leaf("E1", "e1", "store-b", 1)}, "E1"),
		collection(4, step(3), nil, []api.TopologyLeafRow{leaf("E0", "e0", "store-a", 4), leaf("E1", "e1", "store-b", 1)}, all...),
		// e0 restarted as E0B: no drop of store-a.
		collection(5, step(4), nil, []api.TopologyLeafRow{leaf("E0B", "e0", "store-a", 1), leaf("E1", "e1", "store-b", 1)}, "E0B", "E1"),
		// A gap of 20 minutes: store-a's new ID counts nothing.
		collection(6, step(44), nil, []api.TopologyLeafRow{leaf("E0B", "e0", "store-a", 2), leaf("E1", "e1", "store-b", 1)}, all...),
		// store-b goes away from e1, which answers: a drop; it stays away.
		collection(7, step(45), nil, []api.TopologyLeafRow{leaf("E0B", "e0", "store-a", 2)}, all...),
		// store-c appears in the newest collection only.
		collection(8, step(46), nil, []api.TopologyLeafRow{leaf("E0B", "e0", "store-a", 2), leaf("E0B", "e0", "store-c", 3)}, all...),
	}
	f.Epoch, f.EpochAt, f.Leafnodes = 8, h[7].At, h[7].Leafnodes
	history(f, h)
	res := api.FinishTopology(f)
	by := map[string]*v1.TopologyLeafnode{}
	var order []string
	for _, l := range res.GetLeafnodes() {
		by[l.GetName()] = l
		order = append(order, l.GetName())
	}
	if a := by["store-a"]; a == nil || !a.GetConnected() || a.GetDrops_24H() != 1 || a.GetHubServer() != "e0" {
		t.Errorf("store-a = %v, want connected on e0 with 1 drop", a)
	}
	b := by["store-b"]
	if b == nil || b.GetConnected() || b.GetDrops_24H() != 2 || b.GetHubServer() != "e1" || b.GetHubCluster() != "eu" ||
		!b.GetLastSeen().AsTime().Equal(h[5].At) || b.Rtt != nil || b.Subscriptions != nil {
		t.Errorf("store-b = %v, want not connected, 2 drops, last seen on e1 at %v", b, h[5].At)
	}
	if c := by["store-c"]; c == nil || c.Drops_24H != nil {
		t.Errorf("store-c = %v, want drops unset", c)
	}
	if !slices.Equal(order, []string{"store-b", "store-a", "store-c"}) {
		t.Errorf("order = %v", order)
	}
	eu := res.GetClusters()[0]
	if eu.GetName() != "eu" || eu.GetLeafnodesConnected() != 2 || eu.GetLeafnodesNotConnected() != 1 {
		t.Errorf("eu = %v", eu)
	}
	if us := res.GetClusters()[1]; us.LeafnodesConnected == nil || us.GetLeafnodesConnected() != 0 {
		t.Errorf("us = %v, want 0 leaf nodes", us)
	}
	// When a server (here e2) did not answer LEAFZ in the epoch, store-b may
	// be connected to it: listed with its drops, its state not known, and
	// not counted as not connected.
	f.Answered[api.EndpointLeafz] = []string{"e0", "e1", "u0", "u1", "u2"}
	f.Unanswered[api.EndpointLeafz] = []string{"e2"}
	res = api.FinishTopology(f)
	if l := res.GetLeafnodes()[0]; l.GetName() != "store-b" || !l.GetConnectionUnknown() || l.GetConnected() || l.GetDrops_24H() != 2 {
		t.Errorf("first = %v, want store-b of unknown state with 2 drops", l)
	}
	if eu := res.GetClusters()[0]; eu.GetLeafnodesNotConnected() != 0 {
		t.Errorf("eu counts %d not connected, want 0", eu.GetLeafnodesNotConnected())
	}
}

// At most MaxTopologyLeafnodes sites, worst first; the total counts all.
func TestTopologyLeafCap(t *testing.T) {
	f := topoFacts()
	for i := range 600 {
		r := leaf("E0", "e0", fmt.Sprintf("site-%03d", i), int64(i))
		r.RTT = ms(float64(i % 50))
		f.Leafnodes = append(f.Leafnodes, r)
	}
	res := api.FinishTopology(f)
	if res.GetLeafnodesTotal() != 600 || len(res.GetLeafnodes()) != api.MaxTopologyLeafnodes {
		t.Fatalf("total %d, listed %d", res.GetLeafnodesTotal(), len(res.GetLeafnodes()))
	}
	if l := res.GetLeafnodes()[0]; l.GetRtt().AsDuration() != 49*time.Millisecond || l.GetName() != "site-049" {
		t.Errorf("first = %v, want site-049 at 49ms", l)
	}
	if res.GetClusters()[0].GetLeafnodesConnected() != 600 {
		t.Errorf("eu counts %d sites, want 600", res.GetClusters()[0].GetLeafnodesConnected())
	}
}

// Clusters: servers by name, the shared region tag, the meta leader and
// the streams led; missing servers are named in the cluster, not in
// unobserved.
func TestTopologyClusters(t *testing.T) {
	f := topoFacts()
	f.MetaLeader = "u1"
	f.Servers[0].StreamsLed, f.Servers[1].StreamsLed = ptr32(2), ptr32(3)
	f.Servers[4].Tags = []string{"region:us-east"}
	f.Servers[2].Missing = true
	f.Unanswered[api.EndpointRoutez] = []string{"e2", "u0"}
	res := api.FinishTopology(f)
	eu, us := res.GetClusters()[0], res.GetClusters()[1]
	if eu.GetPlace() != "eu" || eu.GetMetaLeader() || eu.GetStreamsLed() != 5 || !eu.GetServers()[2].GetMissing() {
		t.Errorf("eu = %v", eu)
	}
	if us.GetPlace() != "" || !us.GetMetaLeader() || us.StreamsLed != nil || !us.GetServers()[1].GetMetaLeader() {
		t.Errorf("us = %v", us)
	}
	if u := res.GetUnobserved(); len(u) != 1 || !slices.Equal(u[0].GetServers(), []string{"u0"}) {
		t.Errorf("unobserved = %v, want u0 only", u)
	}
}

func ptr32(v int32) *int32 { return &v }

// opt formats an optional count: its value, or "unset".
func opt(p *int32) string {
	if p == nil {
		return "unset"
	}
	return fmt.Sprint(*p)
}
