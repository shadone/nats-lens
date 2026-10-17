package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
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
	eventCount        = 214
	fillerCount       = eventCount - 15
	idConsumerDeleted = "207" // eventCount - 7
)

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
		{Time: ts(s.at(11, 0, 0)), Source: srcFinding, Kind: "finding.flapping", Type: "stream-020 Backlog growing", Severity: warning,
			Summary:    "media-chunk-pool on media-windows: fill 42/s, drain 31/s (4 transitions in 1h)",
			Transition: tr("stream-020", flapping, s.at(11, 0, 0)), Finding: byCode["stream-020"],
			Account: Account, Stream: "media-windows", Consumer: "media-chunk-pool"},
		{Time: ts(s.at(9, 50, 12)), Source: srcK8s, Kind: "k8s.node.upgrade", Type: "NodePoolUpgrade",
			Summary: "pool-1 upgrade complete, 3 of 3 nodes on 1.33.4-gke.1245"},
		{Time: ts(s.at(9, 45, 0)), Source: srcFinding, Kind: "finding.firing", Type: "consumer-012 Creation time differs", Severity: warning,
			Summary:    "resize-worker on order-jobs: nats-0 2026-08-12, nats-1 and nats-2 2026-10-17",
			Transition: tr("consumer-012", firing, s.at(9, 45, 0)), Finding: byCode["consumer-012"],
			Account: Account, Stream: "order-jobs", Consumer: "resize-worker"},
		{Time: ts(s.at(9, 45, 0)), Source: srcFinding, Kind: "finding.resolved", Type: "consumer-008 Consumer quorum lost", Severity: critical,
			Summary:    "job-cache-projector back to 3 of 3 peers after 30m",
			Transition: tr("consumer-008", resolved, s.at(9, 45, 0)), Finding: byCode["consumer-008"],
			Account: Account, Stream: "order-jobs", Consumer: "job-cache-projector"},
		{Time: ts(s.at(9, 44, 21)), Source: srcK8s, Kind: "k8s.pod.started", Type: "Started", Server: "nats-2",
			Summary: "pod nats-2 container nats started on gke-example-pool-1-c40d"},
		{Time: ts(s.at(9, 42, 3)), Source: srcNATS, Kind: "stream.leader_elected", Type: "STREAM.LEADER_ELECTED", Server: "nats-2",
			Account: Account, Stream: "order-jobs", Summary: "order-jobs -> nats-2 (term 412)"},
		{Time: ts(s.at(9, 41, 10)), Source: srcK8s, Kind: "k8s.pod.killing", Type: "Killing", Server: "nats-2",
			Summary: "pod nats-2 Killing (node upgrade), grace 120s"},
		{Time: ts(s.at(9, 31, 7)), Source: srcNATS, Kind: "consumer.deleted", Type: "CONSUMER.DELETED", Severity: critical, Server: "nats-0",
			Account: Account, Stream: "order-archive", Consumer: "analytics-aggregator",
			ClientName: "analytics-worker", ClientHost: "10.20.0.12",
			Summary: "analytics-aggregator on order-archive by analytics-worker@10.20.0.12",
			Raised:  []*v1.Finding{byCode["consumer-006"]}},
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
		{Time: ts(s.at(9, 16, 2)), Source: srcK8s, Kind: "k8s.pod.killing", Type: "Killing", Server: "nats-0",
			Summary: "pod nats-0 Killing (node upgrade), grace 120s"},
		{Time: ts(s.at(9, 14, 48)), Source: srcK8s, Kind: "k8s.node.drained", Type: "NodeDrain",
			Summary: "node gke-example-pool-1-7f3a cordoned and drained"},
	}
	// Routine reconnects of one client fill the hours before the upgrade.
	for k := range fillerCount {
		kind, typ := "client.connect", "CLIENT.CONNECT"
		if k%2 == 0 {
			kind, typ = "client.disconnect", "CLIENT.DISCONNECT"
		}
		host := fmt.Sprintf("10.20.2.%d", 10+k%40)
		out = append(out, &v1.Event{
			Time: ts(s.at(9, 10, 0).Add(-time.Duration(k) * 330 * time.Second)), Source: srcNATS, Kind: kind, Type: typ,
			Server: s.sc.servers[k%3].name, Account: Account, ClientName: "catalog-api", ClientHost: host,
			Summary: "catalog-api " + kind[len("client."):] + " from " + host,
		})
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
