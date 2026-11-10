package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// rconn is server's route connection rid to remote; IDs are the names
// with "#" and the process generation.
func rconn(server, remote string, rid int64) api.TopologyRouteConn {
	return api.TopologyRouteConn{ServerID: server + "#1", Server: server, RID: rid, RemoteID: remote + "#1", Remote: remote}
}

// routeHistory is collections k = 0..len(conns)-1 an interval apart,
// ending at topoAt, each with the given route connections and every
// server ID of ids answering ROUTEZ.
func routeHistory(ids map[string]bool, conns ...[]api.TopologyRouteConn) []api.TopologyCollection {
	var h []api.TopologyCollection
	for k, c := range conns {
		at := topoAt.Add(-time.Duration(len(conns)-1-k) * 30 * time.Second)
		h = append(h, api.TopologyCollection{Epoch: int64(100 - len(conns) + 1 + k), At: at, Routez: ids, Routes: c})
	}
	return h
}

func routeDrops(h []api.TopologyCollection) []string {
	last := h[len(h)-1]
	var out []string
	for _, r := range api.CountTopologyDrops(h, 30*time.Second, last.Epoch, last.At).Routes {
		out = append(out, fmt.Sprintf("%s-%s %d/%d", r.A, r.B, r.Recent, r.Day))
	}
	slices.Sort(out)
	return out
}

// A pool of three routes: one replaced connection is one drop of the
// pair, whether one end or both list it; all three going at once is one
// drop too. A pair that only stays counts 0, observed.
func TestTopologyRouteDropsPool(t *testing.T) {
	ids := answering("e0#1", "e1#1", "e2#1")
	pool := func(gen int64) []api.TopologyRouteConn {
		return []api.TopologyRouteConn{rconn("e0", "e1", 1+10*gen), rconn("e0", "e1", 2), rconn("e0", "e1", 3),
			rconn("e1", "e0", 7+10*gen), rconn("e1", "e0", 8), rconn("e1", "e0", 9),
			rconn("e0", "e2", 4), rconn("e2", "e0", 4)}
	}
	all := func(gen int64) []api.TopologyRouteConn {
		out := pool(0)[6:]
		for _, r := range pool(0)[:6] {
			r.RID += 100 * gen
			out = append(out, r)
		}
		return out
	}
	if got, want := routeDrops(routeHistory(ids, pool(0), pool(1), pool(1))), []string{"e0-e1 1/1", "e0-e2 0/0"}; !slices.Equal(got, want) {
		t.Errorf("one connection replaced: %v, want %v", got, want)
	}
	if got, want := routeDrops(routeHistory(ids, all(0), all(1), all(2))), []string{"e0-e1 2/2", "e0-e2 0/0"}; !slices.Equal(got, want) {
		t.Errorf("the whole pool replaced twice: %v, want %v", got, want)
	}
	// Only e0 lists the replaced connection: still one drop.
	b := pool(0)
	b[0].RID = 50
	if got, want := routeDrops(routeHistory(ids, pool(0), b)), []string{"e0-e1 1/1", "e0-e2 0/0"}; !slices.Equal(got, want) {
		t.Errorf("one end's connection replaced: %v, want %v", got, want)
	}
}

