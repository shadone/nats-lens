package api

import (
	"cmp"
	"slices"
	"time"
)

// TopologyDrops are the drops counted over the window before an epoch:
// per reporting server and remote cluster for gateways, per site for leaf
// nodes, with the sites seen in the window and where they were last.
//
// The rule, which CountTopologyDrops applies to a history and the store
// applies in SQL: two collections are consecutive when they are at most
// TopologyGapIntervals collect intervals apart. Between two consecutive
// collections, an outbound gateway connection drops when its server
// answered GATEWAYZ in both under the same server ID and the connection
// ID it had is gone; a leaf node connection drops when its hub answered
// LEAFZ in both under the same server ID and the site is no longer on
// that connection (gone, reconnected, or on another hub); a route of two
// servers drops when both answered ROUTEZ in both under the same server
// IDs and a route connection one of them listed (its route ID) is gone.
// Routes count once per pair of servers and pair of collections, however
// many of the pair's pooled or pinned routes went, as both ends list
// every route and a pool or a restarting link replaces them together. A
// gap, a failed reply and a restart (a new server ID, of either end for
// a route) count no drop; nor does a route whose server or remote process
// started within the restart window before the earlier collection, as
// routes are replaced while a cluster settles after a start (see
// TopologyDropRules). A pair of collections counts in a window when
// its earlier collection lies in it.
type TopologyDrops struct {
	// WindowStart is the oldest collection of the window; zero without
	// one.
	WindowStart time.Time
	// GatewayPairs are the (reporting cluster, remote cluster) of every
	// outbound gateway row in the window.
	GatewayPairs [][2]string
	// Gateways are the counts per reporting server name and remote
	// cluster, for every outbound connection observed between two
	// consecutive collections.
	Gateways []TopologyGatewayDrops
	// Leafs are the counts of every site observed connected between two
	// consecutive collections.
	Leafs []TopologyLeafDrops
	// LastSeen is, for every site seen in the window before the epoch, its
	// newest row and the time of that collection.
	LastSeen []TopologyLeafSighting
	// Routes are the counts per pair of servers, for every pair with a
	// route observed between two consecutive collections.
	Routes []TopologyRouteDrops
}

// TopologyRouteDrops counts the collections in which a route between
// servers A and B (in name order) dropped, as TopologyGatewayDrops.
type TopologyRouteDrops struct {
	A, B                        string
	Recent, Day                 int32
	ObservedRecent, ObservedDay bool
}

// TopologyRouteConn is one route connection as its reporting server lists
// it, with both ends' server IDs: the rows the route drop rule reads.
type TopologyRouteConn struct {
	ServerID, Server string
	// RID is the route's connection ID on the reporting server.
	RID              int64
	RemoteID, Remote string
}

// TopologyGatewayDrops counts one server's outbound gateway drops to one
// cluster. Observed says whether a consecutive pair of collections of
// the window could count one; Recent counts the last hour.
type TopologyGatewayDrops struct {
	Server, Cluster, Gateway    string
	Recent, Day                 int32
	ObservedRecent, ObservedDay bool
}

// TopologyLeafDrops counts the drops of one site, by SiteName and
// account.
type TopologyLeafDrops struct {
	Name, Account string
	Drops         int32
}

// TopologyLeafSighting is a site's newest row in the window, at At.
type TopologyLeafSighting struct {
	Row TopologyLeafRow
	At  time.Time
}

// TopologyCollection is one collection of the window, as the demo builds
// it: which servers answered, by server ID, and their gateway and leaf
// node connections.
type TopologyCollection struct {
	Epoch int64
	At    time.Time
	// Gatewayz, Leafz and Routez are the server IDs whose GATEWAYZ, LEAFZ
	// and ROUTEZ answered ok.
	Gatewayz, Leafz, Routez map[string]bool
	// Routes are the route connections with a remote server ID.
	Routes []TopologyRouteConn
	// Gateways are the outbound gateway rows with a connection ID; other
	// rows are not read.
	Gateways  []TopologyGatewayRow
	Leafnodes []TopologyLeafRow
}

// TopologyDropRules are the settings of the drop rule beyond the
// collections: a route whose server or remote process (by server ID in
// Started) started less than RestartWindow before the earlier of two
// collections counts neither a drop nor an observation for that pair of
// collections. Zero counts every route.
type TopologyDropRules struct {
	RestartWindow time.Duration
	Started       map[string]time.Time
}

// restarting reports whether the process id started less than the
// restart window before t.
func (r TopologyDropRules) restarting(id string, t time.Time) bool {
	at, ok := r.Started[id]
	return ok && r.RestartWindow > 0 && at.After(t.Add(-r.RestartWindow))
}

// CountTopologyDrops counts the drops of a history by the rule of
// TopologyDrops without a restart window. history runs oldest first up to
// the collection of epoch, which starts at epochAt.
func CountTopologyDrops(history []TopologyCollection, interval time.Duration, epoch int64, epochAt time.Time) TopologyDrops {
	return CountTopologyDropsWith(history, interval, epoch, epochAt, TopologyDropRules{})
}

