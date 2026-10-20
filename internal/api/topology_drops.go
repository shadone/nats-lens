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
// that connection (gone, reconnected, or on another hub). A gap, a failed
// reply and a restart (a new server ID) count no drop. A pair of
// collections counts in a window when its earlier collection lies in it.
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
	// Gatewayz and Leafz are the server IDs whose GATEWAYZ and LEAFZ
	// answered ok.
	Gatewayz, Leafz map[string]bool
	// Gateways are the outbound gateway rows with a connection ID; other
	// rows are not read.
	Gateways  []TopologyGatewayRow
	Leafnodes []TopologyLeafRow
}

// CountTopologyDrops counts the drops of a history by the rule of
// TopologyDrops. history runs oldest first up to the collection of epoch,
// which starts at epochAt.
func CountTopologyDrops(history []TopologyCollection, interval time.Duration, epoch int64, epochAt time.Time) TopologyDrops {
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
	return d
}

// leafConn is one leaf node connection.
type leafConn struct {
	serverID string
	lid      int64
}
