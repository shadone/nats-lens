package fake

import (
	"fmt"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// example-global is 24 servers in three clusters, eu, us and ap, joined by
// gateways. A GKE node upgrade of the us pool (08:40-09:20) restarted
// nats-us-1 to nats-us-3; nats-us-4 was stopped at 09:12 and its pod has
// stayed Pending since, so two streams run on two of three replicas. In
// ap, nats-ap-2's store is 71% full, and nats-ap-4 and nats-ap-5 still run
// the older version; nats-ap-4 also missed the edit of
// customer-notifications at 13:05. nats-us-3 came back from the upgrade
// without its replica of return-requests, and basket-events has no
// leader in the newest epoch. Since 13:38 label-printer has been dropped
// as a slow consumer on nats-us-3.

// Versions of example-global: the majority's and the older one.
const (
	globalVersion = "2.12.4"
	globalOlder   = "2.12.2"
)

// globalDown is when nats-us-4 stopped answering, in the fixed frame.
var globalDown = fixed(9, 12, 5)

// globalServer builds one server: up since 2026-10-11 unless restarted
// on 2026-10-17 at h:m:s.
func globalServer(cluster string, i int, conns int64, cpu float64, storePct float64) serverSpec {
	name := fmt.Sprintf("nats-%s-%d", cluster, i)
	sp := serverSpec{
		name: name, id: fmt.Sprintf("N%sFAKE%dGLOBAL", cluster, i), oldID: fmt.Sprintf("N%sFAKE%dOLDGLOBAL", cluster, i),
		day: 11, h: 6, m: 10 + i, s: 0, conns: conns, cluster: cluster, version: globalVersion,
		maxStore: 100 * gib, store: int64(storePct / 100 * 100 * gib), mem: int64(100+7*i) * mib, cpu: cpu,
		tags: []string{fmt.Sprintf("az:%s-1%c", cluster, 'a'+i%3), "region:" + cluster},
		node: fmt.Sprintf("gke-example-pool-%s-%x", cluster, 0x3a10+0x1d3*i),
	}
	if cluster == "ap" && i >= 4 {
		sp.version = globalOlder
	}
	return sp
}

func globalServers() []serverSpec {
	var out []serverSpec
	eu := []struct {
		conns int64
		cpu   float64
		store float64
	}{{61, 3, 35}, {64, 4, 37}, {61, 5, 36}, {61, 22, 41}, {61, 3, 33}, {61, 6, 36}, {61, 4, 38}, {61, 2, 29}, {61, 5, 39}}
	for i, x := range eu {
		out = append(out, globalServer("eu", i, x.conns, x.cpu, x.store))
	}
	us := []struct {
		conns int64
		cpu   float64
		store float64
	}{{52, 6, 31}, {50, 4, 30}, {50, 8, 33}, {50, 34, 32}, {49, 5, 28}, {50, 5, 31}, {50, 3, 27}, {50, 7, 34}, {50, 4, 29}}
	for i, x := range us {
		sp := globalServer("us", i, x.conns, x.cpu, x.store)
		// The upgrade moved nats-us-1 to nats-us-3 to upgraded nodes;
		// nats-us-4's pod has found none.
		switch i {
		case 1:
			sp.day, sp.h, sp.m, sp.s, sp.node = 0, 8, 46, 10, "gke-example-pool-us-17bd"
		case 2:
			sp.day, sp.h, sp.m, sp.s, sp.node = 0, 8, 58, 30, "gke-example-pool-us-a904"
		case 3:
			sp.day, sp.h, sp.m, sp.s, sp.node = 0, 9, 6, 40, "gke-example-pool-us-5e21"
		case 4:
			sp.down, sp.podPending = globalDown, true
		}
		out = append(out, sp)
	}
	ap := []struct {
		conns int64
		cpu   float64
		store float64
	}{{33, 9, 48}, {31, 12, 52}, {31, 71, 71}, {31, 8, 50}, {31, 11, 53}, {31, 7, 47}}
	for i, x := range ap {
		sp := globalServer("ap", i, x.conns, x.cpu, x.store)
		if i == 3 {
			// Configured apart from the others.
			sp.maxPayload = 8 * mib
		}
		out = append(out, sp)
	}
	return placeGlobalSlow(out)
}

// placeGlobalSlow puts each of globalSlowEvents on a server: the burst
// from 13:38 and the two after it on nats-us-3, where label-printer
// connects; the three before it on nats-eu-3, nats-ap-2 and nats-us-0.
func placeGlobalSlow(servers []serverSpec) []serverSpec {
	ev := globalSlowEvents()
	on := map[string][]time.Time{"nats-eu-3": ev[0:1], "nats-ap-2": ev[1:2], "nats-us-0": ev[2:3], "nats-us-3": ev[3:]}
	for i := range servers {
		servers[i].slow = append([]time.Time{}, on[servers[i].name]...)
	}
	return servers
}

// globalScenario is example-global.
func globalScenario() *scenario {
	peers := func(names ...string) []string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = "nats-" + n
		}
		return out
	}
	return &scenario{
		// The cluster a server reports, as the store provider names a
		// system: the meta leader's.
		cluster: "eu",
		servers: globalServers(),
		streams: []streamSpec{
			{name: "order-jobs", msgs: 182044, bytes: 230 * mib, perDay: 9 * mib, consumers: []string{"resize-worker", "index-worker"}, peers: peers("eu-0", "eu-1", "eu-2")},
			{name: "order-events", msgs: 9204411, bytes: 11.2 * gib, perDay: 310 * mib,
				consumers: []string{"order-projector", "email-sender", "analytics-aggregator", "invoice-renderer"}, peers: peers("eu-3", "eu-4", "eu-5")},
			{name: "payment-events", msgs: 2210980, bytes: 2.6 * gib, perDay: 74 * mib, consumers: []string{"ledger-writer", "fraud-scorer"}, peers: peers("eu-1", "eu-6", "eu-7")},
			{name: "cart-events", msgs: 4120330, bytes: 1.9 * gib, perDay: 120 * mib, consumers: []string{"cart-abandon-notifier"}, peers: peers("eu-2", "eu-5", "eu-8")},
			{name: "inventory-changes", msgs: 1880220, bytes: 1.4 * gib, perDay: 40 * mib, consumers: []string{"stock-projector", "reorder-planner"}, peers: peers("us-0", "us-1", "us-2")},
			{name: "shipping-updates", msgs: 640118, bytes: 820 * mib, perDay: 22 * mib, consumers: []string{"label-printer", "tracking-notifier"}, peers: peers("us-3", "us-4", "us-5")},
			{name: "catalog-changes", msgs: 512880, bytes: 610 * mib, perDay: 8 * mib, consumers: []string{"catalog-applier", "catalog-search-indexer"}, peers: peers("us-6", "us-7", "us-8")},
			{name: "price-changes", msgs: 96410, bytes: 140 * mib, perDay: 3 * mib, consumers: []string{"price-cache-invalidator"}, peers: peers("us-2", "us-4", "us-7")},
			{name: "search-index", msgs: 3301200, bytes: 4.8 * gib, perDay: 160 * mib, consumers: []string{"search-indexer"}, peers: peers("ap-0", "ap-1", "ap-2"),
				behind: "nats-ap-1", lag: 420},
			// nats-ap-4 kept max_age 7d when the stream was edited to 3d.
			{name: "customer-notifications", msgs: 1402200, bytes: 1.1 * gib, perDay: 36 * mib, consumers: []string{"push-sender", "sms-sender"}, peers: peers("ap-2", "ap-3", "ap-4"),
				disagrees: "nats-ap-4"},
			{name: "order-archive", msgs: 61200400, bytes: 52.4 * gib, perDay: 1.1 * gib, consumers: []string{"archive-exporter"}, peers: peers("ap-2", "ap-5", "ap-1")},
			// nats-us-3 came back from the upgrade without its replica.
			{name: "return-requests", msgs: 84120, bytes: 96 * mib, perDay: 2 * mib, consumers: []string{"refund-issuer"}, peers: peers("us-6", "us-3", "us-8"),
				missing: "nats-us-3"},
			// Its leader stepped down and the newest epoch has none yet; a
			// second epoch without one would fire stream-001.
			{name: "basket-events", msgs: 220400, bytes: 180 * mib, perDay: 5 * mib, peers: peers("eu-6", "eu-7", "eu-8"), leaderless: true},
			// Sessions expire by their per-key TTL faster than new ones start
			// since the weekend sale ended: the bucket shrinks.
			{name: "KV_session-cache", msgs: 48210, bytes: 38 * mib, perDay: -1 * mib, ephemeral: 64, peers: peers("eu-0", "eu-3", "eu-6")},
			{name: "KV_feature-flags", msgs: 412, bytes: 210 * kib, perDay: 2 * kib, ephemeral: 22, peers: peers("us-0", "us-5", "us-8")},
			{name: "OBJ_product-images", msgs: 120400, bytes: 9.6 * gib, perDay: 80 * mib, peers: peers("ap-0", "ap-3", "ap-5")},
		},
		buckets: []bucketSpec{
			{stream: "KV_session-cache", description: "Checkout sessions", storage: "memory", maxBytes: 512 * mib, allowMsgTTL: true, keys: 41870},
			{stream: "KV_feature-flags", description: "Feature flags per region", history: 10, keys: 38},
			{stream: "KV_label-templates", missing: true},
			{stream: "OBJ_product-images", description: "Product photos", maxBytes: 64 * gib, compression: "s2"},
		},
		clients: []string{
			"api-gateway", "order-service", "checkout-service", "inventory-service", "shipping-service",
			"search-api", "notification-worker", "label-printer",
		},
		metaLeader: "nats-eu-1",
		down: func(sc *scenario, c time.Time) bool {
			return slices.ContainsFunc(sc.servers, func(sp serverSpec) bool { return globalDownAt(sp, c) })
		},
		collection:    v1.CollectionState_COLLECTION_STATE_DEMO,
		routineClient: "search-api",
		findings:      globalFindings,
		events:        globalEvents,
		rows:          globalRows,
		windows:       globalWindows,
		k8sLanes:      globalK8sLanes,
		sample:        globalSample,
		slowEvents:    globalSlowEvents(),
		mutes:         func() []*storedMute { return []*storedMute{globalSeedMute()} },
		consumer:      globalConsumer,
		created:       globalCreated,
		transitions:   globalTransitions,
		infoUntil:     map[string]time.Time{"server-001": globalPlannedEnd, "stream-002": globalPlannedEnd, "stream-011": globalPlannedEnd},
	}
}