// CountTopologyDropsWith is CountTopologyDrops with the rules.
func CountTopologyDropsWith(history []TopologyCollection, interval time.Duration, epoch int64, epochAt time.Time, rules TopologyDropRules) TopologyDrops {
	var d TopologyDrops
	if len(history) == 0 {
		return d
	}
	d.WindowStart = history[0].At
	recent := epochAt.Add(-TopologyRecentWindow)
	type gwKey struct{ server, cluster, gateway string }
	gws := map[gwKey]*TopologyGatewayDrops{}
	leafs := map[leafSite]*TopologyLeafDrops{}
	pairs := map[[2]string]bool{}
	seen := map[leafSite]TopologyLeafSighting{}
	routes := map[[2]string]*TopologyRouteDrops{}
	for i, b := range history {
		for _, r := range b.Gateways {
			if r.Outbound {
				pairs[[2]string{r.Cluster, r.Gateway}] = true
			}
		}
		if b.Epoch != epoch {
			for _, r := range b.Leafnodes {
				if s, ok := seen[r.site()]; !ok || s.At.Before(b.At) {
					seen[r.site()] = TopologyLeafSighting{Row: r, At: b.At}
				}
			}
		}
		if i == 0 {
			continue
		}
		a := history[i-1]
		if interval > 0 && b.At.Sub(a.At) > TopologyGapIntervals*interval {
			continue
		}
		inHour := !a.At.Before(recent)
		cids := map[[2]string]int64{}
		for _, r := range b.Gateways {
			if r.Outbound && r.CID != nil {
				cids[[2]string{r.ServerID, r.Gateway}] = *r.CID
			}
		}
		for _, r := range a.Gateways {
			if !r.Outbound || r.CID == nil || !a.Gatewayz[r.ServerID] || !b.Gatewayz[r.ServerID] {
				continue
			}
			k := gwKey{r.Server, r.Cluster, r.Gateway}
			g := gws[k]
			if g == nil {
				g = &TopologyGatewayDrops{Server: r.Server, Cluster: r.Cluster, Gateway: r.Gateway}
				gws[k] = g
			}
			g.ObservedDay = true
			g.ObservedRecent = g.ObservedRecent || inHour
			if cid, ok := cids[[2]string{r.ServerID, r.Gateway}]; ok && cid == *r.CID {
				continue
			}
			g.Day++
			if inHour {
				g.Recent++
			}
		}
		countRouteDrops(routes, a, b, inHour, rules)
		now := map[leafSite]map[leafConn]bool{}
		for _, r := range b.Leafnodes {
			if now[r.site()] == nil {
				now[r.site()] = map[leafConn]bool{}
			}
			now[r.site()][leafConn{r.ServerID, r.LID}] = true
		}
		for _, r := range a.Leafnodes {
			if !a.Leafz[r.ServerID] || !b.Leafz[r.ServerID] {
				continue
			}
			l := leafs[r.site()]
			if l == nil {
				l = &TopologyLeafDrops{Name: r.SiteName(), Account: r.Account}
				leafs[r.site()] = l
			}
			if !now[r.site()][leafConn{r.ServerID, r.LID}] {
				l.Drops++
			}
		}
	}
	for p := range pairs {
		d.GatewayPairs = append(d.GatewayPairs, p)
	}
	slices.SortFunc(d.GatewayPairs, func(a, b [2]string) int { return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1])) })
	for _, g := range gws {
		d.Gateways = append(d.Gateways, *g)
	}
	for _, l := range leafs {
		d.Leafs = append(d.Leafs, *l)
	}
	for _, s := range seen {
		d.LastSeen = append(d.LastSeen, s)
	}
	for _, r := range routes {
		d.Routes = append(d.Routes, *r)
	}
	return d
}

// countRouteDrops adds the route drops between consecutive collections a
// and b to counts, keyed by pair of server names.
func countRouteDrops(counts map[[2]string]*TopologyRouteDrops, a, b TopologyCollection, inHour bool, rules TopologyDropRules) {
	type conn struct {
		server string
		rid    int64
	}
	still := map[conn]bool{}
	for _, r := range b.Routes {
		still[conn{r.ServerID, r.RID}] = true
	}
	dropped := map[[2]string]bool{}
	for _, r := range a.Routes {
		if r.RemoteID == "" || !a.Routez[r.ServerID] || !b.Routez[r.ServerID] || !a.Routez[r.RemoteID] || !b.Routez[r.RemoteID] ||
			rules.restarting(r.ServerID, a.At) || rules.restarting(r.RemoteID, a.At) {
			continue
		}
		k := pairOf(r.Server, r.Remote)
		dropped[k] = dropped[k] || !still[conn{r.ServerID, r.RID}]
	}
	for k, gone := range dropped {
		c := counts[k]
		if c == nil {
			c = &TopologyRouteDrops{A: k[0], B: k[1]}
			counts[k] = c
		}
		c.ObservedDay = true
		c.ObservedRecent = c.ObservedRecent || inHour
		if gone {
			c.Day++
			if inHour {
				c.Recent++
			}
		}
	}
}

// leafConn is one leaf node connection.
type leafConn struct {
	serverID string
	lid      int64
}
