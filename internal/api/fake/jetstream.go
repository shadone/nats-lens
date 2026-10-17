package fake

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

type streamSpec struct {
	name      string
	msgs      int64
	bytes     float64
	perDay    float64 // bytes per day
	consumers []string
	expected  []string // consumers the inventory lists that are missing
	ephemeral int
}

var streamSpecs = []streamSpec{
	{name: "order-jobs", msgs: 48211, bytes: 61.2 * mib, perDay: 2.1 * mib,
		consumers: []string{"resize-worker", "index-worker", "job-cache-projector"}},
	{name: "order-archive", msgs: 2914330, bytes: 3.92 * gib, perDay: 96 * mib,
		consumers: []string{"archive-exporter", "archive-indexer"}, expected: []string{"analytics-aggregator"}},
	{name: "audit-archive", msgs: 1108402, bytes: 1.07 * gib, perDay: 18 * mib, consumers: []string{"audit-archive-writer"}},
	{name: "catalog-changes", msgs: 312880, bytes: 402 * mib, perDay: 6.4 * mib,
		consumers: []string{"catalog-applier", "catalog-search-indexer"}},
	{name: "catalog-access", msgs: 640115, bytes: 188 * mib, perDay: 3.1 * mib, consumers: []string{"catalog-access-logger"}},
	{name: "order-callbacks", msgs: 9402, bytes: 12.7 * mib, perDay: 0.4 * mib, consumers: []string{"callback-dispatcher"}},
	{name: "media-windows", msgs: 18420, bytes: 208 * mib, perDay: 71 * mib,
		consumers: []string{"media-chunk-pool", "media-window-finalizer"}},
	{name: "account-changes", msgs: 1207, bytes: 1.8 * mib, perDay: 20 * kib, consumers: []string{"customer-cache-invalidator"}},
	{name: "KV_customer-config", msgs: 214, bytes: 96 * kib, perDay: 1 * kib, ephemeral: 41},
}

func isKV(name string) bool { return strings.HasPrefix(name, "KV_") || strings.HasPrefix(name, "OBJ_") }

func (s scen) streamSummary(sp streamSpec, cur []*v1.Finding, r *v1.TimeRange) *v1.StreamSummary {
	e := streamRef(sp.name)
	st, codes := worstStatus(cur, e)
	out := &v1.StreamSummary{
		Entity: e, Replicas: ptr(int32(3)), Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)), Consumers: ptr(int32(len(sp.consumers) + sp.ephemeral)),
		BytesPerDay: ptr(sp.perDay), Status: st, FindingCodes: codes, Leader: "nats-0",
		Bytes_7D: s.series("stream.bytes", e, &v1.TimeRange{Start: ts(r.GetEnd().AsTime().Add(-7 * 24 * time.Hour)), End: r.GetEnd()}, 28),
	}
	if len(sp.expected) > 0 {
		out.ConsumersExpected = ptr(int32(len(sp.consumers) + len(sp.expected)))
	}
	return out
}

func sortByStatus[T any](items []T, status func(T) v1.Status, key func(T) string) {
	slices.SortStableFunc(items, func(a, b T) int {
		return cmp.Or(cmp.Compare(statusRank(status(a)), statusRank(status(b))), cmp.Compare(key(a), key(b)))
	})
}

// ListStreams returns the streams, worst first.
func (p *Provider) ListStreams(_ context.Context, req *v1.ListStreamsRequest) (*v1.ListStreamsResponse, error) {
	s := p.scen()
	cur := current(p.findings(s), req.GetRange())
	res := &v1.ListStreamsResponse{Totals: &v1.StreamTotals{}}
	for _, sp := range streamSpecs {
		if (req.GetAccount() != "" && req.GetAccount() != Account) || (isKV(sp.name) && !req.GetIncludeKv()) {
			continue
		}
		res.Streams = append(res.Streams, s.streamSummary(sp, cur, req.GetRange()))
		res.Totals.Bytes += int64(sp.bytes)
		res.Totals.DurableConsumers += int32(len(sp.consumers))
		res.Totals.EphemeralConsumers += int32(sp.ephemeral)
	}
	if !req.GetIncludeKv() {
		res.Totals.EphemeralConsumers = 41 // the KV watchers exist whether or not their bucket is listed
	}
	sortByStatus(res.Streams, (*v1.StreamSummary).GetStatus, func(x *v1.StreamSummary) string { return x.GetEntity().GetName() })
	return res, nil
}

