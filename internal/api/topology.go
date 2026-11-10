package api

import (
	"cmp"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"github.com/shadone/nats-lens/internal/routehealth"
)

// Topology limits and rules.
const (
	// MaxTopologyLeafnodes is the most leaf node sites a response lists.
	MaxTopologyLeafnodes = 500
	// TopologyDropWindow is how far before the epoch drops are counted.
	TopologyDropWindow = 24 * time.Hour
	// TopologyRecentWindow is the window of a gateway's drops_1h.
	TopologyRecentWindow = time.Hour
	// TopologyGapIntervals is how many collect intervals may lie between
	// two collections that count as consecutive; a longer gap counts no
	// drop.
	TopologyGapIntervals = routehealth.GapIntervals
)

// The endpoints whose rows the topology reads.
const (
	EndpointRoutez   = "ROUTEZ"
	EndpointGatewayz = "GATEWAYZ"
	EndpointLeafz    = "LEAFZ"
)

// TopologyFacts are what a provider read for one GetTopology: the epoch's
// rows as stored and the drops counted over the window before it, without
// pairing or order. FinishTopology turns them into the response, so the
// store and the demo apply the same rules.
type TopologyFacts struct {
	// Epoch is the range's newest epoch; 0 when the range holds none.
	Epoch   int64
	EpochAt time.Time
	// Interval is the collect interval.
	Interval time.Duration
	// Servers are the servers expected in the epoch, those that answered
	// and those that did not.
	Servers []TopologyServerFact
	// MetaLeader is the JetStream meta leader most servers report; empty
	// without one.
	MetaLeader string
	// Answered holds, by endpoint (ROUTEZ, GATEWAYZ, LEAFZ), the names of
	// the servers whose reply in the epoch was ok; Unanswered those whose
	// reply was missing or an error.
	Answered, Unanswered map[string][]string
	// RoutezBefore names the servers whose ROUTEZ answered ok in the
	// collection before the epoch (the newest earlier one, at most
	// TopologyGapIntervals collect intervals earlier) and in the epoch,
	// under the same server ID; empty without such a collection.
	RoutezBefore []string
	Routes       []TopologyRouteRow
	Gateways     []TopologyGatewayRow
	Leafnodes    []TopologyLeafRow
	// Drops are counted over the window before the epoch, by the store in
	// SQL and by the demo with CountTopologyDrops.
	Drops TopologyDrops
	// Earlier is the collection the slow rule compares the epoch with;
	// nil when there is none.
	Earlier *TopologyEarlier
	// Replication are the mirrors and sources of the epoch's streams whose
	// origin is placed, in any cluster; FinishTopology keeps those
	// between clusters.
	Replication []TopologyReplicationFact
}

// TopologyEarlier is the collection before the epoch that the slow rule
// reads: the newest one before it with route rows, started no earlier
// than TopologyEarliestEarlier. A route is slow only when both this
// collection and the epoch measure it slow.
type TopologyEarlier = routehealth.Earlier

// TopologyEarliestEarlier is the earliest start of the collection the slow
// rule compares an epoch that started at epochAt with: TopologyGapIntervals
// collect intervals before it, as for drops. False when the interval is
// not known: then no collection counts as the one before, and no route is
// slow.
func TopologyEarliestEarlier(epochAt time.Time, interval time.Duration) (time.Time, bool) {
	return routehealth.EarliestEarlier(epochAt, interval)
}

// TopologyServerFact is one server expected in the epoch.
type TopologyServerFact struct {
	// Cluster is empty when not known.
	Name, Cluster string
	Tags          []string
	Missing       bool
	// StreamsLed is the streams the server leads; nil when not reported.
	StreamsLed *int32
	// Mem is the resident memory of the process, JSMemory and JSMaxMemory
	// its JetStream memory store and limit, bytes; nil when not reported.
	Mem, JSMemory, JSMaxMemory *int64
}

// TopologyRouteRow is one route as its reporting server lists it.
type TopologyRouteRow = routehealth.Row

// TopologyGatewayRow is one gateway connection as its reporting server
// lists it.
type TopologyGatewayRow struct {
	// ServerID and Server are the reporting server, Cluster its cluster,
	// Gateway the remote cluster.
	ServerID, Server, Cluster, Gateway string
	Outbound                           bool
	// CID is the connection ID; nil for a configured gateway that is not
	// connected.
	CID     *int64
	RTT     *time.Duration
	Pending *int64
}

// TopologyLeafRow is one leaf node connection as its hub server lists it.
type TopologyLeafRow struct {
	// ServerID and Server are the hub, Cluster its cluster.
	ServerID, Server, Cluster string
	LID                       int64
	// Name is the remote server's name; IP is its address, the site's
	// key when it reports no name.
	Name, Account, IP string
	RTT               *time.Duration
	Subscriptions     *int64
	Compression       string
}

