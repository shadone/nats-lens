package api

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Applications limits and names.
const (
	// MaxApplications is the most applications a GetApplications response
	// lists, MaxApplicationPods the most pod names and
	// MaxApplicationAddresses the most client addresses per application.
	MaxApplications         = 500
	MaxApplicationPods      = 10
	MaxApplicationAddresses = 10
	// ApplicationDay is the span before the epoch the 24-hour counts
	// cover, and ApplicationHours the hours of traffic.
	ApplicationDay   = 24 * time.Hour
	ApplicationHours = 24
	// UnnamedApplication groups the connections without a name or with
	// nothing before the "@", as the client checks name them.
	UnnamedApplication = "(unnamed)"
	// GlobalAccount names the account of a connection the servers report
	// without one.
	GlobalAccount = "$G"
	// StaleReason is the disconnect reason of a client that stopped
	// answering pings.
	StaleReason = "Stale Connection"
)

// ApplicationOf is the application of a connection name: the name before
// the first "@", UnnamedApplication when that is empty. A finding's
// client key maps to its application the same way: client-001 keys the
// application, client-002 to client-005 the whole name.
func ApplicationOf(name string) string {
	app, _, _ := strings.Cut(name, "@")
	if app == "" {
		return UnnamedApplication
	}
	return app
}

// PodOf is the pod a connection name gives: the part between the first
// "@" and the next, as client-001 splits it; empty when there is none.
func PodOf(name string) string {
	_, rest, _ := strings.Cut(name, "@")
	pod, _, _ := strings.Cut(rest, "@")
	return pod
}

// AccountOf is the account a connection or an event names, GlobalAccount
// for none.
func AccountOf(account string) string { return cmp.Or(account, GlobalAccount) }

// TrafficHours are the hours whose traffic a page of the epoch at at
// shows: the 24 whole hours before the hour of the epoch, oldest first.
func TrafficHours(at time.Time) []time.Time {
	last := at.UTC().Truncate(time.Hour)
	out := make([]time.Time, ApplicationHours)
	for i := range out {
		out[i] = last.Add(-time.Duration(ApplicationHours-i) * time.Hour)
	}
	return out
}

// ApplicationFacts are what a provider read for one GetApplications: the
// connections of the range's newest epoch, the client events of the
// ApplicationDay before it, the hourly traffic of its TrafficHours and the
// findings firing at it. FinishApplications groups and counts them, so the
// store and the demo agree. A provider may leave out what lies outside the
// request's account; FinishApplications scopes again.
type ApplicationFacts struct {
	// Epoch is the range's newest epoch; 0 when the range holds none.
	Epoch   int64
	EpochAt time.Time
	// Missing are the servers whose connections the epoch does not know:
	// their CONNZ request failed, or they answered nothing. The page is
	// partial when there is one.
	Missing []string
	// Connections are the epoch's client connections, tooling marked.
	Connections []AppConnectionFact
	// ConnzCut is whether a server's connection list in the epoch was cut
	// at the connz limit.
	ConnzCut bool
	// EventsCaptured is whether any event is stored up to the epoch;
	// CaptureGaps whether capture, while it ran, stopped for part of the
	// ApplicationDay before it, by the Accounts and auth page's rule.
	EventsCaptured, CaptureGaps bool
	// Events are the client events of the ApplicationDay before the epoch
	// counted by connection name, account and tooling.
	Events []AppEventFact
	// Traffic is the hourly rollup of TrafficHours by application,
	// account and tooling.
	Traffic []AppTrafficFact
	// Findings are the findings firing at the epoch; only the client
	// findings are read.
	Findings []*v1.Finding
	// ClientPodsWatched is whether the system watches client pods; the
	// connections' pods are read only then.
	ClientPodsWatched bool
	// ClientPodsUnwatched are the client pod watches failing at the epoch.
	ClientPodsUnwatched []*v1.ClientPodWatchGap
}

// ClientPodFact is the pod a connection's client address belonged to at
// the epoch.
type ClientPodFact struct {
	Namespace, Name, Node  string
	WorkloadKind, Workload string
}

// AppConnectionFact is one connection of the epoch.
type AppConnectionFact struct {
	Server                      string
	CID                         int64
	Name, Account, IP           string
	Lang, Version               string
	RTT                         *time.Duration
	Subscriptions, PendingBytes *int64
	Tooling                     bool
	// Pod is the one pod whose address the connection's IP was at the
	// epoch; PodAmbiguous is set instead when a host-network pod or more
	// than one pod held it. Both are unset for an address no watched pod
	// held.
	Pod          *ClientPodFact
	PodAmbiguous bool
}

