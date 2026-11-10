package api_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/shadone/nats-lens/internal/api"
)

// downAfter runs the drop count over h and FinishTopology on topoFacts
// with no routes listed in the epoch, and reports whether e0-e1 is in the
// response and down.
func downAfter(t *testing.T, h []api.TopologyCollection) (listed, down bool) {
	t.Helper()
	last := h[len(h)-1]
	f := topoFacts()
	f.Epoch, f.EpochAt = last.Epoch, last.At
	f.Drops = api.CountTopologyDrops(h, f.Interval, last.Epoch, last.At)
	// Every server answered ROUTEZ in the collection before the epoch too.
	f.RoutezBefore = []string{"e0", "e1", "e2", "u0", "u1", "u2"}
	for _, r := range api.FinishTopology(f).GetRoutes() {
		if r.GetServerA() == "e0" && r.GetServerB() == "e1" {
			return true, r.GetDown()
		}
	}
	return false, false
}

// e1 restarts and never gets a route to e0 back, while both answer
// ROUTEZ for many collections: the pair is partitioned.
func TestReviewRestartNeverRejoinsIsDown(t *testing.T) {
	a := []api.TopologyRouteConn{rconn("e0", "e1", 1), rconn("e1", "e0", 1)}
	h := routeHistory(answering("e0#1", "e1#1"), a, nil, nil, nil)
	for i := 1; i < len(h); i++ {
		h[i].Routez = answering("e0#1", "e1#2")
	}
	if listed, down := downAfter(t, h); !listed || !down {
		t.Errorf("restarted e1 never rejoined e0: listed %v down %v, want a down pair", listed, down)
	}
}

// The routes go in the collection in which e1's ROUTEZ failed, and stay
// gone while both answer afterwards.
func TestReviewDropDuringFailedRoutezIsDown(t *testing.T) {
	a := []api.TopologyRouteConn{rconn("e0", "e1", 1), rconn("e1", "e0", 1)}
	h := routeHistory(answering("e0#1", "e1#1"), a, nil, nil, nil)
	h[1].Routez = answering("e0#1")
	if listed, down := downAfter(t, h); !listed || !down {
		t.Errorf("drop hidden by a failed ROUTEZ: listed %v down %v, want a down pair", listed, down)
	}
}

// A pair stays down for more than a day: no drop and no route row of it
// is left in the window, and as both servers still route to e2, the pair
// is still down.
func TestTopologyDownWithoutDropsInWindow(t *testing.T) {
	f := topoFacts()
	f.RoutezBefore = []string{"e0", "e1", "e2"}
	f.Routes = []api.TopologyRouteRow{route("e0", "e2", 0.4, ""), route("e2", "e0", 0.4, ""), route("e1", "e2", 0.4, ""), route("e2", "e1", 0.4, "")}
	f.RoutesBefore = slices.Clone(f.Routes)
	if got, want := downOf(f), []string{"e0-e1"}; !slices.Equal(got, want) {
		t.Errorf("pair down for over a day: down %v, want %v", got, want)
	}
}

// A server that did not answer ROUTEZ in the collection before the epoch
// (just started, or its ROUTEZ failed then) makes no pair down; a pair
// one end lists is one-sided, not down.
func TestTopologyDownNeedsTwoCollections(t *testing.T) {
	f := topoFacts()
	f.Routes = []api.TopologyRouteRow{route("e0", "e1", 0.4, "")}
	f.RoutezBefore = []string{"e0", "e1"}
	var got []string
	for _, r := range api.FinishTopology(f).GetRoutes() {
		got = append(got, fmt.Sprintf("%s-%s down %v one-sided %v", r.GetServerA(), r.GetServerB(), r.GetDown(), r.GetOneSided()))
	}
	if want := []string{"e0-e1 down false one-sided true"}; !slices.Equal(got, want) {
		t.Errorf("routes = %v, want %v", got, want)
	}
}

