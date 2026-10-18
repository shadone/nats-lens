package fake

import (
	"cmp"
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
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
	// peers are the servers with a replica, the leader first; empty means
	// the first three servers of the scenario.
	peers []string
	// behind is a follower the leader reports not current, lag entries back.
	behind string
	lag    int64
	// disagrees is a follower whose configuration differs from the
	// leader's; missing a follower the leader lists that does not report
	// the stream; leaderless is a stream no replica leads in the newest
	// epoch.
	disagrees  string
	missing    string
	leaderless bool
	// maxBytes and discard are the stream's byte limit and discard
	// policy; zero means no limit and "" discard old.
	maxBytes int64
	discard  string
}

// maxBytesText is max_bytes as the server reports it: -1 without a limit.
func (sp streamSpec) maxBytesText() string {
	if sp.maxBytes <= 0 {
		return "-1"
	}
	return strconv.FormatInt(sp.maxBytes, 10)
}

func isKV(name string) bool { return strings.HasPrefix(name, "KV_") || strings.HasPrefix(name, "OBJ_") }

// leader is the stream's leader: its first peer, none for a leaderless
// stream.
func (s scen) leader(sp streamSpec) string {
	if sp.leaderless {
		return ""
	}
	return s.sc.peers(sp)[0]
}

func (s scen) streamSummary(sp streamSpec, cur []*v1.Finding, r *v1.TimeRange) *v1.StreamSummary {
	e := streamRef(sp.name)
	st, codes := worstStatus(cur, e)
	out := &v1.StreamSummary{
		Entity: e, Replicas: ptr(int32(len(s.sc.peers(sp)))), Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)), Consumers: ptr(int32(len(sp.consumers) + sp.ephemeral)),
		BytesPerDay: s.growth(e, r), Status: st, FindingCodes: codes, Leader: s.leader(sp), Placement: s.placementAt(sp, s.epochTime(r), false),
		ServersDisagree: sp.disagrees != "",
		Bytes_7D:        s.series("stream.bytes", e, &v1.TimeRange{Start: ts(r.GetEnd().AsTime().Add(-7 * 24 * time.Hour)), End: r.GetEnd()}, 28),
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
	var watchers int32
	for _, sp := range p.sc.streams {
		watchers += int32(sp.ephemeral)
		if (req.GetAccount() != "" && req.GetAccount() != Account) || (isKV(sp.name) && !req.GetIncludeKv()) {
			continue
		}
		res.Streams = append(res.Streams, s.streamSummary(sp, cur, req.GetRange()))
		res.Totals.Bytes += int64(sp.bytes)
		res.Totals.DurableConsumers += int32(len(sp.consumers))
		res.Totals.EphemeralConsumers += int32(sp.ephemeral)
	}
	if !req.GetIncludeKv() {
		res.Totals.EphemeralConsumers = watchers // the KV watchers exist whether or not their bucket is listed
	}
	sortByStatus(res.Streams, (*v1.StreamSummary).GetStatus, func(x *v1.StreamSummary) string { return x.GetEntity().GetName() })
	return api.LimitStreams(res, req.GetLimit()), nil
}

func (sc *scenario) findStream(account, name string) (streamSpec, error) {
	for _, sp := range sc.streams {
		if account == Account && sp.name == name {
			return sp, nil
		}
	}
	return streamSpec{}, fmt.Errorf("stream %s/%s: %w", account, name, api.ErrNotFound)
}