// A restart of either end, a failed ROUTEZ of either end and a gap count
// no drop; a pair gone while both ends answer is a drop.
func TestTopologyRouteDropsExclusions(t *testing.T) {
	ids := answering("e0#1", "e1#1")
	a := []api.TopologyRouteConn{rconn("e0", "e1", 1), rconn("e1", "e0", 1)}
	moved := []api.TopologyRouteConn{rconn("e0", "e1", 2), rconn("e1", "e0", 2)}

	// e1 restarted: its new process has another ID.
	restarted := []api.TopologyRouteConn{rconn("e0", "e1", 2), rconn("e1", "e0", 2)}
	restarted[0].RemoteID, restarted[1].ServerID = "e1#2", "e1#2"
	h := routeHistory(ids, a, restarted)
	h[1].Routez = answering("e0#1", "e1#2")
	if got := routeDrops(h); len(got) != 0 {
		t.Errorf("remote restart: %v, want no observation", got)
	}

	// e1's ROUTEZ failed in the second collection.
	h = routeHistory(ids, a, moved)
	h[1].Routez = answering("e0#1")
	if got := routeDrops(h); len(got) != 0 {
		t.Errorf("failed ROUTEZ: %v, want no observation", got)
	}

	// Two collections 4 intervals apart.
	h = routeHistory(ids, a, moved)
	h[0].At = h[1].At.Add(-4 * 30 * time.Second)
	if got := routeDrops(h); len(got) != 0 {
		t.Errorf("gap: %v, want no observation", got)
	}

	// Gone, both answering.
	if got, want := routeDrops(routeHistory(ids, a, nil)), []string{"e0-e1 1/1"}; !slices.Equal(got, want) {
		t.Errorf("gone: %v, want %v", got, want)
	}

	// A route without a remote ID is not observed.
	noID := []api.TopologyRouteConn{rconn("e0", "e1", 1)}
	noID[0].RemoteID = ""
	if got := routeDrops(routeHistory(ids, noID, nil)); len(got) != 0 {
		t.Errorf("no remote ID: %v, want no observation", got)
	}
}

// The response: a listed pair carries its drops; an unobserved pair has
// them unset; two servers of a cluster that answered ROUTEZ in this and
// the collection before and do not list each other are down, sorted
// first, with their drops; with an end missing or not answering before,
// they are left out.
func TestTopologyRouteDropsResponse(t *testing.T) {
	f := topoFacts()
	f.Routes = []api.TopologyRouteRow{route("e0", "e1", 0.4, ""), route("e1", "e0", 0.4, ""), route("e0", "e2", 0.4, ""), route("e2", "e0", 0.4, "")}
	f.Drops.Routes = []api.TopologyRouteDrops{
		{A: "e0", B: "e1", Recent: 2, Day: 3, ObservedRecent: true, ObservedDay: true},
		{A: "e1", B: "e2", Recent: 0, Day: 1, ObservedRecent: true, ObservedDay: true},
		{A: "u0", B: "u1", Recent: 1, Day: 1, ObservedRecent: true, ObservedDay: true},
	}
	f.Servers[4].Missing = true // u1
	f.RoutezBefore = []string{"e0", "e1", "e2", "u0", "u1"}
	var got []string
	for _, r := range api.FinishTopology(f).GetRoutes() {
		got = append(got, fmt.Sprintf("%s-%s %s %s %v", r.GetServerA(), r.GetServerB(), opt(r.Drops_1H), opt(r.Drops_24H), r.GetDown()))
	}
	want := []string{"e1-e2 0 1 true", "e0-e1 2 3 false", "e0-e2 unset unset false"}
	if !slices.Equal(got, want) {
		t.Errorf("routes = %v, want %v", got, want)
	}
}

// Memory is far above the others at 512 MiB and more than 1.5 times the
// median of the other servers that report it; a missing server's memory
// is not sent and does not count.
func TestTopologyMemHigh(t *testing.T) {
	const mib = 1 << 20
	f := topoFacts()
	for i, m := range []int64{200, 210, 900, 220, 2000, 230} {
		f.Servers[i].Mem = i64(m * mib)
	}
	f.Servers[4].Missing = true
	f.Servers[5].Mem = nil
	high := map[string]bool{}
	mem := map[string]bool{}
	for _, c := range api.FinishTopology(f).GetClusters() {
		for _, s := range c.GetServers() {
			high[s.GetName()], mem[s.GetName()] = s.GetMemHigh(), s.MemBytes != nil
		}
	}
	if !high["e2"] || high["e0"] || high["u1"] || mem["u1"] || mem["u2"] {
		t.Errorf("high %v, reported %v", high, mem)
	}
	// 400 MiB is twice the others' median, but under 512 MiB.
	f.Servers[2].Mem = i64(400 * mib)
	for _, c := range api.FinishTopology(f).GetClusters() {
		for _, s := range c.GetServers() {
			if s.GetMemHigh() {
				t.Errorf("%s high at 400 MiB", s.GetName())
			}
		}
	}
}