// edgeFacts are two sites whose 2-server clusters are both named edge,
// each meshed inside, never routed to each other, every server answering
// ROUTEZ in this and the collection before.
func edgeFacts() *api.TopologyFacts {
	f := topoFacts()
	for _, n := range []string{"l0", "l1", "m0", "m1"} {
		f.Servers = append(f.Servers, api.TopologyServerFact{Name: n, Cluster: "edge"})
		f.Answered[api.EndpointRoutez] = append(f.Answered[api.EndpointRoutez], n)
		f.RoutezBefore = append(f.RoutezBefore, n)
	}
	mk := func(a, b string) api.TopologyRouteRow {
		return api.TopologyRouteRow{Server: a, Remote: b, Cluster: "edge", RTT: ms(0.4), Pending: i64(1)}
	}
	f.Routes = []api.TopologyRouteRow{mk("l0", "l1"), mk("l1", "l0"), mk("m0", "m1"), mk("m1", "m0")}
	f.RoutesBefore = slices.Clone(f.Routes)
	return f
}

func downOf(f *api.TopologyFacts) []string {
	var out []string
	for _, r := range api.FinishTopology(f).GetRoutes() {
		if r.GetDown() {
			out = append(out, r.GetServerA()+"-"+r.GetServerB())
		}
	}
	slices.Sort(out)
	return out
}

// Two sites sharing a cluster name are two meshes, not one with every
// cross pair down; one site's server that lists no route at all is down
// to every server of the name; a site in another JetStream domain is
// never paired.
func TestTopologyDownByMesh(t *testing.T) {
	if got := downOf(edgeFacts()); len(got) != 0 {
		t.Errorf("two sites named edge: down %v, want none", got)
	}
	f := edgeFacts()
	f.Routes, f.RoutesBefore = f.Routes[2:], f.RoutesBefore[2:] // l0 and l1 list nothing
	if got := downOf(f); len(got) != 0 {
		t.Errorf("site l without routes, none in the window: down %v, want none", got)
	}
	// l0 and l1 were meshed earlier in the window: they are down to each
	// other, not to site m, with which they never routed.
	f.Drops.RoutePairs = [][2]string{{"l0", "l1"}, {"m0", "m1"}}
	if got, want := downOf(f), []string{"l0-l1"}; !slices.Equal(got, want) {
		t.Errorf("site l lost its routes: down %v, want %v", got, want)
	}
	for i := range f.Servers {
		if f.Servers[i].Name[0] == 'm' {
			f.Servers[i].Domain = "shop-2"
		}
	}
	if got, want := downOf(f), []string{"l0-l1"}; !slices.Equal(got, want) {
		t.Errorf("site m in its own domain: down %v, want %v", got, want)
	}
}

// Single-server sites built from one template share a cluster name and
// never route to each other: none is down. One that routed to another in
// the window and lost it is.
func TestTopologyDownSingleServerSites(t *testing.T) {
	f := topoFacts()
	for _, n := range []string{"s1", "s2", "s3"} {
		f.Servers = append(f.Servers, api.TopologyServerFact{Name: n, Cluster: "edge"})
		f.Answered[api.EndpointRoutez] = append(f.Answered[api.EndpointRoutez], n)
		f.RoutezBefore = append(f.RoutezBefore, n)
	}
	if got := downOf(f); len(got) != 0 {
		t.Errorf("single-server sites: down %v, want none", got)
	}
	f.Drops.RoutePairs = [][2]string{{"s1", "s2"}}
	if got, want := downOf(f), []string{"s1-s2"}; !slices.Equal(got, want) {
		t.Errorf("s1 and s2 routed in the window: down %v, want %v", got, want)
	}
}

// A pair one end listed in the collection before is not down yet: a
// route reconnecting while the two ROUTEZ replies were taken does not
// read as down for one collection.
func TestTopologyDownNeedsTwoCollectionsUnlisted(t *testing.T) {
	f := edgeFacts()
	f.Routes = f.Routes[2:] // l0-l1 gone now, listed before
	if got := downOf(f); len(got) != 0 {
		t.Errorf("listed in the collection before: down %v, want none", got)
	}
	f.RoutesBefore = f.RoutesBefore[2:]
	f.RoutesBefore = append(f.RoutesBefore, api.TopologyRouteRow{Server: "l0", Remote: "m0", Cluster: "edge"}) // joins the sites
	f.Drops.RoutePairs = [][2]string{{"l0", "l1"}}
	if got, want := downOf(f), []string{"l0-l1", "l0-m1"}; !slices.Equal(got, want) {
		t.Errorf("gone in both: down %v, want %v", got, want)
	}
}

