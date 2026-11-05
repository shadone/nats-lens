// Package fake serves fixed scenarios through the api.Provider interface,
// for the demo command and for tests. New serves example-prod: a
// three-server cluster on 2026-10-17 at 14:30 UTC, five hours after a GKE
// node upgrade (09:16-09:44) restarted all three NATS servers; it tells the
// story of the reference mockup (docs/ui-reference/reference.html), with
// the real checks' codes, names and details. Demo
// adds example-global, 24 servers in three clusters joined by gateways,
// and example-staging, whose collector stopped three hours ago.
//
// In fixed mode "now" is 2026-10-17T14:30:00Z, so every response is the same
// on every run. In live mode the scenario is shifted so that its "now" is
// the wall clock.
package fake

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// FixedNow is the scenario's present in fixed mode.
var FixedNow = time.Date(2026, 10, 17, 14, 30, 0, 0, time.UTC)

// Provider is the fake api.Provider for one scenario. Mutes created
// through the API are kept in memory.
type Provider struct {
	sc    *scenario
	live  bool
	clock func() time.Time

	mu     sync.Mutex
	mutes  []*storedMute
	nextID int
}

var _ api.Provider = (*Provider)(nil)

// New returns the provider of example-prod. With live set the scenario
// follows the wall clock; otherwise it is fixed at FixedNow.
func New(live bool) *Provider { return newProvider(prodScenario(), live) }

func newProvider(sc *scenario, live bool) *Provider {
	p := &Provider{sc: sc, live: live, clock: time.Now, nextID: 1}
	if sc.mutes != nil {
		p.mutes = sc.mutes()
		p.nextID += len(p.mutes)
	}
	return p
}

// Demo returns the demo's systems in order: example-prod (the default),
// example-global and example-staging.
func Demo(live bool) api.Systems {
	return api.Systems{
		{Ref: &v1.SystemRef{Id: Cluster, Name: Cluster, Environment: "production"}, Provider: New(live)},
		{Ref: &v1.SystemRef{Id: GlobalSystem, Name: GlobalSystem, Environment: "production"}, Provider: newProvider(globalScenario(), live)},
		{Ref: &v1.SystemRef{Id: StagingSystem, Name: StagingSystem, Environment: "staging"}, Provider: newProvider(stagingScenario(), live)},
	}
}

// Now is FixedNow, or the wall clock to the second in live mode.
func (p *Provider) Now() time.Time {
	if p.live {
		return p.clock().UTC().Truncate(time.Second)
	}
	return FixedNow
}

// scen returns the scenario as of the current Now.
func (p *Provider) scen() scen {
	now := p.Now()
	return scen{now: now, shift: now.Sub(FixedNow), sc: p.sc}
}

// scen places the scenario's wall-clock times relative to now.
type scen struct {
	now   time.Time
	shift time.Duration
	sc    *scenario
}

// at is hh:mm:ss on 2026-10-17 in the scenario.
func (s scen) at(h, m, sec int) time.Time {
	return time.Date(2026, 10, 17, h, m, sec, 0, time.UTC).Add(s.shift)
}

// on is a time on another day of October 2026 in the scenario.
func (s scen) on(day, h, m int) time.Time {
	return time.Date(2026, 10, day, h, m, 0, 0, time.UTC).Add(s.shift)
}

// clock maps a scenario time back to the fixed frame, so that values
// computed from it do not depend on the shift.
func (s scen) clock(t time.Time) time.Time { return t.Add(-s.shift) }

// dataEnd is where the scenario's data ends: now, or earlier for a system
// that stopped collecting.
func (s scen) dataEnd() time.Time { return s.now.Add(-s.sc.stale) }

// defaultHistory is how far back a scenario's history reaches unless it
// sets its own.
const defaultHistory = 30 * 24 * time.Hour

// dataStart is where the scenario's stored history begins.
func (s scen) dataStart() time.Time { return s.now.Add(-cmp.Or(s.sc.history, defaultHistory)) }

// lastEpoch is when the scenario's newest epoch started.
func (s scen) lastEpoch() time.Time { return s.dataEnd().Add(-4 * time.Second) }

// windows are the Kubernetes windows that overlap the range, derived from
// the scenario's Kubernetes events as the store derives them.
func (p *Provider) windows(s scen, r *v1.TimeRange) []*v1.TimeWindow {
	if !s.sc.kubernetes {
		return nil
	}
	// The events the store reads: from 12 hours before the range to the
	// window gap after it.
	from, to := r.GetStart().AsTime().Add(-12*time.Hour), r.GetEnd().AsTime().Add(api.K8sUpgradeGap)
	var evs []api.K8sWindowEvent
	for _, e := range p.events(s) {
		if at := e.GetTime().AsTime(); at.Before(from) || !at.Before(to) {
			continue
		}
		if e.GetSource() != srcK8s || !slices.Contains(api.K8sWindowKinds, e.GetKind()) ||
			(strings.HasPrefix(e.GetKind(), "k8s.pod.") && e.GetServer() == "") {
			continue
		}
		var raw struct {
			Node    string `json:"node"`
			Pool    string `json:"pool"`
			Kubelet string `json:"kubelet_version"`
			Cause   string `json:"cause"`
		}
		_ = json.Unmarshal([]byte(e.GetRaw()), &raw)
		evs = append(evs, api.K8sWindowEvent{At: e.GetTime().AsTime(), Kind: e.GetKind(), Server: e.GetServer(), Node: raw.Node,
			Version: raw.Kubelet, Cause: raw.Cause, Pool: raw.Pool})
	}
	slices.Reverse(evs) // oldest first
	var out []*v1.TimeWindow
	for _, w := range api.DeriveK8sWindows(evs, api.K8sUpgradeGap) {
		if w.GetStart().AsTime().Before(r.GetEnd().AsTime()) && !w.GetEnd().AsTime().Before(r.GetStart().AsTime()) {
			out = append(out, w)
		}
	}
	return out
}

