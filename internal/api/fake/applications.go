package fake

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Applications reads the facts of the Applications page from the
// scenario: the connections of the range's newest epoch as the
// Connections page lists them, the client events of the day before it as
// the Events page lists them, the hourly traffic of the same rates as the
// connections' counters, and the findings firing at the epoch.
func (p *Provider) Applications(_ context.Context, req *v1.GetApplicationsRequest) (*api.ApplicationFacts, error) {
	s := p.scen()
	at, epoch, ok := s.epochBefore(req.GetRange())
	if !ok {
		return &api.ApplicationFacts{}, nil
	}
	// A server down has no CONNZ; the demo's CONNZ never fails otherwise.
	f := &api.ApplicationFacts{Epoch: epoch, EpochAt: at}
	for _, sp := range s.sc.servers {
		if s.downAt(sp, at) {
			f.Missing = append(f.Missing, sp.name)
		}
	}
	specs := map[string]serverSpec{}
	for _, sp := range s.sc.servers {
		specs[sp.name] = sp
	}
	f.ClientPodsWatched = s.sc.clientPods != nil
	for _, c := range s.connections(at) {
		x := api.AppConnectionFact{Server: c.GetServer(), CID: c.GetCid(), Name: c.GetName(), Account: c.GetAccount(), IP: c.GetIp(),
			Lang: c.GetLang(), Version: c.GetVersion(), Subscriptions: c.Subscriptions, PendingBytes: c.PendingBytes, Tooling: c.GetTooling()}
		if c.GetRtt() != nil {
			d := c.GetRtt().AsDuration()
			x.RTT = &d
		}
		// The clients' connections are numbered from 100 on each server.
		if i := c.GetCid() - 100; f.ClientPodsWatched && i >= 0 && i < specs[c.GetServer()].conns && c.GetAccount() == Account {
			x.Pod, x.PodAmbiguous = s.sc.clientPods(specs[c.GetServer()], c.GetName(), i)
		}
		f.Connections = append(f.Connections, x)
	}
	evs := p.events(s)
	f.EventsCaptured, f.Events = appEvents(evs, at)
	// Capture stopped in the day by the Accounts and auth page's rule.
	capture := &api.AccountsFacts{EpochAt: at, WindowStart: at.Add(-api.ApplicationDay)}
	captureGaps(capture, evs)
	f.CaptureGaps = capture.CaptureGaps
	f.Traffic = s.appTraffic(at)
	f.Findings = current(p.findings(s), req.GetRange())
	return f, nil
}

// appEvents counts the client events of the api.ApplicationDay before at
// by name, account and the demo's tooling rule, and reports whether any
// event happened up to at.
func appEvents(evs []*v1.Event, at time.Time) (bool, []api.AppEventFact) {
	type key struct {
		name, account string
		tooling       bool
	}
	from := at.Add(-api.ApplicationDay)
	captured := false
	counts := map[key]*api.AppEventFact{}
	var order []key
	for _, e := range evs {
		t := e.GetTime().AsTime()
		if t.After(at) {
			continue
		}
		captured = true
		if !t.After(from) || !strings.HasPrefix(e.GetKind(), "client.") {
			continue
		}
		k := key{e.GetClientName(), e.GetAccount(), toolingClient(e.GetClientName(), e.GetClientHost())}
		c := counts[k]
		if c == nil {
			c = &api.AppEventFact{Name: k.name, Account: k.account, Tooling: k.tooling}
			counts[k] = c
			order = append(order, k)
		}
		api.CountClientEvent(c, e.GetKind(), e.GetReason(), churnConnects(e))
	}
	out := make([]api.AppEventFact, 0, len(order))
	for _, k := range order {
		out = append(out, *counts[k])
	}
	return captured, out
}

// churnConnects is the connects a client.churn event folded, from its
// summary ("... connected and disconnected n times in 1 min ...").
func churnConnects(e *v1.Event) int32 {
	if e.GetKind() != "client.churn" {
		return 0
	}
	_, rest, ok := strings.Cut(e.GetSummary(), " connected and disconnected ")
	var n int32
	if ok {
		_, _ = fmt.Sscanf(rest, "%d times", &n)
	}
	return n
}

// appTraffic is the hourly rollup of api.TrafficHours before at: each
// connection's rates over the time its server was up within the hour
// (and it was connected, for an extra connection with a start), from the
// start of the stored history. An hour none of an application's
// connections covers has no row.
func (s scen) appTraffic(at time.Time) []api.AppTrafficFact {
	type key struct {
		app, account string
		tooling      bool
		hour         time.Time
	}
	type sums struct{ in, out, inBytes, outBytes float64 }
	rows := map[key]*sums{}
	var order []key
	add := func(k key, in, out, inBytes, outBytes float64) {
		r := rows[k]
		if r == nil {
			r = &sums{}
			rows[k] = r
			order = append(order, k)
		}
		r.in, r.out, r.inBytes, r.outBytes = r.in+in, r.out+out, r.inBytes+inBytes, r.outBytes+outBytes
	}
	first := s.dataStart()
	for _, h := range api.TrafficHours(at) {
		from, to := latest(h, first), h.Add(time.Hour)
		if !to.After(from) {
			continue
		}
		for _, sp := range s.sc.servers {
			up := s.upBetween(sp, from, to)
			rates := map[string][2]float64{}
			for i := range sp.conns {
				name := s.sc.clients[int(i)%len(s.sc.clients)]
				rin, rout := connRate(name, i)
				r := rates[name]
				rates[name] = [2]float64{r[0] + rin, r[1] + rout}
			}
			for name, r := range rates {
				k := key{api.ApplicationOf(name), Account, false, h}
				add(k, r[0]*up, r[1]*up, r[0]*up*clientMsgBytes, r[1]*up*clientMsgBytes)
			}
			for _, e := range s.sc.extraConns {
				if e.server != sp.name {
					continue
				}
				start := from
				if !e.since.IsZero() {
					start = latest(from, e.since.Add(s.shift))
				}
				if !to.After(start) {
					continue
				}
				u := s.upBetween(sp, start, to)
				k := key{api.ApplicationOf(e.name), e.account, toolingClient(e.name, e.ip), h}
				add(k, extraIn*u, extraOut*u, extraIn*u*extraInMsgBytes, extraOut*u*extraOutMsgBytes)
			}
		}
	}
	out := make([]api.AppTrafficFact, 0, len(order))
	for _, k := range order {
		r := rows[k]
		out = append(out, api.AppTrafficFact{App: k.app, Account: k.account, Tooling: k.tooling, Hour: k.hour,
			InMsgs: int64(math.Round(r.in)), OutMsgs: int64(math.Round(r.out)), InBytes: int64(math.Round(r.inBytes)),
			OutBytes: int64(math.Round(r.outBytes))})
	}
	return out
}
