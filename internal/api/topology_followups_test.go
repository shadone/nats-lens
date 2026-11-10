package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
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

// finding is a firing finding of code about entity key.
func finding(code, key string, mod ...func(*v1.Finding)) *v1.Finding {
	x := &v1.Finding{Id: code + ":" + key, CheckCode: code, Entity: &v1.EntityRef{Key: key}, State: v1.FindingState_FINDING_STATE_FIRING,
		Severity: v1.Severity_SEVERITY_WARNING, Detail: code + " on " + key}
	for _, m := range mod {
		m(x)
	}
	return x
}

func muted(x *v1.Finding) { x.Mute = &v1.Mute{Reason: "known"} }

// A server's memory finding is server-008, else server-007 when the
// JetStream memory store is at least as full as the file store; a muted
// one is quiet, a resolved one and a missing server's are not shown.
func TestTopologyMemoryFinding(t *testing.T) {
	const mib = 1 << 20
	f := topoFacts()
	for i := range f.Servers {
		f.Servers[i].Mem = i64(200 * mib)
		f.Servers[i].JSMemory, f.Servers[i].JSMaxMemory = i64(80*mib), i64(100*mib)
		f.Servers[i].JSStorage, f.Servers[i].JSMaxStorage = i64(10*mib), i64(100*mib)
	}
	f.Servers[1].JSStorage = i64(90 * mib) // e1: the file store is fuller
	f.Servers[4].Missing = true            // u1
	f.Findings = []*v1.Finding{
		finding("server-007", "e0"), finding("server-007", "e1"), finding("server-008", "e2"), finding("server-007", "e2"),
		finding("server-008", "u0", muted), finding("server-008", "u1"),
		finding("server-008", "u2", func(x *v1.Finding) { x.State = v1.FindingState_FINDING_STATE_RESOLVED }),
	}
	got := map[string]string{}
	for _, c := range api.FinishTopology(f).GetClusters() {
		for _, s := range c.GetServers() {
			got[s.GetName()] = fmt.Sprintf("%s %v", s.GetMemoryFindingId(), s.GetMemoryFindingQuiet())
		}
	}
	want := map[string]string{"e0": "server-007:e0 false", "e1": " false", "e2": "server-008:e2 false", "u0": "server-008:u0 true", "u1": " false", "u2": " false"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("memory findings = %v, want %v", got, want)
	}
}

// A muted server-008 does not hide an unmuted server-007 on the memory
// store; with both muted the first is shown, quiet.
func TestTopologyMemoryFindingUnmutedFirst(t *testing.T) {
	f := topoFacts()
	f.Servers[0].Mem, f.Servers[0].JSMemory, f.Servers[0].JSMaxMemory = i64(1<<30), i64(900), i64(1000)
	f.Servers[0].JSStorage, f.Servers[0].JSMaxStorage = i64(1), i64(1000)
	f.Findings = []*v1.Finding{finding("server-008", "e0", muted), finding("server-007", "e0")}
	of := func() (string, bool) {
		s := api.FinishTopology(f).GetClusters()[0].GetServers()[0]
		return s.GetMemoryFindingId(), s.GetMemoryFindingQuiet()
	}
	if id, q := of(); id != "server-007:e0" || q {
		t.Errorf("muted 008, unmuted 007: %s quiet %v, want server-007:e0 not quiet", id, q)
	}
	muted(f.Findings[1])
	if id, q := of(); id != "server-008:e0" || !q {
		t.Errorf("both muted: %s quiet %v, want server-008:e0 quiet", id, q)
	}
	// An acceptance is told from a plain mute.
	f.Findings[0].Mute.Accepted = true
	if s := api.FinishTopology(f).GetClusters()[0].GetServers()[0]; !s.GetMemoryFindingAccepted() {
		t.Errorf("accepted server-008: %v", s)
	}
}

// Only copies between two clusters are listed; a mirror carries
// stream-017, else stream-015; a source stream-023, else stream-022 when
// its source_ids name it or name none; findings not muted sort first.
func TestTopologyReplication(t *testing.T) {
	f := topoFacts()
	copyOf := func(stream, kind, origin, id string) api.TopologyReplicationFact {
		return api.TopologyReplicationFact{Account: "APP", Stream: stream, Cluster: "ap", Kind: kind, Origin: origin, OriginCluster: "eu", Leader: "a0", SourceID: id}
	}
	local := copyOf("local", api.ReplicationMirror, "o", "")
	local.OriginCluster = "ap"
	f.Replication = []api.TopologyReplicationFact{
		copyOf("a-mirror", api.ReplicationMirror, "o", ""), copyOf("b-mirror", api.ReplicationMirror, "o", ""), copyOf("c-mirror", api.ReplicationMirror, "o", ""),
		copyOf("d-src", api.ReplicationSource, "x", "id-x"), copyOf("d-src", api.ReplicationSource, "y", "id-y"),
		copyOf("e-src", api.ReplicationSource, "z", "id-z"), local,
	}
	ids := func(v string) func(*v1.Finding) {
		return func(x *v1.Finding) { x.RelatedConfig = []*v1.ConfigValue{{Key: "source_ids", Value: v}} }
	}
	f.Findings = []*v1.Finding{
		finding("stream-015", "APP/b-mirror"), finding("stream-017", "APP/b-mirror"),
		finding("stream-015", "APP/c-mirror", muted),
		finding("stream-022", "APP/d-src", ids("id-y,id-w")),
		finding("stream-022", "APP/e-src"),
		finding("stream-015", "APP/local"),
	}
	var got []string
	for _, r := range api.FinishTopology(f).GetReplication() {
		got = append(got, fmt.Sprintf("%s<%s %s %v", r.GetStream(), r.GetOriginStream(), r.GetFindingCode(), r.GetFindingQuiet()))
	}
	want := []string{"b-mirror<o stream-017 false", "d-src<y stream-022 false", "e-src<z stream-022 false",
		"a-mirror<o  false", "c-mirror<o stream-015 true", "d-src<x  false"}
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