func overlaps(a, b time.Time, r *v1.TimeRange) bool {
	return a.Before(r.GetEnd().AsTime()) && !b.Before(r.GetStart().AsTime())
}

func inRange(t time.Time, r *v1.TimeRange) bool {
	return !t.Before(r.GetStart().AsTime()) && t.Before(r.GetEnd().AsTime())
}

// Info describes the demo: its cluster, a 30 s interval and 14 days of
// retention.
func (p *Provider) Info(context.Context) (*v1.GetLensInfoResponse, error) {
	return &v1.GetLensInfoResponse{Cluster: p.sc.cluster, CollectInterval: dur(30 * time.Second),
		Retention: dur(14 * 24 * time.Hour), Live: p.live, Demo: true, NowFixed: !p.live, HistoryStart: ts(p.scen().dataStart())}, nil
}

// freshness describes the newest epoch inside the range.
func (s scen) freshness(r *v1.TimeRange) *v1.Freshness {
	// A system that stopped collecting has no collector writing to it.
	f := &v1.Freshness{Live: s.sc.stale == 0, Interval: dur(30 * time.Second), Collecting: true}
	if st := s.sc.collection; st == v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED || st == v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED {
		f.Collecting, f.NotCollectingSince = false, ts(s.sc.errorSince.Add(s.shift))
	}
	// The range's newest epoch, partial when a server of the scenario
	// was down then; none when the range holds no data.
	if at, n, ok := s.epochBefore(r); ok {
		f.Epoch, f.EpochAt, f.Age, f.Partial = n, ts(at), dur(s.now.Sub(at)), s.anyDown(at)
	}
	return f
}

// epochBefore is the newest epoch inside the range: the last epoch before
// its end that is not before its start, and its number. Epochs lie every
// 30 seconds back from the newest (lastEpoch) to the start of the history,
// numbered from 1 there, so no two epochs share a number.
func (s scen) epochBefore(r *v1.TimeRange) (time.Time, int64, bool) {
	last := s.lastEpoch()
	end := r.GetEnd().AsTime()
	at := last
	if !last.Before(end) {
		at = last.Add(-(last.Sub(end)/sampleEvery + 1) * sampleEvery)
	}
	if at.Before(r.GetStart().AsTime()) || at.Before(s.dataStart()) {
		return time.Time{}, 0, false
	}
	return at, s.epochNumber(at), true
}

// epochNumber is the number of the epoch that started at at: 1 for the
// first epoch of the history, one more every 30 seconds.
func (s scen) epochNumber(at time.Time) int64 {
	last := s.lastEpoch()
	first := last.Add(-last.Sub(s.dataStart()) / sampleEvery * sampleEvery)
	return 1 + int64(at.Sub(first)/sampleEvery)
}

// anyDown reports whether a server of the scenario was down at the
// scenario time at, by the rule its series use, so that an epoch then
// missed it.
func (s scen) anyDown(at time.Time) bool {
	return slices.ContainsFunc(s.sc.servers, func(sp serverSpec) bool { return s.downAt(sp, at) })
}

// downAt reports whether a server was down at the scenario time at.
func (s scen) downAt(sp serverSpec, at time.Time) bool {
	return s.sc.serverDown != nil && s.sc.serverDown(sp, s.clock(at))
}

// GetOverview returns the cluster, freshness, tally, worst finding, tiles
// and the maintenance windows.
func (p *Provider) GetOverview(_ context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	s := p.scen()
	all := p.findings(s)
	cur := current(all, req.GetRange())
	servers := s.servers(req.GetRange(), all)
	res := &v1.GetOverviewResponse{
		Cluster:      p.sc.cluster,
		Range:        req.GetRange(),
		Freshness:    s.freshness(req.GetRange()),
		Servers:      servers,
		Tally:        tally(all, req.GetRange()),
		Tiles:        s.overviewTiles(req.GetRange(), req.GetMaxPoints(), servers, cur),
		Windows:      p.windows(s, req.GetRange()),
		WorstFinding: worstUnmuted(cur),
	}
	return res, nil
}

// ShellStatus returns the overview's freshness, servers and tally over
// the range, read from the range's epoch as GetOverview reads them.
func (p *Provider) ShellStatus(_ context.Context, req *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error) {
	s := p.scen()
	all := p.findings(s)
	return &v1.GetShellStatusResponse{
		Freshness: s.freshness(req.GetRange()),
		Servers:   s.servers(req.GetRange(), all),
		Tally:     tally(all, req.GetRange()),
	}, nil
}

// CollectInterval is the scenario's time between epochs.
func (p *Provider) CollectInterval() time.Duration { return sampleEvery }

// worstUnmuted is the first unmuted finding of a worst-first list.
func worstUnmuted(cur []*v1.Finding) *v1.Finding {
	for _, f := range cur {
		if f.GetMute() == nil {
			return f
		}
	}
	return nil
}

func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
