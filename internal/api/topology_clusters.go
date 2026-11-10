package api

import (
	"cmp"
	"slices"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Memory far above the other servers, as server-008 judges it by default:
// at least MemHighMinBytes and more than MemHighMultiplier times the median
// of the other servers that report it.
const (
	MemHighMinBytes   = 512 << 20
	MemHighMultiplier = 1.5
)

// memHigh reports whether s's memory is far above the other servers'.
func memHigh(s TopologyServerFact, servers []TopologyServerFact) bool {
	if s.Mem == nil || *s.Mem < MemHighMinBytes {
		return false
	}
	var others []float64
	for _, o := range servers {
		if o.Name != s.Name && !o.Missing && o.Mem != nil {
			others = append(others, float64(*o.Mem))
		}
	}
	if len(others) == 0 {
		return false
	}
	slices.Sort(others)
	n := len(others)
	median := others[n/2]
	if n%2 == 0 {
		median = (others[n/2-1] + others[n/2]) / 2
	}
	return median > 0 && float64(*s.Mem) > MemHighMultiplier*median
}

// topologyClusters builds one cluster per cluster name of the servers,
// with the leaf node sites on each counted by name.
func topologyClusters(f *TopologyFacts, servers []TopologyServerFact, leafs []*v1.TopologyLeafnode) []*v1.TopologyCluster {
	byName := map[string]*v1.TopologyCluster{}
	clusterOf := map[string]string{}
	places := map[string][]string{}
	var out []*v1.TopologyCluster
	for _, s := range servers {
		clusterOf[s.Name] = s.Cluster
		c := byName[s.Cluster]
		if c == nil {
			c = &v1.TopologyCluster{Name: s.Cluster}
			byName[s.Cluster] = c
			out = append(out, c)
		}
		lead := s.Name == f.MetaLeader
		ts := &v1.TopologyServer{Name: s.Name, Missing: s.Missing, MetaLeader: lead}
		if !s.Missing {
			ts.MemBytes, ts.JsMemoryBytes, ts.JsMaxMemoryBytes = s.Mem, s.JSMemory, s.JSMaxMemory
			ts.MemHigh = memHigh(s, servers)
		}
		c.Servers = append(c.Servers, ts)
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
	// Leaf node counts, by site name, for every cluster with a server that
	// answered LEAFZ.
	counted := map[string]bool{}
	for _, n := range f.Answered[EndpointLeafz] {
		if cl, ok := clusterOf[n]; ok {
			counted[cl] = true
		}
	}
	connected, notConnected := map[string]map[string]bool{}, map[string]map[string]bool{}
	add := func(m map[string]map[string]bool, cluster, name string) {
		if m[cluster] == nil {
			m[cluster] = map[string]bool{}
		}
		m[cluster][name] = true
	}
	for _, l := range leafs {
		switch {
		case l.GetConnected():
			add(connected, l.GetHubCluster(), l.GetName())
			for _, hub := range l.GetHubServers() {
				if cl, ok := clusterOf[hub]; ok {
					add(connected, cl, l.GetName())
				}
			}
		case !l.GetConnectionUnknown():
			add(notConnected, l.GetHubCluster(), l.GetName())
		}
	}
	for name, c := range byName {
		if !counted[name] {
			continue
		}
		n := 0
		for site := range notConnected[name] {
			if !connected[name][site] {
				n++
			}
		}
		c.LeafnodesConnected, c.LeafnodesNotConnected = ptrTo(int32(len(connected[name]))), ptrTo(int32(n))
	}
	slices.SortFunc(out, func(a, b *v1.TopologyCluster) int { return cmp.Compare(a.GetName(), b.GetName()) })
	return out
}
