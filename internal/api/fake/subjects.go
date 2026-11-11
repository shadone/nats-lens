package fake

import (
	"cmp"
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// subsInterval is how often the demo's collector reads the subscription
// lists, and subszLimit the most subscriptions it keeps per server: the
// defaults of subscriptions_interval and subsz_limit.
const (
	subsInterval = 5 * time.Minute
	subszLimit   = 10_000
)

// serverSubscriptions is what a server holds when its spec sets none.
const serverSubscriptions = 1840

// clientSub is a subscription each connection of a client holds: a subject
// in a queue group (empty for none), or with inbox the connection's own
// _INBOX.<id>.* subject. first, when set, limits it to the client's first
// connections in the connection list. rate is the messages a second each
// connection takes on it (a queue group's members take 1, 0.9 and 1.1
// times it in turn); 0 gives a small rate of the subject's own. times is
// how often each connection holds it, folded into one row (0 is once),
// from timesFrom (in the fixed frame; zero: always) on.
type clientSub struct {
	subject, queue string
	first          int
	inbox          bool
	rate           float64
	times          int64
	timesFrom      time.Time
}

// rateOrigin is how many subscription intervals before the newest epoch
// the demo's delivered counts start, so a collection's counts are whole
// multiples of a rate and the rates read exactly.
const rateOrigin = 100_000

// delivered is the count of messages delivered at the collection at c to
// a subscription taking perSecond.
func (s scen) delivered(c time.Time, perSecond float64) int64 {
	intervals := c.Sub(s.lastEpoch().Add(-rateOrigin*subsInterval)).Seconds() / subsInterval.Seconds()
	return int64(math.Round(perSecond * subsInterval.Seconds() * intervals))
}

// smallRate is a stable rate between 0.05 and 2.5 messages a second for a
// subject without one of its own.
func smallRate(subject string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(subject))
	return float64(h.Sum32()%50+1) / 20
}

// memberShare is a queue group member's share of the rate, by its index.
var memberShare = []float64{1, 0.9, 1.1}

// extraConn is a connection beside those the scenario's clients hold in
// account Account: one of nats-lens's own, or a monitoring tool, with the
// subjects it subscribes to.
type extraConn struct {
	server, name, account, ip, user string
	cid                             int64
	subjects                        []string
	// since is when it connected, in the fixed frame; zero means with the
	// server's process.
	since time.Time
}

// subjectsOf is a stream's subjects: none for a mirror; a KV bucket's and
// an object store's as the server sets them; else the spec's, or its name
// with dots for dashes and .>.
func subjectsOf(sp streamSpec) []string {
	switch {
	case sp.mirror != "":
		return nil
	case strings.HasPrefix(sp.name, "KV_"):
		return []string{"$KV." + strings.TrimPrefix(sp.name, "KV_") + ".>"}
	case strings.HasPrefix(sp.name, "OBJ_"):
		b := strings.TrimPrefix(sp.name, "OBJ_")
		return []string{"$O." + b + ".C.>", "$O." + b + ".M.>"}
	case sp.subjects != nil:
		return sp.subjects
	}
	return []string{strings.ReplaceAll(sp.name, "-", ".") + ".>"}
}

// subsTotal is the subscriptions a server holds.
func (sp serverSpec) subsTotal() int64 { return cmp.Or(sp.subs, serverSubscriptions) }

// token is a stable inbox-like token for a text.
func token(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return strings.ToUpper(fmt.Sprintf("%012x", h.Sum64()&0xffffffffffff))
}

// deliverSubject is the deliver subject of a stream's ephemeral push
// consumer.
func deliverSubject(stream, consumer string) string {
	return api.InboxPrefix + token(stream+"/"+consumer)
}

// subsCollection is when the subscription collection at or before the
// epoch at started: every subsInterval back from the newest epoch, within
// the stored history and api.SubscriptionsMaxAge; false when the scenario
// collects none.
func (s scen) subsCollection(at time.Time) (time.Time, bool) {
	if len(s.sc.subs) == 0 {
		return time.Time{}, false
	}
	back := s.lastEpoch().Sub(at)
	n := (back + subsInterval - 1) / subsInterval
	c := s.lastEpoch().Add(-n * subsInterval)
	if c.Before(s.dataStart()) || at.Sub(c) > api.SubscriptionsMaxAge {
		return time.Time{}, false
	}
	return c, true
}

// rateWindow reports whether the 15 minutes before the range's end hold
// two collections: the stored and API rates need two reports.
func (s scen) rateWindow(r *v1.TimeRange) bool {
	end := r.GetEnd().AsTime()
	last := earliest(end, s.dataEnd())
	return last.Sub(end.Add(-api.RateSpan)) > sampleEvery
}

// storedRate is a stream's stored messages per second: a mirror's is its
// origin's.
func (sc *scenario) storedRate(sp streamSpec) float64 {
	if sp.mirror != "" {
		if o, err := sc.findStream(Account, sp.mirror); err == nil {
			return sc.storedRate(o)
		}
	}
	if sp.storedRate != nil {
		return *sp.storedRate
	}
	if sp.msgs <= 0 || sp.bytes <= 0 || sp.perDay <= 0 {
		return 0
	}
	return sp.perDay / (sp.bytes / float64(sp.msgs)) / 86400
}

