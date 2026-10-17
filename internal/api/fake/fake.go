// Package fake serves a fixed scenario through the api.Provider interface:
// a three-server cluster on 2026-10-17 at 14:30 UTC, five hours after a
// GKE node upgrade (09:14-09:50) restarted all three NATS servers. It is the
// data of the reference mockup (docs/ui-reference/reference.html), for the
// demo command and for tests.
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

// Provider is the fake api.Provider. Mutes created through the API are kept
// in memory.
type Provider struct {
	live  bool
	clock func() time.Time

	mu     sync.Mutex
	mutes  []*storedMute
	nextID int
}

var _ api.Provider = (*Provider)(nil)

// New returns the fake provider. With live set the scenario follows the
// wall clock; otherwise it is fixed at FixedNow.
func New(live bool) *Provider {
	p := &Provider{live: live, clock: time.Now, nextID: 2}
	p.mutes = []*storedMute{seedMute()}
	return p
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
	return scen{now: now, shift: now.Sub(FixedNow)}
}

// scen places the scenario's wall-clock times relative to now.
type scen struct {
	now   time.Time
	shift time.Duration
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

// lastEpoch is when the scenario's newest epoch started.
func (s scen) lastEpoch() time.Time { return s.now.Add(-4 * time.Second) }

// upgrade is the node upgrade window.
func (s scen) upgrade() (time.Time, time.Time) { return s.at(9, 14, 0), s.at(9, 50, 0) }

func (s scen) windows(r *v1.TimeRange) []*v1.TimeWindow {
	a, b := s.upgrade()
	if !overlaps(a, b, r) {
		return nil
	}
	return []*v1.TimeWindow{{Start: ts(a), End: ts(b), Label: "GKE node upgrade", Detail: "3 drains, 3 pod restarts"}}
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
	return &v1.GetLensInfoResponse{Cluster: Cluster, CollectInterval: dur(30 * time.Second),
		Retention: dur(14 * 24 * time.Hour), Live: p.live, Demo: true}, nil
}

// GetOverview returns the cluster, freshness, tally, worst finding, tiles
// and the upgrade window.
func (p *Provider) GetOverview(_ context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	s := p.scen()
	all := p.findings(s)
	cur := current(all, req.GetRange())
	res := &v1.GetOverviewResponse{
		Cluster:   Cluster,
		Range:     req.GetRange(),
		Freshness: &v1.Freshness{Live: true, Interval: dur(30 * time.Second)},
		Servers:   s.servers(req.GetRange()),
		Tally:     tally(all, req.GetRange()),
		Tiles:     s.tiles(req.GetRange(), req.GetMaxPoints()),
		Windows:   s.windows(req.GetRange()),
	}
	// The newest epoch started 4 seconds ago; a range that does not hold
	// it reports no epoch.
	if at := s.lastEpoch(); inRange(at, req.GetRange()) {
		res.Freshness.Epoch, res.Freshness.EpochAt, res.Freshness.Age = 1734, ts(at), dur(s.now.Sub(at))
	}
	for _, f := range cur {
		if f.GetMute() == nil {
			res.WorstFinding = f
			break
		}
	}
	return res, nil
}

func earliest(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
