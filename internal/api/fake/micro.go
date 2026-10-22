package fake

import (
	"context"
	"math"
	"slices"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// microQueue is the queue group the micro framework gives an endpoint by
// default.
const microQueue = "q"

// microSpec is a service built on the micro framework that a client's
// connections run in account Account.
type microSpec struct {
	name, description string
	// client is the name of the connections its instances use.
	client    string
	instances []microInstanceSpec
	endpoints []microEndpointSpec
}

// microInstanceSpec is one instance: its ID and version, and the
// connection it runs on, the conn-th of its client's on server.
type microInstanceSpec struct {
	id, version, server string
	conn                int
	// started is when it started, in the fixed frame; from and to bound
	// when it runs (zero: always, still), and from is its start when set.
	started, from, to time.Time
	// hungFrom and hungTo are when it stops answering $SRV requests while
	// staying registered, and answers again; zero: never.
	hungFrom, hungTo time.Time
}

// microEndpointSpec is one endpoint: its requests per second over the
// service, shared evenly by the instances, their average time and error
// share; from slowFrom on it takes slowBy times as long. lastError is
// reported by the instance at index lastErrorBy.
type microEndpointSpec struct {
	name, subject string
	rate, ms      float64
	errors        float64
	slowFrom      time.Time
	slowBy        float64
	lastError     string
	lastErrorBy   int
}

// microRequestError is what an account's $SRV request to subject says
// when nats-lens's user may not publish to $SRV.>.
func microRequestError(subject string) string {
	return `nats: Permissions Violation for Publish to "` + subject + `"`
}

// runsAt reports whether an instance runs at fixed-frame time c.
func (in microInstanceSpec) runsAt(c time.Time) bool {
	return (in.from.IsZero() || !c.Before(in.from)) && (in.to.IsZero() || c.Before(in.to))
}

// answersAt reports whether a running instance answers at c.
func (in microInstanceSpec) answersAt(c time.Time) bool {
	return in.runsAt(c) && (in.hungFrom.IsZero() || c.Before(in.hungFrom) || !c.Before(in.hungTo))
}

// startedAt is when the instance started, in the fixed frame.
func (in microInstanceSpec) startedAt() time.Time {
	if !in.from.IsZero() {
		return in.from
	}
	return in.started
}

// microConn is the connection an instance runs on.
type microConn struct {
	server, name string
	cid          int64
}

// microConnections are, by instance ID, the connections the scenario's
// micro service instances run on at scenario time c, as connections
// lists them: none for an instance not running then or whose server is
// down. The conn-th connection of a client on a server is its
// (client's index + conn × clients)-th.
func (s scen) microConnections(c time.Time) map[string]microConn {
	out := map[string]microConn{}
	for _, m := range s.sc.micro {
		ci := slices.Index(s.sc.clients, m.client)
		for _, in := range m.instances {
			sp, ok := s.sc.server(in.server)
			if ci < 0 || !ok || !in.runsAt(s.clock(c)) || s.downAt(sp, c) {
				continue
			}
			if i := int64(ci + in.conn*len(s.sc.clients)); i < sp.conns {
				out[in.id] = microConn{server: in.server, name: m.client, cid: 100 + i}
			}
		}
	}
	return out
}

// microSubscriptions are the subscriptions of the instances running at c:
// the framework's nine $SRV subjects and each endpoint's subject in its
// queue group.
func (s scen) microSubscriptions(c time.Time) []api.SubscriptionFact {
	on := s.microConnections(c)
	var out []api.SubscriptionFact
	for _, m := range s.sc.micro {
		for _, in := range m.instances {
			x, ok := on[in.id]
			if !ok {
				continue
			}
			add := func(subject, queue string) {
				out = append(out, api.SubscriptionFact{Server: x.server, Account: Account, Subject: subject, Queue: queue, CID: uint64(x.cid),
					Subs: 1})
			}
			for _, verb := range []string{"PING", "INFO", "STATS"} {
				add(api.SrvPrefix+verb, "")
				add(api.SrvPrefix+verb+"."+m.name, "")
				add(api.SrvPrefix+verb+"."+m.name+"."+in.id, "")
			}
			for _, e := range m.endpoints {
				add(e.subject, microQueue)
			}
		}
	}
	return out
}

// answering are the instances of a service that answer at scenario time
// t: running, not hung, on a connection.
func (s scen) answering(m microSpec, t time.Time) []microInstanceSpec {
	on := s.microConnections(t)
	var out []microInstanceSpec
	for _, in := range m.instances {
		if _, ok := on[in.id]; ok && in.answersAt(s.clock(t)) {
			out = append(out, in)
		}
	}
	return out
}

// microCounters are an instance's counters of an endpoint at fixed-frame
// time c: its share of the endpoint's requests since it started, the
// errors among them, and their processing time, slowBy times as long
// from slowFrom on.
func microCounters(e microEndpointSpec, in microInstanceSpec, share float64, c time.Time) (req, errs, ns int64) {
	start := in.startedAt()
	secs := c.Sub(start).Seconds()
	if secs < 0 {
		return 0, 0, 0
	}
	r := e.rate * share * secs
	proc := r * e.ms * 1e6
	if !e.slowFrom.IsZero() && c.After(e.slowFrom) {
		before := e.rate * share * math.Max(0, e.slowFrom.Sub(start).Seconds())
		proc = before*e.ms*1e6 + (r-before)*e.ms*e.slowBy*1e6
	}
	return int64(r), int64(r * e.errors), int64(proc)
}

// averageAt is an endpoint's average time over the hour from fixed-frame
// time h, in milliseconds.
func (e microEndpointSpec) averageAt(h time.Time) float64 {
	end := h.Add(time.Hour)
	switch {
	case e.slowFrom.IsZero() || !end.After(e.slowFrom):
		return e.ms
	case !h.Before(e.slowFrom):
		return e.ms * e.slowBy
	}
	f := end.Sub(e.slowFrom).Hours()
	return e.ms*(1-f) + e.ms*e.slowBy*f
}

// epochsIn are the scenario's epochs in [from, to), within its data, in
// time order.
func (s scen) epochsIn(from, to time.Time) []time.Time {
	var out []time.Time
	for t := s.lastEpoch(); !t.Before(from) && !t.Before(s.dataStart()); t = t.Add(-sampleEvery) {
		if t.Before(to) {
			out = append([]time.Time{t}, out...)
		}
	}
	return out
}

// microAnswered are the services that answer at the epoch at, for the
// range r: their answering instances, and each endpoint's increase over
// the reports of the RateSpan before the range's end, its level over the
// 7 days before that and its slow run's start, by api's rules.
func (s scen) microAnswered(at time.Time, r *v1.TimeRange) []api.MicroServiceFact {
	end := r.GetEnd().AsTime()
	from := end.Add(-api.RateSpan)
	epochs := s.epochsIn(from, end)
	var out []api.MicroServiceFact
	for _, m := range s.sc.micro {
		now := s.answering(m, at)
		if len(now) == 0 {
			continue
		}
		svc := api.MicroServiceFact{Account: Account, Name: m.name}
		current := map[string]bool{}
		on := s.microConnections(at)
		for _, in := range now {
			current[in.id] = true
			started := in.startedAt().Add(s.shift)
			x := on[in.id]
			svc.Instances = append(svc.Instances, api.MicroInstanceFact{ID: in.id, Version: in.version, Description: m.description,
				Started: &started, Connection: x.name, Server: x.server})
		}
		// Who answers each epoch of the window.
		answers := map[time.Time][]microInstanceSpec{}
		for _, t := range epochs {
			answers[t] = s.answering(m, t)
		}
		share := 1 / float64(len(m.instances)-replaced(m))
		for _, e := range m.endpoints {
			var reports []api.MicroReport
			for _, t := range epochs {
				for _, in := range answers[t] {
					req, errs, ns := microCounters(e, in, share, s.clock(t))
					reports = append(reports, api.MicroReport{Instance: in.id, Started: in.startedAt().Add(s.shift), At: t, Requests: req,
						Errors: errs, ProcessingNs: ns})
				}
			}
			x := api.MicroEndpointFact{Name: e.name, Subject: e.subject, QueueGroup: microQueue,
				Window: api.MicroWindowIncrease(reports, current)}
			if e.lastError != "" && current[m.instances[e.lastErrorBy].id] {
				x.LastError, x.LastErrorInstance = e.lastError, m.instances[e.lastErrorBy].id
			}
			// The level's hours, as the store's hourly rollup holds them:
			// those that start at or after 7 days before the window and
			// end at or before it, within the stored history.
			var hourly []float64
			first := from.Add(-api.MicroLevelSpan)
			for h := first.Truncate(time.Hour); !h.Add(time.Hour).After(from); h = h.Add(time.Hour) {
				if !h.Before(first) && !h.Before(s.dataStart()) {
					hourly = append(hourly, e.averageAt(s.clock(h)))
				}
			}
			x.LevelMs = api.MicroLevel(hourly)
			if !e.slowFrom.IsZero() {
				// The first epoch whose collection interval is slow
				// throughout.
				first := e.slowFrom.Add(s.shift).Add(sampleEvery)
				for t := s.lastEpoch(); !t.Before(first); t = t.Add(-sampleEvery) {
					if !t.After(at) {
						x.SlowSince = &t
					}
				}
			}
			svc.Endpoints = append(svc.Endpoints, x)
		}
		out = append(out, svc)
	}
	return out
}

// replaced is how many of a service's instances a later one replaced:
// those that end, whose share of the requests the replacement takes.
func replaced(m microSpec) int {
	n := 0
	for _, in := range m.instances {
		if !in.to.IsZero() {
			n++
		}
	}
	return n
}

// microGone are, for an asked account, the instances that answered in the
// hour before the epoch at and do not at it; else those seen in the
// subscription collections of that hour and not in the one read.
func (s scen) microGone(at time.Time, asked bool, subsAt *time.Time) []api.MicroGoneFact {
	var out []api.MicroGoneFact
	for _, m := range s.sc.micro {
		// The collections of the hour, newest first, with who answered or
		// was seen in each.
		var times []time.Time
		var who []map[string]microConn
		if asked {
			for t := at; !t.Before(at.Add(-api.MicroMissingWindow)) && !t.Before(s.dataStart()); t = t.Add(-sampleEvery) {
				on := s.microConnections(t)
				w := map[string]microConn{}
				for _, in := range s.answering(m, t) {
					w[in.id] = on[in.id]
				}
				times, who = append(times, t), append(who, w)
			}
		} else if subsAt != nil {
			for t := *subsAt; !t.Before(at.Add(-api.MicroMissingWindow)) && !t.Before(s.dataStart()); t = t.Add(-subsInterval) {
				on := s.microConnections(t)
				w := map[string]microConn{}
				for _, in := range m.instances {
					if x, ok := on[in.id]; ok {
						w[in.id] = x
					}
				}
				times, who = append(times, t), append(who, w)
			}
		}
		if len(times) == 0 {
			continue
		}
		done := map[string]bool{}
		for id := range who[0] {
			done[id] = true
		}
		for i := 1; i < len(times); i++ {
			for id, x := range who[i] {
				if done[id] {
					continue
				}
				done[id] = true
				out = append(out, api.MicroGoneFact{Account: Account, Service: m.name, ID: id, LastSeen: times[i], Since: times[i-1],
					Connection: x.name, InstancesThen: int32(len(who[i])), Absent: int32(i), FromSubscriptions: !asked})
			}
		}
	}
	return out
}

// microAccounts are the accounts the scenario asks, with the newest
// $SRV.INFO request at or before the epoch at (asked with the
// subscription lists) and the epoch's $SRV.STATS.
func (s scen) microAccounts(at time.Time) []api.MicroAccountFact {
	// INFO is asked with the subscription lists, every subsInterval back
	// from the newest epoch.
	info := s.lastEpoch().Add(-(s.lastEpoch().Sub(at) + subsInterval - 1) / subsInterval * subsInterval)
	var out []api.MicroAccountFact
	for _, acc := range s.sc.microAsked {
		a := api.MicroAccountFact{Account: acc}
		if acc != Account {
			a.Info = api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR, At: info, Error: microRequestError(api.SrvPrefix + "INFO")}
			a.Stats = api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_ERROR, At: at, Error: microRequestError(api.SrvPrefix + "STATS")}
			out = append(out, a)
			continue
		}
		var nInfo, nStats int32
		for _, m := range s.sc.micro {
			nInfo += int32(len(s.answering(m, info)))
			nStats += int32(len(s.answering(m, at)))
		}
		a.Info = microReplies(info, nInfo)
		a.Stats = microReplies(at, nStats)
		out = append(out, a)
	}
	return out
}