// subjectStreams are the streams of the epoch at at with their consumers;
// the ephemeral ones are push consumers. Rates only when the range has
// two collections in its last 15 minutes.
func (s scen) subjectStreams(at time.Time, r *v1.TimeRange) []api.SubjectStreamFact {
	rated := s.rateWindow(r)
	var out []api.SubjectStreamFact
	for _, sp := range s.sc.streams {
		l := s.sc.streamLimitsOf(sp)
		x := api.SubjectStreamFact{Account: Account, Stream: sp.name, Retention: l.retention, Mirror: sp.mirror, Subjects: subjectsOf(sp),
			Sources: sp.sources, Messages: ptr(sp.msgs)}
		if rated {
			x.StoredPerSecond = ptr(s.sc.storedRate(sp))
		}
		for _, c := range s.consumersAt(sp, at) {
			x.Consumers = append(x.Consumers, api.SubjectConsumerFact{Name: c, Durable: true})
		}
		for i := range sp.ephemeral {
			name := fmt.Sprintf("oc_%04d", i+1)
			x.Consumers = append(x.Consumers, api.SubjectConsumerFact{Name: name, DeliverSubject: deliverSubject(sp.name, name)})
		}
		out = append(out, x)
	}
	return out
}

// subscriptionsAt are the subscriptions the servers listed at the
// collection at c, and each server's CONNZ then: the clients' own by the
// scenario's subs, the extra connections', the ephemeral consumers'
// deliver subjects held in turn by their watchers' connections, and the
// servers' own (JetStream's, and the system account's up to the count its
// accounts row shows).
func (s scen) subscriptionsAt(c time.Time) ([]api.SubscriptionFact, map[string]api.ServerConnections) {
	return s.subscriptionsOf(c, s.connections(c))
}

// subscriptionsOf is subscriptionsAt over the given connections of the
// collection at c.
func (s scen) subscriptionsOf(c time.Time, list []*v1.Connection) ([]api.SubscriptionFact, map[string]api.ServerConnections) {
	conns := map[string]api.ServerConnections{}
	byName := map[string][]*v1.Connection{}
	var names []string
	for _, x := range list {
		cs := conns[x.GetServer()]
		if cs.Names == nil {
			cs = api.ServerConnections{Complete: true, Names: map[uint64]string{}}
		}
		cs.Names[uint64(x.GetCid())] = x.GetName()
		conns[x.GetServer()] = cs
		if byName[x.GetName()] == nil {
			names = append(names, x.GetName())
		}
		byName[x.GetName()] = append(byName[x.GetName()], x)
	}
	var out []api.SubscriptionFact
	add := func(x *v1.Connection, subject, queue string, times int64, rate float64) {
		times = max(times, 1)
		out = append(out, api.SubscriptionFact{Server: x.GetServer(), Account: x.GetAccount(), Subject: subject, Queue: queue,
			CID: uint64(x.GetCid()), Subs: times, Msgs: times * s.delivered(c, rate)})
	}
	for _, name := range names {
		for _, sub := range s.sc.subs[name] {
			for i, x := range byName[name] {
				rate := cmp.Or(sub.rate, smallRate(name+" "+sub.subject))
				if sub.queue != "" {
					rate *= memberShare[i%len(memberShare)]
				}
				switch {
				case sub.first > 0 && i >= sub.first:
				case sub.inbox:
					add(x, api.InboxPrefix+token(x.GetKey())+".*", "", 1, rate)
				case !sub.timesFrom.IsZero() && c.Before(sub.timesFrom.Add(s.shift)):
					add(x, sub.subject, sub.queue, 1, rate)
				default:
					add(x, sub.subject, sub.queue, sub.times, rate)
				}
			}
		}
	}
	for _, e := range s.sc.extraConns {
		for _, x := range byName[e.name] {
			for _, subj := range e.subjects {
				add(x, subj, "", 1, smallRate(e.name+" "+subj))
			}
		}
	}
	out = append(out, s.microSubscriptions(c)...)
	for _, sp := range s.sc.streams {
		watchers := sp.watchers
		if len(watchers) == 0 {
			watchers = s.sc.clients[:1]
		}
		for i := range sp.ephemeral {
			list := byName[watchers[i%len(watchers)]]
			if len(list) == 0 {
				continue
			}
			add(list[(i/len(watchers))%len(list)], deliverSubject(sp.name, fmt.Sprintf("oc_%04d", i+1)), "", 1, 0)
		}
	}
	var up []string
	for _, sp := range s.sc.servers {
		if !s.downAt(sp, c) {
			up = append(up, sp.name)
		}
	}
	sys := s.sc.systemAccount()
	var sysSubs int64
	if sys != "" {
		for _, o := range s.sc.auth.others {
			if o.id == sys {
				sysSubs = o.subs
			}
		}
		for _, x := range out {
			if x.Account == sys {
				sysSubs -= x.Subs
			}
		}
	}
	for i, name := range up {
		out = append(out, api.SubscriptionFact{Server: name, Account: Account, Subject: "$JS.API.>", CID: 1, Subs: 26})
		if sys != "" {
			n := sysSubs / int64(len(up))
			if i == 0 {
				n += sysSubs % int64(len(up))
			}
			out = append(out, api.SubscriptionFact{Server: name, Account: sys, Subject: "$SYS.REQ.SERVER.PING.>", CID: 2, Subs: n})
		}
	}
	return out, conns
}

