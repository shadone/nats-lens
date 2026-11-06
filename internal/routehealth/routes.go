// Package routehealth pairs route observations and classifies sustained slow pairs.
// It is shared by Topology, the demo and server-025.
package routehealth

import (
	"cmp"
	"context"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Row is one reported route, before pooling or pairing.
type Row struct {
	// Server and Remote are names; Cluster is the reporting server's.
	Server, Remote, Cluster string
	RTT                     *time.Duration
	Pending                 *int64
	// Account is empty for a pooled route and names a pinned account otherwise.
	Account string
}

// Earlier holds the selected previous route-bearing collection.
type Earlier struct {
	Epoch  int64
	At     time.Time
	Routes []Row
}

// GapIntervals is the maximum distance between eligible collections.
const GapIntervals = 3

// EarliestEarlier returns no bound when the configured interval is unknown.
func EarliestEarlier(at time.Time, interval time.Duration) (time.Time, bool) {
	if interval <= 0 {
		return time.Time{}, false
	}
	return at.Add(-GapIntervals * interval), true
}

// Routes reads no external state and mutates no input. Each collection is
// classified independently before intersecting its slow name pairs.
func Routes(rows []Row, earlier *Earlier, at time.Time, interval time.Duration, clusterOf map[string]string, answered map[string]bool) []*v1.TopologyRoute {
	out, _ := RoutesContext(context.Background(), rows, earlier, at, interval, clusterOf, answered)
	return out
}

// RoutesContext applies the identical rule with cancellation between pair
// reference scans, so a large cluster cannot exhaust the check budget.
func RoutesContext(ctx context.Context, rows []Row, earlier *Earlier, at time.Time, interval time.Duration, clusterOf map[string]string, answered map[string]bool) ([]*v1.TopologyRoute, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Resolve unknown reporting clusters from incoming named routes, exactly
	// as Topology places its servers. Work on a copy of the supplied map.
	clusters := map[string]string{}
	for name, cluster := range clusterOf {
		clusters[name] = cluster
	}
	for _, r := range rows {
		if r.Cluster != "" {
			clusters[r.Server] = r.Cluster
		}
	}
	for _, r := range rows {
		if clusters[r.Remote] == "" && r.Cluster != "" {
			clusters[r.Remote] = r.Cluster
		}
	}
	out := pairRoutes(rows, clusters, answered)
	var before []*v1.TopologyRoute
	bound, known := EarliestEarlier(at, interval)
	if earlier != nil && known && earlier.At.Before(at) && !earlier.At.Before(bound) {
		before = pairRoutes(earlier.Routes, clusters, nil)
	}
	if err := markSlowTwice(ctx, out, before); err != nil {
		return nil, err
	}
	return out, ctx.Err()
}

func ptrTo[T any](v T) *T { return &v }
func reported(d *time.Duration) *time.Duration {
	if d == nil || *d <= 0 {
		return nil
	}
	return d
}

// routeEnd is one end's report of a pair.
type routeEnd struct {
	pool   int32
	pinned []string
}

// pairRoutes pairs one collection's routes by server; answered names the
// servers whose ROUTEZ answered, for one-sided pairs.
func pairRoutes(rows []Row, clusterOf map[string]string, answered map[string]bool) []*v1.TopologyRoute {
	byPair := map[[2]string]*v1.TopologyRoute{}
	ends := map[[2]string]map[string]*routeEnd{}
	for _, r := range rows {
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
	return out
}

// pairOf is two names in order.
func pairOf(a, b string) [2]string {
	if b < a {
		a, b = b, a
	}
	return [2]string{a, b}
}

// medianDuration is the median of ds, the mean of the middle two of an
// even count; ds is sorted in place.
func medianDuration(ds []time.Duration) time.Duration {
	slices.Sort(ds)
	n := len(ds)
	if n%2 == 1 {
		return ds[n/2]
	}
	return (ds[n/2-1] + ds[n/2]) / 2
}
