package api

import (
	"cmp"
	"slices"
	"strings"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The checks whose findings Topology shows.
const (
	CheckMemoryHigh    = "server-008"
	CheckMirrorLagging = "stream-015"
	CheckMirrorError   = "stream-017"
	CheckSourceSilent  = "stream-022"
	CheckSourceError   = "stream-023"
)

// topologyFindings are the firing findings by check code and entity key.
func topologyFindings(fs []*v1.Finding) map[string][]*v1.Finding {
	out := map[string][]*v1.Finding{}
	for _, x := range fs {
		if x.GetState() == v1.FindingState_FINDING_STATE_RESOLVED {
			continue
		}
		k := x.GetCheckCode() + ":" + x.GetEntity().GetKey()
		out[k] = append(out[k], x)
	}
	return out
}

// quiet reports whether a finding is muted, an acceptance included.
func quiet(x *v1.Finding) bool { return x.GetMute() != nil }

// memoryFinding is the finding about s's memory: server-008, else
// server-007 when the JetStream memory store is at least as full as the
// file store, which server-007 also judges.
func memoryFinding(s TopologyServerFact, firing map[string][]*v1.Finding) *v1.Finding {
	if fs := firing[CheckMemoryHigh+":"+s.Name]; len(fs) > 0 {
		return fs[0]
	}
	fs := firing[CheckStoreHigh+":"+s.Name]
	if len(fs) == 0 || s.JSMemory == nil || s.JSMaxMemory == nil || *s.JSMaxMemory <= 0 {
		return nil
	}
	mem := float64(*s.JSMemory) / float64(*s.JSMaxMemory)
	if s.JSStorage != nil && s.JSMaxStorage != nil && *s.JSMaxStorage > 0 && float64(*s.JSStorage)/float64(*s.JSMaxStorage) > mem {
		return nil
	}
	return fs[0]
}

// topologyClusters builds one cluster per cluster name of the servers,
// with the leaf node sites on each counted by name.
func topologyClusters(f *TopologyFacts, servers []TopologyServerFact, leafs []*v1.TopologyLeafnode, firing map[string][]*v1.Finding) []*v1.TopologyCluster {
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
			if x := memoryFinding(s, firing); x != nil {
				ts.MemoryFindingId, ts.MemoryFindingCode, ts.MemoryFindingSeverity, ts.MemoryFindingQuiet = x.GetId(), x.GetCheckCode(), x.GetSeverity(), quiet(x)
			}
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