// subszServers are the servers asked at the collection at c: each that
// answered with its subscriptions, listed up to subszLimit.
func (s scen) subszServers(c time.Time) []api.SubszServerFact {
	var out []api.SubszServerFact
	for _, sp := range s.sc.servers {
		x := api.SubszServerFact{Name: sp.name}
		if !s.downAt(sp, c) {
			x.Answered, x.Total, x.Listed = true, ptr(sp.subsTotal()), ptr(min(sp.subsTotal(), int64(subszLimit)))
		}
		out = append(out, x)
	}
	return out
}

// serverLists are the servers' subscription lists at the collection at
// c, by name: those that answered, complete unless cut at subszLimit.
func (s scen) serverLists(c time.Time) map[string]api.ServerList {
	out := map[string]api.ServerList{}
	for _, x := range s.subszServers(c) {
		if x.Answered {
			out[x.Name] = api.ServerList{At: c, Complete: *x.Listed >= *x.Total}
		}
	}
	return out
}

// ratedSubscriptions are the subscriptions of the collection at c with
// their delivered rates from the collection before it, by
// api.DeliveredRates; none has a rate when that collection lies before
// the stored history.
func (s scen) ratedSubscriptions(c time.Time) ([]api.SubscriptionFact, map[string]api.ServerConnections) {
	subs, conns := s.subscriptionsAt(c)
	prev := c.Add(-subsInterval)
	if prev.Before(s.dataStart()) {
		return subs, conns
	}
	before, _ := s.subscriptionsAt(prev)
	return api.DeliveredRates(subs, before, s.serverLists(c), s.serverLists(prev), api.RateGap(subsInterval)), conns
}

// systemAccount is the scenario's system account; empty when it lists
// none.
func (sc *scenario) systemAccount() string {
	if sc.auth != nil {
		for _, o := range sc.auth.others {
			if o.system {
				return o.id
			}
		}
	}
	return ""
}

// Subjects builds the facts of the range's newest epoch from the
// scenario: its streams with their consumers, the stream account's
// JetStream API, and the subscription collection at or before the epoch
// grouped by api.GroupSubscriptions.
func (p *Provider) Subjects(_ context.Context, req *v1.GetSubjectsRequest) (*api.SubjectsFacts, error) {
	s := p.scen()
	r := req.GetRange()
	at, n, ok := s.epochBefore(r)
	if !ok {
		return &api.SubjectsFacts{}, nil
	}
	f := &api.SubjectsFacts{Epoch: n, EpochAt: at, SystemAccount: p.sc.systemAccount(), Streams: s.subjectStreams(at, r)}
	js := api.JetStreamAPIFact{Account: Account}
	if s.rateWindow(r) {
		js.PerSecond, js.ErrorShare = ptr(p.sc.apiRate), ptr(p.sc.apiErrors)
	}
	f.APIs = append(f.APIs, js)
	if c, ok := s.subsCollection(at); ok {
		subs, conns := s.ratedSubscriptions(c)
		deliver := map[[2]string]bool{}
		for _, st := range f.Streams {
			for _, x := range st.Consumers {
				if x.DeliverSubject != "" {
					deliver[[2]string{st.Account, x.DeliverSubject}] = true
				}
			}
		}
		g := api.GroupSubscriptions(subs, conns, deliver)
		f.Subscriptions = &api.SubscriptionFacts{At: c, Servers: s.subszServers(c), Groups: g.Groups, Deliver: g.Deliver,
			ServerSubscriptions: g.ServerSubscriptions, Unlisted: g.Unlisted}
	}
	return f, nil
}

// MatchSubject reads what Subjects reads for the tester: the streams and
// the subscription collection's groups by subject and queue.
func (p *Provider) MatchSubject(_ context.Context, req *v1.MatchSubjectRequest) (*api.MatchFacts, error) {
	s := p.scen()
	r := req.GetRange()
	at, n, ok := s.epochBefore(r)
	if !ok {
		return &api.MatchFacts{}, nil
	}
	f := &api.MatchFacts{Epoch: n, EpochAt: at, SystemAccount: p.sc.systemAccount(), Streams: s.subjectStreams(at, r),
		JetStreamAccounts: []string{Account}}
	if c, ok := s.subsCollection(at); ok {
		subs, conns := s.ratedSubscriptions(c)
		f.SubscriptionsAt, f.Core = &c, api.CoreGroups(subs, conns)
	}
	return f, nil
}
