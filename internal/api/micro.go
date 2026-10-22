package api

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// Micro services limits and rules.
const (
	// MaxMicroServices is the most services a GetMicroServices response
	// lists, MaxMicroEndpoints the most endpoints and MaxMicroInstances the
	// most instances per service.
	MaxMicroServices  = 200
	MaxMicroEndpoints = 100
	MaxMicroInstances = 100
	// MicroMissingWindow is how long before the epoch an instance that
	// answered, or was seen, and is gone counts as missing.
	MicroMissingWindow = time.Hour
	// MicroLevelSpan is how far before the rate window the 7-day level
	// looks, and MicroLevelMinHours the fewest hours with an average it
	// takes.
	MicroLevelSpan     = 7 * 24 * time.Hour
	MicroLevelMinHours = 24
	// An endpoint is slow when its average time is at least
	// MicroSlowFactor times its level and at least MicroSlowMarginMs above
	// it.
	MicroSlowFactor   = 3.0
	MicroSlowMarginMs = 5.0
	// MicroErrorsHigh is the error share above which an endpoint warns.
	MicroErrorsHigh = 0.01
	// SrvPrefix starts the micro framework's discovery subjects.
	SrvPrefix = "$SRV."
)

// MicroFacts are what a provider read for one GetMicroServices: the
// accounts it asked in the epoch, the services that answered the epoch's
// $SRV.STATS with their newest $SRV.INFO, the services seen in the
// subscription collection, and the instances that may be missing.
// FinishMicro merges them by (account, name) and applies the rules, so the
// store and the demo agree. A provider may leave out what lies outside the
// request's account; FinishMicro scopes again.
type MicroFacts struct {
	// Epoch is the range's newest epoch; 0 when the range holds none.
	Epoch   int64
	EpochAt time.Time
	// SystemAccount is the system account's identifier; empty when not
	// known.
	SystemAccount string
	// Accounts are the accounts asked in the epoch.
	Accounts []MicroAccountFact
	// Answered are the services with an instance that answered the
	// epoch's $SRV.STATS: those instances, with their newest INFO, and the
	// endpoints.
	Answered []MicroServiceFact
	// SubscriptionsAt is nil when no subscription collection lies within
	// SubscriptionsMaxAge at or before the epoch; Seen are the services
	// with instances in it, as SeenMicro groups them.
	SubscriptionsAt *time.Time
	Seen            []MicroServiceFact
	// Gone are the instances of the MicroMissingWindow before the epoch
	// that are not in it: for an asked account those that answered
	// $SRV.STATS, else those seen in a subscription collection.
	Gone []MicroGoneFact
}

// MicroAccountFact is an account asked in the epoch: its newest
// $SRV.INFO request at or before the epoch, and the epoch's $SRV.STATS.
type MicroAccountFact struct {
	Account     string
	Info, Stats MicroRequestFact
}

// MicroRequestFact is one $SRV request to an account.
type MicroRequestFact struct {
	State   v1.MicroRequestState
	At      time.Time
	Replies int32
	Error   string
}

// MicroServiceFact is one service, (account, name), as one source knows
// it.
type MicroServiceFact struct {
	Account, Name string
	// Instances are by ID.
	Instances []MicroInstanceFact
	// Endpoints are the answered service's, over its instances; none for a
	// seen one.
	Endpoints []MicroEndpointFact
}

// MicroInstanceFact is one instance of a service.
type MicroInstanceFact struct {
	ID string
	// Version and Description are from its newest INFO; empty when not
	// asked or not described.
	Version, Description string
	// Started is nil when not asked.
	Started *time.Time
	// Connection and Server are the connection that holds its $SRV
	// subscriptions; empty when not found.
	Connection, Server string
}

// MicroEndpointFact is one endpoint of an answered service: its subject
// and queue group from the newest INFO, its counters' increase over the
// RateSpan before the range's end (MicroWindowIncrease), its newest last
// error, its 7-day level (MicroLevel) and the start of its current slow
// run (MicroSlowSince); FinishMicro keeps the start only when the
// endpoint is slow.
type MicroEndpointFact struct {
	Name, Subject, QueueGroup    string
	Window                       MicroIncrease
	LastError, LastErrorInstance string
	LevelMs                      *float64
	SlowSince                    *time.Time
}

