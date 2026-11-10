package storeprov

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
	"github.com/shadone/nats-lens/internal/routehealth"
)

// replicationQuery lists, for one epoch, every mirror and source of a
// stream that some server reports, with the cluster of the copying stream
// and of its origin in the same account, and the leader's report of the
// copy. A copy whose configuration names an external origin (another
// account or domain) is left out, as is one whose origin no server
// reports. The cluster of a stream is the one its leader's report gives,
// else any replica's, else the reporting server's.
const replicationQuery = `
WITH ss AS (
  SELECT ss.*, s.server_name, coalesce(nullif(ss.cluster, ''), s.cluster, '') AS placed
  FROM stream_stats ss JOIN servers s USING (server_id) WHERE ss.epoch = $1),
place AS (
  SELECT account, stream, placed, CASE WHEN is_leader THEN server_name ELSE '' END AS leader, server_id,
    is_leader, mirror_lag, mirror_active_ns, coalesce(mirror_error, '') AS mirror_error
  FROM ss
  QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY coalesce(is_leader, false) DESC, server_id) = 1),
cfg AS (
  SELECT c.account, c.stream, c.mirror, json_extract(c.config, '$.mirror.external') IS NOT NULL AS external
  FROM stream_config c JOIN place p ON p.account = c.account AND p.stream = c.stream
  WHERE c.first_epoch <= $1 AND c.last_epoch >= $1 AND coalesce(c.mirror, '') <> ''
  QUALIFY row_number() OVER (PARTITION BY c.account, c.stream ORDER BY c.server_id = p.server_id DESC, c.server_id) = 1),
mirrors AS (
  SELECT p.account, p.stream, p.placed, 'mirror' AS kind, c.mirror AS origin, '' AS key, p.leader,
    CASE WHEN p.is_leader THEN p.mirror_lag END AS lag, CASE WHEN p.is_leader THEN p.mirror_active_ns END AS active_ns,
    CASE WHEN p.is_leader THEN p.mirror_error ELSE '' END AS error
  FROM place p JOIN cfg c USING (account, stream) WHERE NOT c.external),
srcs AS (
  SELECT p.account, p.stream, p.placed, 'source' AS kind, x.name AS origin, x.source_id AS key, p.leader,
    CASE WHEN x.reported AND p.is_leader THEN CAST(x.lag AS BIGINT) END AS lag,
    CASE WHEN x.reported AND p.is_leader THEN x.active_ns END AS active_ns,
    CASE WHEN x.reported AND p.is_leader THEN coalesce(x.error, '') ELSE '' END AS error
  FROM place p JOIN stream_source_stats x ON x.epoch = $1 AND x.account = p.account AND x.stream = p.stream
  WHERE json_extract(x.config, '$.external') IS NULL
  QUALIFY row_number() OVER (PARTITION BY p.account, p.stream, x.source_id ORDER BY x.server_id = p.server_id DESC, x.server_id) = 1),
copies AS (SELECT * FROM mirrors UNION ALL SELECT * FROM srcs)
SELECT c.account, c.stream, c.placed, c.kind, c.origin, o.placed, c.key, c.leader, c.lag, c.active_ns, c.error
FROM copies c JOIN place o ON o.account = c.account AND o.stream = c.origin`

type replicationRow struct {
	fact api.TopologyReplicationFact
	key  string
}

// topologyReplication reads the mirrors and sources of the epoch's
// streams and, for the lag and activity before, those of the newest
// earlier collection with stream reports, at most
// api.TopologyGapIntervals collect intervals earlier; none when the
// collect interval is not known. The start is compared with the column
// directly, so the session time zone does not shift it.
func (p *Provider) topologyReplication(ctx context.Context, f *api.TopologyFacts) error {
	now, err := p.replicationRows(ctx, f.Epoch)
	if err != nil || len(now) == 0 {
		return err
	}
	before := map[string]replicationRow{}
	if bound, known := routehealth.EarliestEarlier(f.EpochAt, p.Interval); known {
		var earlier int64
		err := p.db().QueryRowContext(ctx, `SELECT e.epoch FROM epochs e
WHERE e.epoch < ? AND e.started_at >= ? AND e.started_at < ?
  AND EXISTS (SELECT 1 FROM stream_stats ss WHERE ss.epoch = e.epoch)
ORDER BY e.epoch DESC LIMIT 1`, f.Epoch, bound, f.EpochAt).Scan(&earlier)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("topology replication earlier collection: %w", err)
		default:
			rows, err := p.replicationRows(ctx, earlier)
			if err != nil {
				return err
			}
			for _, r := range rows {
				before[r.id()] = r
			}
		}
	}
	for _, r := range now {
		if b, ok := before[r.id()]; ok {
			r.fact.LagBefore, r.fact.ActiveBefore = b.fact.Lag, b.fact.Active
		}
		f.Replication = append(f.Replication, r.fact)
	}
	return nil
}

func (r replicationRow) id() string {
	return r.fact.Account + "\x00" + r.fact.Stream + "\x00" + r.fact.Kind + "\x00" + r.fact.Origin + "\x00" + r.key
}

func (p *Provider) replicationRows(ctx context.Context, epoch int64) ([]replicationRow, error) {
	rows, err := p.db().QueryContext(ctx, replicationQuery, epoch)
	if err != nil {
		return nil, fmt.Errorf("topology replication: %w", err)
	}
	defer rows.Close()
	var out []replicationRow
	for rows.Next() {
		var r replicationRow
		var lag, active sql.NullInt64
		f := &r.fact
		if err := rows.Scan(&f.Account, &f.Stream, &f.Cluster, &f.Kind, &f.Origin, &f.OriginCluster, &r.key, &f.Leader,
			&lag, &active, &f.Error); err != nil {
			return nil, err
		}
		f.Lag, f.Active = int64Ptr(lag), durPtr(active)
		out = append(out, r)
	}
	return out, rows.Err()
}
