package api

import (
	"cmp"
	"maps"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The slow route rule: a round trip is slow at slowRouteFloor or more
// when it is slowRouteFactor times its reference; a slow link needs at
// least slowLinkReferences pairs to compare with.
const (
	slowRouteFactor    = 3
	slowRouteFloor     = 2 * time.Millisecond
	slowLinkReferences = 2
)

// markSlowRoutes marks the slow routes of each cluster, by the round trip
// of the pairs that report one:
//
//  1. The core: start from the fastest pair (by name on a tie) and add,
//     until none is left, every server whose fastest pair to the core is
//     under slowRouteFactor times the median of the core's pairs, or under
//     slowRouteFloor.
//  2. Only a core of more than half the cluster's measured servers is a
//     reference: otherwise the cluster is split into groups of similar
//     size, none of which is the normal one, and nothing in it is slow.
//  3. A server outside the core is slow; all its routes are slow.
//  4. A pair inside the core is slow (a slow link) when it is at least
//     slowRouteFloor and slowRouteFactor times the median of the core's
//     pairs that touch neither of its servers, given at least
//     slowLinkReferences of them.
//
// Growing the core from the fastest pair keeps slow servers out of the
// reference, so one slow server stands out in a cluster of 3 or more and
// two in a cluster of 5 or more; two of 4 make an even split, not judged.
// The majority keeps the result from depending on which half of an even
// split holds the fastest pair, which jitter changes between refreshes,
// and never marks most of a cluster slow. Normal
// layouts stay quiet: jitter and zones under 2 ms join the core by the
// floor. With 2 servers, or all pairs alike, nothing is slow.
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
	slowAgainst := func(d, ref time.Duration) bool { return d >= slowRouteFloor && d >= slowRouteFactor*ref }
	fastest := slices.MinFunc(rs, func(a, b *v1.TopologyRoute) int {
		return cmp.Or(cmp.Compare(rtt(a), rtt(b)), cmp.Compare(a.GetServerA(), b.GetServerA()), cmp.Compare(a.GetServerB(), b.GetServerB()))
	})
	core := map[string]bool{fastest.GetServerA(): true, fastest.GetServerB(): true}
	inCore := func(r *v1.TopologyRoute) bool { return core[r.GetServerA()] && core[r.GetServerB()] }
	coreMedian := func() time.Duration {
		var ds []time.Duration
		for _, r := range rs {
			if inCore(r) {
				ds = append(ds, rtt(r))
			}
		}
		return medianDuration(ds)
	}
	for grown := true; grown; {
		grown = false
		ref := coreMedian()
		// toCore is each outside server's fastest pair to the core.
		toCore := map[string]time.Duration{}
		for _, r := range rs {
			for _, pair := range [][2]string{{r.GetServerA(), r.GetServerB()}, {r.GetServerB(), r.GetServerA()}} {
				if out, in := pair[0], pair[1]; !core[out] && core[in] {
					if d, ok := toCore[out]; !ok || rtt(r) < d {
						toCore[out] = rtt(r)
					}
				}
			}
		}
		for s, d := range toCore {
			if !slowAgainst(d, ref) {
				core[s], grown = true, true
			}
		}
	}
	// The measured servers: the core and those a pair measures against it.
	measured := maps.Clone(core)
	for _, r := range rs {
		if a, b := r.GetServerA(), r.GetServerB(); core[a] != core[b] {
			measured[a], measured[b] = true, true
		}
	}
	if 2*len(core) <= len(measured) {
		return
	}
	// A server outside the core is slow when a pair measures it against
	// the core; one with pairs only to other outside servers is not judged.
	slow := map[string]bool{}
	for _, r := range rs {
		if a, b := r.GetServerA(), r.GetServerB(); core[a] != core[b] {
			slow[a], slow[b] = !core[a], !core[b]
		}
	}
	for _, r := range rs {
		if !inCore(r) {
			r.Slow = slow[r.GetServerA()] || slow[r.GetServerB()]
			continue
		}
		var ds []time.Duration
		for _, o := range rs {
			if inCore(o) && o.GetServerA() != r.GetServerA() && o.GetServerA() != r.GetServerB() &&
				o.GetServerB() != r.GetServerA() && o.GetServerB() != r.GetServerB() {
				ds = append(ds, rtt(o))
			}
		}
		r.Slow = len(ds) >= slowLinkReferences && slowAgainst(rtt(r), medianDuration(ds))
	}
}
