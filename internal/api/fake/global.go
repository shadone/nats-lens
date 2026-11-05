package fake

import (
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
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

// The order events of example-global: messages of orderMsgSize bytes that
// order-events stores at orderEventsRate a second, 1.1 GiB a day, which its
// mirror order-archive, made orderArchiveDays ago, grows by.
const (
	orderMsgSize     = 1200
	orderEventsRate  = 1.1 * gib / orderMsgSize / 86400
	orderArchiveDays = 48
)

// orderEventsCreated is when order-events was created, in the fixed frame:
// orderEventsAge days ago, long enough to have reached its 30-day limit.
const orderEventsAge = 77

var orderEventsCreated = FixedNow.Add(-orderEventsAge * day)

// orderMsgs is how many order events are stored in days.
func orderMsgs(days float64) int64 { return int64(orderEventsRate * days * 86400) }

// globalDown is when nats-us-4 stopped answering, in the fixed frame.
var globalDown = fixed(9, 12, 5)

// globalVanished is when restock-alerts and stock-audit-export were last
// held, in the fixed frame: nats-us-2's new process started then, and
// from its first collection on no server reports them.
var globalVanished = fixed(8, 58, 30)

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
		sp := globalServer("eu", i, x.conns, x.cpu, x.store)
		if i == 3 {
			// More subscriptions than the collector keeps: its list is cut.
			sp.subs = 14212
		}
		out = append(out, sp)
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
			{name: "order-jobs", msgs: 182044, bytes: 230 * mib, perDay: 9 * mib, maxAge: 7 * day, maxMsgs: 1_000_000, consumers: []string{"resize-worker", "index-worker"}, peers: peers("eu-0", "eu-1", "eu-2")},
			// At its 30-day age limit: it stores orderEventsRate messages a
			// second and drops as many, so it does not grow.
			{name: "order-events", msgs: orderMsgs(30), bytes: float64(orderMsgs(30)) * orderMsgSize,
				maxAge: 30 * day, maxBytes: 64 * gib, storedRate: ptr(orderEventsRate), created: orderEventsCreated, lastSeq: orderMsgs(orderEventsAge),
				consumers: []string{"order-projector", "email-sender", "analytics-aggregator", "invoice-renderer"}, peers: peers("eu-3", "eu-4", "eu-5")},
			{name: "payment-events", msgs: 2210980, bytes: 2.6 * gib, perDay: 74 * mib, maxAge: 90 * day, held: 40 * day, consumers: []string{"ledger-writer", "fraud-scorer"}, peers: peers("eu-1", "eu-6", "eu-7")},
			{name: "cart-events", msgs: 4120330, bytes: 1.9 * gib, perDay: 120 * mib, retention: "interest", maxAge: 14 * day, held: 3 * day, consumers: []string{"cart-abandon-notifier"}, peers: peers("eu-2", "eu-5", "eu-8")},
			// Two durables no server held after nats-us-2 restarted, with
			// no delete event.
			{name: "inventory-changes", msgs: 1880220, bytes: 1.4 * gib, perDay: 40 * mib, maxAge: 30 * day, consumers: []string{"stock-projector", "reorder-planner"}, peers: peers("us-0", "us-1", "us-2"),
				vanished: []vanishedConsumer{{"restock-alerts", globalVanished}, {"stock-audit-export", globalVanished}}},
			{name: "shipping-updates", msgs: 640118, bytes: 820 * mib, perDay: 22 * mib, retention: "workqueue", maxMsgs: 1_000_000, held: 2 * day, consumers: []string{"label-printer", "tracking-notifier"}, peers: peers("us-3", "us-4", "us-5")},
			{name: "catalog-changes", msgs: 512880, bytes: 610 * mib, perDay: 8 * mib, maxAge: 30 * day, consumers: []string{"catalog-applier", "catalog-search-indexer"}, peers: peers("us-6", "us-7", "us-8")},
			// At its message limit with discard new: it refuses new prices.
			{name: "price-changes", msgs: 96410, bytes: 140 * mib, maxMsgs: 96_500, discard: "new", held: 45 * day, consumers: []string{"price-cache-invalidator"}, peers: peers("us-2", "us-4", "us-7")},
			{name: "search-index", msgs: 3301200, bytes: 4.8 * gib, perDay: 160 * mib, sources: []string{"catalog-changes", "price-changes"}, maxAge: 30 * day, held: 21 * day, consumers: []string{"search-indexer"}, peers: peers("ap-0", "ap-1", "ap-2"),
				behind: "nats-ap-1", lag: 420},
			// nats-ap-4 kept max_age 7d when the stream was edited to 3d.
			{name: "customer-notifications", msgs: 1402200, bytes: 1.1 * gib, perDay: 36 * mib, maxAge: 3 * day, consumers: []string{"push-sender", "sms-sender"}, peers: peers("ap-2", "ap-3", "ap-4"),
				disagrees: "nats-ap-4"},
			// A mirror of order-events that keeps everything since it was
			// made orderArchiveDays ago, so it grows by what order-events
			// stores; stream-005 is muted while its retention is decided
			// (globalSeedArchiveMute).
			{name: "order-archive", msgs: orderMsgs(orderArchiveDays), bytes: float64(orderMsgs(orderArchiveDays)) * orderMsgSize,
				perDay: orderEventsRate * 86400 * orderMsgSize, mirror: "order-events", held: orderArchiveDays * day, consumers: []string{"archive-exporter"},
				created: FixedNow.Add(-orderArchiveDays * day), lastSeq: orderMsgs(orderEventsAge),
				peers: peers("ap-2", "ap-5", "ap-1")},
			// nats-us-3 came back from the upgrade without its replica.
			{name: "return-requests", msgs: 84120, bytes: 96 * mib, perDay: 2 * mib, retention: "workqueue", maxAge: 7 * day, held: 2 * day, consumers: []string{"refund-issuer"}, peers: peers("us-6", "us-3", "us-8"),
				missing: "nats-us-3"},
			// Its leader stepped down and the newest epoch has none yet; a
			// second epoch without one would fire stream-001.
			{name: "basket-events", msgs: 220400, bytes: 180 * mib, perDay: 5 * mib, maxAge: day, peers: peers("eu-6", "eu-7", "eu-8"), leaderless: true},
			// Sessions expire by their per-key TTL faster than new ones start
			// since the weekend sale ended: the bucket shrinks.
			{name: "KV_session-cache", msgs: 48210, bytes: 38 * mib, perDay: -1 * mib, ephemeral: 64, held: 9 * day, peers: peers("eu-0", "eu-3", "eu-6"),
				storedRate: ptr(14.2), watchers: []string{"checkout-service", "api-gateway"}},
			// Few flags that changed rarely until a release began writing
			// flags per user (globalFlagsChange); its series bends there.
			{name: "KV_feature-flags", msgs: 21200, bytes: globalFlagsNow, perDay: globalFlagsRate, ephemeral: 22, held: 200 * day, peers: peers("us-0", "us-5", "us-8"),
				watchers: []string{"api-gateway", "checkout-service", "search-api", "order-service"}},
			{name: "OBJ_product-images", msgs: 120400, bytes: 9.6 * gib, perDay: 80 * mib, held: 400 * day, peers: peers("ap-0", "ap-3", "ap-5")},
			// Written by last week's checkout load test; nothing reads it.
			{name: "checkout-load-test", subjects: []string{"loadtest.checkout.>"}, msgs: 1_250_000, bytes: 610 * mib, maxAge: 14 * day,
				held: 6 * day, storedRate: ptr(0.0), peers: peers("eu-6", "eu-7", "eu-8")},
			// Low-stock alerts, kept only while inventory-service listens
			// through an ephemeral push consumer.
			{name: "stock-alerts", retention: "interest", msgs: 3, bytes: 2 * kib, maxAge: day, held: 40 * time.Second, ephemeral: 1,
				watchers: []string{"inventory-service"}, storedRate: ptr(0.6), peers: peers("eu-3", "eu-4", "eu-5")},
			// A work queue its issuer keeps empty.
			{name: "gift-card-jobs", subjects: []string{"jobs.gift-card.*"}, retention: "workqueue", maxAge: day, maxMsgs: 100_000,
				consumers: []string{"gift-card-issuer"}, storedRate: ptr(0.2), peers: peers("eu-0", "eu-1", "eu-2")},
		},
		buckets: []bucketSpec{
			// Listed with per-message TTLs and no other limit: its writers bound
			// it, so stream-005 leaves it out.
			{stream: "KV_session-cache", description: "Checkout sessions", storage: "memory", allowMsgTTL: true, keys: 41870},
			{stream: "KV_feature-flags", description: "Feature flags per region and user", history: 10, keys: 20400},
			{stream: "KV_label-templates", missing: true},
			{stream: "OBJ_product-images", description: "Product photos", maxBytes: 64 * gib, compression: "s2"},
		},
		clients: []string{
			"api-gateway", "order-service", "checkout-service", "inventory-service", "shipping-service",
			"search-api", "notification-worker", "label-printer",
		},
		// shipping-service in ap runs at the carrier's site, far from the
		// servers.
		rtt: func(sp serverSpec, client string) time.Duration {
			if client == "shipping-service" && sp.cluster == "ap" {
				return 140 * time.Millisecond
			}
			return 0
		},
		// shipping-service in ap has not taken the new client library yet.
		library: func(sp serverSpec, client string) (string, string) {
			if client == "shipping-service" && sp.cluster == "ap" {
				return "nats.js", "2.29.3"
			}
			return "", ""
		},
		subs:       globalSubs(),
		micro:      globalMicro(),
		microAsked: []string{Account, SuppliersAccount},
		// One collection's $SRV requests to APP timed out.
		microTimeouts: []time.Time{fixed(11, 29, 56)},
		// nats-lens's own connection to the system account, and a
		// monitoring tool that listens to every system event.
		extraConns: []extraConn{
			{server: "nats-us-0", name: "natslens-" + GlobalSystem, account: "SYS", ip: "10.30.9.40", user: "natslens", cid: 900,
				subjects: []string{"$SYS.ACCOUNT.>", "$SYS.SERVER.>", api.InboxPrefix + "NATSLENSGLOBAL.*"}, since: fixed(9, 12, 4)},
			{server: "nats-eu-0", name: "ops-monitor", account: "SYS", ip: "10.30.9.52", user: "ops-monitor", cid: 901, subjects: []string{">"}},
		},
		apiRate:    4.6,
		apiErrors:  0.0013,
		metaLeader: "nats-eu-1",
		auth:       globalAuth(),
		links:      globalLinks(),
		serverDown: globalDownAt,
		archive:    demoArchive(90, 30),
		// Four days before, one collection timed out on every server and
		// the next ones waited: no collection for five minutes.
		noEpochs:      []hole{{from: time.Date(2026, 10, 13, 2, 10, 26, 0, time.UTC), to: time.Date(2026, 10, 13, 2, 15, 26, 0, time.UTC)}},
		collection:    v1.CollectionState_COLLECTION_STATE_DEMO,
		routineClient: "search-api",
		findings:      globalFindings,
		events:        globalEvents,
		rows:          globalRows,
		kubernetes:    true,
		sample:        globalSample,
		slowEvents:    globalSlowEvents(),
		mutes: func() []*storedMute {
			return []*storedMute{globalSeedMute(), globalSeedEndedAcceptance(), globalSeedArchiveMute(), globalSeedClientMute()}
		},
		consumer:    globalConsumer,
		created:     globalCreated,
		transitions: globalTransitions,
		infoUntil:   map[string]time.Time{"server-001": globalPlannedEnd, "stream-002": globalPlannedEnd, "stream-011": globalPlannedEnd},
		gone:        globalGone,
		// nats-us-1 led two consumers of inventory-changes until its restart.
		initialLeaders: map[string]string{
			Account + "/inventory-changes/stock-projector": "nats-us-1",
			Account + "/inventory-changes/reorder-planner": "nats-us-1",
		},
	}
}

