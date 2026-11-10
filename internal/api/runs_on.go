package api

import (
	"cmp"
	"maps"
	"slices"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// MaxRunsOnItems is the most workloads and nodes an application's Runs
// on lists.
const MaxRunsOnItems = 10

// runsOn sums where an application's connections run from the pods they
// were matched to.
func runsOn(conns []AppConnectionFact) *v1.ApplicationRunsOn {
	r := &v1.ApplicationRunsOn{}
	type wkey struct{ ns, kind, name string }
	type wsum struct {
		pods  map[string]bool
		conns int32
	}
	workloads := map[wkey]*wsum{}
	nodes, pods := map[string]bool{}, map[[2]string]bool{}
	for _, c := range conns {
		switch {
		case c.PodAmbiguous:
			r.Ambiguous++
		case c.Pod != nil:
			r.Matched++
			p := c.Pod
			k := wkey{p.Namespace, p.WorkloadKind, p.Workload}
			w := workloads[k]
			if w == nil {
				w = &wsum{pods: map[string]bool{}}
				workloads[k] = w
			}
			w.pods[p.Name] = true
			w.conns++
			pods[[2]string{p.Namespace, p.Name}] = true
			if p.Node != "" {
				nodes[p.Node] = true
			}
		}
	}
	for k, w := range workloads {
		r.Workloads = append(r.Workloads, &v1.ApplicationWorkload{Namespace: k.ns, Kind: k.kind, Name: k.name,
			Pods: int32(len(w.pods)), Connections: w.conns})
	}
	slices.SortFunc(r.Workloads, func(x, y *v1.ApplicationWorkload) int {
		return cmp.Or(cmp.Compare(y.GetConnections(), x.GetConnections()), cmp.Compare(x.GetNamespace(), y.GetNamespace()),
			cmp.Compare(x.GetKind(), y.GetKind()), cmp.Compare(x.GetName(), y.GetName()))
	})
	r.WorkloadsTotal = int32(len(r.Workloads))
	r.Workloads = r.Workloads[:min(len(r.Workloads), MaxRunsOnItems)]
	r.NodesTotal = int32(len(nodes))
	r.Nodes = slices.Sorted(maps.Keys(nodes))
	r.Nodes = r.Nodes[:min(len(r.Nodes), MaxRunsOnItems)]
	r.Pods = int32(len(pods))
	return r
}
