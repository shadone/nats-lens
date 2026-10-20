package api

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
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
	TopologyGapIntervals = 3
	// slowRouteFactor and slowRouteFloor make a route slow: its round
	// trip at least slowRouteFactor times the cluster's median and at
	// least slowRouteFloor.
	slowRouteFactor = 3
	slowRouteFloor  = time.Millisecond
)

// The endpoints whose rows the topology reads.
const (
	EndpointRoutez   = "ROUTEZ"
	EndpointGatewayz = "GATEWAYZ"
	EndpointLeafz    = "LEAFZ"
)

// TopologyFacts are what a provider read for one GetTopology: rows as
// stored, without pairing, counting or order. FinishTopology turns them
// into the response, so the store and the demo apply the same rules.
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
	Routes               []TopologyRouteRow
	Gateways             []TopologyGatewayRow
	Leafnodes            []TopologyLeafRow
	// History are the collections from TopologyDropWindow before the
	// epoch up to the epoch itself, oldest first; drops are counted
	// between consecutive ones.
	History []TopologyCollection
}

// TopologyServerFact is one server expected in the epoch.
type TopologyServerFact struct {
	Name, Cluster string
	Tags          []string
	Missing       bool
	// StreamsLed is the streams the server leads; nil when not reported.
	StreamsLed *int32
}

// TopologyRouteRow is one route as its reporting server lists it.
type TopologyRouteRow struct {
	// Server and Remote are the reporting server and the other end, by
	// name; Cluster is the reporting server's.
	Server, Remote, Cluster string
	RTT                     *time.Duration
	Pending                 *int64
	// Account is the account of a pinned route; empty for a pooled one.
	Account string
}

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

// site is the key of a leaf node site.
func (l TopologyLeafRow) site() leafSite {
	return leafSite{name: cmp.Or(l.Name, l.IP), account: l.Account}
}

// TopologyCollection is one collection of the drop window: which servers
// answered, by server ID, and their gateway and leaf node connections.
type TopologyCollection struct {
	Epoch int64
	At    time.Time
	// Gatewayz and Leafz are the server IDs whose GATEWAYZ and LEAFZ
	// answered ok.
	Gatewayz, Leafz map[string]bool
	// Gateways are the outbound gateway rows; inbound and unconnected
	// rows may be left out, as they are not read.
	Gateways  []TopologyGatewayRow
	Leafnodes []TopologyLeafRow
}

type leafSite struct{ name, account string }

// consecutive reports whether b follows a closely enough that a change
// between them is a drop.
func (f *TopologyFacts) consecutive(a, b TopologyCollection) bool {
	return f.Interval <= 0 || b.At.Sub(a.At) <= TopologyGapIntervals*f.Interval
}

func durOf(d *time.Duration) *durationpb.Duration {
	if d == nil {
		return nil
	}
	return durationpb.New(*d)
}

// FinishTopology builds the response from the facts: clusters, gateway
// pairs, route pairs with the slow rule, leaf node sites, drops and
// order.
func FinishTopology(f *TopologyFacts) *v1.GetTopologyResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.GetTopologyResponse{}
	}
	res := &v1.GetTopologyResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt)}
	if f.Interval > 0 {
		res.CollectInterval = durationpb.New(f.Interval)
	}
	if len(f.History) > 0 {
		res.DropsWindowStart = timestamppb.New(f.History[0].At)
	}
	clusterOf := map[string]string{}
	for _, s := range f.Servers {
		clusterOf[s.Name] = s.Cluster
	}
	res.Gateways = topologyGateways(f)
	res.Routes = topologyRoutes(f, clusterOf)
	leafs := topologyLeafnodes(f)
	res.LeafnodesTotal = int32(len(leafs))
	res.Leafnodes = leafs[:min(len(leafs), MaxTopologyLeafnodes)]
	res.Clusters = topologyClusters(f, leafs)
	for _, e := range []string{EndpointRoutez, EndpointGatewayz, EndpointLeafz} {
		missing := map[string]bool{}
		for _, s := range f.Servers {
			missing[s.Name] = s.Missing
		}
		var names []string
		for _, n := range f.Unanswered[e] {
			if !missing[n] && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			slices.Sort(names)
			res.Unobserved = append(res.Unobserved, &v1.TopologyUnobserved{Endpoint: e, Servers: names})
		}
	}
	return res
}