// MicroGoneFact is an instance that answered (or was seen) within the
// MicroMissingWindow before the epoch and is not in it.
type MicroGoneFact struct {
	Account, Service, ID string
	// LastSeen is its last answer or listing; Since the first collection
	// of the same kind after it.
	LastSeen, Since time.Time
	// Connection is the name of its connection when last seen; empty when
	// unnamed or not found.
	Connection string
	// InstancesThen is how many instances of the service answered (or were
	// seen) in that last collection.
	InstancesThen int32
}

// MicroReport is one instance's $SRV.STATS counters of one endpoint in one
// collection.
type MicroReport struct {
	Instance string
	// Started is when the instance started or its stats were reset.
	Started time.Time
	// At is when the collection's epoch started.
	At                             time.Time
	Requests, Errors, ProcessingNs int64
}

// MicroIncrease is an endpoint's counters' increase over its instances'
// runs.
type MicroIncrease struct {
	// PerSecond is the sum of the runs' request rates; nil without a run of
	// two reports.
	PerSecond *float64
	// Requests, Errors and ProcessingNs are the sums of the increases.
	Requests, Errors, ProcessingNs float64
}

// MicroDelta is the increase between two consecutive reports of one
// instance's run, at the later one's time.
type MicroDelta struct {
	At                     time.Time
	Requests, ProcessingNs float64
}

// MicroAverage is an average processing time at a time.
type MicroAverage struct {
	At time.Time
	Ms float64
}

// MicroInstanceOf parses a $SRV.<verb>.<name>.<id> subject, verb PING,
// INFO or STATS, into the service name and instance ID.
func MicroInstanceOf(subject string) (name, id string, ok bool) {
	t := strings.Split(subject, ".")
	if len(t) != 4 || t[0]+"." != SrvPrefix || t[2] == "" || t[3] == "" || strings.ContainsAny(t[2]+t[3], "*>") {
		return "", "", false
	}
	switch t[1] {
	case "PING", "INFO", "STATS":
		return t[2], t[3], true
	}
	return "", "", false
}

// SeenMicro groups a subscription collection's $SRV.<verb>.<name>.<id>
// subscriptions into services by account and name, one instance per ID,
// with the name and server of the connection that holds them when its
// server's CONNZ lists it. Services and instances are in order.
func SeenMicro(subs []SubscriptionFact, conns map[string]ServerConnections) []MicroServiceFact {
	type key struct{ account, name string }
	insts := map[key]map[string]MicroInstanceFact{}
	for _, s := range subs {
		name, id, ok := MicroInstanceOf(s.Subject)
		if !ok {
			continue
		}
		k := key{s.Account, name}
		if insts[k] == nil {
			insts[k] = map[string]MicroInstanceFact{}
		}
		in, seen := insts[k][id]
		if !seen {
			in = MicroInstanceFact{ID: id}
		}
		if conn, ok := conns[s.Server].Names[s.CID]; ok && in.Server == "" {
			in.Connection, in.Server = conn, s.Server
		}
		insts[k][id] = in
	}
	var out []MicroServiceFact
	for k, m := range insts {
		svc := MicroServiceFact{Account: k.account, Name: k.name}
		for _, id := range slices.Sorted(maps.Keys(m)) {
			svc.Instances = append(svc.Instances, m[id])
		}
		out = append(out, svc)
	}
	slices.SortFunc(out, func(a, b MicroServiceFact) int {
		return cmp.Or(cmp.Compare(a.Account, b.Account), cmp.Compare(a.Name, b.Name))
	})
	return out
}

// microRuns splits each instance's reports, in time order, into runs: a
// run ends where the start time changes or a counter falls.
func microRuns(reports []MicroReport) map[string][][]MicroReport {
	by := map[string][]MicroReport{}
	for _, r := range reports {
		by[r.Instance] = append(by[r.Instance], r)
	}
	out := map[string][][]MicroReport{}
	for id, rs := range by {
		slices.SortStableFunc(rs, func(a, b MicroReport) int { return a.At.Compare(b.At) })
		var runs [][]MicroReport
		for i, r := range rs {
			if i == 0 || !r.Started.Equal(rs[i-1].Started) || r.Requests < rs[i-1].Requests || r.Errors < rs[i-1].Errors ||
				r.ProcessingNs < rs[i-1].ProcessingNs {
				runs = append(runs, nil)
			}
			runs[len(runs)-1] = append(runs[len(runs)-1], r)
		}
		out[id] = runs
	}
	return out
}