// globalUpgrade is the us node pool upgrade, in the fixed frame, as the
// traffic series draw it; its window comes from the Kubernetes events.
var globalUpgrade = [2]time.Time{fixed(8, 40, 0), fixed(9, 20, 30)}

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

// globalFlagsChange is when a release began writing feature flags per
// user, in the fixed frame: KV_feature-flags grew 2 KiB a day before and
// 1.6 MiB a day since.
var globalFlagsChange = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

// KV_feature-flags' bytes now, its growth per day since
// globalFlagsChange and before it.
const (
	globalFlagsNow    = 10.8 * mib
	globalFlagsRate   = 1.6 * mib
	globalFlagsBefore = 2 * kib
)

// globalFlagsEnded is when the review ended the acceptance of
// KV_feature-flags, in the fixed frame: the first collection whose week's
// growth (api.WeeklyGrowth) differs from the recorded +2.0 KiB/day by more
// than the 1 MiB floor's half.
var globalFlagsEnded = time.Date(2026, 10, 13, 16, 24, 0, 0, time.UTC)

// globalFlagsBytes is KV_feature-flags' bytes at fixed-frame time c: its
// spec's growth back to globalFlagsChange, globalFlagsBefore before it.
func globalFlagsBytes(sp streamSpec, c time.Time) float64 {
	if !c.Before(globalFlagsChange) {
		return sp.bytes - FixedNow.Sub(c).Hours()/24*sp.perDay
	}
	at := sp.bytes - FixedNow.Sub(globalFlagsChange).Hours()/24*sp.perDay
	return at - globalFlagsChange.Sub(c).Hours()/24*globalFlagsBefore
}