// GetStream returns one stream with each replica's view.
func (p *Provider) GetStream(_ context.Context, req *v1.GetStreamRequest) (*v1.GetStreamResponse, error) {
	sp, err := p.sc.findStream(req.GetAccount(), req.GetStream())
	if err != nil {
		return nil, err
	}
	s := p.scen()
	cur := current(p.findings(s), req.GetRange())
	peers := p.sc.peers(sp)
	res := &v1.GetStreamResponse{
		Stream: s.streamSummary(sp, cur, req.GetRange()),
		Config: cfg([][2]string{
			{"subjects", strings.ReplaceAll(sp.name, "-", ".") + ".>"}, {"retention", "limits"}, {"storage", "file"},
			{"replicas", fmt.Sprint(len(peers))}, {"max_bytes", sp.maxBytesText()}, {"max_msgs", "-1"}, {"max_age", "0"},
			{"discard", cmp.Or(sp.discard, "old")},
		}),
	}
	res.Servers = s.streamViews(sp, s.epochTime(req.GetRange()))
	if sp.disagrees != "" {
		hashes := &v1.Disagreement{Field: "config_hash"}
		for _, v := range res.GetServers() {
			hashes.Values = append(hashes.Values, &v1.ServerValue{Server: v.GetServer(), Value: v.GetConfigHash()})
		}
		res.Disagreements = append(res.Disagreements, hashes)
	}
	for _, f := range cur {
		if matchesEntity(f.GetEntity(), res.GetStream().GetEntity()) {
			res.Findings = append(res.Findings, f)
		}
	}
	res.Consumers = s.withRates(s.consumers(sp, cur, true), req.GetRange())
	return res, nil
}

// streamViews is each server's view of a stream at the scenario time at:
// the servers that report it, which leaves out the follower the leader
// lists as missing and every server offline then.
func (s scen) streamViews(sp streamSpec, at time.Time) []*v1.StreamServerView {
	var out []*v1.StreamServerView
	for i, name := range s.sc.peers(sp) {
		if name == sp.missing || s.offline(name, at) {
			continue
		}
		v := &v1.StreamServerView{
			Server: name, IsLeader: i == 0, Current: true, Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)),
			FirstSeq: ptr(int64(1)), LastSeq: ptr(sp.msgs), Created: ts(s.on(2, 8, 30)), ConfigHash: "c4f1a0", Lag: followerLag(i),
		}
		switch name {
		case sp.behind:
			v.Current, v.Lag = false, ptr(sp.lag)
		case sp.disagrees:
			v.ConfigHash = "7d3e91"
		}
		if sp.leaderless {
			// Without a leader no server says how a replica stands.
			v.IsLeader, v.Current, v.Lag = false, false, nil
		}
		out = append(out, v)
	}
	return out
}

// backlog is a consumer's steady pending and ack pending counts, fixed
// per stream and consumer name so that the rows of the demo differ.
func backlog(stream, consumer string) (pending, ackPending int64) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(stream + "/" + consumer))
	n := int64(h.Sum32())
	pending = n % 260
	if n%7 == 0 {
		pending = 0
	}
	return pending, min(pending, (n>>9)%40)
}

func (s scen) consumerSummary(sp streamSpec, name string, cur []*v1.Finding, present bool) *v1.ConsumerSummary {
	e := consumerRef(sp.name, name)
	st, codes := worstStatus(cur, e)
	pending, acks := backlog(sp.name, name)
	c := &v1.ConsumerSummary{
		Entity: e, Durable: !strings.HasPrefix(name, "oc_"), Replicas: ptr(int32(len(s.sc.peers(sp)))), Leader: s.leader(sp), Status: st, FindingCodes: codes,
		NumAckPending: ptr(acks), NumRedelivered: ptr(int64(0)), NumPending: ptr(pending), NumWaiting: ptr(int64(0)), LastDelivered: ts(s.now.Add(-2 * time.Second)), Present: present,
	}
	if !c.GetDurable() {
		c.Replicas = ptr(int32(1))
	}
	if !present {
		// A consumer missing from every server reports no numbers; its
		// replicas come from the inventory.
		c.NumAckPending, c.NumRedelivered, c.NumPending, c.NumWaiting = nil, nil, nil, nil
		c.Leader, c.LastDelivered = "", nil
	}
	if s.sc.consumer != nil {
		s.sc.consumer(s, c)
	}
	return c
}