// MicroWindowIncrease is an endpoint's increase over the reports of the
// RateSpan before the range's end: for each instance in current (those of
// the epoch), its newest run with two reports at different times, the
// counters' increase from its first to its last report, and the request
// rate over that time; summed over the instances. A gone instance is left
// out, so a restart is not counted twice.
func MicroWindowIncrease(reports []MicroReport, current map[string]bool) MicroIncrease {
	var out MicroIncrease
	var rate float64
	n := 0
	for id, runs := range microRuns(reports) {
		if !current[id] {
			continue
		}
		run := runs[len(runs)-1]
		first, last := run[0], run[len(run)-1]
		if len(run) < 2 || !last.At.After(first.At) {
			continue
		}
		n++
		d := float64(last.Requests - first.Requests)
		rate += d / last.At.Sub(first.At).Seconds()
		out.Requests += d
		out.Errors += float64(last.Errors - first.Errors)
		out.ProcessingNs += float64(last.ProcessingNs - first.ProcessingNs)
	}
	if n > 0 {
		out.PerSecond = &rate
	}
	return out
}

// MicroDeltas are the increases between consecutive reports of each
// instance's runs, in time order.
func MicroDeltas(reports []MicroReport) []MicroDelta {
	var out []MicroDelta
	for _, runs := range microRuns(reports) {
		for _, run := range runs {
			for i := 1; i < len(run); i++ {
				out = append(out, MicroDelta{At: run[i].At, Requests: float64(run[i].Requests - run[i-1].Requests),
					ProcessingNs: float64(run[i].ProcessingNs - run[i-1].ProcessingNs)})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b MicroDelta) int { return a.At.Compare(b.At) })
	return out
}

// averages sums deltas by key and divides processing time by requests,
// in milliseconds; keys without a request have none.
func averages(deltas []MicroDelta, key func(time.Time) time.Time) []MicroAverage {
	type sum struct{ req, ns float64 }
	sums := map[time.Time]*sum{}
	for _, d := range deltas {
		k := key(d.At)
		if sums[k] == nil {
			sums[k] = &sum{}
		}
		sums[k].req += d.Requests
		sums[k].ns += d.ProcessingNs
	}
	var out []MicroAverage
	for _, k := range slices.SortedFunc(maps.Keys(sums), func(a, b time.Time) int { return a.Compare(b) }) {
		if s := sums[k]; s.req > 0 {
			out = append(out, MicroAverage{At: k, Ms: s.ns / s.req / 1e6})
		}
	}
	return out
}

// MicroHourlyAverages are the average processing times of each UTC hour
// with requests, in hour order: the deltas of the hour summed.
func MicroHourlyAverages(deltas []MicroDelta) []MicroAverage {
	return averages(deltas, func(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) })
}

// MicroCollectionAverages are the average processing times of each
// collection with requests, in time order: the deltas that end in it
// summed over the instances.
func MicroCollectionAverages(deltas []MicroDelta) []MicroAverage {
	return averages(deltas, func(t time.Time) time.Time { return t })
}

// MicroLevel is the median of the hourly averages of the MicroLevelSpan
// before the rate window; nil with fewer than MicroLevelMinHours of them.
func MicroLevel(hourly []float64) *float64 {
	if len(hourly) < MicroLevelMinHours {
		return nil
	}
	v := median(hourly)
	return &v
}

// MicroIsSlow reports whether an average time is slow against a level.
func MicroIsSlow(avgMs, levelMs *float64) bool {
	return avgMs != nil && levelMs != nil && *avgMs >= MicroSlowFactor**levelMs && *avgMs >= *levelMs+MicroSlowMarginMs
}

// MicroSlowSince is when the current slow run began: the collections with
// requests, in time order, up to the epoch; the run of collections each
// slow on its own that ends at the newest slow one; its first. nil when
// none is slow. A slow average over the rate window is a weighted mean of
// its collections', so one of them is slow too.
func MicroSlowSince(avgs []MicroAverage, levelMs float64) *time.Time {
	end := -1
	for i := len(avgs) - 1; i >= 0; i-- {
		if MicroIsSlow(&avgs[i].Ms, &levelMs) {
			end = i
			break
		}
	}
	if end < 0 {
		return nil
	}
	start := end
	for start > 0 && MicroIsSlow(&avgs[start-1].Ms, &levelMs) {
		start--
	}
	at := avgs[start].At
	return &at
}

