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
	"github.com/shadone/nats-lens/internal/checks"
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
	// retention and storage are "" for limits and file; maxMsgs,
	// perSubject and maxAge are the other limits, zero for none. A KV
	// bucket's or object store's limits come from its bucketSpec instead.
	retention, storage  string
	maxMsgs, perSubject int64
	maxAge              time.Duration
	// held is the age of the oldest message at the scenario's present;
	// zero means maxAge (a stream at its age limit).
	held time.Duration
	// mirror is the stream this one mirrors; sources those it sources.
	mirror  string
	sources []string
	// declared is a stream the inventory declares unbounded on purpose.
	declared bool
	// vanished are durables the servers held until a time and no server
	// holds since; they are listed for a range whose newest epoch is
	// before that time.
	vanished []vanishedConsumer
	// subjects are the stream's subjects; nil means its name with dots
	// for dashes and .> (see subjectsOf).
	subjects []string
	// storedRate is the messages stored per second; nil means its growth
	// in bytes over its mean message size.
	storedRate *float64
	// watchers are the clients whose connections subscribe to the deliver
	// subjects of the ephemeral consumers, in turn; nil means the
	// scenario's first client.
	watchers []string
	// created is when the stream was created, in the fixed frame; zero
	// means 2026-10-02 08:30. lastSeq is its last sequence; zero means
	// msgs, so that the first is 1.
	created time.Time
	lastSeq int64
}

// createdAt is when a stream was created, in the scenario.
func (s scen) createdAt(sp streamSpec) time.Time {
	if sp.created.IsZero() {
		return s.on(2, 8, 30)
	}
	return sp.created.Add(s.shift)
}

// seqs are a stream's first and last sequence: the last, and the first
// msgs before it.
func (sp streamSpec) seqs() (first, last int64) {
	last = cmp.Or(sp.lastSeq, sp.msgs)
	return last - sp.msgs + 1, last
}

// vanishedConsumer is a durable that no server held from at, a time in
// the fixed frame, on.
type vanishedConsumer struct {
	name string
	at   time.Time
}

// consumersAt are the durables of a stream at the scenario time at: its
// own and the vanished ones not yet gone.
func (s scen) consumersAt(sp streamSpec, at time.Time) []string {
	out := slices.Clone(sp.consumers)
	for _, v := range sp.vanished {
		if at.Before(v.at.Add(s.shift)) {
			out = append(out, v.name)
		}
	}
	return out
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
		Entity: e, Replicas: ptr(int32(len(s.sc.peers(sp)))), Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)), Consumers: ptr(int32(len(s.consumersAt(sp, s.epochTime(r))) + sp.ephemeral)),
		BytesPerDay: s.growth(e, r), Status: st, FindingCodes: codes, Leader: s.streamLeaderAt(sp, s.epochTime(r)), Placement: s.placementAt(sp, s.epochTime(r), false),
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
		res.Totals.DurableConsumers += int32(len(s.consumersAt(sp, s.epochTime(req.GetRange()))))
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
	res := &v1.GetStreamResponse{
		Stream: s.streamSummary(sp, cur, req.GetRange()),
		Config: s.streamConfig(sp),
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
	res.Consumers = s.withRates(s.consumers(sp, cur, true, s.epochTime(req.GetRange())), req.GetRange())
	return res, nil
}

// streamConfig is the configuration GetStream lists: the limits the
// Capacity page reads.
func (s scen) streamConfig(sp streamSpec) []*v1.ConfigValue {
	l := s.sc.streamLimitsOf(sp)
	pairs := [][2]string{
		{"retention", l.retention}, {"storage", l.storage},
		{"replicas", fmt.Sprint(len(s.sc.peers(sp)))}, {"max_bytes", strconv.FormatInt(l.maxBytes, 10)},
		{"max_msgs", strconv.FormatInt(l.maxMsgs, 10)}, {"max_age", checks.DurationText(l.maxAge)},
		{"max_msgs_per_subject", strconv.FormatInt(l.perSubject, 10)}, {"discard", l.discard},
	}
	if subj := subjectsOf(sp); len(subj) > 0 {
		pairs = append([][2]string{{"subjects", strings.Join(subj, ", ")}}, pairs...)
	}
	if l.mirror != "" {
		pairs = append(pairs, [2]string{"mirror", l.mirror})
	}
	if len(sp.sources) > 0 {
		pairs = append(pairs, [2]string{"sources", strings.Join(sp.sources, ", ")})
	}
	if l.allowMsgTTL {
		pairs = append(pairs, [2]string{"allow_msg_ttl", "true"})
	}
	return cfg(pairs)
}