// globalUpgrade is the us node pool upgrade, in the fixed frame.
var globalUpgrade = [2]time.Time{fixed(8, 40, 0), fixed(9, 20, 30)}

func globalWindows(s scen, r *v1.TimeRange) []*v1.TimeWindow {
	a, b := globalUpgrade[0].Add(s.shift), globalUpgrade[1].Add(s.shift)
	if !overlaps(a, b, r) {
		return nil
	}
	return []*v1.TimeWindow{{Start: ts(a), End: ts(b), Label: "GKE node upgrade",
		Detail: "4 drains, 4 pod restarts"}}
}

// globalInvoiceCreated is when invoice-renderer was created on
// order-events, in the fixed frame: the newest epoch is the first to
// report it, so it has no delivery rate yet.
var globalInvoiceCreated = fixed(14, 29, 20)

// globalConsumer gives label-printer the backlog behind the slow consumer
// finding, pauses reorder-planner between its nightly runs, and leaves the
// new invoice-renderer without deliveries.
func globalConsumer(s scen, c *v1.ConsumerSummary) {
	if !c.GetPresent() {
		return
	}
	switch c.GetEntity().GetConsumer() {
	case "label-printer":
		c.NumPending, c.NumAckPending, c.NumRedelivered = ptr(int64(6210)), ptr(int64(1000)), ptr(int64(212))
	case "reorder-planner":
		c.Paused, c.LastDelivered = true, ts(s.at(2, 30, 0))
	case "invoice-renderer":
		// Created with deliver policy new: nothing waits yet.
		c.NumPending, c.NumAckPending, c.LastDelivered = ptr(int64(0)), ptr(int64(0)), nil
	}
}

// globalCreated gives invoice-renderer its creation time on every peer.
func globalCreated(s scen, _, consumer string) ([]time.Time, bool) {
	if consumer != "invoice-renderer" {
		return nil, false
	}
	at := globalInvoiceCreated.Add(s.shift)
	return []time.Time{at, at, at}, false
}

func globalSeedMute() *storedMute {
	s := scen{now: FixedNow}
	return &storedMute{relative: true, m: &v1.Mute{
		Id: "1", CheckCode: "server-005",
		Reason:    "The ap pool moves to 2.12.4 in next week's maintenance window.",
		MutedBy:   "ops",
		CreatedAt: ts(s.on(14, 11, 0)), ExpiresAt: ts(s.on(28, 0, 0)),
	}}
}