// microService gathers one service's facts in FinishMicro.
type microService struct {
	account, name string
	answered      *MicroServiceFact
	seen          *MicroServiceFact
	gone          []MicroGoneFact
}

// FinishMicro builds the response from the facts for the request's
// account scope: one service per (account, name) of the answered, the
// seen and the gone; its instances, versions, missing instances,
// endpoints with their rates and the slow rule, the note, the order and
// the cuts.
func FinishMicro(f *MicroFacts, account string) *v1.GetMicroServicesResponse {
	if f == nil || f.Epoch == 0 {
		return &v1.GetMicroServicesResponse{}
	}
	res := &v1.GetMicroServicesResponse{Epoch: f.Epoch, EpochAt: timestamppb.New(f.EpochAt)}
	if f.SubscriptionsAt != nil {
		res.SubscriptionsAt = timestamppb.New(*f.SubscriptionsAt)
	}
	asked := map[string]MicroAccountFact{}
	for _, a := range f.Accounts {
		if inScope(account, a.Account) {
			asked[a.Account] = a
		}
	}
	type key struct{ account, name string }
	all := map[key]*microService{}
	get := func(acc, name string) *microService {
		k := key{acc, name}
		if all[k] == nil {
			all[k] = &microService{account: acc, name: name}
		}
		return all[k]
	}
	for i, s := range f.Answered {
		if inScope(account, s.Account) {
			get(s.Account, s.Name).answered = &f.Answered[i]
		}
	}
	for i, s := range f.Seen {
		if inScope(account, s.Account) {
			get(s.Account, s.Name).seen = &f.Seen[i]
		}
	}
	for _, g := range f.Gone {
		if inScope(account, g.Account) && !g.LastSeen.Before(f.EpochAt.Add(-MicroMissingWindow)) {
			m := get(g.Account, g.Service)
			m.gone = append(m.gone, g)
		}
	}
	counts := map[string]int32{}
	var services []*v1.MicroService
	for _, m := range all {
		_, isAsked := asked[m.account]
		s := microServiceOf(m, isAsked, f.SubscriptionsAt)
		if s.GetInstances() == 0 && len(s.GetMissing()) == 0 {
			continue
		}
		s.SystemAccount = f.SystemAccount != "" && s.GetAccount() == f.SystemAccount
		counts[s.GetAccount()]++
		services = append(services, s)
	}
	slices.SortFunc(services, compareMicroServices)
	res.ServicesTotal = int32(len(services))
	res.Services = services[:min(len(services), MaxMicroServices)]
	accounts := map[string]bool{}
	for acc := range asked {
		accounts[acc] = true
	}
	for acc := range counts {
		accounts[acc] = true
	}
	for acc := range accounts {
		st := &v1.MicroAccountState{Account: acc, Services: counts[acc], SystemAccount: f.SystemAccount != "" && acc == f.SystemAccount}
		if a, ok := asked[acc]; ok {
			st.Asked, st.Info, st.Stats = true, microRequest(a.Info), microRequest(a.Stats)
		}
		res.Accounts = append(res.Accounts, st)
	}
	slices.SortFunc(res.Accounts, func(a, b *v1.MicroAccountState) int {
		return cmp.Or(compareBool(a.GetSystemAccount(), b.GetSystemAccount()), cmp.Compare(a.GetAccount(), b.GetAccount()))
	})
	return res
}

func microRequest(r MicroRequestFact) *v1.MicroRequest {
	out := &v1.MicroRequest{State: r.State, Replies: r.Replies, Error: r.Error}
	if r.State != v1.MicroRequestState_MICRO_REQUEST_STATE_UNSPECIFIED {
		out.At = timestamppb.New(r.At)
	}
	return out
}

