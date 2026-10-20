package api

import (
	"cmp"
	"slices"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// topologyLeafnodes lists the sites of the epoch, one per site name and
// account with every hub it is on, and the sites seen in the window and
// not connected now, with their drops, worst first. When a server's
// LEAFZ failed in the epoch a site not seen may be connected to it: such
// sites are marked connection_unknown rather than left out, so their
// drops still show.
func topologyLeafnodes(f *TopologyFacts) []*v1.TopologyLeafnode {
	unknown := len(f.Unanswered[EndpointLeafz]) > 0
	bySite := map[leafSite]*v1.TopologyLeafnode{}
	var out []*v1.TopologyLeafnode
	rows := slices.Clone(f.Leafnodes)
	slices.SortFunc(rows, func(a, b TopologyLeafRow) int {
		return cmp.Or(cmp.Compare(a.Server, b.Server), cmp.Compare(a.LID, b.LID))
	})
	for _, r := range rows {
		k := r.site()
		if l := bySite[k]; l != nil {
			if !slices.Contains(l.HubServers, r.Server) {
				l.HubServers = append(l.HubServers, r.Server)
			}
			continue
		}
		l := &v1.TopologyLeafnode{Name: k.name, Account: r.Account, HubServer: r.Server, HubServers: []string{r.Server},
			HubCluster: r.Cluster, Connected: true, Rtt: durOf(r.RTT), Subscriptions: r.Subscriptions, Compression: r.Compression}
		bySite[k] = l
		out = append(out, l)
	}
	seen := slices.Clone(f.Drops.LastSeen)
	slices.SortFunc(seen, func(a, b TopologyLeafSighting) int {
		return cmp.Or(cmp.Compare(a.Row.SiteName(), b.Row.SiteName()), cmp.Compare(a.Row.Account, b.Row.Account))
	})
	for _, s := range seen {
		k := s.Row.site()
		if bySite[k] != nil {
			continue
		}
		l := &v1.TopologyLeafnode{Name: k.name, Account: s.Row.Account, HubServer: s.Row.Server, HubCluster: s.Row.Cluster,
			LastSeen: timestamppb.New(s.At), Compression: s.Row.Compression, ConnectionUnknown: unknown}
		bySite[k] = l
		out = append(out, l)
	}
	for _, d := range f.Drops.Leafs {
		if l := bySite[leafSite{d.Name, d.Account}]; l != nil {
			l.Drops_24H = ptrTo(d.Drops)
		}
	}
	rank := func(l *v1.TopologyLeafnode) int {
		switch {
		case !l.GetConnected() && !l.GetConnectionUnknown():
			return 0
		case !l.GetConnected():
			return 1
		}
		return 2
	}
	slices.SortFunc(out, func(a, b *v1.TopologyLeafnode) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)),
			cmp.Compare(b.GetDrops_24H(), a.GetDrops_24H()),
			cmp.Compare(b.GetRtt().AsDuration(), a.GetRtt().AsDuration()),
			cmp.Compare(a.GetName(), b.GetName()), cmp.Compare(a.GetAccount(), b.GetAccount()))
	})
	return out
}
