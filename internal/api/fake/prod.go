package fake

import (
	"fmt"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// prodServer is one of example-prod's three servers.
func prodServer(name, id, oldID string, h, m, s int, conns int64) serverSpec {
	return serverSpec{name: name, id: id, oldID: oldID, h: h, m: m, s: s, conns: conns, cluster: Cluster, version: Version,
		store: 1980 * mib, maxStore: 50 * gib, maxMemStore: gib, mem: 412 * mib, cpu: 7.5}
}

// prodScenario is example-prod: the reference scenario.
func prodScenario() *scenario {
	return &scenario{
		cluster: Cluster,
		archive: demoArchive(90, 90),
		servers: []serverSpec{
			// The clients that left nats-1 and nats-2 during the upgrade
			// stayed on nats-0, which came back first: it holds most of
			// them.
			prodServer("nats-0", "NDQ4ZFAKE0SERVER0", "NB71FAKE0OLDPROCESS", 9, 18, 0, 98),
			prodServer("nats-1", "NC2XFAKE1SERVER1", "NA09FAKE1OLDPROCESS", 9, 31, 2, 42),
			prodServer("nats-2", "NE5KFAKE2SERVER2", "NF33FAKE2OLDPROCESS", 9, 44, 21, 42),
		},
		streams: []streamSpec{
			{name: "order-jobs", msgs: 48211, bytes: 61.2 * mib, perDay: 2.1 * mib, maxAge: 7 * day, maxMsgs: 1_000_000, held: 4*day + 6*time.Hour,
				consumers: []string{"resize-worker", "index-worker", "job-cache-projector"}},
			{name: "order-archive", msgs: 2914330, bytes: 3.92 * gib, perDay: 96 * mib, held: 300 * day,
				consumers: []string{"archive-exporter", "archive-indexer"}, expected: []string{"analytics-aggregator"},
				vanished: []vanishedConsumer{{"analytics-aggregator", fixed(9, 31, 7)}}},
			{name: "audit-archive", msgs: 1108402, bytes: 1.07 * gib, perDay: 18 * mib, consumers: []string{"audit-archive-writer"},
				maxBytes: 1395864371, discard: "new", perSubject: 100_000_000, held: 200 * day},
			{name: "catalog-changes", msgs: 312880, bytes: 402 * mib, perDay: 6.4 * mib, maxAge: 30 * day,
				consumers: []string{"catalog-applier", "catalog-search-indexer"}},
			{name: "catalog-access", msgs: 640115, bytes: 188 * mib, perDay: 3.1 * mib, maxAge: 14 * day, consumers: []string{"catalog-access-logger"}},
			// A work queue its dispatcher keeps empty.
			{name: "order-callbacks", retention: "workqueue", maxAge: day, maxMsgs: 1_000_000, consumers: []string{"callback-dispatcher"},
				storedRate: ptr(1.8)},
			{name: "media-windows", msgs: 18420, bytes: 208 * mib, perDay: 71 * mib, retention: "interest", maxAge: 3 * day, held: 28 * time.Hour,
				consumers: []string{"media-chunk-pool", "media-window-finalizer"}, storedRate: ptr(9.4)},
			{name: "account-changes", msgs: 1207, bytes: 1.8 * mib, perDay: 20 * kib, maxMsgs: 100_000, held: 60 * day, consumers: []string{"customer-cache-invalidator"}},
			{name: "KV_customer-config", msgs: 214, bytes: 96 * kib, perDay: 1 * kib, ephemeral: 35, held: 200 * day,
				watchers: []string{"api-gateway", "catalog-api", "order-service"}},
			{name: "KV_worker-presence", msgs: 18, bytes: 4 * kib, perDay: 0, ephemeral: 6, peers: []string{"nats-1"},
				storedRate: ptr(0.4), watchers: []string{"media-worker", "video-transcoder"}},
			{name: "KV_order-jobs-cache", msgs: 3120, bytes: 2.4 * mib, perDay: 90 * kib},
			{name: "OBJ_image-input", msgs: 2214, bytes: 1.3 * gib, perDay: 40 * mib},
			{name: "OBJ_image-output", msgs: 5870, bytes: 3.1 * gib, perDay: 95 * mib},
		},
		buckets: []bucketSpec{
			// No TTL and no max_bytes: accepted as by design (prodSeedAcceptance).
			{stream: "KV_customer-config", description: "Per-customer settings", history: 5, keys: 41, compression: "s2"},
			{stream: "KV_worker-presence", description: "Live workers and their heartbeats", storage: "memory", ttl: 2 * time.Minute},
			{stream: "KV_order-jobs-cache", description: "Order job state for the API", ttl: 48 * time.Hour, maxBytes: 256 * mib, allowMsgTTL: true, keys: 3388},
			{stream: "OBJ_image-input", description: "Uploaded images awaiting resize", ttl: 48 * time.Hour, maxBytes: 8 * gib},
			{stream: "OBJ_image-output", description: "Resized images", ttl: 48 * time.Hour, maxBytes: 8 * gib},
		},
		clients: []string{
			"media-worker", "order-service", "analytics-worker", "ledger-service", "api-gateway",
			"catalog-api", "callback-dispatcher", "video-transcoder",
		},
		// resize-worker names each pod's connection; one pod waited for a
		// node until 11:22 (prodPending). A script connects without a
		// name.
		extraConns: []extraConn{
			{server: "nats-0", name: "resize-worker@resize-worker-6c9d4-7d9fq", account: Account, ip: "10.20.6.21", user: "resize-worker", cid: 950},
			{server: "nats-0", name: "resize-worker@resize-worker-6c9d4-k2p4m", account: Account, ip: "10.20.6.22", user: "resize-worker", cid: 951},
			{server: "nats-1", name: "resize-worker@resize-worker-6c9d4-m8w2z", account: Account, ip: "10.20.6.23", user: "resize-worker", cid: 952},
			{server: "nats-2", name: "resize-worker@resize-worker-6c9d4-x2k4l", account: Account, ip: "10.20.6.24", user: "resize-worker", cid: 953,
				since: fixed(11, 22, 30)},
			{server: "nats-1", name: "", account: Account, ip: "10.20.5.17", user: "order-service", cid: 954, since: fixed(10, 5, 0)},
		},
		subs:          prodSubs(),
		micro:         prodMicro(),
		apiRate:       2.2,
		apiErrors:     0,
		metaLeader:    "nats-0",
		auth:          prodAuth(),
		links:         prodLinks(),
		serverDown:    func(sp serverSpec, c time.Time) bool { return down(sp.name, c) },
		collection:    v1.CollectionState_COLLECTION_STATE_DEMO,
		routineClient: "catalog-api",
		findings:      prodFindings,
		events:        prodEvents,
		rows:          prodRows,
		kubernetes:    true,
		sample:        prodSample,
		slowEvents:    prodSlowConsumerEvents,
		mutes:         func() []*storedMute { return []*storedMute{prodSeedMute(), prodSeedAcceptance()} },
		acks:          func() []*storedAck { return []*storedAck{prodSeedAck()} },
		transitions:   prodTransitions,
		consumer:      prodConsumer,
		created:       prodCreated,
		// The leads are spread over the three servers; order-archive's
		// leader is nats-0, so that nats-2 takes it when nats-0 stops.
		spreadLeaders:  true,
		initialLeaders: map[string]string{Account + "/order-archive": "nats-0"},
	}
}

// prodConsumer gives resize-worker its disagreement, media-chunk-pool its
// backlog, and the missing analytics-aggregator its last delivery.
func prodConsumer(s scen, c *v1.ConsumerSummary) {
	name := c.GetEntity().GetConsumer()
	c.ServersDisagree = name == "resize-worker"
	switch {
	case !c.GetPresent():
		c.LastDelivered = ts(s.at(9, 31, 0))
	case name == "media-chunk-pool":
		c.NumPending, c.NumAckPending, c.NumRedelivered = ptr(int64(18420)), ptr(int64(640)), ptr(int64(37))
	}
}

// prodCreated gives resize-worker the creation time disagreement of
// finding consumer-009: nats-0 reports an earlier timestamp.
func prodCreated(s scen, _, consumer string) ([]time.Time, bool) {
	if consumer != "resize-worker" {
		return nil, false
	}
	return []time.Time{time.Date(2026, 8, 12, 10, 2, 11, 0, time.UTC).Add(s.shift), s.at(9, 38, 44), s.at(9, 38, 44)}, true
}

// prodTransitions gives the flapping consumer-002 its history, and
// client-003 on media-worker its firing and resolve.
func prodTransitions(s scen, f *v1.Finding) []*v1.FindingTransition {
	if f.GetCheckCode() == "client-003" && f.GetEntity().GetKey() == "media-worker" {
		// It fired with the third disconnect, and its detail then named
		// those three.
		return []*v1.FindingTransition{
			{FindingId: f.GetId(), Time: f.GetResolvedAt(), State: resolved, Severity: f.GetSeverity(), Detail: f.GetDetail()},
			{FindingId: f.GetId(), Time: f.GetSince(), State: firing, Severity: f.GetSeverity(), Detail: prodSlowDetail(3, "nats-1, nats-2", s.at(9, 17, 30))},
		}
	}
	if f.GetCheckCode() != "consumer-002" {
		return nil
	}
	tr := func(at time.Time, st v1.FindingState) *v1.FindingTransition {
		return &v1.FindingTransition{FindingId: f.GetId(), Time: ts(at), State: st, Severity: f.GetSeverity(), Detail: f.GetDetail()}
	}
	return []*v1.FindingTransition{
		tr(s.at(14, 12, 0), flapping), tr(s.at(14, 5, 0), resolved), tr(s.at(13, 48, 0), firing),
		tr(s.at(13, 41, 0), resolved), tr(s.at(11, 0, 0), firing), tr(s.at(8, 30, 0), resolved),
		tr(s.at(8, 15, 0), firing), tr(s.at(7, 45, 0), resolved), tr(s.at(7, 30, 0), firing),
	}
}

// prodSlowDetail is client-003's detail for media-worker: n disconnects
// on the servers, the last at last, a scenario time.
func prodSlowDetail(n int, servers string, last time.Time) string {
	return fmt.Sprintf("%d slow consumer disconnects in the last 30m (%s), the last at %s UTC: Slow Consumer (Write Deadline).", n, servers,
		last.UTC().Format("15:04:05"))
}

// prodSeedAcceptance accepts stream-005 on KV_customer-config as by
// design, with the bucket's growth when it was accepted and a review date
// six months later.
func prodSeedAcceptance() *storedMute {
	s := scen{now: FixedNow}
	return &storedMute{relative: true, m: &v1.Mute{
		Id: "2", CheckCode: "stream-005", Entity: streamRef("KV_customer-config"), Accepted: true, GrowthPerDay: ptr(1.0 * kib),
		Reason:    "Customer records are kept for the account's life.",
		MutedBy:   "ops@example.com",
		CreatedAt: ts(s.on(3, 9, 0)), ExpiresAt: ts(s.on(3, 9, 0).AddDate(0, 6, 0)),
	}}
}

// prodSeedAck acknowledges consumer-010 on analytics-aggregator, missing
// since the node upgrade, with a quiet of two hours.
func prodSeedAck() *storedAck {
	s := scen{now: FixedNow}
	return &storedAck{relative: true, since: s.at(9, 31, 7), a: &v1.Acknowledgement{
		Id: "1", FindingId: "consumer-010:" + Account + "/order-archive/analytics-aggregator",
		Note:    "Recreating the durable; the worker's drain fix is in review",
		AckedBy: "alice@example.com", CreatedAt: ts(s.at(13, 35, 0)), QuietUntil: ts(s.at(15, 35, 0)),
	}}
}

func prodSeedMute() *storedMute {
	s := scen{now: FixedNow}
	return &storedMute{relative: true, m: &v1.Mute{
		Id: "1", CheckCode: "stream-006", Entity: streamRef("audit-archive"),
		Reason:    "Retention cut to 90d is in review; growth after the backfill is expected.",
		MutedBy:   "alice",
		CreatedAt: ts(s.at(12, 0, 0)), ExpiresAt: ts(s.on(24, 12, 0)),
	}}
}