func findStream(account, name string) (streamSpec, error) {
	for _, sp := range streamSpecs {
		if account == Account && sp.name == name {
			return sp, nil
		}
	}
	return streamSpec{}, fmt.Errorf("stream %s/%s: %w", account, name, api.ErrNotFound)
}

// GetStream returns one stream with each server's view.
func (p *Provider) GetStream(_ context.Context, req *v1.GetStreamRequest) (*v1.GetStreamResponse, error) {
	sp, err := findStream(req.GetAccount(), req.GetStream())
	if err != nil {
		return nil, err
	}
	s := p.scen()
	cur := current(p.findings(s), req.GetRange())
	res := &v1.GetStreamResponse{
		Stream: s.streamSummary(sp, cur, req.GetRange()),
		Config: cfg([][2]string{
			{"subjects", strings.ReplaceAll(sp.name, "-", ".") + ".>"}, {"retention", "limits"}, {"storage", "file"},
			{"replicas", "3"}, {"max_bytes", "-1"}, {"max_msgs", "-1"}, {"max_age", "0"}, {"discard", "old"},
		}),
	}
	for i, sv := range serverSpecs {
		res.Servers = append(res.Servers, &v1.StreamServerView{
			Server: sv.name, IsLeader: i == 0, Current: true, Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)),
			FirstSeq: ptr(int64(1)), LastSeq: ptr(sp.msgs), Created: ts(s.on(2, 8, 30)), ConfigHash: "c4f1a0", Lag: followerLag(i),
		})
	}
	for _, f := range cur {
		if matchesEntity(f.GetEntity(), res.GetStream().GetEntity()) {
			res.Findings = append(res.Findings, f)
		}
	}
	res.Consumers = s.consumers(sp, cur, true)
	return res, nil
}

func (s scen) consumerSummary(stream, name string, cur []*v1.Finding, present bool) *v1.ConsumerSummary {
	e := consumerRef(stream, name)
	st, codes := worstStatus(cur, e)
	c := &v1.ConsumerSummary{
		Entity: e, Durable: !strings.HasPrefix(name, "oc_"), Replicas: ptr(int32(3)), Leader: "nats-0", Status: st, FindingCodes: codes,
		NumAckPending: ptr(int64(12)), NumRedelivered: ptr(int64(0)), NumPending: ptr(int64(40)), NumWaiting: ptr(int64(0)), LastDelivered: ts(s.now.Add(-2 * time.Second)), Present: present,
		ServersDisagree: name == "resize-worker",
	}
	if !c.GetDurable() {
		c.Replicas = ptr(int32(1))
	}
	switch {
	case !present:
		// A consumer missing from every server reports no numbers; its
		// replicas come from the inventory.
		c.NumAckPending, c.NumRedelivered, c.NumPending, c.NumWaiting = nil, nil, nil, nil
		c.Leader, c.LastDelivered = "", ts(s.at(9, 31, 0))
	case name == "media-chunk-pool":
		c.NumPending, c.NumAckPending, c.NumRedelivered = ptr(int64(18420)), ptr(int64(640)), ptr(int64(37))
	}
	return c
}

