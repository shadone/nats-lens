package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

const (
	srcNATS    = v1.EventSource_EVENT_SOURCE_NATS
	srcK8s     = v1.EventSource_EVENT_SOURCE_KUBERNETES
	srcFinding = v1.EventSource_EVENT_SOURCE_FINDING
)

// Event counts and IDs of example-prod. IDs grow with time; the newest
// event has ID eventCount.
const (
	fillerCount    = 199
	prodChurnCount = 360
	// eventCount is the 27 events of the upgrade story, the filler, the
	// routine events (360 churn, 72 sampled acks, 48 account counts, 6 API
	// audit, 6 collector reconnects, 34 CLI), the 8 of the pending pod and
	// the clients' reconnects as the servers restarted (61, 60 and 61
	// connections, each a disconnect and a connect).
	eventCount        = 27 + fillerCount + prodChurnCount + 72 + 48 + 6 + 6 + 34 + 8 + 2*(61+60+61)
	idConsumerDeleted = "783"
)

// nodeRaw is a node event's payload as the Kubernetes watcher writes it:
// the node, its GKE node pool and its kubelet version.
func nodeRaw(node, pool, kubelet string) string {
	return `{"node":"` + node + `","pool":"` + pool + `","kubelet_version":"` + kubelet + `"}`
}

// poolEvent is a node pool's report of its upgrade. It names no node, so
// it is kept as an Event of no kind of its own (k8s.event), not as a
// node upgrade, which the node counts would need a node for.
func poolEvent(at time.Time, pool, summary string) *v1.Event {
	return &v1.Event{Time: ts(at), Source: srcK8s, Kind: "k8s.event", Type: "NodePoolUpgrade", Summary: summary,
		Raw: `{"pool":"` + pool + `"}`}
}

// events are every event of the scenario, newest first, with IDs set:
// the newest has the highest.
func (p *Provider) events(s scen) []*v1.Event {
	out := s.sc.events(s, p.findings(s))
	for i, e := range out {
		e.Id = strconv.Itoa(len(out) - i)
		if e.GetReceivedAt() == nil {
			e.ReceivedAt = ts(e.GetTime().AsTime().Add(40 * time.Millisecond))
		}
	}
	return out
}

// The kubelet versions of example-prod's node upgrade.
const (
	prodOldKubelet = "1.32.9-gke.1130"
	prodKubelet    = "1.33.4-gke.1245"
)

// prodNode is a node event of example-prod's pool-1.
func prodNode(at time.Time, kind, typ, suffix, kubelet, summary string) *v1.Event {
	return &v1.Event{Time: ts(at), Source: srcK8s, Kind: kind, Type: typ, Summary: summary,
		Raw: nodeRaw("gke-example-pool-1-"+suffix, "pool-1", kubelet)}
}

