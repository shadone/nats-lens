package storeprov

import (
	"context"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetOverview reads the cluster, freshness, servers, tiles, the finding
// tally and the worst unmuted firing finding.
func (p *Provider) GetOverview(ctx context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	sh, err := p.shell(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	res := &v1.GetOverviewResponse{Range: req.GetRange(), Freshness: sh.freshness, Servers: sh.servers.GetServers(), Tally: sh.tally}
	for _, s := range res.GetServers() {
		if s.GetCluster() != "" {
			res.Cluster = s.GetCluster()
			break
		}
	}
	cur := sh.cur
	if res.Tiles, err = p.tiles(ctx, req.GetRange(), req.GetMaxPoints(), sh.servers, cur); err != nil {
		return nil, err
	}
	if res.Windows, err = p.k8sWindows(ctx, req.GetRange()); err != nil {
		return nil, err
	}
	for _, f := range cur {
		if f.GetMute() == nil {
			res.WorstFinding = f
			break
		}
	}
	ran := ranFor(func(...string) bool { return false })
	if res.GetFreshness().GetEpoch() != 0 {
		if ran, err = p.checksRan(ctx, res.GetFreshness().GetEpoch()); err != nil {
			return nil, err
		}
	}
	tileStatus(res.Tiles, cur, ran)
	return res, nil
}

// ShellStatus reads the overview's freshness, servers and finding tally
// over the range, without its tiles and windows.
func (p *Provider) ShellStatus(ctx context.Context, req *v1.GetShellStatusRequest) (*v1.GetShellStatusResponse, error) {
	sh, err := p.shell(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	return &v1.GetShellStatusResponse{Freshness: sh.freshness, Servers: sh.servers.GetServers(), Tally: sh.tally}, nil
}

// shellParts are the parts of the overview the shell shows, with the
// firing findings they count.
type shellParts struct {
	freshness *v1.Freshness
	servers   *v1.ListServersResponse
	cur       []*v1.Finding
	tally     *v1.FindingTally
}

// shell reads the freshness, servers, firing findings and tally of the
// range.
func (p *Provider) shell(ctx context.Context, r *v1.TimeRange) (shellParts, error) {
	var sh shellParts
	var err error
	if sh.freshness, err = p.freshness(ctx, r); err != nil {
		return sh, err
	}
	if sh.servers, err = p.listServers(ctx, r); err != nil {
		return sh, err
	}
	if sh.cur, err = p.firingFindings(ctx, r); err != nil {
		return sh, err
	}
	sh.tally, err = p.tally(ctx, r, sh.cur)
	return sh, err
}

// tally counts the firing findings as the range's end sees them and the
// resolve transitions in the 24 hours before it.
func (p *Provider) tally(ctx context.Context, r *v1.TimeRange, cur []*v1.Finding) (*v1.FindingTally, error) {
	_, end := span(r)
	if !p.past(r) {
		end = p.Now()
	}
	t := &v1.FindingTally{}
	for _, f := range cur {
		if f.GetState() == v1.FindingState_FINDING_STATE_FLAPPING {
			t.Flapping++
		}
		if f.GetAcknowledgement() != nil {
			t.Acknowledged++
		}
		switch {
		case f.GetMute() != nil:
			t.Muted++
		case f.GetSeverity() == v1.Severity_SEVERITY_CRITICAL:
			t.Critical++
		case f.GetSeverity() == v1.Severity_SEVERITY_WARNING:
			t.Warning++
		default:
			t.Info++
		}
	}
	n, err := p.St.ResolvedBetween(ctx, end.Add(-24*time.Hour), end)
	t.Resolved_24H = int32(n)
	return t, err
}

// tileStatus sets each tile's status: no data without a value or a rule,
// else the worst firing finding of its checks, else ok when the checks
// ran.
func tileStatus(tiles []*v1.Tile, cur []*v1.Finding, ran ranFor) {
	for _, t := range tiles {
		rule, ok := api.TileRules[t.GetId()]
		if t.Value == nil || !ok {
			t.Status = v1.Status_STATUS_NO_DATA
			continue
		}
		t.Status = orNoData(v1.Status_STATUS_OK, ran(rule.Types...))
		for _, f := range cur {
			if !rule.Concerns(f) {
				continue
			}
			st := statusOfSeverity(f.GetSeverity())
			if f.GetMute() != nil {
				st = v1.Status_STATUS_MUTED
			}
			t.Status = worse(t.Status, st)
		}
	}
}

// freshness describes the newest epoch inside the range; the epoch is
// zero when the range holds none. The collector collects unless a failure
// of its connection is stored after the newest epoch; the handler replaces
// that with the runtime's status when there is one.
func (p *Provider) freshness(ctx context.Context, r *v1.TimeRange) (*v1.Freshness, error) {
	f := &v1.Freshness{Live: p.Live, Interval: dur(p.Interval), Collecting: true}
	fail, err := p.failure(ctx)
	if err != nil {
		return f, err
	}
	if !fail.since.IsZero() {
		f.Collecting, f.NotCollectingSince = false, ts(fail.since)
	}
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil || !ok {
		return f, err
	}
	f.Epoch, f.Partial = e.epoch, e.partial
	f.EpochAt, f.Age = ts(e.startedAt), dur(p.Now().Sub(e.startedAt).Truncate(time.Second))
	return f, nil
}

func ptr[T any](v T) *T { return &v }