func (s scen) consumers(sp streamSpec, cur []*v1.Finding, ephemeral bool) []*v1.ConsumerSummary {
	var out []*v1.ConsumerSummary
	for _, n := range sp.consumers {
		out = append(out, s.consumerSummary(sp.name, n, cur, true))
	}
	for _, n := range sp.expected {
		out = append(out, s.consumerSummary(sp.name, n, cur, false))
	}
	if ephemeral {
		for i := range sp.ephemeral {
			out = append(out, s.consumerSummary(sp.name, fmt.Sprintf("oc_%04d", i+1), cur, true))
		}
	}
	return out
}

// ListConsumers returns the consumers, worst first.
func (p *Provider) ListConsumers(_ context.Context, req *v1.ListConsumersRequest) (*v1.ListConsumersResponse, error) {
	s := p.scen()
	cur := current(p.findings(s), req.GetRange())
	res := &v1.ListConsumersResponse{}
	for _, sp := range streamSpecs {
		if (req.GetAccount() != "" && req.GetAccount() != Account) || (req.GetStream() != "" && req.GetStream() != sp.name) {
			continue
		}
		res.Consumers = append(res.Consumers, s.consumers(sp, cur, req.GetIncludeEphemeral())...)
	}
	sortByStatus(res.Consumers, (*v1.ConsumerSummary).GetStatus, func(x *v1.ConsumerSummary) string { return x.GetEntity().GetKey() })
	return res, nil
}

// GetConsumer returns one consumer; resize-worker shows the creation
// time disagreement of finding consumer-012.
func (p *Provider) GetConsumer(_ context.Context, req *v1.GetConsumerRequest) (*v1.GetConsumerResponse, error) {
	sp, err := findStream(req.GetAccount(), req.GetStream())
	if err != nil {
		return nil, err
	}
	s := p.scen()
	cur := current(p.findings(s), req.GetRange())
	var c *v1.ConsumerSummary
	for _, x := range s.consumers(sp, cur, true) {
		if x.GetEntity().GetConsumer() == req.GetConsumer() {
			c = x
		}
	}
	if c == nil {
		return nil, fmt.Errorf("consumer %s/%s/%s: %w", req.GetAccount(), req.GetStream(), req.GetConsumer(), api.ErrNotFound)
	}
	res := &v1.GetConsumerResponse{Consumer: c, Config: cfg([][2]string{
		{"durable_name", req.GetConsumer()}, {"ack_policy", "explicit"}, {"ack_wait", "60s"}, {"max_deliver", "5"}, {"max_ack_pending", "1000"},
	})}
	for _, f := range cur {
		if matchesEntity(f.GetEntity(), c.GetEntity()) {
			res.Findings = append(res.Findings, f)
		}
	}
	if !c.GetPresent() {
		return res, nil
	}
	created := []time.Time{s.on(12, 10, 2), s.on(12, 10, 2), s.on(12, 10, 2)}
	if req.GetConsumer() == "resize-worker" {
		created = []time.Time{time.Date(2026, 8, 12, 10, 2, 11, 0, time.UTC).Add(s.shift), s.at(9, 38, 44), s.at(9, 38, 44)}
		dis := &v1.Disagreement{Field: "created"}
		for i, sv := range serverSpecs {
			dis.Values = append(dis.Values, &v1.ServerValue{Server: sv.name, Value: created[i].Format(time.RFC3339)})
		}
		res.Disagreements = append(res.Disagreements, dis)
	}
	for i, sv := range serverSpecs {
		res.Servers = append(res.Servers, &v1.ConsumerServerView{
			Server: sv.name, IsLeader: i == 0, Current: true, DeliveredStreamSeq: ptr(sp.msgs), AckFloorStreamSeq: ptr(sp.msgs - 12), Lag: followerLag(i),
			Created: ts(created[i]), ConfigHash: "9e02b7",
		})
	}
	return res, nil
}

// followerLag is the lag the leader (server 0) reports for server i: none
// for itself, zero for a follower that is current.
func followerLag(i int) *int64 {
	if i == 0 {
		return nil
	}
	return ptr(int64(0))
}
