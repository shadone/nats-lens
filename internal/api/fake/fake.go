// Package fake serves fixed scenarios through the api.Provider interface,
// for the demo command and for tests. New serves example-prod: a
// three-server cluster on 2026-10-17 at 14:30 UTC, five hours after a GKE
// node upgrade (09:14-09:50) restarted all three NATS servers; it is the
// data of the reference mockup (docs/ui-reference/reference.html). Demo
// adds example-global, 24 servers in three clusters joined by gateways,
// and example-staging, whose collector stopped three hours ago.
//
// In fixed mode "now" is 2026-10-17T14:30:00Z, so every response is the same
// on every run. In live mode the scenario is shifted so that its "now" is
// the wall clock.
package fake

import (
	"context"
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

// lastEpoch is when the scenario's newest epoch started.
func (s scen) lastEpoch() time.Time { return s.dataEnd().Add(-4 * time.Second) }

func (s scen) windows(r *v1.TimeRange) []*v1.TimeWindow {
	if s.sc.windows == nil {
		return nil
	}
	return s.sc.windows(s, r)
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
		Retention: dur(14 * 24 * time.Hour), Live: p.live, Demo: true}, nil
}

// freshness describes the newest epoch inside the range.
func (s scen) freshness(r *v1.TimeRange) *v1.Freshness {
	// A system that stopped collecting has no collector writing to it.
	f := &v1.Freshness{Live: s.sc.stale == 0, Interval: dur(30 * time.Second)}
	// A range that does not hold the newest epoch reports no epoch.
	if at := s.lastEpoch(); inRange(at, r) {
		f.Epoch, f.EpochAt, f.Age, f.Partial = s.sc.epoch, ts(at), dur(s.now.Sub(at)), s.sc.partial
	}
	return f
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
		Windows:      s.windows(req.GetRange()),
		WorstFinding: worstUnmuted(cur),
	}
	return res, nil
}

// worstUnmuted is the first unmuted finding of a worst-first list.
func worstUnmuted(cur []*v1.Finding) *v1.Finding {
	for _, f := range cur {
		if f.GetMute() == nil {
			return f
		}
	}
	return nil
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
