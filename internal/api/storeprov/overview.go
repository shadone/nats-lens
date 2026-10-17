package storeprov

import (
	"context"
	"strings"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// GetOverview reads the cluster, freshness, servers, tiles, the finding
// tally and the worst unmuted firing finding.
func (p *Provider) GetOverview(ctx context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	res := &v1.GetOverviewResponse{Range: req.GetRange(), Tally: &v1.FindingTally{}}
	f, err := p.freshness(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	res.Freshness = f
	servers, err := p.listServers(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	res.Servers = servers.GetServers()
	for _, s := range res.GetServers() {
		if s.GetCluster() != "" {
			res.Cluster = s.GetCluster()
			break
		}
	}
	cur, err := p.firingFindings(ctx, req.GetRange())
	if err != nil {
		return nil, err
	}
	if res.Tiles, err = p.tiles(ctx, req.GetRange(), req.GetMaxPoints(), servers, cur); err != nil {
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
	if res.Tally, err = p.tally(ctx, req.GetRange(), cur); err != nil {
		return nil, err
	}
	return res, nil
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

// tileRule says which findings concern a tile: those whose check code is
// one of codes, or starts with one ending in "-", and whose entity the
// filter accepts (all when nil). types are the entity types whose checks
// must have run for a tile without a firing finding to be ok; a tile
// without a rule has no check and no data.
type tileRule struct {
	codes  []string
	types  []string
	entity func(*v1.EntityRef) bool
}

// streamKind accepts the findings of streams whose name has the prefix,
// or of plain streams for an empty prefix.
func streamKind(prefix string) func(*v1.EntityRef) bool {
	return func(e *v1.EntityRef) bool {
		name := e.GetStream()
		if prefix == "" {
			return !isKV(name)
		}
		return strings.HasPrefix(name, prefix)
	}
}

var tileRules = map[string]tileRule{
	api.TileServers:      {codes: []string{"server-001", "server-002", "server-003", "server-004", "server-005", "server-006", "system-"}, types: []string{"server", "system"}},
	api.TileMetaLeader:   {codes: []string{"system-002", "system-003"}, types: []string{"system"}},
	api.TileStreams:      {codes: []string{"stream-"}, types: []string{"stream"}, entity: streamKind("")},
	api.TileKV:           {codes: []string{"stream-"}, types: []string{"stream"}, entity: streamKind("KV_")},
	api.TileObjectStores: {codes: []string{"stream-"}, types: []string{"stream"}, entity: streamKind("OBJ_")},
	api.TileConsumers:    {codes: []string{"consumer-"}, types: []string{"consumer"}},
	api.TileClients: {codes: []string{"client-", "server-011", "server-015", "server-009", "server-010"},
		types: []string{"client", "server"}},
	api.TileStorage: {codes: []string{"server-007", "server-014", "stream-004", "stream-006"}, types: []string{"server", "stream"}},
}

// tileStatus sets each tile's status: no data without a value or a rule,
// else the worst firing finding of its checks, else ok when the checks
// ran.
func tileStatus(tiles []*v1.Tile, cur []*v1.Finding, ran ranFor) {
	for _, t := range tiles {
		rule, ok := tileRules[t.GetId()]
		if t.Value == nil || !ok {
			t.Status = v1.Status_STATUS_NO_DATA
			continue
		}
		t.Status = orNoData(v1.Status_STATUS_OK, ran(rule.types...))
		for _, f := range cur {
			if !rule.concerns(f) {
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

// concerns reports whether the finding is one of the rule's.
func (r tileRule) concerns(f *v1.Finding) bool {
	if r.entity != nil && !r.entity(f.GetEntity()) {
		return false
	}
	for _, c := range r.codes {
		if f.GetCheckCode() == c || (strings.HasSuffix(c, "-") && strings.HasPrefix(f.GetCheckCode(), c)) {
			return true
		}
	}
	return false
}

// freshness describes the newest epoch inside the range; the epoch is
// zero when the range holds none.
func (p *Provider) freshness(ctx context.Context, r *v1.TimeRange) (*v1.Freshness, error) {
	f := &v1.Freshness{Live: p.Live, Interval: dur(p.Interval)}
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil || !ok {
		return f, err
	}
	f.Epoch, f.Partial = e.epoch, e.partial
	f.EpochAt, f.Age = ts(e.startedAt), dur(p.Now().Sub(e.startedAt).Truncate(time.Second))
	return f, nil
}

func ptr[T any](v T) *T { return &v }