// topologyClusters builds one cluster per cluster name of the servers,
// with the leaf node counts of the sites.
func topologyClusters(f *TopologyFacts, leafs []*v1.TopologyLeafnode) []*v1.TopologyCluster {
	byName := map[string]*v1.TopologyCluster{}
	places := map[string][]string{}
	var out []*v1.TopologyCluster
	for _, s := range f.Servers {
		c := byName[s.Cluster]
		if c == nil {
			c = &v1.TopologyCluster{Name: s.Cluster}
			byName[s.Cluster] = c
			out = append(out, c)
		}
		lead := s.Name == f.MetaLeader
		c.Servers = append(c.Servers, &v1.TopologyServer{Name: s.Name, Missing: s.Missing, MetaLeader: lead})
		c.MetaLeader = c.MetaLeader || lead
		if s.StreamsLed != nil {
			c.StreamsLed = ptrTo(c.GetStreamsLed() + *s.StreamsLed)
		}
		place := ""
		for _, t := range s.Tags {
			if v, ok := strings.CutPrefix(t, "region:"); ok {
				place = v
				break
			}
		}
		places[s.Cluster] = append(places[s.Cluster], place)
	}
	for name, c := range byName {
		if p := places[name]; len(p) > 0 && p[0] != "" && !slices.ContainsFunc(p, func(x string) bool { return x != p[0] }) {
			c.Place = p[0]
		}
		slices.SortFunc(c.Servers, func(a, b *v1.TopologyServer) int { return cmp.Compare(a.GetName(), b.GetName()) })
	}
	// Leaf node counts for every cluster with a server that answered
	// LEAFZ.
	for _, n := range f.Answered[EndpointLeafz] {
		for _, s := range f.Servers {
			if s.Name == n {
				c := byName[s.Cluster]
				c.LeafnodesConnected, c.LeafnodesNotConnected = ptrTo(c.GetLeafnodesConnected()), ptrTo(c.GetLeafnodesNotConnected())
			}
		}
	}
	for _, l := range leafs {
		c := byName[l.GetHubCluster()]
		if c == nil || c.LeafnodesConnected == nil {
			continue
		}
		if l.GetConnected() {
			*c.LeafnodesConnected++
		} else {
			*c.LeafnodesNotConnected++
		}
	}
	slices.SortFunc(out, func(a, b *v1.TopologyCluster) int { return cmp.Compare(a.GetName(), b.GetName()) })
	return out
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

// topologyGateways pairs the epoch's gateway rows by cluster and counts
// their drops.
func topologyGateways(f *TopologyFacts) []*v1.TopologyGateway {
	byPair := map[[2]string]*v1.TopologyGateway{}
	rtts := map[[2]string][]time.Duration{}
	get := func(p [2]string) *v1.TopologyGateway {
		g := byPair[p]
		if g == nil {
			g = &v1.TopologyGateway{ClusterA: p[0], ClusterB: p[1]}
			byPair[p] = g
		}
		return g
	}
	for _, r := range f.Gateways {
		if r.Gateway == r.Cluster {
			continue
		}
		p := pairOf(r.Cluster, r.Gateway)
		g := get(p)
		if r.CID != nil && r.Pending != nil {
			g.PendingBytes = ptrTo(g.GetPendingBytes() + *r.Pending)
		}
		if !r.Outbound || r.CID == nil {
			continue
		}
		g.Connections++
		if r.RTT != nil {
			rtts[p] = append(rtts[p], *r.RTT)
		}
	}
	// Expected: one outbound from every server of either cluster that
	// answered GATEWAYZ, to every other cluster with a gateway row.
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointGatewayz] {
		answered[n] = true
	}
	for p, g := range byPair {
		for _, s := range f.Servers {
			if answered[s.Name] && !s.Missing && (s.Cluster == p[0] || s.Cluster == p[1]) {
				g.Expected++
			}
		}
		if ds := rtts[p]; len(ds) > 0 {
			g.RttMax = durationpb.New(slices.Max(ds))
			g.RttMedian = durationpb.New(medianDuration(ds))
		}
	}
	countGatewayDrops(f, byPair)
	out := make([]*v1.TopologyGateway, 0, len(byPair))
	for _, g := range byPair {
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b *v1.TopologyGateway) int {
		return cmp.Or(cmp.Compare(b.GetDrops_1H(), a.GetDrops_1H()),
			cmp.Compare(b.GetRttMax().AsDuration(), a.GetRttMax().AsDuration()),
			cmp.Compare(a.GetClusterA(), b.GetClusterA()), cmp.Compare(a.GetClusterB(), b.GetClusterB()))
	})
	return out
}