// prodEvents are example-prod's events, newest first.
func prodEvents(s scen, fs []*v1.Finding) []*v1.Event {
	byCode := map[string]*v1.Finding{}
	for _, f := range fs {
		byCode[f.GetCheckCode()] = f
	}
	tr := func(code string, st v1.FindingState, at time.Time) *v1.FindingTransition {
		f := byCode[code]
		return &v1.FindingTransition{FindingId: f.GetId(), Time: ts(at), State: st, Severity: f.GetSeverity(), Detail: f.GetDetail()}
	}
	out := []*v1.Event{
		{Time: ts(s.at(11, 0, 0)), Source: srcFinding, Kind: "finding.flapping", Type: "consumer-002 Consumer backlog growing", Severity: warning,
			Summary:    "media-chunk-pool on media-windows: fill 37.4/s, drain 31.6/s (4 transitions in 1h)",
			Transition: tr("consumer-002", flapping, s.at(11, 0, 0)), Finding: byCode["consumer-002"],
			Account: Account, Stream: "media-windows", Consumer: "media-chunk-pool"},
		poolEvent(s.at(9, 50, 12), "pool-1", "pool-1 upgrade complete, 3 of 3 nodes on 1.33.4-gke.1245"),
		// Each node is cordoned, drained as its NATS pod leaves,
		// upgraded and uncordoned.
		prodNode(s.at(9, 43, 45), "k8s.node.uncordoned", "Uncordoned", "c40d", prodKubelet, "node gke-example-pool-1-c40d uncordoned"),
		prodNode(s.at(9, 41, 9), "k8s.node.drained", "NodeDrain", "c40d", prodOldKubelet, "node gke-example-pool-1-c40d cordoned and drained: nats-2 leaves it"),
		prodNode(s.at(9, 40, 0), "k8s.node.cordoned", "Cordoned", "c40d", prodOldKubelet, "node gke-example-pool-1-c40d cordoned (unschedulable), running [nats-2]"),
		prodNode(s.at(9, 30, 35), "k8s.node.uncordoned", "Uncordoned", "91c2", prodKubelet, "node gke-example-pool-1-91c2 uncordoned"),
		prodNode(s.at(9, 29, 4), "k8s.node.drained", "NodeDrain", "91c2", prodOldKubelet, "node gke-example-pool-1-91c2 cordoned and drained: nats-1 leaves it"),
		prodNode(s.at(9, 27, 0), "k8s.node.cordoned", "Cordoned", "91c2", prodOldKubelet, "node gke-example-pool-1-91c2 cordoned (unschedulable), running [nats-1]"),
		{Time: ts(s.at(9, 18, 0)), Source: srcK8s, Kind: "k8s.pod.started", Type: "Started", Server: "nats-0",
			Summary: "pod nats-0 container nats started on gke-example-pool-1-7f3a"},
		prodNode(s.at(9, 17, 25), "k8s.node.uncordoned", "Uncordoned", "7f3a", prodKubelet, "node gke-example-pool-1-7f3a uncordoned"),
		prodNode(s.at(9, 16, 1), "k8s.node.drained", "NodeDrain", "7f3a", prodOldKubelet, "node gke-example-pool-1-7f3a cordoned and drained: nats-0 leaves it"),
		prodNode(s.at(9, 14, 48), "k8s.node.cordoned", "Cordoned", "7f3a", prodOldKubelet, "node gke-example-pool-1-7f3a cordoned (unschedulable), running [nats-0]"),
		prodNode(s.at(9, 43, 40), "k8s.node.upgrade", "KubeletVersion", "c40d", prodKubelet,
			"node gke-example-pool-1-c40d kubelet "+prodOldKubelet+" -> "+prodKubelet),
		prodNode(s.at(9, 30, 30), "k8s.node.upgrade", "KubeletVersion", "91c2", prodKubelet,
			"node gke-example-pool-1-91c2 kubelet "+prodOldKubelet+" -> "+prodKubelet),
		prodNode(s.at(9, 17, 20), "k8s.node.upgrade", "KubeletVersion", "7f3a", prodKubelet,
			"node gke-example-pool-1-7f3a kubelet "+prodOldKubelet+" -> "+prodKubelet),
		{Time: ts(s.at(9, 45, 0)), Source: srcFinding, Kind: "finding.firing", Type: "consumer-009 Consumer creation time differs across servers", Severity: warning,
			Summary:    "resize-worker on order-jobs: nats-0 2026-08-12, nats-1 and nats-2 2026-10-17",
			Transition: tr("consumer-009", firing, s.at(9, 45, 0)), Finding: byCode["consumer-009"],
			Account: Account, Stream: "order-jobs", Consumer: "resize-worker"},
		{Time: ts(s.at(9, 45, 0)), Source: srcFinding, Kind: "finding.resolved", Type: "consumer-001 Consumer quorum lost", Severity: critical,
			Summary:    "job-cache-projector back to 3 of 3 peers after 30m",
			Transition: tr("consumer-001", resolved, s.at(9, 45, 0)), Finding: byCode["consumer-001"],
			Account: Account, Stream: "order-jobs", Consumer: "job-cache-projector"},
		{Time: ts(s.at(9, 44, 21)), Source: srcK8s, Kind: "k8s.pod.started", Type: "Started", Server: "nats-2",
			Summary: "pod nats-2 container nats started on gke-example-pool-1-c40d"},
		{Time: ts(s.at(9, 41, 10)), Source: srcK8s, Kind: "k8s.pod.killing", Type: "Killing", Server: "nats-2",
			Summary: "pod nats-2 Killing (node upgrade), grace 120s"},
		{Time: ts(s.at(9, 31, 7)), Source: srcNATS, Kind: "consumer.deleted", Type: "CONSUMER.DELETED", Severity: critical, Server: "nats-0",
			Account: Account, Stream: "order-archive", Consumer: "analytics-aggregator",
			ClientName: "analytics-worker", ClientHost: "10.20.0.12",
			Summary: "analytics-aggregator on order-archive by analytics-worker@10.20.0.12",
			Raised:  []*v1.Finding{byCode["consumer-010"]}},
		{Time: ts(s.at(9, 31, 2)), Source: srcK8s, Kind: "k8s.pod.started", Type: "Started", Server: "nats-1",
			Summary: "pod nats-1 container nats started on gke-example-pool-1-91c2"},
		{Time: ts(s.at(9, 30, 40)), Source: srcNATS, Kind: "consumer.quorum_lost", Type: "QUORUM_LOST", Severity: critical, Server: "nats-0",
			Account: Account, Stream: "order-jobs", Consumer: "job-cache-projector",
			Summary: "consumer job-cache-projector on order-jobs (1 of 3 peers)"},
		{Time: ts(s.at(9, 30, 11)), Source: srcNATS, Kind: "meta.leader_elected", Type: "LEADER_ELECTED", Server: "nats-0",
			Summary: "meta leader -> nats-0"},
		{Time: ts(s.at(9, 29, 5)), Source: srcK8s, Kind: "k8s.pod.killing", Type: "Killing", Server: "nats-1",
			Summary: "pod nats-1 Killing (node upgrade), grace 120s"},
		{Time: ts(s.at(9, 16, 30)), Source: srcNATS, Kind: "stream.quorum_lost", Type: "STREAM.QUORUM_LOST", Severity: critical, Server: "nats-1",
			Account: Account, Stream: "media-windows", Summary: "media-windows (1 of 3 peers)"},
		// nats-1 took the lead of order-jobs while nats-0 restarted.
		{Time: ts(s.at(9, 16, 3)), Source: srcNATS, Kind: "stream.leader_elected", Type: "STREAM.LEADER_ELECTED", Server: "nats-1",
			Account: Account, Stream: "order-jobs", Summary: "order-jobs -> nats-1 (term 412)"},
		{Time: ts(s.at(9, 16, 2)), Source: srcK8s, Kind: "k8s.pod.killing", Type: "Killing", Server: "nats-0",
			Summary: "pod nats-0 Killing (node upgrade), grace 120s"},
	}
	// Short connections of one client fill the hours before the upgrade,
	// one churn summary per minute in which it reconnected; 6 to 10 every
	// 5.5 minutes peak at 19 in any 10 minutes, below client-005's threshold.
	for k := range fillerCount {
		host := fmt.Sprintf("10.20.2.%d", 10+k%40)
		out = append(out, &v1.Event{
			Time: ts(s.at(9, 10, 0).Add(-time.Duration(k) * 330 * time.Second)), Source: srcNATS, Kind: "client.churn", Type: "CLIENT.CHURN",
			Server: s.sc.servers[k%3].name, Account: Account, ClientName: "catalog-api", ClientHost: host, Reason: "Client Closed",
			Summary: fmt.Sprintf("catalog-api connected and disconnected %d times in 1 min (each closed by the client within 10 s)", 6+k%5),
		})
	}
	out = append(out, prodRoutine(s)...)
	out = append(out, prodPending(s)...)
	out = append(out, restartReconnects(s, map[string]time.Time{"nats-0": s.at(9, 16, 2), "nats-1": s.at(9, 29, 5), "nats-2": s.at(9, 41, 10)})...)
	slices.SortStableFunc(out, func(a, b *v1.Event) int { return b.GetTime().AsTime().Compare(a.GetTime().AsTime()) })
	return out
}