// AppEventFact counts the client events of one connection name in one
// account: Drops are the disconnects for a reason other than "Client
// Closed" and "Server Shutdown"; Connects the client.connect events plus
// the connects of client.churn events; Slow the disconnects whose reason
// contains "slow consumer" (any case); Stale those with StaleReason;
// ShortLived the connects of client.churn events. Tooling is the rule of
// the event's epoch.
type AppEventFact struct {
	Name, Account                            string
	Tooling                                  bool
	Drops, Connects, Slow, Stale, ShortLived int32
}

// AppTrafficFact is one row of the hourly rollup: an application's
// messages and bytes in one hour, in one account, tooling or not.
type AppTrafficFact struct {
	App, Account                       string
	Tooling                            bool
	Hour                               time.Time
	InMsgs, OutMsgs, InBytes, OutBytes int64
}

// CountClientEvent adds one event to its name's counts by the rules of
// AppEventFact; churnConnects are the connects a client.churn event
// folded.
func CountClientEvent(f *AppEventFact, kind, reason string, churnConnects int32) {
	switch kind {
	case "client.disconnect":
		if reason != "Client Closed" && reason != "Server Shutdown" {
			f.Drops++
		}
		if strings.Contains(strings.ToLower(reason), "slow consumer") {
			f.Slow++
		}
		if reason == StaleReason {
			f.Stale++
		}
	case "client.connect":
		f.Connects++
	case "client.churn":
		f.Connects += churnConnects
		f.ShortLived += churnConnects
	}
}

// appGroup gathers one application's facts.
type appGroup struct {
	name            string
	conns           []AppConnectionFact
	tooling         bool
	slow, stale, sl int32
	// rec are the reconnects of the application's names, each paired as
	// client-004 pairs it.
	rec      int32
	traffic  map[time.Time]*v1.HourTraffic
	findings []*v1.FindingRef
	unmuted  bool
}

// FinishApplications builds the page from the facts: the connections in
// account (empty for all), tooling ones left out unless includeTooling,
// grouped by ApplicationOf; each application's instances, pods, servers,
// accounts, libraries, round trip median and sums, its 24-hour counts
// from the events of its names, its traffic and its client findings; the
// order, the limit and the three summaries.
func FinishApplications(f *ApplicationFacts, account string, includeTooling bool) *v1.GetApplicationsResponse {
	if f.Epoch == 0 {
		return &v1.GetApplicationsResponse{}
	}
	res := &v1.GetApplicationsResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt), ConnzCut: f.ConnzCut,
		EventsCaptured: f.EventsCaptured, CaptureGaps: f.EventsCaptured && f.CaptureGaps, Partial: len(f.Missing) > 0,
		Missing: slices.Compact(slices.Sorted(slices.Values(f.Missing))), ClientPodsWatched: f.ClientPodsWatched}
	if f.ClientPodsWatched {
		res.ClientPodsUnwatched = f.ClientPodsUnwatched
	}
	in := func(acc string) bool { return account == "" || AccountOf(acc) == account }
	groups := map[string]*appGroup{}
	for _, c := range f.Connections {
		if !in(c.Account) {
			continue
		}
		if c.Tooling && !includeTooling {
			res.ToolingHidden++
			continue
		}
		name := ApplicationOf(c.Name)
		g := groups[name]
		if g == nil {
			g = &appGroup{name: name, tooling: true, traffic: map[time.Time]*v1.HourTraffic{}}
			groups[name] = g
		}
		g.conns = append(g.conns, c)
		g.tooling = g.tooling && c.Tooling
	}
	// Drops and connects pair per name across accounts, as client-004
	// pairs them; a scoped page counts a name with an event in its scope.
	type pair struct {
		drops, connects int32
		inScope         bool
	}
	pairs := map[string]*pair{}
	for _, e := range f.Events {
		if e.Tooling && !includeTooling {
			continue
		}
		p := pairs[e.Name]
		if p == nil {
			p = &pair{}
			pairs[e.Name] = p
		}
		p.drops, p.connects, p.inScope = p.drops+e.Drops, p.connects+e.Connects, p.inScope || in(e.Account)
		if g := groups[ApplicationOf(e.Name)]; g != nil && in(e.Account) {
			g.slow += e.Slow
			g.stale += e.Stale
			g.sl += e.ShortLived
		}
	}
	for name, p := range pairs {
		if g := groups[ApplicationOf(name)]; g != nil && p.inScope {
			g.rec += min(p.drops, p.connects)
		}
	}

	for _, t := range f.Traffic {
		g := groups[t.App]
		if g == nil || !in(t.Account) || (t.Tooling && !includeTooling) {
			continue
		}
		h := g.traffic[t.Hour.UTC()]
		if h == nil {
			h = &v1.HourTraffic{InMsgs: ptrTo(int64(0)), OutMsgs: ptrTo(int64(0)), InBytes: ptrTo(int64(0)), OutBytes: ptrTo(int64(0))}
			g.traffic[t.Hour.UTC()] = h
		}
		*h.InMsgs += t.InMsgs
		*h.OutMsgs += t.OutMsgs
		*h.InBytes += t.InBytes
		*h.OutBytes += t.OutBytes
	}
	for _, fd := range f.Findings {
		if fd.GetEntity().GetType() != v1.EntityType_ENTITY_TYPE_CLIENT || fd.GetState() == v1.FindingState_FINDING_STATE_RESOLVED {
			continue
		}
		g := groups[ApplicationOf(fd.GetEntity().GetKey())]
		if g == nil {
			continue
		}
		muted := fd.GetMute() != nil
		g.findings = append(g.findings, &v1.FindingRef{Id: fd.GetId(), CheckCode: fd.GetCheckCode(), CheckName: fd.GetCheckName(),
			Severity: fd.GetSeverity(), Muted: muted, EntityKey: fd.GetEntity().GetKey()})
		g.unmuted = g.unmuted || !muted
	}

	hours := TrafficHours(f.EpochAt)
	var apps []*v1.Application
	unmuted := map[*v1.Application]bool{}
	for _, g := range groups {
		a := g.application(hours, f.EventsCaptured)
		if f.ClientPodsWatched {
			a.RunsOn = runsOn(g.conns)
		}
		unmuted[a] = g.unmuted
		apps = append(apps, a)
	}
	slices.SortFunc(apps, func(a, b *v1.Application) int {
		return cmp.Or(compareBool(unmuted[b], unmuted[a]), cmp.Compare(b.GetConnections(), a.GetConnections()), cmp.Compare(a.GetName(), b.GetName()))
	})
	res.ApplicationsTotal = int32(len(apps))
	res.Spread, res.Libraries, res.LastDay = summarize(apps, f.EventsCaptured)
	// The missing servers' connections are not known.
	res.Spread.Concentrated = res.Spread.GetConcentrated() && !res.GetPartial()
	for _, a := range apps {
		res.ConnectionsTotal += a.GetConnections()
	}
	res.Applications = apps[:min(len(apps), MaxApplications)]
	return res
}