// microReplies is a request that gathered n replies at t: ok, or no
// responders without one.
func microReplies(t time.Time, n int32) api.MicroRequestFact {
	if n == 0 {
		return api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_NO_RESPONDERS, At: t}
	}
	return api.MicroRequestFact{State: v1.MicroRequestState_MICRO_REQUEST_STATE_OK, At: t, Replies: n}
}

// MicroServices builds the facts of the range's newest epoch from the
// scenario: the accounts it asks, the services that answer there, those
// seen in the subscription collection, and the instances gone in the hour
// before.
func (p *Provider) MicroServices(_ context.Context, req *v1.GetMicroServicesRequest) (*api.MicroFacts, error) {
	s := p.scen()
	r := req.GetRange()
	at, n, ok := s.epochBefore(r)
	if !ok {
		return &api.MicroFacts{}, nil
	}
	f := &api.MicroFacts{Epoch: n, EpochAt: at, SystemAccount: p.sc.systemAccount(), Accounts: s.microAccounts(at)}
	asked := false
	for _, a := range p.sc.microAsked {
		asked = asked || a == Account
	}
	if asked {
		f.Answered = s.microAnswered(at, r)
	}
	if c, ok := s.subsCollection(at); ok {
		subs, conns := s.subscriptionsAt(c)
		f.SubscriptionsAt, f.Seen = &c, api.SeenMicro(subs, conns)
	}
	f.Gone = s.microGone(at, asked, f.SubscriptionsAt)
	return f, nil
}