// prodRoutine are example-prod's routine events over the last day: a
// poller's short connections, sampled acks, account connection counts,
// API audit records, the collector's own reconnects during the upgrade
// and a CLI through a port-forward.
func prodRoutine(s scen) []*v1.Event {
	var out []*v1.Event
	add := func(e *v1.Event) { out = append(out, e) }
	end := s.at(14, 28, 0)
	// stock-poller: every 4 minutes a summary of 2 to 5, and of 10 to 13 since
	// 12:44, when it began to open a connection per poll: at least 21 in any
	// 10 minutes from 12:48, which client-005 flags.
	for k := range prodChurnCount {
		sv := s.sc.servers[k%3].name
		n := 2 + k%4
		if k < 27 {
			n += 8
		}
		add(&v1.Event{Time: ts(end.Add(-time.Duration(k) * 4 * time.Minute)), Source: srcNATS, Kind: "client.churn", Type: "CLIENT.CHURN",
			Server: sv, Account: Account, ClientName: "stock-poller", ClientHost: "10.20.4.31", Reason: "Client Closed",
			Summary: fmt.Sprintf("stock-poller connected and disconnected %d times in 1 min (each closed by the client within 10 s)", n)})
	}
	for k := range 72 {
		add(&v1.Event{Time: ts(end.Add(-time.Duration(k)*20*time.Minute - 50*time.Second)), Source: srcNATS, Kind: "consumer.ack_sampled",
			Type: "CONSUMER.ACK_SAMPLED", Server: s.sc.servers[k%3].name, Account: Account, Stream: "order-jobs", Consumer: "resize-worker",
			Summary: fmt.Sprintf("resize-worker on order-jobs acked after %d ms (1 delivery)", 40+k%7*13)})
	}
	for k := range 48 {
		add(&v1.Event{Time: ts(end.Add(-time.Duration(k)*30*time.Minute - 25*time.Second)), Source: srcNATS, Kind: "account.connections",
			Type: "ACCOUNT.CONNECTIONS", Server: s.sc.servers[k%3].name, Account: Account,
			Summary: fmt.Sprintf("account APP: %d connections, %d leaf nodes", 176+k%9, 0)})
	}
	for k := range 6 {
		add(&v1.Event{Time: ts(s.at(9, 38, 44).Add(time.Duration(k) * 3 * time.Second)), Source: srcNATS, Kind: "api.audit", Type: "API.AUDIT",
			Server: "nats-1", Account: Account, Stream: "order-jobs", ClientName: "order-service", ClientHost: "10.20.1.7",
			Summary: "$JS.API.CONSUMER.INFO.order-jobs.resize-worker by order-service@10.20.1.7"})
	}
	// The collector's own connection moves when a server restarts.
	for _, at := range []time.Time{s.at(9, 16, 3), s.at(9, 29, 6), s.at(9, 41, 11)} {
		for i, kind := range []string{"client.disconnect", "client.connect"} {
			add(&v1.Event{Time: ts(at.Add(time.Duration(i) * 2 * time.Second)), Source: srcNATS, Kind: kind, Type: strings.ToUpper(kind),
				Server: "nats-0", Account: "SYS", ClientName: "natslens-" + Cluster, ClientHost: "10.20.0.40",
				Summary: "natslens-" + Cluster + " " + strings.TrimPrefix(kind, "client.") + " from 10.20.0.40"})
		}
	}
	// Someone checks the cluster with the CLI through a port-forward.
	for k := range 17 {
		at := s.at(9, 52, 0).Add(time.Duration(k) * 3 * time.Minute)
		for i, kind := range []string{"client.connect", "client.disconnect"} {
			add(&v1.Event{Time: ts(at.Add(time.Duration(i) * 4 * time.Second)), Source: srcNATS, Kind: kind, Type: strings.ToUpper(kind),
				Server: "nats-0", Account: "SYS", ClientName: "NATS CLI Version 0.2.2", ClientHost: "127.0.0.1",
				Summary: "NATS CLI Version 0.2.2 " + strings.TrimPrefix(kind, "client.") + " from 127.0.0.1"})
		}
	}
	return out
}

