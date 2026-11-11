package fake

import (
	"fmt"
	"slices"
	"strings"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// globalFindings is every finding of example-global, before mutes are
// applied.
func globalFindings(s scen) []*v1.Finding {
	us4 := finding("server-001", "Server not reporting", warning, firing, serverRef("nats-us-4"), s.at(9, 12, 35), s.at(9, 12, 35),
		"nats-us-4 answered no monitoring request; 1 of 24 expected servers missing; last answered 2026-10-17 09:12:05 UTC.")
	us4.Value = &v1.FindingValue{Name: "minutes since last answer", Display: "317.9", Number: ptr(317.9), Basis: "time since the server last answered"}
	us4.Remediation = "Its pod is Pending: no node of pool-us has room after the upgrade. Check why the pod does not schedule (`kubectl describe pod nats-us-4`). " +
		"The streams with a replica on it run on two of three until it returns."

	replica := func(stream, leader string) *v1.Finding {
		f := finding("stream-002", "Stream replica offline", warning, firing, streamRef(stream), s.at(9, 13, 0), s.at(9, 13, 0),
			fmt.Sprintf("Leader %s reports 1 replica offline: nats-us-4 (last seen 19075s ago).", leader))
		f.Value = &v1.FindingValue{Name: "replicas offline", Display: "1", Number: ptr(1.0), Basis: "replicas the leader reports offline"}
		f.RelatedConfig = cfg([][2]string{{"replicas", "3"}, {"placement.cluster", "us"}}, "replicas")
		f.RelatedConfigSource = "nats-us-3"
		return f
	}

	slow := finding("server-009", "Slow consumers", warning, firing, serverRef("nats-us-3"), s.at(13, 41, 0), s.at(13, 41, 0),
		"nats-us-3: 2 slow consumers in the last 15m (2 clients, 0 routes, gateways or leaf nodes).")
	slow.Value = &v1.FindingValue{Name: "slow consumers", Display: "2", Number: ptr(2.0), Basis: "growth of the slow consumer counter within the lookback"}
	slow.Remediation = "label-printer reads slower than shipping-updates delivers to it on nats-us-3. " +
		"Raise its pending limits or add workers; the backlog on its consumer grows meanwhile."

	backlog := finding("consumer-002", "Consumer backlog growing", warning, firing, consumerRef("shipping-updates", "label-printer"),
		s.at(13, 40, 0), s.at(13, 40, 0), "Fill 24.5 msg/s, drain 21.4 msg/s over 00:30:00. 6210 pending, up 5580 since 14:00 UTC.")
	backlog.Value = &v1.FindingValue{Name: "backlog growth, messages per second", Display: "3.1", Number: ptr(3.1),
		Basis: "growth of num_pending over the lookback"}

	// An R1 bucket whose only replica was on nats-us-4.
	labels := finding("stream-011", "Expected stream or bucket missing", warning, firing, streamRef("KV_label-templates"),
		s.at(9, 12, 40), s.at(9, 12, 40), "KV bucket label-templates (stream KV_label-templates) is in the inventory and no server reports it; "+
			"last reported by nats-us-4 at 2026-10-17 09:11:35 UTC (may be on nats-us-4, which did not answer). No delete event recorded.")
	labels.RelatedConfig = cfg([][2]string{{"kind", "kv"}, {"replicas", "1"}, {"max_age", "0"}, {"max_bytes", "67108864"}, {"max_msgs", "-1"}}, "replicas")
	labels.RelatedConfigSource = "inventory"
	labels.Remediation = "Bring nats-us-4 back: its pod is Pending, and the bucket's only replica is on it. " +
		"Then give the bucket more than one replica, so that one server cannot take it away."

	full := finding("server-014", "JetStream store full soon", warning, firing, serverRef("nats-ap-2"), s.at(12, 6, 0), s.at(12, 6, 0),
		"nats-ap-2 file store grows 2.1 GiB per day over the last 6h: full in 13.8 days (71.0 GiB of 100.0 GiB).")
	full.Value = &v1.FindingValue{Name: "days until full", Display: "13.8", Number: ptr(13.8),
		Basis: "days until the JetStream store is full at the current growth"}

	version := func(server string) *v1.Finding {
		return finding("server-005", "Version differs from the majority", info, firing, serverRef(server), s.on(14, 10, 0), s.on(14, 10, 0),
			server+" runs "+globalOlder+"; 22 of 24 servers run "+globalVersion+".")
	}

	restarted := func(server string, h, m, sec int) *v1.Finding {
		at := s.at(h, m, sec)
		return resolvedFinding("server-004", "Server restarted", info, serverRef(server), at, at.Add(10*time.Minute),
			fmt.Sprintf("%s restarted at %02d:%02d:%02d UTC with version %s unchanged; 1 restart in the last 1h.", server, h, m, sec, globalVersion))
	}

	// nats-ap-4 missed the edit of max_age from 7d to 3d at 13:05.
	config := finding("stream-007", "Stream configuration differs across servers", warning, firing, streamRef("customer-notifications"),
		s.at(13, 5, 30), s.at(13, 5, 30), "2 configurations (nats-ap-2, nats-ap-3 | nats-ap-4); differing: max_age.")
	config.Value = &v1.FindingValue{Name: "distinct configurations", Display: "2", Number: ptr(2.0), Basis: "configuration hashes the servers report"}
	config.Remediation = "nats-ap-4 kept max_age 7d when the stream was edited to 3d at 13:05, so it keeps messages the others removed. " +
		"Apply the definition again (`nats stream edit customer-notifications --max-age 3d`); if nats-ap-4 still differs, " +
		"step the leader down (`nats stream cluster step-down customer-notifications`) so the replicas resynchronise."

	// nats-us-3 answered again at 09:06 without its replica; the check
	// leaves a restarting server out for 10 minutes.
	absent := finding("stream-010", "Stream missing on an assigned server", warning, firing, streamRef("return-requests"),
		s.at(9, 17, 0), s.at(9, 17, 0), "Leader nats-us-6 assigns the stream to nats-us-3, which answered JSZ without it.")
	absent.Value = &v1.FindingValue{Name: "assigned servers without the stream", Display: "1", Number: ptr(1.0),
		Basis: "assigned peers that answered JSZ and do not report the stream"}
	absent.Remediation = "nats-us-3 came back from the upgrade without its replica of return-requests. If it does not appear, " +
		"remove it (`nats stream cluster peer-remove return-requests nats-us-3`) so the meta leader places a new replica that copies the stream from nats-us-6."

	// The checks raised these as info while nats-us-4's announced shutdown
	// counted as a planned restart, and changed them to warning when the
	// restart window ended.
	for _, f := range []*v1.Finding{us4, labels} {
		f.State, f.TransitionedAt = changed, ts(globalPlannedEnd.Add(s.shift))
	}
	offline := []*v1.Finding{replica("shipping-updates", "nats-us-5"), replica("price-changes", "nats-us-2")}
	for _, f := range offline {
		f.State, f.TransitionedAt = changed, ts(globalPlannedEnd.Add(s.shift))
	}

	// No server reports the two durables from nats-us-2's first
	// collection after its restart on; the check fires once all three
	// holders answer again, and keeps firing for its 1h lookback.
	vanished := func(consumer string) *v1.Finding {
		f := resolvedFinding("consumer-013", "Durable consumer disappeared", critical, consumerRef("inventory-changes", consumer),
			globalVanishedFiring.Add(s.shift), globalVanishedResolved.Add(s.shift),
			"Durable "+consumer+" on inventory-changes was last reported by nats-us-0, nats-us-1 at 2026-10-17 08:58:26 UTC and no server reports it now. "+
				"No delete event recorded; the durable may have been renamed or removed by the server.")
		f.Value = &v1.FindingValue{Name: "minutes since last reported", Display: "1.1", Number: ptr(1.1),
			Basis: "time since a server last reported the durable"}
		return f
	}

	// order-archive is muted (globalSeedArchiveMute); KV_feature-flags
	// fires again since nats-lens ended its acceptance.
	archive := s.unboundedFinding("order-archive", s.on(1, 8, 30))
	flags := s.unboundedFinding("KV_feature-flags", s.on(1, 8, 30))

	// shipping-service in ap runs at the carrier's site, 140 ms away;
	// muted as expected (globalSeedClientMute).
	rtt := s.clientRTT("shipping-service", s.on(16, 9, 0))

	// SUPPLIERS' account JWT and pos-terminal's user JWT (globalAuth)
	// fired when they came within 14 days of their expiry.
	supExp, posExp := FixedNow.Add(12*day+s.shift), FixedNow.Add(5*day+s.shift)
	supJWT := finding("account-003", "Account JWT expires soon", warning, firing, accountRef(SuppliersAccount),
		supExp.Add(-14*day), supExp.Add(-14*day),
		"SUPPLIERS: account JWT expires "+supExp.Format("2006-01-02 15:04")+" UTC, in 12 days.")
	supJWT.Value = &v1.FindingValue{Name: "days until the account JWT expires", Display: "12", Number: ptr(12.0),
		Basis: "from the collection's start to the JWT's expiry, negative once expired; empty when only the server's expired flag says so"}
	supJWT.RelatedConfig = cfg([][2]string{{"exp", supExp.Format("2006-01-02 15:04:05") + " UTC"}})
	posJWT := finding("account-004", "User JWTs expire soon", warning, firing, accountRef(Account),
		posExp.Add(-14*day), posExp.Add(-14*day),
		"APP: user pos-terminal expires "+posExp.Format("2006-01-02 15:04")+" UTC, in 5 days (3 connections).")
	posJWT.Value = &v1.FindingValue{Name: "days until a user JWT expires", Display: "5", Number: ptr(5.0),
		Basis: "from the collection's start to the soonest user JWT expiry among the account's connections, negative once expired"}

	// search-api subscribes to search.query twice since its release; the
	// check fires at the first subscription collection after it.
	dupSince, _ := s.subsCollection(globalSearchRelease.Add(s.shift).Add(subsInterval))
	dups := s.duplicateSubscriptions(dupSince)

	return append([]*v1.Finding{us4, offline[0], offline[1], labels, slow, backlog, full, config, absent, archive, flags, rtt, supJWT, posJWT,
		vanished("restock-alerts"), vanished("stock-audit-export"),
		version("nats-ap-4"), version("nats-ap-5"),
		restarted("nats-us-1", 8, 46, 10), restarted("nats-us-2", 8, 58, 30), restarted("nats-us-3", 9, 6, 40),
		resolvedFinding("stream-001", "Stream quorum lost", critical, streamRef("inventory-changes"), s.at(8, 58, 40), s.at(9, 1, 0),
			"Leader nats-us-0 sees 1 of 3 replicas online; quorum needs 2, for 4 collections."),
	}, dups...)
}

// globalVanishedFiring and globalVanishedResolved bound consumer-013 on
// restock-alerts and stock-audit-export, in the fixed frame: it fires in
// the first collection after nats-us-2 answered again and resolves an
// hour after their last report.
var (
	globalVanishedFiring   = fixed(8, 59, 26)
	globalVanishedResolved = fixed(9, 58, 56)
)

// globalPlannedEnd is the first epoch after the restart window that began
// when nats-us-4 announced its shutdown at 09:12:05, in the fixed frame:
// from then on its absence is no planned restart.
var globalPlannedEnd = fixed(9, 22, 30)

// globalPlannedDetail is the detail a finding changed at globalPlannedEnd
// had while it was info, and whether it is one of them.
func globalPlannedDetail(f *v1.Finding) (string, bool) {
	if f.GetState() != changed {
		return "", false
	}
	d := f.GetDetail()
	switch f.GetCheckCode() {
	case "server-001":
		return strings.TrimSuffix(d, ".") + "; announced shutdown at 09:12:05 UTC.", true
	case "stream-002":
		return strings.Replace(d, "(last seen 19075s ago)", "(last seen 55s ago; announced shutdown at 09:12:05 and did not answer)", 1), true
	case "stream-011":
		return strings.Replace(d, "which did not answer", "which announced shutdown at 09:12:05 and did not answer", 1), true
	}
	return "", false
}

// globalTransitions gives the findings that changed severity their firing
// as info and their change to warning.
func globalTransitions(s scen, f *v1.Finding) []*v1.FindingTransition {
	before, ok := globalPlannedDetail(f)
	if !ok {
		return nil
	}
	return []*v1.FindingTransition{
		{FindingId: f.GetId(), Time: f.GetTransitionedAt(), State: changed, Severity: f.GetSeverity(), Detail: f.GetDetail()},
		{FindingId: f.GetId(), Time: f.GetSince(), State: firing, Severity: info, Detail: before},
	}
}

// The kubelet versions of the pool-us upgrade.
const (
	globalOldKubelet = "1.32.9-gke.1130"
	globalKubelet    = "1.33.4-gke.1245"
)

// globalSlowEvents are example-global's slow consumer disconnects: one
// each in three earlier hours, and nine on nats-us-3 since 13:38.
func globalSlowEvents() []time.Time {
	out := []time.Time{on16(23, 12), fixed(5, 40, 0), fixed(11, 7, 0)}
	for i := range 9 {
		out = append(out, fixed(13, 38, 0).Add(time.Duration(i)*50*time.Second))
	}
	return append(out, fixed(14, 21, 0), fixed(14, 24, 0))
}

// globalEvents are example-global's events, newest first.
func globalEvents(s scen, fs []*v1.Finding) []*v1.Event {
	byID := map[string]*v1.Finding{}
	for _, f := range fs {
		byID[f.GetId()] = f
	}
	tr := func(id string, st v1.FindingState, at time.Time) (*v1.FindingTransition, *v1.Finding) {
		f := byID[id]
		t := &v1.FindingTransition{FindingId: id, Time: ts(at), State: st, Severity: f.GetSeverity(), Detail: f.GetDetail()}
		if before, ok := globalPlannedDetail(f); ok && st == firing {
			// It fired as info.
			t.Severity, t.Detail = info, before
		}
		return t, f
	}
	findingEvent := func(id string, st v1.FindingState, at time.Time, kind, summary string) *v1.Event {
		t, f := tr(id, st, at)
		return &v1.Event{Time: ts(at), Source: srcFinding, Kind: kind, Type: f.GetCheckCode() + " " + f.GetCheckName(),
			Severity: t.GetSeverity(), Summary: summary, Transition: t, Finding: f, Server: f.GetEntity().GetServer(),
			Account: f.GetEntity().GetAccount(), Stream: f.GetEntity().GetStream(), Consumer: f.GetEntity().GetConsumer()}
	}
	k8s := func(at time.Time, kind, typ, server, summary string) *v1.Event {
		return &v1.Event{Time: ts(at), Source: srcK8s, Kind: kind, Type: typ, Server: server, Summary: summary}
	}
	// node events carry the node, its pool and its kubelet version in
	// their payload, as the watcher writes them.
	node := func(at time.Time, kind, typ, suffix, version, summary string) *v1.Event {
		e := k8s(at, kind, typ, "", summary)
		e.Raw = nodeRaw("gke-example-pool-us-"+suffix, "pool-us", version)
		return e
	}
	nats := func(at time.Time, kind, typ, server, stream, summary string) *v1.Event {
		e := &v1.Event{Time: ts(at), Source: srcNATS, Kind: kind, Type: typ, Server: server, Summary: summary}
		if stream != "" {
			e.Account, e.Stream = Account, stream
		}
		return e
	}
	var out []*v1.Event
	slow := globalSlowEvents()
	for i := len(slow) - 1; i >= 0; i-- {
		at := slow[i].Add(s.shift)
		out = append(out, &v1.Event{Time: ts(at), Source: srcNATS, Kind: "client.disconnect", Type: "CLIENT.DISCONNECT", Severity: warning,
			Server: "nats-us-3", Account: Account, ClientName: "label-printer", ClientHost: fmt.Sprintf("10.30.3.%d", 20+i%6),
			Reason: "Slow Consumer (Write Deadline)", Summary: "label-printer disconnected: Slow Consumer (Write Deadline)"})
	}
	out = append(out,
		findingEvent("server-009:nats-us-3", firing, s.at(13, 41, 0), "finding.firing", "nats-us-3: 4 slow consumers in the last 15m"),
		findingEvent("consumer-002:"+Account+"/shipping-updates/label-printer", firing, s.at(13, 40, 0), "finding.firing",
			"label-printer on shipping-updates: fill 24.5/s, drain 21.4/s"),
	)
	// The consumer advisories as the capture decodes them.
	consumerEvent := func(at time.Time, kind, typ, stream, consumer, summary string) *v1.Event {
		e := nats(at, kind, typ, "", stream, summary)
		e.Consumer = consumer
		return e
	}
	out = append(out,
		consumerEvent(globalInvoiceCreated.Add(s.shift), "consumer.created", "CONSUMER.CREATED", "order-events", "invoice-renderer",
			"consumer APP > order-events > invoice-renderer created"),
		consumerEvent(s.at(2, 30, 0), "consumer.paused", "CONSUMER.PAUSED", "inventory-changes", "reorder-planner",
			"consumer APP > inventory-changes > reorder-planner paused until 2026-10-18T02:00:00Z"),
	)
	out = append(out,
		findingEvent("stream-007:"+Account+"/customer-notifications", firing, s.at(13, 5, 30), "finding.firing",
			"customer-notifications: nats-ap-4 differs in max_age"),
		nats(s.at(13, 5, 2), "stream.updated", "STREAM.UPDATED", "nats-ap-2", "customer-notifications", "customer-notifications updated: max_age 7d -> 3d"),
		findingEvent("stream-010:"+Account+"/return-requests", firing, s.at(9, 17, 0), "finding.firing",
			"return-requests: nats-us-3 answers without its replica"),
	)
	// The restart window after nats-us-4's announced shutdown ended.
	planned := globalPlannedEnd.Add(s.shift)
	out = append(out,
		findingEvent("server-001:nats-us-4", changed, planned, "finding.changed", "nats-us-4 not reporting: warning, no longer a planned restart"),
		findingEvent("stream-011:"+Account+"/KV_label-templates", changed, planned, "finding.changed", "KV_label-templates missing: warning"),
		findingEvent("stream-002:"+Account+"/price-changes", changed, planned, "finding.changed", "price-changes: replica on nats-us-4 offline: warning"),
		findingEvent("stream-002:"+Account+"/shipping-updates", changed, planned, "finding.changed", "shipping-updates: replica on nats-us-4 offline: warning"),
	)
	consumerElected := func(at time.Time, server, stream, consumer, term string) *v1.Event {
		e := nats(at, "consumer.leader_elected", "CONSUMER.LEADER_ELECTED", server, stream, consumer+" on "+stream+" -> "+server+" (term "+term+")")
		e.Consumer = consumer
		return e
	}
	lameDuck := func(at time.Time, server string) *v1.Event {
		return nats(at, "server.lameduck", "SERVER.LAMEDUCK", server, "", "server "+server+" entered lame duck mode")
	}
	vanished := func(st v1.FindingState, at time.Time, kind, consumer, summary string) *v1.Event {
		return findingEvent("consumer-013:"+Account+"/inventory-changes/"+consumer, st, at, kind, summary)
	}
	out = append(out,
		vanished(resolved, globalVanishedResolved.Add(s.shift), "finding.resolved", "restock-alerts", "restock-alerts on inventory-changes: 1h since its last report"),
		vanished(resolved, globalVanishedResolved.Add(s.shift), "finding.resolved", "stock-audit-export", "stock-audit-export on inventory-changes: 1h since its last report"),
		vanished(firing, globalVanishedFiring.Add(s.shift), "finding.firing", "restock-alerts", "restock-alerts on inventory-changes reported by no server"),
		vanished(firing, globalVanishedFiring.Add(s.shift), "finding.firing", "stock-audit-export", "stock-audit-export on inventory-changes reported by no server"),
		// The node pool's own report names no node: an Event of its own
		// kind, not a node upgrade.
		poolEvent(s.at(9, 20, 30), "pool-us", "pool-us upgrade stopped: 3 of 4 nodes on 1.33.4-gke.1245"),
		// Each node is cordoned, drained as its NATS pod leaves,
		// upgraded and uncordoned; the upgrade stopped before it upgraded
		// c3f8, which was uncordoned again.
		node(s.at(9, 20, 30), "k8s.node.uncordoned", "Uncordoned", "c3f8", globalOldKubelet, "node gke-example-pool-us-c3f8 uncordoned"),
		node(s.at(9, 11, 59), "k8s.node.drained", "NodeDrain", "c3f8", globalOldKubelet, "node gke-example-pool-us-c3f8 cordoned and drained: nats-us-4 leaves it"),
		node(s.at(9, 11, 0), "k8s.node.cordoned", "Cordoned", "c3f8", globalOldKubelet, "node gke-example-pool-us-c3f8 cordoned (unschedulable), running [nats-us-4]"),
		node(s.at(9, 6, 15), "k8s.node.uncordoned", "Uncordoned", "5e21", globalKubelet, "node gke-example-pool-us-5e21 uncordoned"),
		node(s.at(9, 4, 49), "k8s.node.drained", "NodeDrain", "5e21", globalOldKubelet, "node gke-example-pool-us-5e21 cordoned and drained: nats-us-3 leaves it"),
		node(s.at(9, 3, 0), "k8s.node.cordoned", "Cordoned", "5e21", globalOldKubelet, "node gke-example-pool-us-5e21 cordoned (unschedulable), running [nats-us-3]"),
		node(s.at(8, 58, 5), "k8s.node.uncordoned", "Uncordoned", "a904", globalKubelet, "node gke-example-pool-us-a904 uncordoned"),
		node(s.at(8, 56, 19), "k8s.node.drained", "NodeDrain", "a904", globalOldKubelet, "node gke-example-pool-us-a904 cordoned and drained: nats-us-2 leaves it"),
		node(s.at(8, 54, 0), "k8s.node.cordoned", "Cordoned", "a904", globalOldKubelet, "node gke-example-pool-us-a904 cordoned (unschedulable), running [nats-us-2]"),
		node(s.at(8, 45, 45), "k8s.node.uncordoned", "Uncordoned", "17bd", globalKubelet, "node gke-example-pool-us-17bd uncordoned"),
		node(s.at(8, 44, 4), "k8s.node.drained", "NodeDrain", "17bd", globalOldKubelet, "node gke-example-pool-us-17bd cordoned and drained: nats-us-1 leaves it"),
		node(s.at(8, 40, 12), "k8s.node.cordoned", "Cordoned", "17bd", globalOldKubelet, "node gke-example-pool-us-17bd cordoned (unschedulable), running [nats-us-1]"),
		node(s.at(9, 6, 10), "k8s.node.upgrade", "KubeletVersion", "5e21", globalKubelet, "node gke-example-pool-us-5e21 kubelet "+globalOldKubelet+" -> "+globalKubelet),
		node(s.at(8, 58, 0), "k8s.node.upgrade", "KubeletVersion", "a904", globalKubelet, "node gke-example-pool-us-a904 kubelet "+globalOldKubelet+" -> "+globalKubelet),
		node(s.at(8, 45, 40), "k8s.node.upgrade", "KubeletVersion", "17bd", globalKubelet, "node gke-example-pool-us-17bd kubelet "+globalOldKubelet+" -> "+globalKubelet),
		// Each restarting server announced lame duck mode a second after
		// its pod was killed; the leaders it held were elected elsewhere.
		lameDuck(s.at(8, 44, 6), "nats-us-1"),
		consumerElected(s.at(8, 44, 8), "nats-us-0", "inventory-changes", "stock-projector", "17"),
		consumerElected(s.at(8, 44, 9), "nats-us-0", "inventory-changes", "reorder-planner", "23"),
		lameDuck(s.at(8, 56, 21), "nats-us-2"),
		nats(s.at(8, 56, 31), "stream.leader_elected", "STREAM.LEADER_ELECTED", "nats-us-4", "price-changes", "price-changes -> nats-us-4 (term 87)"),
		consumerElected(s.at(8, 56, 32), "nats-us-4", "price-changes", "price-cache-invalidator", "41"),
		lameDuck(s.at(9, 4, 51), "nats-us-3"),
		consumerElected(s.at(9, 5, 3), "nats-us-5", "shipping-updates", "label-printer", "64"),
		consumerElected(s.at(9, 5, 4), "nats-us-5", "shipping-updates", "tracking-notifier", "58"),
		consumerElected(s.at(9, 12, 7), "nats-us-2", "price-changes", "price-cache-invalidator", "42"),
	)
	out = append(out,
		findingEvent("stream-002:"+Account+"/price-changes", firing, s.at(9, 13, 0), "finding.firing", "price-changes: replica on nats-us-4 offline"),
		findingEvent("stream-002:"+Account+"/shipping-updates", firing, s.at(9, 13, 0), "finding.firing", "shipping-updates: replica on nats-us-4 offline"),
		findingEvent("server-001:nats-us-4", firing, s.at(9, 12, 35), "finding.firing", "nats-us-4 not reporting (23 of 24)"),
		k8s(s.at(9, 12, 20), "k8s.pod.failed_scheduling", "FailedScheduling", "nats-us-4", "pod nats-us-4: 0/4 nodes are available: 4 Insufficient memory"),
		nats(s.at(9, 12, 6), "stream.leader_elected", "STREAM.LEADER_ELECTED", "nats-us-2", "price-changes", "price-changes -> nats-us-2 (term 88)"),
		nats(s.at(9, 12, 5), "server.shutdown", "SERVER.SHUTDOWN", "nats-us-4", "", "nats-us-4 shut down"),
		k8s(s.at(9, 12, 0), "k8s.pod.killing", "Killing", "nats-us-4", "pod nats-us-4 Killing (node upgrade), grace 120s"),
		k8s(s.at(9, 6, 40), "k8s.pod.started", "Started", "nats-us-3", "pod nats-us-3 container nats started on gke-example-pool-us-5e21"),
		nats(s.at(9, 5, 2), "stream.leader_elected", "STREAM.LEADER_ELECTED", "nats-us-5", "shipping-updates", "shipping-updates -> nats-us-5 (term 131)"),
		k8s(s.at(9, 4, 50), "k8s.pod.killing", "Killing", "nats-us-3", "pod nats-us-3 Killing (node upgrade), grace 120s"),
		findingEvent("stream-001:"+Account+"/inventory-changes", resolved, s.at(9, 1, 0), "finding.resolved", "inventory-changes back to 3 of 3 peers"),
		nats(s.at(8, 58, 40), "stream.quorum_lost", "STREAM.QUORUM_LOST", "nats-us-0", "inventory-changes", "inventory-changes (1 of 3 peers)"),
		k8s(s.at(8, 58, 30), "k8s.pod.started", "Started", "nats-us-2", "pod nats-us-2 container nats started on gke-example-pool-us-a904"),
		k8s(s.at(8, 56, 20), "k8s.pod.killing", "Killing", "nats-us-2", "pod nats-us-2 Killing (node upgrade), grace 120s"),
		k8s(s.at(8, 46, 10), "k8s.pod.started", "Started", "nats-us-1", "pod nats-us-1 container nats started on gke-example-pool-us-17bd"),
		nats(s.at(8, 44, 30), "meta.leader_elected", "LEADER_ELECTED", "nats-eu-1", "", "meta leader stays nats-eu-1 (term 52)"),
		k8s(s.at(8, 44, 5), "k8s.pod.killing", "Killing", "nats-us-1", "pod nats-us-1 Killing (node upgrade), grace 120s"),
	)
	// Short connections of search-api fill the hours before the upgrade,
	// one churn summary per minute in which it reconnected; 2 to 5 each,
	// below client-005's threshold in any 10 minutes.
	for k := range 240 {
		sv := s.sc.servers[k%len(s.sc.servers)]
		if !sv.answering() {
			sv = s.sc.servers[0]
		}
		host := fmt.Sprintf("10.30.%d.%d", k%3, 10+k%50)
		out = append(out, &v1.Event{
			Time: ts(s.at(8, 30, 0).Add(-time.Duration(k) * 160 * time.Second)), Source: srcNATS, Kind: "client.churn", Type: "CLIENT.CHURN",
			Server: sv.name, Account: Account, ClientName: "search-api", ClientHost: host, Reason: "Client Closed",
			Summary: fmt.Sprintf("search-api connected and disconnected %d times in 1 min (each closed by the client within 10 s)", 2+k%4),
		})
	}
	// Three days before, the Kubernetes API refused the watch for four
	// minutes.
	k8sFrom, k8sTo := s.on(14, 3, 10), s.on(14, 3, 14)
	out = append(out, &v1.Event{Time: ts(k8sFrom), ReceivedAt: ts(k8sTo), Source: v1.EventSource_EVENT_SOURCE_COLLECTOR,
		Kind: "collector.k8s_gap", Reason: "the server is currently unable to handle the request",
		Summary: "kubernetes pods not watched for 4m0s, from " + k8sFrom.Format(time.RFC3339) + " to " + k8sTo.Format(time.RFC3339),
		Raw:     `{"source":"pods","from":"` + k8sFrom.Format(time.RFC3339) + `","to":"` + k8sTo.Format(time.RFC3339) + `","seconds":240}`})
	out = append(out, globalRoutineEvents(s)...)
	out = append(out, restartReconnects(s, map[string]time.Time{"nats-us-1": s.at(8, 44, 5), "nats-us-2": s.at(8, 56, 20), "nats-us-3": s.at(9, 4, 50)})...)
	slices.SortStableFunc(out, func(a, b *v1.Event) int { return b.GetTime().AsTime().Compare(a.GetTime().AsTime()) })
	return out
}

// globalRows are the checks of example-global's timeline.
func globalRows(*scenario) []rowSpec {
	f := fixed
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	us := func(i int) *v1.EntityRef { return serverRef(fmt.Sprintf("nats-us-%d", i)) }
	return []rowSpec{
		{"server-001", "Server not reporting", warning, []*v1.EntityRef{us(4)}, []span{{f(9, 12, 35), open}}},
		{"stream-002", "Stream replica offline", warning, []*v1.EntityRef{streamRef("shipping-updates"), streamRef("price-changes")},
			[]span{{f(9, 13, 0), open}}},
		{"stream-011", "Expected stream or bucket missing", warning, []*v1.EntityRef{streamRef("KV_label-templates")},
			[]span{{f(9, 12, 40), open}}},
		{"server-009", "Slow consumers", warning, []*v1.EntityRef{us(3)},
			[]span{{on16(23, 10), on16(23, 25)}, {f(11, 5, 0), f(11, 20, 0)}, {f(13, 41, 0), open}}},
		{"consumer-002", "Consumer backlog growing", warning, []*v1.EntityRef{consumerRef("shipping-updates", "label-printer")},
			[]span{{f(13, 40, 0), open}}},
		{"stream-001", "Stream quorum lost", critical, []*v1.EntityRef{streamRef("inventory-changes")}, []span{{f(8, 58, 40), f(9, 1, 0)}}},
		{"consumer-013", "Durable consumer disappeared", critical,
			[]*v1.EntityRef{consumerRef("inventory-changes", "restock-alerts"), consumerRef("inventory-changes", "stock-audit-export")},
			[]span{{globalVanishedFiring, globalVanishedResolved}}},
		{"server-004", "Server restarted", info, []*v1.EntityRef{us(1), us(2), us(3)},
			[]span{{f(8, 46, 10), f(8, 56, 10)}, {f(8, 58, 30), f(9, 8, 30)}, {f(9, 6, 40), f(9, 16, 40)}}},
		{"stream-007", "Stream configuration differs across servers", warning, []*v1.EntityRef{streamRef("customer-notifications")},
			[]span{{f(13, 5, 30), open}}},
		{"stream-010", "Stream missing on an assigned server", warning, []*v1.EntityRef{streamRef("return-requests")},
			[]span{{f(9, 17, 0), open}}},
		{"server-014", "JetStream store full soon", warning, []*v1.EntityRef{serverRef("nats-ap-2")}, []span{{f(12, 6, 0), open}}},
		{"server-005", "Version differs from the majority", info, []*v1.EntityRef{serverRef("nats-ap-4"), serverRef("nats-ap-5")},
			[]span{{day(14, 10, 0), open}}},
		{"stream-005", "Stream without any limit", info, []*v1.EntityRef{streamRef("order-archive"), streamRef("KV_feature-flags")},
			[]span{{day(1, 8, 30), open}}},
		{"account-003", "Account JWT expires soon", warning, []*v1.EntityRef{accountRef(SuppliersAccount)},
			[]span{{day(15, 14, 30), open}}},
		{"account-004", "User JWTs expire soon", warning, []*v1.EntityRef{accountRef(Account)}, []span{{day(8, 14, 30), open}}},
	}
}