// globalSeedEndedAcceptance is an acceptance of stream-005 on
// KV_feature-flags at its growth then, which the review ended at
// globalFlagsEnded with the end reason it writes from the bucket's series.
// stream-005 fires again since.
func globalSeedEndedAcceptance() *storedMute {
	s := scen{now: FixedNow}
	sp := streamSpec{bytes: globalFlagsNow, perDay: globalFlagsRate}
	g := reviewGrowth(func(c time.Time) (float64, bool) { return globalFlagsBytes(sp, c), true }, globalFlagsEnded)
	return &storedMute{relative: true, seededLift: true, m: &v1.Mute{
		Id: "2", CheckCode: "stream-005", Entity: streamRef("KV_feature-flags"), Accepted: true, GrowthPerDay: ptr(float64(globalFlagsBefore)),
		Reason:    "Flags are few and change rarely.",
		MutedBy:   "ops@example.com",
		CreatedAt: ts(s.on(1, 9, 0)), ExpiresAt: ts(s.on(1, 9, 0).AddDate(0, 3, 0)),
		UnmutedAt: ts(globalFlagsEnded.Add(s.shift)), UnmutedBy: api.AcceptEndedBy,
		EndReason: g.EndReason(globalFlagsBefore),
	}}
}

// globalSeedArchiveMute mutes stream-005 on order-archive while its
// retention is decided.
func globalSeedArchiveMute() *storedMute {
	s := scen{now: FixedNow}
	return &storedMute{relative: true, m: &v1.Mute{
		Id: "3", CheckCode: "stream-005", Entity: streamRef("order-archive"),
		Reason:    "Retention of the order archive is decided with finance by mid-November.",
		MutedBy:   "ops@example.com",
		CreatedAt: ts(s.on(15, 10, 0)), ExpiresAt: ts(time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC).Add(s.shift)),
	}}
}

// globalSeedClientMute mutes client-001 on shipping-service: its ap
// instances run at the carrier's site.
func globalSeedClientMute() *storedMute {
	s := scen{now: FixedNow}
	return &storedMute{relative: true, m: &v1.Mute{
		Id: "4", CheckCode: "client-001", Entity: clientRef("shipping-service"),
		Reason:    "The ap instances run at the carrier's site; 140 ms is expected there.",
		MutedBy:   "ops@example.com",
		CreatedAt: ts(s.on(16, 9, 30)), ExpiresAt: ts(s.on(31, 0, 0)),
	}}
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
