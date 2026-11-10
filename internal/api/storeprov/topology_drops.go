package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/checks"
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
	if err := query("route drops", topologyWindow+routeDropsSQL, append(p.windowArgs(f), f.EpochAt.Add(-api.TopologyRecentWindow), p.restartWindow().Microseconds()), func(rows *sql.Rows) error {
		d := api.TopologyRouteDrops{ObservedDay: true}
		if err := rows.Scan(&d.A, &d.B, &d.Recent, &d.Day, &d.ObservedRecent); err != nil {
			return err
		}
		f.Drops.Routes = append(f.Drops.Routes, d)
		return nil
	}); err != nil {
		return err
	}
	if err := query("route pairs", topologyWindow+`
SELECT DISTINCT least(s.server_name, x.server_name), greatest(s.server_name, x.server_name)
FROM routes r JOIN servers s ON s.server_id = r.server_id JOIN servers x ON x.server_id = r.remote_id
WHERE r.epoch IN (SELECT epoch FROM ep) AND s.server_name <> x.server_name
ORDER BY 1, 2`, p.windowArgs(f), func(rows *sql.Rows) error {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return err
		}
		f.Drops.RoutePairs = append(f.Drops.RoutePairs, pair)
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

// routeDropsSQL follows topologyWindow and counts route drops by the rule
// of api.TopologyDrops, per pair of server names: the collections in the
// last hour (its first argument; the second is the restart window in
// microseconds) and in the window in which a route of the pair
// dropped, and whether a pair of collections in the last hour observed
// one. As for leaf nodes, a route connection stored in every collection
// from its first to its last can only have dropped after its last, which
// an aggregate finds; the rare others are read row by row with a window
// over the next epoch, so the window does not sort every route row.
var routeDropsSQL = `,
rt AS (SELECT epoch, server_id, rid, remote_id FROM routes WHERE remote_id IS NOT NULL AND epoch IN (SELECT epoch FROM ep)),
ok AS (SELECT DISTINCT epoch, server_id FROM epoch_endpoints
  WHERE endpoint = 'ROUTEZ' AND state = 'ok' AND epoch IN (SELECT epoch FROM ep)),
-- A server that answered ROUTEZ at a and at the collection after it, and
-- whose process did not start within the restart window (microseconds,
-- the second argument) before a.
good AS (
  SELECT p.a, p.b, p.a_at >= ? AS recent, oa.server_id FROM pairs p
  JOIN ok oa ON oa.epoch = p.a JOIN ok ob ON ob.epoch = p.b AND ob.server_id = oa.server_id
  JOIN servers sv ON sv.server_id = oa.server_id
  WHERE NOT coalesce(sv.start_time > p.a_at - to_microseconds(?::BIGINT), false)),
-- Server and remote pairs with a route between two collections both
-- answered, and whether one such pair of collections is in the last hour.
seen AS (
  SELECT r.server_id, r.remote_id, bool_or(g.recent) AS recent FROM rt r
  JOIN good g ON g.a = r.epoch AND g.server_id = r.server_id
  JOIN good h ON h.a = r.epoch AND h.server_id = r.remote_id
  GROUP BY ALL),
pos AS (SELECT epoch, row_number() OVER (ORDER BY epoch) AS i FROM ep),
conns AS (
  SELECT c.*, c.n = c.d AND c.d = pl.i - pf.i + 1 AS unbroken FROM (
    SELECT server_id, rid, remote_id, count(*) AS n, count(DISTINCT epoch) AS d, min(epoch) AS first, max(epoch) AS last
    FROM rt GROUP BY ALL) c
  JOIN pos pf ON pf.epoch = c.first JOIN pos pl ON pl.epoch = c.last),
broken AS (
  SELECT r.*, ` + nextSeen("server_id, rid, remote_id") + ` AS nxt FROM rt r
  WHERE (server_id, rid, remote_id) IN (SELECT (server_id, rid, remote_id) FROM conns WHERE NOT unbroken)),
-- Each pair of collections in which a route of a server and remote
-- dropped: after the last row of an unbroken connection, or where a
-- broken one's next row is not the next collection.
dropped AS (
  SELECT c.server_id, c.remote_id, g.a, g.recent FROM conns c
  JOIN good g ON g.a = c.last AND g.server_id = c.server_id
  JOIN good h ON h.a = c.last AND h.server_id = c.remote_id
  WHERE c.unbroken
  UNION
  SELECT l.server_id, l.remote_id, g.a, g.recent FROM broken l
  JOIN good g ON g.a = l.epoch AND g.server_id = l.server_id
  JOIN good h ON h.a = l.epoch AND h.server_id = l.remote_id
  WHERE l.nxt IS DISTINCT FROM g.b),
named AS (
  SELECT o.*, least(s.server_name, x.server_name) AS sa, greatest(s.server_name, x.server_name) AS sb
  FROM (SELECT server_id, remote_id, NULL::BIGINT AS a, recent, false AS is_drop FROM seen
        UNION ALL SELECT server_id, remote_id, a, recent, true AS is_drop FROM dropped) o
  JOIN servers s ON s.server_id = o.server_id JOIN servers x ON x.server_id = o.remote_id
  WHERE s.server_name <> x.server_name)
SELECT sa, sb, count(DISTINCT a) FILTER (WHERE is_drop AND recent), count(DISTINCT a) FILTER (WHERE is_drop),
  bool_or(recent) FILTER (WHERE NOT is_drop)
FROM named GROUP BY sa, sb
HAVING bool_or(NOT is_drop)`

// restartWindow is how long after a process started its routes count no
// drop: checks.restart_window, else its default.
func (p *Provider) restartWindow() time.Duration {
	if p.Checks != nil {
		return p.Checks.RestartWindow()
	}
	return checks.DefaultRestartWindow
}
