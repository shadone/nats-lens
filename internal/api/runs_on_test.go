package api_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Runs on sums the matched connections by workload and node, counts the
// ambiguous apart, and is unset while client pods are not watched.
func TestFinishApplicationsRunsOn(t *testing.T) {
	pod := func(ns, name, node, kind, workload string) *api.ClientPodFact {
		return &api.ClientPodFact{Namespace: ns, Name: name, Node: node, WorkloadKind: kind, Workload: workload}
	}
	conns := []api.AppConnectionFact{
		appConn("nats-0", 1, "cart", "10.4.0.1"), appConn("nats-1", 2, "cart", "10.4.0.1"),
		appConn("nats-0", 3, "cart", "10.4.0.2"), appConn("nats-2", 4, "cart", "10.4.0.3"),
		appConn("nats-0", 5, "cart", "10.9.0.9"), appConn("nats-1", 6, "cart", "192.168.1.5"),
		appConn("nats-1", 7, "ledger", "10.5.0.1"),
	}
	conns[0].Pod = pod("shop", "cart-a", "node-1", "Deployment", "cart")
	conns[1].Pod = conns[0].Pod
	conns[2].Pod = pod("shop", "cart-b", "node-2", "Deployment", "cart")
	conns[3].Pod = pod("shop", "cart-sync-0", "node-2", "StatefulSet", "cart-sync")
	conns[4].PodAmbiguous = true
	f := &api.ApplicationFacts{Epoch: 3, EpochAt: appT0, Connections: conns}
	res := api.FinishApplications(f, "", false)
	if res.GetClientPodsWatched() || appByName(res, "cart").GetRunsOn() != nil {
		t.Fatalf("runs on without the watch: %v", appByName(res, "cart").GetRunsOn())
	}
	gap := &v1.ClientPodWatchGap{Source: "client pods in shop", Since: nil, Error: "Kubernetes API returned HTTP 403"}
	f.ClientPodsWatched, f.ClientPodsUnwatched = true, []*v1.ClientPodWatchGap{gap}
	res = api.FinishApplications(f, "", false)
	if !res.GetClientPodsWatched() || len(res.GetClientPodsUnwatched()) != 1 {
		t.Fatalf("watched %v, unwatched %v", res.GetClientPodsWatched(), res.GetClientPodsUnwatched())
	}
	r := appByName(res, "cart").GetRunsOn()
	if r.GetMatched() != 4 || r.GetAmbiguous() != 1 || r.GetPods() != 3 || r.GetNodesTotal() != 2 || !slices.Equal(r.GetNodes(), []string{"node-1", "node-2"}) {
		t.Fatalf("cart runs on %v", r)
	}
	w := r.GetWorkloads()
	if r.GetWorkloadsTotal() != 2 || len(w) != 2 || w[0].GetKind() != "Deployment" || w[0].GetName() != "cart" || w[0].GetPods() != 2 ||
		w[0].GetConnections() != 3 || w[1].GetName() != "cart-sync" || w[1].GetConnections() != 1 {
		t.Fatalf("cart workloads %v", w)
	}
	// An application none of whose connections matched has an empty Runs on.
	if l := appByName(res, "ledger").GetRunsOn(); l == nil || l.GetMatched() != 0 || len(l.GetWorkloads()) != 0 {
		t.Fatalf("ledger runs on %v", l)
	}
}

// Workloads and nodes are listed up to the limit, with their totals.
func TestFinishApplicationsRunsOnLimit(t *testing.T) {
	var conns []api.AppConnectionFact
	for i := range 12 {
		c := appConn("nats-0", int64(i), "batch", fmt.Sprintf("10.4.0.%d", i))
		c.Pod = &api.ClientPodFact{Namespace: "jobs", Name: fmt.Sprintf("p%d", i), Node: fmt.Sprintf("node-%02d", i), WorkloadKind: "Job", Workload: fmt.Sprintf("j%02d", i)}
		conns = append(conns, c)
	}
	res := api.FinishApplications(&api.ApplicationFacts{Epoch: 1, EpochAt: appT0, Connections: conns, ClientPodsWatched: true}, "", false)
	r := appByName(res, "batch").GetRunsOn()
	if len(r.GetWorkloads()) != api.MaxRunsOnItems || r.GetWorkloadsTotal() != 12 || len(r.GetNodes()) != api.MaxRunsOnItems || r.GetNodesTotal() != 12 ||
		r.GetWorkloads()[0].GetName() != "j00" || r.GetNodes()[9] != "node-09" {
		t.Fatalf("runs on %v", r)
	}
}
