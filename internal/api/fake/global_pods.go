package fake

import (
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
)

// globalNamespaces are the namespaces of example-global's clients that
// run in its clusters; label-printer runs in the warehouses, outside.
var globalNamespaces = map[string]string{
	"api-gateway": "edge", "order-service": "shop", "checkout-service": "shop", "inventory-service": "shop",
	"shipping-service": "shipping", "search-api": "search", "notification-worker": "notify",
}

// globalClientPods is example-global's client pod watch. Each client
// runs three pods per cluster, as a Deployment (inventory-service as a
// StatefulSet), on three nodes of the cluster. shipping-service in ap
// connects from the carrier's site through NAT, so its addresses are no
// pod's; notification-worker in us runs on the host network, so its
// addresses are its nodes'.
func globalClientPods(sp serverSpec, client string, i int64) (*api.ClientPodFact, bool) {
	ns, ok := globalNamespaces[client]
	if !ok || (client == "shipping-service" && sp.cluster == "ap") {
		return nil, false
	}
	if client == "notification-worker" && sp.cluster == "us" {
		return nil, true
	}
	n := i % 3
	p := &api.ClientPodFact{Namespace: ns, Node: fmt.Sprintf("node-%s-%d", sp.cluster, n+1), WorkloadKind: "Deployment", Workload: client,
		Name: fmt.Sprintf("%s-6b7c9-%s%d", client, sp.cluster, n)}
	if client == "inventory-service" {
		p.WorkloadKind, p.Name = "StatefulSet", fmt.Sprintf("%s-%s-%d", client, sp.cluster, n)
	}
	return p, false
}