// application builds one row from its group.
func (g *appGroup) application(hours []time.Time, events bool) *v1.Application {
	a := &v1.Application{Name: g.name, Connections: int32(len(g.conns)), Tooling: g.tooling}
	pods, addrs, accounts := map[string]bool{}, map[string]bool{}, map[string]bool{}
	servers := map[string]int32{}
	libs := map[[2]string]int32{}
	var rtts []time.Duration
	type conn struct {
		server string
		cid    int64
	}
	podless := map[conn]bool{}
	for _, c := range g.conns {
		if p := PodOf(c.Name); p != "" {
			pods[p] = true
		} else {
			podless[conn{c.Server, c.CID}] = true
		}
		if c.IP != "" {
			addrs[c.IP] = true
		}
		accounts[AccountOf(c.Account)] = true
		servers[c.Server]++
		if c.Lang != "" || c.Version != "" {
			// A connection that reports no library is no library.
			libs[[2]string{c.Lang, c.Version}]++
		}
		if c.RTT != nil {
			rtts = append(rtts, *c.RTT)
		}
		if c.Subscriptions != nil {
			a.Subscriptions = ptrTo(a.GetSubscriptions() + *c.Subscriptions)
		}
		if c.PendingBytes != nil {
			a.PendingBytes = ptrTo(a.GetPendingBytes() + *c.PendingBytes)
			a.PendingBytesMax = ptrTo(max(a.GetPendingBytesMax(), *c.PendingBytes))
		}
	}
	var names []string
	for _, c := range g.conns {
		if c.Name != "" {
			names = append(names, c.Name)
		}
	}
	a.ConnectionNames, a.MoreNames = cutNames(names, 0)
	a.Instances = int32(len(pods) + len(podless))
	a.PodsTotal = int32(len(pods))
	a.Pods = firstSorted(pods, MaxApplicationPods)
	a.AddressesTotal = int32(len(addrs))
	a.Addresses = firstSorted(addrs, MaxApplicationAddresses)
	for _, acc := range slices.Sorted(maps.Keys(accounts)) {
		a.Accounts = append(a.Accounts, &v1.ApplicationAccount{Account: acc})
	}
	for s, n := range servers {
		a.Servers = append(a.Servers, &v1.ServerCount{Server: s, Connections: n})
	}
	slices.SortFunc(a.Servers, func(x, y *v1.ServerCount) int {
		return cmp.Or(cmp.Compare(y.GetConnections(), x.GetConnections()), cmp.Compare(x.GetServer(), y.GetServer()))
	})
	for l, n := range libs {
		a.Libraries = append(a.Libraries, &v1.LibraryCount{Lang: l[0], Version: l[1], Connections: n})
	}
	slices.SortFunc(a.Libraries, func(x, y *v1.LibraryCount) int {
		return cmp.Or(cmp.Compare(y.GetConnections(), x.GetConnections()), cmp.Compare(x.GetLang(), y.GetLang()), cmp.Compare(x.GetVersion(), y.GetVersion()))
	})
	if len(rtts) > 0 {
		a.RttP50 = durationpb.New(medianDuration(rtts))
	}
	if events {
		a.Reconnects_24H, a.SlowConsumers_24H, a.Stale_24H, a.ShortLived_24H = ptrTo(g.rec), ptrTo(g.slow), ptrTo(g.stale), ptrTo(g.sl)
	}
	for _, h := range hours {
		t := g.traffic[h]
		if t == nil {
			t = &v1.HourTraffic{}
		} else {
			a.InMsgs_24H, a.OutMsgs_24H = ptrTo(a.GetInMsgs_24H()+t.GetInMsgs()), ptrTo(a.GetOutMsgs_24H()+t.GetOutMsgs())
			a.InBytes_24H, a.OutBytes_24H = ptrTo(a.GetInBytes_24H()+t.GetInBytes()), ptrTo(a.GetOutBytes_24H()+t.GetOutBytes())
		}
		t.Hour = timestamppb.New(h)
		a.Traffic = append(a.Traffic, t)
	}
	slices.SortFunc(g.findings, func(x, y *v1.FindingRef) int {
		return cmp.Or(compareBool(x.GetMuted(), y.GetMuted()), cmp.Compare(y.GetSeverity(), x.GetSeverity()), cmp.Compare(x.GetCheckCode(), y.GetCheckCode()),
			cmp.Compare(x.GetEntityKey(), y.GetEntityKey()))
	})
	a.Findings = g.findings
	return a
}