// microServiceOf applies the rules to one service. Its instances are
// those that answered when any did, else those seen (registered and not
// answering, or in an account not asked), each named by the connection
// that holds its $SRV subscriptions. A gone instance not among them is
// missing when its $SRV subscriptions are in a subscription collection
// taken after it went missing, or the service has fewer instances than
// when it was last seen.
func microServiceOf(m *microService, asked bool, subsAt *time.Time) *v1.MicroService {
	s := &v1.MicroService{Account: m.account, Name: m.name, Asked: asked || m.answered != nil}
	seen := map[string]MicroInstanceFact{}
	if m.seen != nil {
		for _, in := range m.seen.Instances {
			seen[in.ID] = in
		}
	}
	var insts []MicroInstanceFact
	switch {
	case m.answered != nil && len(m.answered.Instances) > 0:
		s.Answered = true
		for _, in := range m.answered.Instances {
			if sn, ok := seen[in.ID]; ok && in.Server == "" {
				in.Connection, in.Server = sn.Connection, sn.Server
			}
			insts = append(insts, in)
		}
	case m.seen != nil:
		insts = slices.Clone(m.seen.Instances)
	}
	slices.SortFunc(insts, func(a, b MicroInstanceFact) int { return cmp.Compare(a.ID, b.ID) })
	insts = slices.CompactFunc(insts, func(a, b MicroInstanceFact) bool { return a.ID == b.ID })
	s.Instances = int32(len(insts))
	current := map[string]bool{}
	versions, descriptions := map[string]int32{}, map[string]int32{}
	var names []string
	for _, in := range insts {
		current[in.ID] = true
		if in.Version != "" {
			versions[in.Version]++
		}
		if in.Description != "" {
			descriptions[in.Description]++
		}
		if in.Connection != "" {
			names = append(names, in.Connection)
		}
	}
	for _, in := range insts[:min(len(insts), MaxMicroInstances)] {
		x := &v1.MicroInstance{Id: in.ID, Version: in.Version, ConnectionName: in.Connection, Server: in.Server}
		if in.Started != nil {
			x.Started = timestamppb.New(*in.Started)
		}
		s.InstanceList = append(s.InstanceList, x)
	}
	s.ConnectionNames, s.MoreNames = cutNames(names, 0)
	for v, n := range versions {
		s.Versions = append(s.Versions, &v1.MicroVersionCount{Version: v, Instances: n})
	}
	slices.SortFunc(s.Versions, func(a, b *v1.MicroVersionCount) int {
		return cmp.Or(cmp.Compare(b.GetInstances(), a.GetInstances()), cmp.Compare(a.GetVersion(), b.GetVersion()))
	})
	best := int32(0)
	for _, d := range slices.Sorted(maps.Keys(descriptions)) {
		if descriptions[d] > best {
			s.Description, best = d, descriptions[d]
		}
	}
	gone := slices.Clone(m.gone)
	slices.SortFunc(gone, func(a, b MicroGoneFact) int { return cmp.Or(cmp.Compare(a.ID, b.ID), b.LastSeen.Compare(a.LastSeen)) })
	gone = slices.CompactFunc(gone, func(a, b MicroGoneFact) bool { return a.ID == b.ID })
	for _, g := range gone {
		if current[g.ID] {
			continue
		}
		_, subscribed := seen[g.ID]
		subscribed = subscribed && asked && subsAt != nil && !subsAt.Before(g.Since)
		if !subscribed && g.InstancesThen <= s.GetInstances() {
			continue
		}
		s.Missing = append(s.Missing, &v1.MicroMissingInstance{Id: g.ID, Since: timestamppb.New(g.Since), LastSeen: timestamppb.New(g.LastSeen),
			ConnectionName: g.Connection, StillSubscribed: subscribed})
	}
	if m.answered != nil && s.GetAnswered() {
		var rate, req, errs float64
		rated := false
		for _, e := range m.answered.Endpoints {
			x := microEndpointOf(e)
			if e.Window.PerSecond != nil {
				rate += *e.Window.PerSecond
				rated = true
			}
			req += e.Window.Requests
			errs += e.Window.Errors
			s.Endpoints = append(s.Endpoints, x)
		}
		if rated {
			s.RequestsPerSecond = &rate
		}
		if req > 0 {
			s.ErrorShare = ptrOf(errs / req)
		}
		slices.SortFunc(s.Endpoints, compareMicroEndpoints)
		s.EndpointsTotal = int32(len(s.Endpoints))
		s.Endpoints = s.Endpoints[:min(len(s.Endpoints), MaxMicroEndpoints)]
	}
	s.Note = microNote(s)
	return s
}