// streamViews is each server's view of a stream at the scenario time at:
// the servers that report it, which leaves out the follower the leader
// lists as missing and every server offline then.
func (s scen) streamViews(sp streamSpec, at time.Time) []*v1.StreamServerView {
	var out []*v1.StreamServerView
	leader := s.streamLeaderAt(sp, at)
	first, last := sp.seqs()
	for _, name := range s.sc.peers(sp) {
		if name == sp.missing || s.offline(name, at) {
			continue
		}
		v := &v1.StreamServerView{
			Server: name, IsLeader: name == leader, Current: true, Messages: ptr(sp.msgs), Bytes: ptr(int64(sp.bytes)),
			FirstSeq: ptr(first), LastSeq: ptr(last), Created: ts(s.createdAt(sp)), ConfigHash: "c4f1a0", Lag: followerLag(name == leader),
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

func (s scen) consumerSummary(sp streamSpec, name string, cur []*v1.Finding, present bool, at time.Time) *v1.ConsumerSummary {
	e := consumerRef(sp.name, name)
	st, codes := worstStatus(cur, e)
	pending, acks := backlog(sp.name, name)
	c := &v1.ConsumerSummary{
		Entity: e, Durable: !strings.HasPrefix(name, "oc_"), Replicas: ptr(int32(len(s.sc.peers(sp)))), Leader: s.consumerLeaderAt(sp, name, at), Status: st, FindingCodes: codes,
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

// consumers are a stream's consumers at the scenario time at: the
// durables then, the expected ones no server reports, and the ephemeral
// ones when asked for.
func (s scen) consumers(sp streamSpec, cur []*v1.Finding, ephemeral bool, at time.Time) []*v1.ConsumerSummary {
	var out []*v1.ConsumerSummary
	present := s.consumersAt(sp, at)
	for _, n := range present {
		out = append(out, s.consumerSummary(sp, n, cur, true, at))
	}
	for _, n := range sp.expected {
		if !slices.Contains(present, n) {
			out = append(out, s.consumerSummary(sp, n, cur, false, at))
		}
	}
	if ephemeral {
		for i := range sp.ephemeral {
			out = append(out, s.consumerSummary(sp, fmt.Sprintf("oc_%04d", i+1), cur, true, at))
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
		res.Consumers = append(res.Consumers, s.consumers(sp, cur, req.GetIncludeEphemeral(), s.epochTime(req.GetRange()))...)
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
	for _, x := range s.consumers(sp, cur, true, s.epochTime(req.GetRange())) {
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
	leader := s.consumerLeaderAt(sp, c.GetEntity().GetConsumer(), at)
	_, last := sp.seqs()
	for i, name := range peers {
		if name == sp.missing || s.offline(name, at) {
			// A server that does not report the stream reports no view.
			continue
		}
		out = append(out, &v1.ConsumerServerView{
			Server: name, IsLeader: name == leader, Current: !sp.leaderless, DeliveredStreamSeq: ptr(last),
			AckFloorStreamSeq: ptr(last - c.GetNumAckPending()), Lag: followerLag(name == leader), Created: ts(created[i]), ConfigHash: "9e02b7",
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

// followerLag is the lag the leader reports for a replica: none for
// itself, zero for a follower that is current.
func followerLag(leader bool) *int64 {
	if leader {
		return nil
	}
	return ptr(int64(0))
}