// prodPending is a resize-worker pod that cannot be scheduled for six
// minutes after the media-worker rollout took the room, then fails its
// readiness probe three times.
func prodPending(s scen) []*v1.Event {
	const pod = "pod shop/resize-worker-6c9d4-x2k4l"
	var out []*v1.Event
	for _, at := range []time.Time{s.at(11, 12, 5), s.at(11, 13, 20), s.at(11, 14, 40), s.at(11, 16, 10), s.at(11, 18, 0)} {
		out = append(out, &v1.Event{Time: ts(at), Source: srcK8s, Kind: "k8s.pod.failed_scheduling", Type: "FailedScheduling",
			Severity: warning, Reason: "FailedScheduling",
			Summary: pod + " FailedScheduling: 0/6 nodes are available: 6 Insufficient cpu"})
	}
	for _, at := range []time.Time{s.at(11, 21, 2), s.at(11, 21, 32), s.at(11, 22, 2)} {
		out = append(out, &v1.Event{Time: ts(at), Source: srcK8s, Kind: "k8s.pod.not_ready", Type: "Unhealthy",
			Severity: warning, Reason: "Unhealthy",
			Summary: pod + " Unhealthy: Readiness probe failed: HTTP probe failed with statuscode: 503"})
	}
	return out
}

func keepEvent(e *v1.Event, req *v1.ListEventsRequest) bool {
	if !inRange(e.GetTime().AsTime(), req.GetRange()) {
		return false
	}
	if len(req.GetSources()) > 0 && !slices.Contains(req.GetSources(), e.GetSource()) {
		return false
	}
	if len(req.GetKinds()) > 0 && !slices.Contains(req.GetKinds(), e.GetKind()) {
		return false
	}
	if req.GetClientName() != "" && e.GetClientName() != req.GetClientName() {
		return false
	}
	w := req.GetEntity()
	switch w.GetType() {
	case v1.EntityType_ENTITY_TYPE_SERVER:
		return e.GetServer() == w.GetKey()
	case v1.EntityType_ENTITY_TYPE_ACCOUNT:
		return e.GetAccount() == w.GetKey()
	case v1.EntityType_ENTITY_TYPE_STREAM:
		return e.GetAccount()+"/"+e.GetStream() == w.GetKey()
	case v1.EntityType_ENTITY_TYPE_CONSUMER:
		return e.GetAccount()+"/"+e.GetStream()+"/"+e.GetConsumer() == w.GetKey()
	case v1.EntityType_ENTITY_TYPE_CLIENT:
		return e.GetClientName() == w.GetKey()
	case v1.EntityType_ENTITY_TYPE_CONNECTION:
		return false // no scenario event names a connection ID
	}
	return true
}

