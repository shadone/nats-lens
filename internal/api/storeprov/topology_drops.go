package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/leafhealth"
)

// topologyWindow are the CTEs every drop query starts with: ep, the
// collections of the window up to the epoch, and pairs, the consecutive
// ones (a, its start a_at, and b). Its arguments are windowArgs.
const topologyWindow = `
WITH ep AS (SELECT epoch, started_at FROM epochs WHERE started_at >= ? AND epoch <= ?),
pairs AS (
  SELECT a, a_at, b FROM (
    SELECT epoch AS a, started_at AS a_at, lead(epoch) OVER (ORDER BY epoch) AS b,
      lead(started_at) OVER (ORDER BY epoch) AS b_at FROM ep)
  WHERE b IS NOT NULL AND date_diff('microsecond', a_at, b_at) <= ?)`

// windowArgs are the arguments of topologyWindow for f's epoch.
func (p *Provider) windowArgs(f *api.TopologyFacts) []any {
	gap := int64(math.MaxInt64)
	if p.Interval > 0 {
		gap = (api.TopologyGapIntervals * p.Interval).Microseconds()
	}
	return []any{f.EpochAt.Add(-api.TopologyDropWindow), f.Epoch, gap}
}

// topologyDrops counts the drops of the window before the epoch by the
// rule of api.TopologyDrops, in the database, so that only the counts and
// one row per site leave it: per reporting server and remote cluster for
// gateways, per site for leaf nodes, and each site's newest row before
// the epoch.
func (p *Provider) topologyDrops(ctx context.Context, f *api.TopologyFacts) error {
	var start sql.NullTime
	if err := p.db().QueryRowContext(ctx, `SELECT min(started_at) FROM epochs WHERE started_at >= ? AND epoch <= ?`,
		f.EpochAt.Add(-api.TopologyDropWindow), f.Epoch).Scan(&start); err != nil {
		return fmt.Errorf("topology window: %w", err)
	}
	f.Drops.WindowStart = ntime(start)
	query := func(what, q string, args []any, scan func(*sql.Rows) error) error {
		rows, err := p.db().QueryContext(ctx, q, args...)
		if err != nil {
			return fmt.Errorf("topology %s: %w", what, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	window := p.windowArgs(f)
	if err := query("gateway pairs", topologyWindow+`
SELECT DISTINCT coalesce(s.cluster, ''), g.gateway FROM gateways g JOIN servers s USING (server_id)
WHERE g.direction = 'outbound' AND g.epoch IN (SELECT epoch FROM ep)`, window, func(rows *sql.Rows) error {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return err
		}
		f.Drops.GatewayPairs = append(f.Drops.GatewayPairs, pair)
		return nil
	}); err != nil {
		return err
	}
	if err := query("gateway drops", topologyWindow+`,
gw AS (SELECT epoch, server_id, gateway, cid,
    `+nextSeen("server_id, gateway, cid")+` AS nxt
  FROM gateways WHERE direction = 'outbound' AND cid IS NOT NULL AND epoch IN (SELECT epoch FROM ep)),
ok AS (SELECT DISTINCT epoch, server_id FROM epoch_endpoints
  WHERE endpoint = 'GATEWAYZ' AND state = 'ok' AND epoch IN (SELECT epoch FROM ep)),
obs AS (
  SELECT g.server_id, g.gateway, p.a_at >= ? AS recent, g.nxt IS DISTINCT FROM p.b AS dropped
  FROM pairs p JOIN gw g ON g.epoch = p.a
  JOIN ok oa ON oa.epoch = p.a AND oa.server_id = g.server_id
  JOIN ok ob ON ob.epoch = p.b AND ob.server_id = g.server_id)
SELECT s.server_name, coalesce(s.cluster, ''), o.gateway,
  count(*) FILTER (WHERE dropped AND recent), count(*) FILTER (WHERE dropped), bool_or(recent)
FROM obs o JOIN servers s USING (server_id) GROUP BY ALL`, append(window, f.EpochAt.Add(-api.TopologyRecentWindow)), func(rows *sql.Rows) error {
		d := api.TopologyGatewayDrops{ObservedDay: true}
		if err := rows.Scan(&d.Server, &d.Cluster, &d.Gateway, &d.Recent, &d.Day, &d.ObservedRecent); err != nil {
			return err
		}
		f.Drops.Gateways = append(f.Drops.Gateways, d)
		return nil
	}); err != nil {
		return err
	}
	if err := query("leaf node drops", topologyWindow+`,
`+leafhealth.Counts(false)+` SELECT * FROM leaf_counts`, window, func(rows *sql.Rows) error {
		var d api.TopologyLeafDrops
		if err := rows.Scan(&d.Name, &d.Account, &d.Drops); err != nil {
			return err
		}
		f.Drops.Leafs = append(f.Drops.Leafs, d)
		return nil
	}); err != nil {
		return err
	}
	return query("leaf node sightings", topologyWindow+`,
-- Each site's newest collection before the epoch, then its rows there:
-- an aggregate over the window, not a sort of it.
newest AS (
  SELECT coalesce(nullif(name, ''), ip, '') AS site, coalesce(account, '') AS account, max(epoch) AS epoch
  FROM leafnodes WHERE epoch < ? AND epoch IN (SELECT epoch FROM ep) AND `+leafSite+` GROUP BY ALL),
x AS (
  SELECT `+leafCols+`, row_number() OVER (PARTITION BY n.site, n.account ORDER BY s.server_name, l.lid) AS rn
  FROM leafnodes l JOIN servers s USING (server_id)
  JOIN newest n ON n.epoch = l.epoch AND n.site = coalesce(nullif(l.name, ''), l.ip, '') AND n.account = coalesce(l.account, '')
  WHERE `+leafWhere+`)
SELECT x.* EXCLUDE (rn), e.started_at FROM x JOIN epochs e USING (epoch) WHERE rn = 1`, append(window, f.Epoch), func(rows *sql.Rows) error {
		var s api.TopologyLeafSighting
		var epoch int64
		var rtt, subs sql.NullInt64
		r := &s.Row
		if err := rows.Scan(&epoch, &r.ServerID, &r.Server, &r.Cluster, &r.LID, &r.Name, &r.Account, &r.IP, &rtt, &subs, &r.Compression, &s.At); err != nil {
			return err
		}
		r.RTT, r.Subscriptions, s.At = durPtr(rtt), int64Ptr(subs), s.At.UTC()
		f.Drops.LastSeen = append(f.Drops.LastSeen, s)
		return nil
	})
}

// leafSite keeps the rows of the leafnodes table that are sites, not the
// server's own uplinks; leafWhere is the same on alias l.
const leafSite = `NOT coalesce(is_spoke, false)`

// nextSeen is the next epoch, after a row's own, in which a row of the
// same key (the columns given) is stored; NULL when none is. A connection
// is still there at b exactly when it is b, as b is the collection after
// a. The window replaces a self-join on the next collection, which DuckDB
// ran about three times slower.
func nextSeen(key string) string {
	return `min(epoch) OVER (PARTITION BY ` + key + ` ORDER BY epoch RANGE BETWEEN 1 FOLLOWING AND UNBOUNDED FOLLOWING)`
}
