package api

import (
	"maps"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The slow route rule: servers are near when their round trip is under
// slowRouteFloor or under slowRouteFactor times the cluster's typical
// nearest round trip; a slow link needs at least slowLinkReferences pairs
// to compare with.
const (
	slowRouteFactor    = 3
	slowRouteFloor     = 2 * time.Millisecond
	slowLinkReferences = 2
)

// markSlowTwice marks a route slow when markSlowRoutes finds it slow both
// in routes, the epoch's pairs, and in earlier, the pairs of the
// collection before; so one spike in one collection marks nothing. A pair
// the earlier collection did not report, or reported without a round
// trip, is not slow, nor is any pair when earlier is empty (no earlier
// collection). Each collection is judged by the full rule on its own
// pairs, so an earlier collection with only some servers' routes is
// judged on those.
func markSlowTwice(routes, earlier []*v1.TopologyRoute) {
	markSlowRoutes(routes)
	markSlowRoutes(earlier)
	slowBefore := map[[2]string]bool{}
	for _, r := range earlier {
		if r.GetSlow() {
			slowBefore[pairOf(r.GetServerA(), r.GetServerB())] = true
		}
	}
	for _, r := range routes {
		r.Slow = r.GetSlow() && slowBefore[pairOf(r.GetServerA(), r.GetServerB())]
	}
}

// markSlowRoutes marks the slow routes of each cluster, by the round trip
// of the pairs that report one:
//
//  1. The near threshold: the larger of slowRouteFloor and slowRouteFactor
//     times the median, over the measured servers (those with a reported
//     pair), of each server's fastest pair.
//  2. Groups: servers joined by pairs under the threshold, directly or
//     through others. The largest group is the reference only when it holds
//     more than half of the measured servers; otherwise the cluster is
//     split into groups of similar size, none of which is the normal one,
//     and nothing in it is slow.
//  3. A server outside the reference group with a reported pair to it is
//     slow; all its routes with a round trip are slow. One without such a pair is not judged.
//  4. A pair inside the reference group is slow (a slow link) when it is at
//     least slowRouteFloor and slowRouteFactor times the median of the
//     group's pairs that touch neither of its servers, given at least
//     slowLinkReferences of them.
//
// Every step reads all pairs and none depends on which pair is the
// fastest, so jitter that keeps each round trip on its side of the
// threshold gives the same result on every refresh. One slow server stands
// out in a cluster of 3 or more and two in a cluster of 5 or more (two of
// 4 raise the typical nearest round trip to theirs, and nothing is slow); a
// group of fewer than half far from the rest is slow; an even split,
// jitter, zones under 2 ms apart, 2 servers alone and pairs all alike are
// never slow.
func markSlowRoutes(routes []*v1.TopologyRoute) {
	byCluster := map[string][]*v1.TopologyRoute{}
	for _, r := range routes {
		r.Slow = false
		if r.Rtt != nil {
			byCluster[r.GetCluster()] = append(byCluster[r.GetCluster()], r)
		}
	}
	for _, rs := range byCluster {
		markSlowInCluster(rs)
	}
}

func markSlowInCluster(rs []*v1.TopologyRoute) {
	rtt := func(r *v1.TopologyRoute) time.Duration { return r.GetRtt().AsDuration() }
	fastest := map[string]time.Duration{}
	for _, r := range rs {
		for _, s := range []string{r.GetServerA(), r.GetServerB()} {
			if d, ok := fastest[s]; !ok || rtt(r) < d {
				fastest[s] = rtt(r)
			}
		}
	}
	threshold := max(slowRouteFloor, slowRouteFactor*medianDuration(slices.Collect(maps.Values(fastest))))
	// Groups of servers joined by near pairs (union-find); a group is named
	// by its smallest server name.
	parent := map[string]string{}
	var find func(string) string
	find = func(s string) string {
		if p := parent[s]; p != s {
			parent[s] = find(p)
		}
		return parent[s]
	}
	for s := range fastest {
		parent[s] = s
	}
	for _, r := range rs {
		if rtt(r) < threshold {
			a, b := find(r.GetServerA()), find(r.GetServerB())
			parent[max(a, b)] = min(a, b)
		}
	}
	size := map[string]int{}
	for s := range fastest {
		size[find(s)]++
	}
	group, largest := "", 0
	for _, root := range slices.Sorted(maps.Keys(size)) {
		if size[root] > largest {
			group, largest = root, size[root]
		}
	}
	if 2*largest <= len(fastest) {
		return
	}
	in := func(s string) bool { return find(s) == group }
	// A server outside the group is slow when a pair measures it against
	// the group; one with pairs only to other outside servers is not judged.
	slow := map[string]bool{}
	for _, r := range rs {
		if a, b := r.GetServerA(), r.GetServerB(); in(a) != in(b) {
			slow[a], slow[b] = !in(a), !in(b)
		}
	}
	for _, r := range rs {
		a, b := r.GetServerA(), r.GetServerB()
		if !in(a) || !in(b) {
			r.Slow = slow[a] || slow[b]
			continue
		}
		var ds []time.Duration
		for _, o := range rs {
			if in(o.GetServerA()) && in(o.GetServerB()) && o.GetServerA() != a && o.GetServerA() != b &&
				o.GetServerB() != a && o.GetServerB() != b {
				ds = append(ds, rtt(o))
			}
		}
		d := rtt(r)
		r.Slow = len(ds) >= slowLinkReferences && d >= slowRouteFloor && d >= slowRouteFactor*medianDuration(ds)
	}
}