// ListEvents filters the events, newest first, paging by time and ID.
func (p *Provider) ListEvents(_ context.Context, req *v1.ListEventsRequest) (*v1.ListEventsResponse, error) {
	cur, err := api.DecodeCursor(req.GetPageToken())
	if err != nil {
		return nil, err
	}
	if richRequest(req) {
		return p.listEventsRich(req, cur), nil
	}
	res := &v1.ListEventsResponse{}
	for _, e := range p.events(p.scen()) {
		if !keepEvent(e, req) {
			continue
		}
		res.TotalCount++
		if cur.ID != 0 && !olderThan(e, cur) {
			continue
		}
		if len(res.Events) == int(req.GetPageSize()) {
			l := res.Events[len(res.Events)-1]
			res.NextPageToken = api.EncodeCursor(api.Cursor{Time: l.GetTime().AsTime(), ID: num(l.GetId())})
			continue
		}
		res.Events = append(res.Events, e)
	}
	return res, nil
}

// olderThan reports whether e sorts after the cursor, newest first.
func olderThan(e *v1.Event, c api.Cursor) bool {
	t := e.GetTime().AsTime()
	return t.Before(c.Time) || (t.Equal(c.Time) && num(e.GetId()) < c.ID)
}

// num is the number of a scenario event ID.
func num(id string) int64 {
	n, _ := strconv.ParseInt(id, 10, 64)
	return n
}

// GetEvent returns one event with a payload built from its fields.
func (p *Provider) GetEvent(_ context.Context, req *v1.GetEventRequest) (*v1.GetEventResponse, error) {
	for _, e := range p.events(p.scen()) {
		if e.GetId() != req.GetId() {
			continue
		}
		raw, _ := json.Marshal(map[string]any{
			"type": e.GetType(), "time": e.GetTime().AsTime(), "server": e.GetServer(), "stream": e.GetStream(),
			"consumer": e.GetConsumer(), "client": map[string]string{"name": e.GetClientName(), "host": e.GetClientHost()},
		})
		e.Raw = string(raw)
		return &v1.GetEventResponse{Event: e}, nil
	}
	return nil, fmt.Errorf("event %q: %w", req.GetId(), api.ErrNotFound)
}