// SiteName is the name a leaf node site is listed and keyed by: its
// server name, else its IP address.
func (l TopologyLeafRow) SiteName() string { return cmp.Or(l.Name, l.IP) }

type leafSite struct{ name, account string }

func (l TopologyLeafRow) site() leafSite { return leafSite{l.SiteName(), l.Account} }

// reported is a round trip as reported: nil for none and for 0, which a
// server sends before its first ping.
func reported(d *time.Duration) *time.Duration {
	if d == nil || *d <= 0 {
		return nil
	}
	return d
}

func durOf(d *time.Duration) *durationpb.Duration {
	if d = reported(d); d == nil {
		return nil
	}
	return durationpb.New(*d)
}

func ptrTo[T any](v T) *T { return &v }

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

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

// FinishTopology builds the response from the facts: clusters, gateway
// pairs, route pairs with the slow rule, leaf node sites with their drops,
// and order.
func FinishTopology(f *TopologyFacts) *v1.GetTopologyResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.GetTopologyResponse{}
	}
	res := &v1.GetTopologyResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt)}
	if f.Interval > 0 {
		res.CollectInterval = durationpb.New(f.Interval)
	}
	if !f.Drops.WindowStart.IsZero() {
		res.DropsWindowStart = timestamppb.New(f.Drops.WindowStart)
	}
	servers := placeServers(f)
	f = placeRows(f, servers)
	res.Gateways = topologyGateways(f, servers)
	res.Routes = topologyRoutes(f, servers)
	leafs := topologyLeafnodes(f, servers)
	res.LeafnodesTotal = int32(len(leafs))
	res.Leafnodes = leafs[:min(len(leafs), MaxTopologyLeafnodes)]
	res.Clusters = topologyClusters(f, servers, leafs)
	res.Unobserved = topologyUnobserved(f, servers)
	res.Replication = topologyReplication(f.Replication)
	return res
}

// placeServers is the facts' servers with each server of unknown cluster
// placed in the cluster of a server whose route names it, when one does.
func placeServers(f *TopologyFacts) []TopologyServerFact {
	out := slices.Clone(f.Servers)
	for i, s := range out {
		if s.Cluster != "" {
			continue
		}
		for _, r := range f.Routes {
			if r.Remote == s.Name && r.Cluster != "" {
				out[i].Cluster = r.Cluster
				break
			}
		}
	}
	return out
}

// topologyUnobserved names, per endpoint, the servers whose reply failed;
// a server that answered nothing is drawn missing instead.
func topologyUnobserved(f *TopologyFacts, servers []TopologyServerFact) []*v1.TopologyUnobserved {
	missing := map[string]bool{}
	for _, s := range servers {
		missing[s.Name] = s.Missing
	}
	var out []*v1.TopologyUnobserved
	for _, e := range []string{EndpointRoutez, EndpointGatewayz, EndpointLeafz} {
		var names []string
		for _, n := range f.Unanswered[e] {
			if !missing[n] && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			slices.Sort(names)
			out = append(out, &v1.TopologyUnobserved{Endpoint: e, Servers: names})
		}
	}
	return out
}

// placeRows is f with the rows of each server placed by placeServers in
// that server's cluster: its routes, gateways, leaf nodes, gateway drops
// and sightings, so that they count where the server is drawn.
func placeRows(f *TopologyFacts, servers []TopologyServerFact) *TopologyFacts {
	placed := map[string]string{}
	for i, s := range servers {
		if f.Servers[i].Cluster == "" && s.Cluster != "" {
			placed[s.Name] = s.Cluster
		}
	}
	if len(placed) == 0 {
		return f
	}
	cluster := func(server, cluster string) string {
		if cl, ok := placed[server]; ok && cluster == "" {
			return cl
		}
		return cluster
	}
	g := *f
	g.Routes = slices.Clone(f.Routes)
	for i := range g.Routes {
		g.Routes[i].Cluster = cluster(g.Routes[i].Server, g.Routes[i].Cluster)
	}
	g.Gateways = slices.Clone(f.Gateways)
	for i := range g.Gateways {
		g.Gateways[i].Cluster = cluster(g.Gateways[i].Server, g.Gateways[i].Cluster)
	}
	g.Leafnodes = slices.Clone(f.Leafnodes)
	for i := range g.Leafnodes {
		g.Leafnodes[i].Cluster = cluster(g.Leafnodes[i].Server, g.Leafnodes[i].Cluster)
	}
	g.Drops.Gateways = slices.Clone(f.Drops.Gateways)
	for i := range g.Drops.Gateways {
		g.Drops.Gateways[i].Cluster = cluster(g.Drops.Gateways[i].Server, g.Drops.Gateways[i].Cluster)
	}
	g.Drops.LastSeen = slices.Clone(f.Drops.LastSeen)
	for i := range g.Drops.LastSeen {
		r := &g.Drops.LastSeen[i].Row
		r.Cluster = cluster(r.Server, r.Cluster)
	}
	return &g
}