// meshCase is cluster c of the named servers, all answering ROUTEZ now and
// in the collection before, with the same routes (both directions) in
// both collections and the given pairs routed in the 24-hour window.
func meshCase(names []string, edges, window [][2]string) *api.TopologyFacts {
	f := &api.TopologyFacts{Epoch: 100, EpochAt: topoAt, Interval: 30 * time.Second, Answered: map[string][]string{}, Unanswered: map[string][]string{}}
	for _, n := range names {
		f.Servers = append(f.Servers, api.TopologyServerFact{Name: n, Cluster: "c"})
		f.Answered[api.EndpointRoutez] = append(f.Answered[api.EndpointRoutez], n)
		f.RoutezBefore = append(f.RoutezBefore, n)
	}
	for _, e := range edges {
		f.Routes = append(f.Routes, api.TopologyRouteRow{Server: e[0], Remote: e[1], Cluster: "c", RTT: ms(0.4)},
			api.TopologyRouteRow{Server: e[1], Remote: e[0], Cluster: "c", RTT: ms(0.4)})
	}
	f.RoutesBefore = slices.Clone(f.Routes)
	f.Drops.RoutePairs = window
	return f
}

// fullMesh is every pair of names, in name order.
func fullMesh(names ...string) [][2]string {
	var out [][2]string
	for i, a := range names {
		for _, b := range names[i+1:] {
			out = append(out, [2]string{a, b})
		}
	}
	return out
}

// A pair is down when it had a route in the window, or when both servers
// route and are joined through others; a mesh that split into meshed
// halves shows every cross pair down while the window holds them.
func TestTopologyDownCases(t *testing.T) {
	without := func(all [][2]string, drop ...string) [][2]string {
		var out [][2]string
		for _, e := range all {
			if !slices.Contains(drop, e[0]) && !slices.Contains(drop, e[1]) {
				out = append(out, e)
			}
		}
		return out
	}
	n3, n5 := []string{"n0", "n1", "n2"}, []string{"n0", "n1", "n2", "n3", "n4"}
	split := []string{"a1", "a2", "b1", "b2"}
	split32 := []string{"a1", "a2", "a3", "b1", "b2"}
	for _, c := range []struct {
		name string
		f    *api.TopologyFacts
		want []string
	}{
		{"3-node mesh", meshCase(n3, fullMesh(n3...), fullMesh(n3...)), nil},
		{"5-node mesh", meshCase(n5, fullMesh(n5...), fullMesh(n5...)), nil},
		{"n2 restarted and never rejoined", meshCase(n3, without(fullMesh(n3...), "n2"), fullMesh(n3...)), []string{"n0-n2", "n1-n2"}},
		{"2+2 split", meshCase(split, [][2]string{{"a1", "a2"}, {"b1", "b2"}}, fullMesh(split...)),
			[]string{"a1-b1", "a1-b2", "a2-b1", "a2-b2"}},
		{"3+2 split", meshCase(split32, append(fullMesh("a1", "a2", "a3"), [2]string{"b1", "b2"}), fullMesh(split32...)),
			[]string{"a1-b1", "a1-b2", "a2-b1", "a2-b2", "a3-b1", "a3-b2"}},
		{"templated sites", meshCase(split, [][2]string{{"a1", "a2"}, {"b1", "b2"}}, [][2]string{{"a1", "a2"}, {"b1", "b2"}}), nil},
	} {
		if got := downOf(c.f); !slices.Equal(got, c.want) {
			t.Errorf("%s: down %v, want %v", c.name, got, c.want)
		}
	}
}