// gatewayKey is one server's outbound gateway to one cluster.
type gatewayKey struct{ serverID, gateway string }

// countGatewayDrops counts, per pair, the outbound connections whose
// connection ID changed or that went away between two consecutive
// collections in which their server answered GATEWAYZ. Each connection is
// one server's outbound and another's inbound, so only outbound rows are
// read, and a disconnect counts once. A server that restarted has a new
// server ID and answers under it, so its old connections count no drop.
func countGatewayDrops(f *TopologyFacts, byPair map[[2]string]*v1.TopologyGateway) {
	recent := f.EpochAt.Add(-TopologyRecentWindow)
	dropping := map[[2]string]map[string]bool{}
	for i := 1; i < len(f.History); i++ {
		a, b := f.History[i-1], f.History[i]
		if !f.consecutive(a, b) {
			continue
		}
		inHour := !a.At.Before(recent)
		now := map[gatewayKey]int64{}
		for _, r := range b.Gateways {
			if r.Outbound && r.CID != nil {
				now[gatewayKey{r.ServerID, r.Gateway}] = *r.CID
			}
		}
		// The pairs observed: a server of the pair answered both times.
		for p, g := range byPair {
			if observedPair(a, b, p) {
				g.Drops_24H = ptrTo(g.GetDrops_24H())
				if inHour {
					g.Drops_1H = ptrTo(g.GetDrops_1H())
				}
			}
		}
		for _, r := range a.Gateways {
			if !r.Outbound || r.CID == nil || r.Gateway == r.Cluster || !a.Gatewayz[r.ServerID] || !b.Gatewayz[r.ServerID] {
				continue
			}
			if cid, ok := now[gatewayKey{r.ServerID, r.Gateway}]; ok && cid == *r.CID {
				continue
			}
			p := pairOf(r.Cluster, r.Gateway)
			g := byPair[p]
			if g == nil {
				continue
			}
			*g.Drops_24H++
			if inHour {
				*g.Drops_1H++
				if dropping[p] == nil {
					dropping[p] = map[string]bool{}
				}
				dropping[p][r.Server] = true
			}
		}
	}
	for p, names := range dropping {
		byPair[p].DroppingServers = slices.Sorted(maps.Keys(names))
	}
}

// observedPair reports whether a server of either cluster of p answered
// GATEWAYZ in both a and b, as its outbound rows in a tell.
func observedPair(a, b TopologyCollection, p [2]string) bool {
	for _, r := range a.Gateways {
		if r.Outbound && (r.Cluster == p[0] || r.Cluster == p[1]) && pairOf(r.Cluster, r.Gateway) == p &&
			a.Gatewayz[r.ServerID] && b.Gatewayz[r.ServerID] {
			return true
		}
	}
	return false
}

// routeEnd is one end's report of a pair.
type routeEnd struct {
	pool   int32
	pinned []string
}