// microEndpointOf applies the endpoint rules: the average time and error
// share over the window's requests, the slow rule against the level, and
// the slow run's start only when slow.
func microEndpointOf(e MicroEndpointFact) *v1.MicroEndpoint {
	x := &v1.MicroEndpoint{Name: e.Name, Subject: e.Subject, QueueGroup: e.QueueGroup, RequestsPerSecond: e.Window.PerSecond,
		LastError: e.LastError, LastErrorInstance: e.LastErrorInstance, LevelMs: e.LevelMs}
	if e.Window.Requests > 0 {
		x.AverageMs = ptrOf(e.Window.ProcessingNs / e.Window.Requests / 1e6)
		x.ErrorShare = ptrOf(e.Window.Errors / e.Window.Requests)
		x.ErrorsHigh = x.GetErrorShare() > MicroErrorsHigh
	}
	x.Slow = MicroIsSlow(x.AverageMs, e.LevelMs)
	if x.Slow && e.SlowSince != nil {
		x.SlowSince = timestamppb.New(*e.SlowSince)
	}
	return x
}

func ptrOf(v float64) *float64 { return &v }

// compareMicroEndpoints puts slow endpoints first, then those with errors
// above MicroErrorsHigh, the highest share first, then by name.
func compareMicroEndpoints(a, b *v1.MicroEndpoint) int {
	return cmp.Or(compareBool(!a.GetSlow(), !b.GetSlow()), compareBool(!a.GetErrorsHigh(), !b.GetErrorsHigh()),
		func() int {
			if a.GetErrorsHigh() && b.GetErrorsHigh() {
				return cmp.Compare(b.GetErrorShare(), a.GetErrorShare())
			}
			return 0
		}(), cmp.Compare(a.GetName(), b.GetName()))
}

// microNote is the first note that applies: an instance missing, mixed
// versions, a slow endpoint, errors above 1% on an endpoint, not asked,
// asked without an answer, else healthy.
func microNote(s *v1.MicroService) v1.MicroNote {
	switch {
	case len(s.GetMissing()) > 0:
		return v1.MicroNote_MICRO_NOTE_INSTANCE_MISSING
	case len(s.GetVersions()) > 1:
		return v1.MicroNote_MICRO_NOTE_MIXED_VERSIONS
	case slices.ContainsFunc(s.GetEndpoints(), (*v1.MicroEndpoint).GetSlow):
		return v1.MicroNote_MICRO_NOTE_SLOW_ENDPOINT
	case slices.ContainsFunc(s.GetEndpoints(), (*v1.MicroEndpoint).GetErrorsHigh):
		return v1.MicroNote_MICRO_NOTE_ERRORS
	case !s.GetAsked():
		return v1.MicroNote_MICRO_NOTE_NOT_ASKED
	case !s.GetAnswered():
		return v1.MicroNote_MICRO_NOTE_NO_ANSWER
	}
	return v1.MicroNote_MICRO_NOTE_HEALTHY
}

// MicroNoteWarns reports whether a note is a warning: all but not asked
// and healthy.
func MicroNoteWarns(n v1.MicroNote) bool {
	switch n {
	case v1.MicroNote_MICRO_NOTE_UNSPECIFIED, v1.MicroNote_MICRO_NOTE_NOT_ASKED, v1.MicroNote_MICRO_NOTE_HEALTHY:
		return false
	}
	return true
}

// compareMicroServices puts services whose note warns first, the system
// account's last; then by requests per second, most first (unset last);
// then by name and account.
func compareMicroServices(a, b *v1.MicroService) int {
	rate := func(s *v1.MicroService) *float64 {
		if s.RequestsPerSecond == nil {
			return nil
		}
		return ptrOf(-s.GetRequestsPerSecond())
	}
	return cmp.Or(compareBool(!MicroNoteWarns(a.GetNote()), !MicroNoteWarns(b.GetNote())), compareBool(a.GetSystemAccount(), b.GetSystemAccount()),
		compareSoonest(rate(a), rate(b)), cmp.Compare(a.GetName(), b.GetName()), cmp.Compare(a.GetAccount(), b.GetAccount()))
}
