package fake

import (
	"context"
	"hash/fnv"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// linkSpec is how a scenario's servers connect: routes inside each
// cluster, gateways between clusters and leaf node sites at the edge.
// Times are in the fixed frame.
type linkSpec struct {
	// pool is the pooled routes between two servers, pinned the accounts
	// with a route of their own.
	pool   int
	pinned []string
	// routeBase is added to every route round trip of a cluster whose
	// servers lie further apart.
	routeBase map[string]time.Duration
	// slowRoutes gives the round trip of every route of a server that is
	// slower than the rest.
	slowRoutes map[string]time.Duration
	// gatewayRTT is the round trip between two clusters, keyed in name
	// order.
	gatewayRTT map[[2]string]time.Duration
	// gatewayPending is the bytes pending on one server's outbound
	// gateway, keyed "server>cluster".
	gatewayPending map[string]int64
	// flaps are when one server's outbound gateway reconnected, keyed
	// "server>cluster".
	flaps map[string][]time.Time
	// stopped is when a server first stopped answering on the scenario's
	// day; a gateway or leaf node connected to it moves then to the next
	// server of its cluster that never stops.
	stopped func(name string) (time.Time, bool)
	sites   []siteSpec
}

// siteSpec is one leaf node site: a store's own server connected to a hub
// of the system.
type siteSpec struct {
	name    string
	cluster string
	// hub is the index of its preferred hub in the cluster.
	hub  int
	rtt  time.Duration
	subs int64
	// since is when the site first connected; zero means before any
	// range.
	since time.Time
	// reconnects are drops followed by a reconnect within one collect
	// interval; outages are times without a connection, an outage
	// without an end lasting until now.
	reconnects []time.Time
	outages    [][2]time.Time
}

// linkCompression is the compression mode the demo's leaf nodes report.
const linkCompression = "s2_auto"

// clusterLayout is the servers of a scenario by cluster.
type clusterLayout struct {
	names     []string
	servers   map[string][]serverSpec
	positions map[string]int
}

func (sc *scenario) clusterLayout() *clusterLayout {
	sc.layoutOnce.Do(func() {
		l := clusterLayout{servers: map[string][]serverSpec{}, positions: map[string]int{}}
		for _, sp := range sc.servers {
			if _, ok := l.servers[sp.cluster]; !ok {
				l.names = append(l.names, sp.cluster)
			}
			l.positions[sp.name] = len(l.servers[sp.cluster])
			l.servers[sp.cluster] = append(l.servers[sp.cluster], sp)
		}
		sc.layout = l
	})
	return &sc.layout
}

// clusterServers are the servers of a cluster in scenario order.
func (sc *scenario) clusterServers(cluster string) []serverSpec {
	return sc.clusterLayout().servers[cluster]
}

// clusters are the scenario's cluster names in order of appearance.
func (sc *scenario) clusters() []string { return sc.clusterLayout().names }

// indexIn is a server's position in its cluster.
func (sc *scenario) indexIn(sp serverSpec) int { return sc.clusterLayout().positions[sp.name] }

// stop is when a server first stopped answering, by the scenario's
// links; false when it never stops or the scenario has no links.
func (l *linkSpec) stop(name string) (time.Time, bool) {
	if l == nil || l.stopped == nil {
		return time.Time{}, false
	}
	return l.stopped(name)
}

// processID is the server ID of sp's process at the scenario time at.
func (s scen) processID(sp serverSpec, at time.Time) string {
	if at.Before(s.started(sp)) {
		return sp.oldID
	}
	return sp.id
}

// routeRTT is the round trip a reports on its routes to b: a little
// above 0.3 ms, steady per pair, or the slow server's.
func (sc *scenario) routeRTT(a, b string) time.Duration {
	if l := sc.links; l != nil {
		if d, ok := l.slowRoutes[a]; ok {
			return d + time.Duration(len(b))*7*time.Microsecond
		}
		if d, ok := l.slowRoutes[b]; ok {
			return d + time.Duration(len(a))*5*time.Microsecond
		}
	}
	x, y := a, b
	if y < x {
		x, y = y, x
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(x + "|" + y))
	d := time.Duration(300+h.Sum32()%280) * time.Microsecond
	if a > b {
		d += 9 * time.Microsecond
	}
	if sp, ok := sc.server(a); ok && sc.links != nil {
		d += sc.links.routeBase[sp.cluster]
	}
	return d
}

// routeAccounts are the accounts of the routes between two servers: ""
// for each pooled route, then the pinned accounts.
func (sc *scenario) routeAccounts() []string {
	pool, pinned := 1, []string(nil)
	if l := sc.links; l != nil {
		pool, pinned = max(l.pool, 1), l.pinned
	}
	out := make([]string, pool, pool+len(pinned))
	return append(out, pinned...)
}

// gatewayTarget is the server of cluster to which sp's outbound gateway
// connects at fixed-frame time c, and how often it moved since sp's
// process started: to the server at sp's position in that cluster, or,
// once that one stopped answering, to the next that never stops.
func (s scen) gatewayTarget(sp serverSpec, cluster string, c time.Time) (serverSpec, int) {
	servers := s.sc.clusterServers(cluster)
	pref := servers[s.sc.indexIn(sp)%len(servers)]
	start := s.clock(s.started(sp))
	if c.Before(start) {
		start = time.Time{}
	}
	stop, stops := s.sc.links.stop(pref.name)
	if !stops || c.Before(stop) {
		return pref, 0
	}
	moved := 0
	if !stop.Before(start) {
		// Connected before it stopped: the move is a reconnect.
		moved = 1
	}
	for k := 1; k < len(servers); k++ {
		x := servers[(s.sc.indexIn(pref)+k)%len(servers)]
		if _, ok := s.sc.links.stop(x.name); !ok {
			return x, moved
		}
	}
	return pref, moved
}

// routeRows are the routes at the scenario time at between the answering
// servers of each cluster.
func (s scen) routeRows(at time.Time) []api.TopologyRouteRow {
	var out []api.TopologyRouteRow
	for _, sp := range s.sc.servers {
		if s.downAt(sp, at) {
			continue
		}
		for _, x := range s.sc.servers {
			if x.cluster != sp.cluster || x.name == sp.name || s.downAt(x, at) {
				continue
			}
			for _, acc := range s.sc.routeAccounts() {
				out = append(out, api.TopologyRouteRow{Server: sp.name, Remote: x.name, Cluster: sp.cluster,
					RTT: ptr(s.sc.routeRTT(sp.name, x.name)), Pending: ptr(int64(0)), Account: acc})
			}
		}
	}
	return out
}

// earlierRoutes is the collection the slow rule compares the epoch at at
// with, as the store finds it: the newest collection before it with
// routes, no earlier than api.TopologyEarliestEarlier and within the
// history; nil when there is none.
func (s scen) earlierRoutes(at time.Time) *api.TopologyEarlier {
	bound := api.TopologyEarliestEarlier(at, sampleEvery)
	for t := at.Add(-sampleEvery); !t.Before(bound) && !t.Before(s.dataStart()); t = t.Add(-sampleEvery) {
		if rows := s.routeRows(t); len(rows) > 0 {
			return &api.TopologyEarlier{Epoch: s.epochNumber(t), At: t, Routes: rows}
		}
	}
	return nil
}

// timesIn counts the times in (from, to].
func timesIn(ts []time.Time, from, to time.Time) int {
	n := 0
	for _, t := range ts {
		if t.After(from) && !t.After(to) {
			n++
		}
	}
	return n
}

// gatewayRows are the outbound gateway connections at the scenario time
// at of every answering server.
func (s scen) gatewayRows(at time.Time) []api.TopologyGatewayRow {
	l := s.sc.links
	if l == nil || len(l.gatewayRTT) == 0 {
		return nil
	}
	c := s.clock(at)
	var out []api.TopologyGatewayRow
	for _, sp := range s.sc.servers {
		if s.downAt(sp, at) {
			continue
		}
		start := s.clock(s.started(sp))
		if c.Before(start) {
			start = time.Time{}
		}
		for ci, cluster := range s.sc.clusters() {
			if cluster == sp.cluster {
				continue
			}
			_, moved := s.gatewayTarget(sp, cluster, c)
			key := sp.name + ">" + cluster
			cid := int64(100+10*ci+moved) + int64(timesIn(l.flaps[key], start, c))
			rtt := l.gatewayRTT[[2]string{min(sp.cluster, cluster), max(sp.cluster, cluster)}] +
				time.Duration(s.sc.indexIn(sp)%3-1)*600*time.Microsecond
			out = append(out, api.TopologyGatewayRow{ServerID: s.processID(sp, at), Server: sp.name, Cluster: sp.cluster,
				Gateway: cluster, Outbound: true, CID: &cid, RTT: &rtt, Pending: ptr(l.gatewayPending[key])})
		}
	}
	return out
}

// siteAt reports whether a site is connected at fixed-frame time c, its
// hub then and its connection ID.
func (s scen) siteAt(i int, site siteSpec, c time.Time) (serverSpec, int64, bool) {
	if c.Before(site.since) {
		return serverSpec{}, 0, false
	}
	gen := int64(timesIn(site.reconnects, time.Time{}, c))
	for _, o := range site.outages {
		if !c.Before(o[0]) && (o[1].IsZero() || c.Before(o[1])) {
			return serverSpec{}, 0, false
		}
		if !o[1].IsZero() && !c.Before(o[1]) {
			gen++
		}
	}
	servers := s.sc.clusterServers(site.cluster)
	hub := servers[site.hub%len(servers)]
	if stop, ok := s.sc.links.stop(hub.name); ok && !c.Before(stop) {
		for k := 1; k < len(servers); k++ {
			x := servers[(site.hub+k)%len(servers)]
			if _, ok := s.sc.links.stop(x.name); !ok {
				hub = x
				break
			}
		}
	}
	return hub, int64(1000*(i+1)) + gen, true
}

// leafRows are the leaf node sites connected at the scenario time at.
func (s scen) leafRows(at time.Time) []api.TopologyLeafRow {
	l := s.sc.links
	if l == nil {
		return nil
	}
	c := s.clock(at)
	var out []api.TopologyLeafRow
	for i, site := range l.sites {
		hub, lid, ok := s.siteAt(i, site, c)
		if !ok || s.downAt(hub, at) {
			continue
		}
		out = append(out, api.TopologyLeafRow{ServerID: s.processID(hub, at), Server: hub.name, Cluster: hub.cluster, LID: lid,
			Name: site.name, Account: Account, RTT: ptr(site.rtt), Subscriptions: ptr(site.subs), Compression: linkCompression})
	}
	return out
}

// sitesConnected counts the leaf node sites connected at the scenario
// time at.
func (s scen) sitesConnected(at time.Time) int {
	return len(s.leafRows(at))
}

// leafCount counts the leaf node connections on sp at the scenario time
// at.
func (s scen) leafCount(sp serverSpec, at time.Time) int32 {
	var n int32
	for _, r := range s.leafRows(at) {
		if r.Server == sp.name {
			n++
		}
	}
	return n
}

// answeringIDs are the server IDs of the servers answering at at.
func (s scen) answeringIDs(at time.Time) map[string]bool {
	out := map[string]bool{}
	for _, sp := range s.sc.servers {
		if !s.downAt(sp, at) {
			out[s.processID(sp, at)] = true
		}
	}
	return out
}

func sameIDs(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sameGateways(a, b []api.TopologyGatewayRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ServerID != b[i].ServerID || a[i].Gateway != b[i].Gateway || *a[i].CID != *b[i].CID {
			return false
		}
	}
	return true
}

func sameLeafs(a, b []api.TopologyLeafRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ServerID != b[i].ServerID || a[i].LID != b[i].LID || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// Topology builds the facts of the range's epoch from the scenario: the
// servers answering then, their routes, gateways and leaf node sites, and
// every collection of the 24 hours before.
func (p *Provider) Topology(_ context.Context, req *v1.GetTopologyRequest) (*api.TopologyFacts, error) {
	s := p.scen()
	at, epoch, ok := s.epochBefore(req.GetRange())
	if !ok {
		return &api.TopologyFacts{}, nil
	}
	f := &api.TopologyFacts{Epoch: epoch, EpochAt: at, Interval: sampleEvery, MetaLeader: s.metaLeaderAt(at),
		Answered: map[string][]string{}, Unanswered: map[string][]string{}}
	for _, sp := range p.sc.servers {
		down := s.downAt(sp, at)
		sf := api.TopologyServerFact{Name: sp.name, Cluster: sp.cluster, Tags: sp.tags, Missing: down}
		if down {
			f.Servers = append(f.Servers, sf)
			continue
		}
		streams, _ := s.leads(sp, at)
		sf.StreamsLed = ptr(streams)
		f.Servers = append(f.Servers, sf)
		for _, e := range []string{api.EndpointRoutez, api.EndpointGatewayz, api.EndpointLeafz} {
			f.Answered[e] = append(f.Answered[e], sp.name)
		}
	}
	f.Routes = s.routeRows(at)
	f.Earlier = s.earlierRoutes(at)
	f.Gateways, f.Leafnodes = s.gatewayRows(at), s.leafRows(at)
	// The collections of the drop window, oldest first, sharing rows and
	// answers between collections where nothing changed.
	from := latest(at.Add(-api.TopologyDropWindow), s.dataStart())
	first := at.Add(-at.Sub(from) / sampleEvery * sampleEvery)
	var prev api.TopologyCollection
	var history []api.TopologyCollection
	for t := first; !t.After(at); t = t.Add(sampleEvery) {
		c := api.TopologyCollection{Epoch: s.epochNumber(t), At: t, Gatewayz: s.answeringIDs(t),
			Gateways: s.gatewayRows(t), Leafnodes: s.leafRows(t)}
		if prev.Gatewayz != nil && sameIDs(prev.Gatewayz, c.Gatewayz) {
			c.Gatewayz = prev.Gatewayz
		}
		if prev.Gateways != nil && sameGateways(prev.Gateways, c.Gateways) {
			c.Gateways = prev.Gateways
		}
		if prev.Leafnodes != nil && sameLeafs(prev.Leafnodes, c.Leafnodes) {
			c.Leafnodes = prev.Leafnodes
		}
		c.Leafz = c.Gatewayz
		history = append(history, c)
		prev = c
	}
	f.Drops = api.CountTopologyDrops(history, sampleEvery, epoch, at)
	return f, nil
}