// topologyRoutes pairs the epoch's routes by server and marks the slow
// ones.
func topologyRoutes(f *TopologyFacts, clusterOf map[string]string) []*v1.TopologyRoute {
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointRoutez] {
		answered[n] = true
	}
	byPair := map[[2]string]*v1.TopologyRoute{}
	ends := map[[2]string]map[string]*routeEnd{}
	for _, r := range f.Routes {
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
		if r.RTT != nil && (t.Rtt == nil || *r.RTT > t.GetRtt().AsDuration()) {
			t.Rtt = durationpb.New(*r.RTT)
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
	// The slow rule, per cluster over the pairs' round trips.
	rtts := map[string][]time.Duration{}
	for _, t := range out {
		if t.Rtt != nil {
			rtts[t.GetCluster()] = append(rtts[t.GetCluster()], t.GetRtt().AsDuration())
		}
	}
	medians := map[string]time.Duration{}
	for c, ds := range rtts {
		medians[c] = medianDuration(ds)
	}
	for _, t := range out {
		if t.Rtt == nil {
			continue
		}
		d := t.GetRtt().AsDuration()
		t.Slow = d >= slowRouteFloor && d >= slowRouteFactor*medians[t.GetCluster()]
	}
	slices.SortFunc(out, func(a, b *v1.TopologyRoute) int {
		return cmp.Or(compareBool(b.GetSlow(), a.GetSlow()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetCluster(), b.GetCluster()),
			cmp.Compare(a.GetServerA(), b.GetServerA()), cmp.Compare(a.GetServerB(), b.GetServerB()))
	})
	return out
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

// leafConn is one leaf node connection.
type leafConn struct {
	serverID string
	lid      int64
}

// topologyLeafnodes lists the sites of the epoch and those seen in the
// drop window and not connected now, with their drops, worst first.
func topologyLeafnodes(f *TopologyFacts) []*v1.TopologyLeafnode {
	answered := map[string]bool{}
	for _, n := range f.Answered[EndpointLeafz] {
		answered[n] = true
	}
	bySite := map[leafSite]*v1.TopologyLeafnode{}
	var out []*v1.TopologyLeafnode
	rows := slices.Clone(f.Leafnodes)
	slices.SortFunc(rows, func(a, b TopologyLeafRow) int {
		return cmp.Or(cmp.Compare(a.Server, b.Server), cmp.Compare(a.LID, b.LID))
	})
	for _, r := range rows {
		k := r.site()
		if bySite[k] != nil {
			continue
		}
		l := &v1.TopologyLeafnode{Name: k.name, Account: r.Account, HubServer: r.Server, HubCluster: r.Cluster, Connected: true,
			Rtt: durOf(r.RTT), Subscriptions: r.Subscriptions, Compression: r.Compression}
		bySite[k] = l
		out = append(out, l)
	}
	// Sites seen in the window and not now: listed with their last hub
	// when that hub's server answered LEAFZ in the epoch; otherwise
	// whether they are connected is not known, and they are left out.
	for i := len(f.History) - 1; i >= 0; i-- {
		c := f.History[i]
		if c.Epoch == f.Epoch {
			continue
		}
		for _, r := range c.Leafnodes {
			k := r.site()
			if bySite[k] != nil {
				continue
			}
			l := &v1.TopologyLeafnode{Name: k.name, Account: r.Account, HubServer: r.Server, HubCluster: r.Cluster,
				LastSeen: timestamppb.New(c.At), Compression: r.Compression}
			bySite[k] = l
			if answered[r.Server] {
				out = append(out, l)
			}
		}
	}
	countLeafDrops(f, bySite)
	slices.SortFunc(out, func(a, b *v1.TopologyLeafnode) int {
		return cmp.Or(compareBool(a.GetConnected(), b.GetConnected()),
			cmp.Compare(b.GetDrops_24H(), a.GetDrops_24H()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetName(), b.GetName()), cmp.Compare(a.GetAccount(), b.GetAccount()))
	})
	return out
}

// countLeafDrops counts, per site, the connections that went away (the
// site gone, or on another connection or hub) between two consecutive
// collections in which the old hub answered LEAFZ under the same server
// ID. A hub that restarted counts no drop of its sites.
func countLeafDrops(f *TopologyFacts, bySite map[leafSite]*v1.TopologyLeafnode) {
	for i := 1; i < len(f.History); i++ {
		a, b := f.History[i-1], f.History[i]
		if !f.consecutive(a, b) {
			continue
		}
		now := map[leafSite]map[leafConn]bool{}
		for _, r := range b.Leafnodes {
			k := r.site()
			if now[k] == nil {
				now[k] = map[leafConn]bool{}
			}
			now[k][leafConn{r.ServerID, r.LID}] = true
		}
		for _, r := range a.Leafnodes {
			l := bySite[r.site()]
			if l == nil || !a.Leafz[r.ServerID] || !b.Leafz[r.ServerID] {
				continue
			}
			l.Drops_24H = ptrTo(l.GetDrops_24H())
			if !now[r.site()][leafConn{r.ServerID, r.LID}] {
				*l.Drops_24H++
			}
		}
	}
}