// Only copies between two clusters are listed; a copy is not active after
// 2 minutes without hearing from its origin, or never heard twice, and
// falls behind at 1000 messages and more than before; warnings first.
func TestTopologyReplication(t *testing.T) {
	d := func(v time.Duration) *time.Duration { return &v }
	f := topoFacts()
	base := api.TopologyReplicationFact{Account: "APP", Kind: api.ReplicationSource, Cluster: "ap", OriginCluster: "eu", Leader: "a0"}
	with := func(stream string, mod func(*api.TopologyReplicationFact)) api.TopologyReplicationFact {
		x := base
		x.Stream, x.Origin = stream, stream+"-origin"
		mod(&x)
		return x
	}
	f.Replication = []api.TopologyReplicationFact{
		with("a-ok", func(x *api.TopologyReplicationFact) { x.Lag, x.LagBefore, x.Active = i64(5), i64(900), d(time.Second) }),
		with("b-same-cluster", func(x *api.TopologyReplicationFact) { x.OriginCluster = "ap" }),
		with("c-unknown", func(x *api.TopologyReplicationFact) { x.OriginCluster = "" }),
		with("d-quiet", func(x *api.TopologyReplicationFact) { x.Active = d(3 * time.Minute) }),
		with("e-never-once", func(x *api.TopologyReplicationFact) { x.Active, x.ActiveBefore = d(-1), d(time.Second) }),
		with("f-never-twice", func(x *api.TopologyReplicationFact) { x.Active, x.ActiveBefore = d(-1), d(-1) }),
		with("g-behind", func(x *api.TopologyReplicationFact) { x.Lag, x.LagBefore = i64(1500), i64(1200) }),
		with("h-small", func(x *api.TopologyReplicationFact) { x.Lag, x.LagBefore = i64(999), i64(10) }),
		with("i-error", func(x *api.TopologyReplicationFact) { x.Error = "stream not found" }),
	}
	var got []string
	for _, r := range api.FinishTopology(f).GetReplication() {
		got = append(got, fmt.Sprintf("%s %v %v %v %v", r.GetStream(), r.GetNotActive(), r.GetFallingBehind(), r.GetNeverHeard(), r.GetLastActive() != nil))
	}
	want := []string{
		"d-quiet true false false true", "f-never-twice true false true false", "g-behind false true false false", "i-error false false false false",
		"a-ok false false false true", "e-never-once false false true false", "h-small false false false false",
	}
	if !slices.Equal(got, want) {
		t.Errorf("replication =\n%v\nwant\n%v", got, want)
	}
}

// A route of a server whose process started within the restart window
// before the earlier collection counts no drop and no observation; after
// the window it counts.
func TestTopologyRouteDropsRestartWindow(t *testing.T) {
	ids := answering("e0#1", "e1#1")
	h := routeHistory(ids, []api.TopologyRouteConn{rconn("e0", "e1", 1), rconn("e1", "e0", 1)}, []api.TopologyRouteConn{rconn("e0", "e1", 2), rconn("e1", "e0", 2)})
	last := h[len(h)-1]
	count := func(startedAgo time.Duration) []string {
		rules := api.TopologyDropRules{RestartWindow: 10 * time.Minute, Started: map[string]time.Time{"e1#1": h[0].At.Add(-startedAgo)}}
		var out []string
		for _, r := range api.CountTopologyDropsWith(h, 30*time.Second, last.Epoch, last.At, rules).Routes {
			out = append(out, fmt.Sprintf("%s-%s %d/%d", r.A, r.B, r.Recent, r.Day))
		}
		return out
	}
	if got := count(5 * time.Minute); len(got) != 0 {
		t.Errorf("e1 started 5 minutes before: %v, want no observation", got)
	}
	if got, want := count(15*time.Minute), []string{"e0-e1 1/1"}; !slices.Equal(got, want) {
		t.Errorf("e1 started 15 minutes before: %v, want %v", got, want)
	}
}