// summarize builds the three summaries over every application: the
// spread over the servers, the library versions by application, and the
// 24-hour sums (unset without events).
func summarize(apps []*v1.Application, events bool) (*v1.ApplicationSpread, []*v1.LibraryUse, *v1.ApplicationsLastDay) {
	sp := &v1.ApplicationSpread{}
	servers := map[string]int32{}
	libs := map[[2]string]int32{}
	day := &v1.ApplicationsLastDay{}
	if events {
		day.SlowConsumers, day.Stale, day.Reconnecting = ptrTo(int32(0)), ptrTo(int32(0)), ptrTo(int32(0))
	}
	for _, a := range apps {
		for _, s := range a.GetServers() {
			servers[s.GetServer()] += s.GetConnections()
			sp.Total += s.GetConnections()
		}
		for _, l := range a.GetLibraries() {
			libs[[2]string{l.GetLang(), l.GetVersion()}]++
		}
		if events {
			*day.SlowConsumers += a.GetSlowConsumers_24H()
			*day.Stale += a.GetStale_24H()
			if a.GetReconnects_24H() > 0 {
				*day.Reconnecting++
			}
		}
	}
	for _, s := range slices.Sorted(maps.Keys(servers)) {
		if n := servers[s]; n > sp.Connections {
			sp.Server, sp.Connections = s, n
		}
	}
	sp.Servers = int32(len(servers))
	sp.Concentrated = sp.Servers > 1 && 2*sp.Connections > sp.Total
	var uses []*v1.LibraryUse
	for l, n := range libs {
		uses = append(uses, &v1.LibraryUse{Lang: l[0], Version: l[1], Applications: n})
	}
	slices.SortFunc(uses, func(x, y *v1.LibraryUse) int {
		return cmp.Or(cmp.Compare(y.GetApplications(), x.GetApplications()), cmp.Compare(x.GetLang(), y.GetLang()), cmp.Compare(x.GetVersion(), y.GetVersion()))
	})
	return sp, uses, day
}

// firstSorted is the first n keys of set, sorted.
func firstSorted(set map[string]bool, n int) []string {
	keys := slices.Sorted(maps.Keys(set))
	return keys[:min(len(keys), n)]
}