func (s scen) consumers(sp streamSpec, cur []*v1.Finding, ephemeral bool) []*v1.ConsumerSummary {
	var out []*v1.ConsumerSummary
	for _, n := range sp.consumers {
		out = append(out, s.consumerSummary(sp, n, cur, true))
	}
	for _, n := range sp.expected {
		out = append(out, s.consumerSummary(sp, n, cur, false))
	}
	if ephemeral {
		for i := range sp.ephemeral {
			out = append(out, s.consumerSummary(sp, fmt.Sprintf("oc_%04d", i+1), cur, true))
		}
	}
	return out
}

// ListConsumers returns the consumers, worst first.
func (p *Provider) ListConsumers(_ context.Context, req *v1.ListConsumersRequest) (*v1.ListConsumersResponse, error) {
	s := p.scen()
	cur := current(p.findings(s), req.GetRange())
	res := &v1.ListConsumersResponse{}
	for _, sp := range p.sc.streams {
		if (req.GetAccount() != "" && req.GetAccount() != Account) || (req.GetStream() != "" && req.GetStream() != sp.name) {
			continue
		}
		res.Consumers = append(res.Consumers, s.consumers(sp, cur, req.GetIncludeEphemeral())...)
	}
	sortByStatus(res.Consumers, (*v1.ConsumerSummary).GetStatus, func(x *v1.ConsumerSummary) string { return x.GetEntity().GetKey() })
	res.Consumers = sortedConsumers(s.withRates(res.Consumers, req.GetRange()), req)
	return res, nil
}

// GetConsumer returns one consumer with each replica's view, and the
// creation times when they disagree.
func (p *Provider) GetConsumer(_ context.Context, req *v1.GetConsumerRequest) (*v1.GetConsumerResponse, error) {
	sp, err := p.sc.findStream(req.GetAccount(), req.GetStream())
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
	s.withRates([]*v1.ConsumerSummary{c}, req.GetRange())
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
	res.Servers, res.Disagreements = s.consumerViews(sp, c, s.epochTime(req.GetRange()))
	return res, nil
}

// consumerViews is each server's view of a present consumer at the
// scenario time at, and the creation time disagreement when the
// scenario gives one: the servers that report its stream then.
func (s scen) consumerViews(sp streamSpec, c *v1.ConsumerSummary, at time.Time) ([]*v1.ConsumerServerView, []*v1.Disagreement) {
	peers := s.sc.peers(sp)
	var created []time.Time
	disagree := false
	if s.sc.created != nil {
		created, disagree = s.sc.created(s, sp.name, c.GetEntity().GetConsumer())
	}
	if created == nil {
		for range peers {
			created = append(created, s.on(12, 10, 2))
		}
	}
	var out []*v1.ConsumerServerView
	dis := &v1.Disagreement{Field: "created"}
	for i, name := range peers {
		if name == sp.missing || s.offline(name, at) {
			// A server that does not report the stream reports no view.
			continue
		}
		out = append(out, &v1.ConsumerServerView{
			Server: name, IsLeader: i == 0 && !sp.leaderless, Current: !sp.leaderless, DeliveredStreamSeq: ptr(sp.msgs),
			AckFloorStreamSeq: ptr(sp.msgs - c.GetNumAckPending()), Lag: followerLag(i), Created: ts(created[i]), ConfigHash: "9e02b7",
		})
		dis.Values = append(dis.Values, &v1.ServerValue{Server: name, Value: created[i].Format(time.RFC3339)})
	}
	if sp.leaderless {
		for _, v := range out {
			v.Lag = nil
		}
	}
	if !disagree {
		return out, nil
	}
	return out, []*v1.Disagreement{dis}
}

// followerLag is the lag the leader (peer 0) reports for peer i: none
// for itself, zero for a follower that is current.
func followerLag(i int) *int64 {
	if i == 0 {
		return nil
	}
	return ptr(int64(0))
}
