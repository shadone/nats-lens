package storeprov

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	if res.Tiles, err = p.tiles(ctx, req.GetRange(), req.GetMaxPoints(), servers); err != nil {
		return nil, err
	}
	if res.Windows, err = p.k8sWindows(ctx, req.GetRange()); err != nil {
		return nil, err
	}
	cur, err := p.firingFindings(ctx, req.GetRange())
	if err != nil {
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

// tileFindings says which checks concern each tile: a code, or an entity
// prefix such as "consumer-".
var tileFindings = map[string][]string{
	"servers":        {"server-001", "server-002", "server-003", "server-004", "server-005", "server-006", "system-"},
	"connections":    {"client-", "server-011", "server-015"},
	"slow_consumers": {"client-003", "server-009", "server-010"},
	"js_storage":     {"server-007", "server-014", "stream-004", "stream-006"},
	"consumers":      {"consumer-"},
}

// tileTypes are the entity types whose checks must have run for a tile
// without a firing finding to be ok.
var tileTypes = map[string][]string{
	"servers":        {"server", "system"},
	"connections":    {"client", "server"},
	"slow_consumers": {"client", "server"},
	"js_storage":     {"server", "stream"},
	"msgs_rate":      {"server"},
	"consumers":      {"consumer"},
}

// tileStatus sets each tile's status: no data without a value, else the
// worst firing finding of its checks, else ok when the checks ran.
func tileStatus(tiles []*v1.Tile, cur []*v1.Finding, ran ranFor) {
	for _, t := range tiles {
		if t.Value == nil {
			t.Status = v1.Status_STATUS_NO_DATA
			continue
		}
		t.Status = orNoData(v1.Status_STATUS_OK, ran(tileTypes[t.GetId()]...))
		for _, f := range cur {
			for _, c := range tileFindings[t.GetId()] {
				if f.GetCheckCode() == c || (strings.HasSuffix(c, "-") && strings.HasPrefix(f.GetCheckCode(), c)) {
					st := statusOfSeverity(f.GetSeverity())
					if f.GetMute() != nil {
						st = v1.Status_STATUS_MUTED
					}
					t.Status = worse(t.Status, st)
				}
			}
		}
	}
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

// total is the sum of the series' values; nil without points.
func total(s *v1.Series) *float64 {
	if len(s.GetPoints()) == 0 {
		return nil
	}
	var t float64
	for _, x := range s.GetPoints() {
		t += x.GetValue()
	}
	return &t
}

// tiles are the overview numbers the snapshots can give. No check covers
// them yet, so their status is no data.
func (p *Provider) tiles(ctx context.Context, r *v1.TimeRange, maxPoints int32, servers *v1.ListServersResponse) ([]*v1.Tile, error) {
	sys := &v1.EntityRef{Type: v1.EntityType_ENTITY_TYPE_SYSTEM}
	get := map[string]*v1.Series{}
	for _, m := range []string{"cluster.servers_up", "cluster.connections", "cluster.slow_consumers", "cluster.js_storage_bytes",
		"cluster.in_msgs_rate", "cluster.out_msgs_rate", "cluster.durable_consumers"} {
		s, err := p.series(ctx, m, sys, r, maxPoints)
		if err != nil {
			return nil, err
		}
		get[m] = s
	}
	versions := map[string]bool{}
	var limit, used float64
	reporting := false
	for _, s := range servers.GetServers() {
		versions[s.GetVersion()] = true
		if s.GetPresent() && s.JsStorageBytes != nil {
			limit += float64(s.GetJsMaxStorageBytes())
			used += float64(s.GetJsStorageBytes())
			reporting = true
		}
	}
	verText := ""
	switch {
	case len(versions) == 1:
		for v := range versions {
			verText = "v" + v + " on all"
		}
	case len(versions) > 1:
		verText = "versions differ"
	}
	// A tile shows its chart's last point, so that the number and the
	// chart use the same reduction: the last value of the bucket for a
	// count, the bucket's average for a rate.
	newest := map[string]*float64{}
	for _, m := range []string{"cluster.servers_up", "cluster.connections", "cluster.in_msgs_rate", "cluster.out_msgs_rate", "cluster.durable_consumers"} {
		if pts := get[m].GetPoints(); len(pts) > 0 {
			newest[m] = ptr(pts[len(pts)-1].GetValue())
		}
	}
	ephemeral, err := p.ephemeralConsumers(ctx, r)
	if err != nil {
		return nil, err
	}
	nd := v1.Status_STATUS_NO_DATA
	storage := &v1.Tile{Id: "js_storage", Label: "JetStream storage", Unit: v1.Unit_UNIT_BYTES,
		Status: nd, Series: get["cluster.js_storage_bytes"]}
	if reporting {
		storage.Value = ptr(used)
		// The servers' newest report may lie outside the bucket averages;
		// the chart's range takes it in.
		if ser := storage.GetSeries(); ser != nil && len(ser.GetPoints()) > 0 {
			ser.Min, ser.Max = ptr(min(ser.GetMin(), used)), ptr(max(ser.GetMax(), used))
		}
	}
	if limit > 0 {
		storage.Limit = &limit
		storage.ValueSuffix = fmt.Sprintf("of %.0f GiB", limit/(1<<30))
		storage.Subtitle = fmt.Sprintf("%.1f%% used", used/limit*100)
	}
	serversTile := &v1.Tile{Id: "servers", Label: "Servers", Value: newest["cluster.servers_up"], Unit: v1.Unit_UNIT_COUNT,
		Subtitle: verText, Status: nd, Series: get["cluster.servers_up"]}
	if n := len(servers.GetExpected()); n > 0 {
		serversTile.ValueSuffix = fmt.Sprintf("/ %d", n)
	}
	return []*v1.Tile{
		serversTile,
		{Id: "connections", Label: "Connections", Value: newest["cluster.connections"], Unit: v1.Unit_UNIT_COUNT,
			Status: nd, Series: get["cluster.connections"]},
		{Id: "slow_consumers", Label: "Slow consumer events", Value: total(get["cluster.slow_consumers"]), Unit: v1.Unit_UNIT_COUNT,
			ValueSuffix: "in range", Status: nd, Series: get["cluster.slow_consumers"]},
		storage,
		{Id: "msgs_rate", Label: "Messages / s", Value: newest["cluster.in_msgs_rate"], Unit: v1.Unit_UNIT_PER_SECOND,
			ValueSuffix: "in", Value2: newest["cluster.out_msgs_rate"], Value2Suffix: "out", Status: nd,
			Series: get["cluster.in_msgs_rate"], Series2: get["cluster.out_msgs_rate"]},
		{Id: "consumers", Label: "Consumers", Value: newest["cluster.durable_consumers"], Unit: v1.Unit_UNIT_COUNT,
			ValueSuffix: "durable", Value2: ephemeral, Value2Suffix: "ephemeral", Status: nd,
			Series: get["cluster.durable_consumers"]},
	}, nil
}

func ptr[T any](v T) *T { return &v }

// ephemeralConsumers counts consumers without a durable name in the
// newest epoch of the range.
func (p *Provider) ephemeralConsumers(ctx context.Context, r *v1.TimeRange) (*float64, error) {
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil || !ok {
		return nil, err
	}
	var n float64
	err = p.db().QueryRowContext(ctx, `
SELECT count(DISTINCT (cs.account, cs.stream, cs.consumer)) FROM consumer_stats cs
LEFT JOIN consumer_config cc ON cc.server_id = cs.server_id AND cc.account = cs.account AND cc.stream = cs.stream
  AND cc.consumer = cs.consumer AND cs.epoch BETWEEN cc.first_epoch AND cc.last_epoch
WHERE cs.epoch = ? AND coalesce(cc.durable_name, '') = ''`, e.epoch).Scan(&n)
	if err != nil {
		return nil, err
	}
	return &n, nil
}
