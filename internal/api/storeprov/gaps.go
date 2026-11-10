package storeprov

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// The reads behind the gap rules of api/gaps.go, for any span: Replay
// reads them for A to B, Data history for its range.

// collectionGaps reads the times without collections that overlap a to b:
// no epoch for more than ReplayEpochReach intervals, the series rule,
// leading and trailing gaps included. A window over started_at returns
// only the epochs either side of a gap, the first and the last, so that a
// long span is not read epoch by epoch.
func (p *Provider) collectionGaps(ctx context.Context, a, b time.Time) ([]api.ReplayGap, error) {
	reach := api.ReplayEpochReach * p.Interval
	rows, err := p.db().QueryContext(ctx, `
WITH e AS (
    SELECT epoch, started_at FROM (SELECT epoch, started_at FROM epochs WHERE started_at < ? ORDER BY epoch DESC LIMIT 1)
    UNION ALL SELECT epoch, started_at FROM epochs WHERE started_at >= ? AND started_at <= ?
    UNION ALL SELECT epoch, started_at FROM (SELECT epoch, started_at FROM epochs WHERE started_at > ? ORDER BY epoch LIMIT 1)
), w AS (
    SELECT epoch, started_at, lag(epoch) OVER (ORDER BY epoch) AS pe, lag(started_at) OVER (ORDER BY epoch) AS pa,
        row_number() OVER (ORDER BY epoch DESC) AS rd
    FROM e
)
SELECT coalesce(pe, 0), pa, epoch, started_at FROM w
WHERE pe IS NULL OR rd = 1 OR epoch_us(started_at) - epoch_us(pa) > ?
ORDER BY epoch`, a.Add(-reach), a.Add(-reach), b, b, reach.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("collection gaps: %w", err)
	}
	defer rows.Close()
	var pairs []api.EpochPair
	for rows.Next() {
		var e api.EpochPair
		var pa sql.NullTime
		if err := rows.Scan(&e.Prev, &pa, &e.Epoch, &e.At); err != nil {
			return nil, err
		}
		e.PrevAt, e.At = ntime(pa), e.At.UTC()
		pairs = append(pairs, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return api.CollectionGaps(a, b, reach, pairs), nil
}

// collectorGaps reads the times nats-lens was not running
// (collector.gap) and Kubernetes was not watched (collector.k8s_gap, not
// a client pod watch's) that began by b and ended at or after from: each
// from its time to its receipt, when the restarted collector recorded it.
func (p *Provider) collectorGaps(ctx context.Context, from, b time.Time) ([]api.ReplayGap, error) {
	evs, err := p.db().QueryContext(ctx, `SELECT `+eventCols+`, e.received_at FROM events e
WHERE e.kind IN ('collector.gap', 'collector.k8s_gap') AND NOT `+api.ClientPodWatchSQL+` AND e.time <= ? AND e.received_at >= ?
ORDER BY e.time, e.id`, b, from)
	if err != nil {
		return nil, fmt.Errorf("collector gaps: %w", err)
	}
	defer evs.Close()
	var out []api.ReplayGap
	for evs.Next() {
		var end time.Time
		ev, err := scanEvent(evs, &end)
		if err != nil {
			return nil, err
		}
		g := api.ReplayGap{Kind: api.GapCollector, Start: ev.GetTime().AsTime(), End: end.UTC(), Event: ev}
		if ev.GetKind() == "collector.k8s_gap" {
			g.Kind = api.GapKubernetes
		}
		out = append(out, g)
	}
	return out, evs.Err()
}

// eventsConnectionGaps reads the times the connections that capture
// events were lost, up to b: for each such connection still lost at from
// the failure that began it, and every state change of those connections
// and every collector.started from from to b. The collector's own
// connection is left out in the query.
func (p *Provider) eventsConnectionGaps(ctx context.Context, from, b time.Time) ([]api.ReplayGap, error) {
	rows, err := p.db().QueryContext(ctx, `
WITH s AS (
  SELECT e.*, coalesce(json_extract_string(e.raw, '$.connection'), '') AS conn FROM events e
  WHERE e.kind = 'collector.started'
     OR (e.kind IN ('collector.disconnected', 'collector.not_connected', 'collector.connection_closed', 'collector.reconnected')
         AND coalesce(json_extract_string(e.raw, '$.connection'), '') NOT IN ('', 'collector'))
),
open AS (
  SELECT * FROM s
  WHERE s.kind IN ('collector.disconnected', 'collector.not_connected', 'collector.connection_closed') AND s.time < ?
    AND s.time > coalesce((SELECT max(time) FROM s r WHERE (r.kind = 'collector.started' OR (r.kind = 'collector.reconnected' AND r.conn = s.conn))
        AND r.time < ?), TIMESTAMP '1970-01-01')
  QUALIFY row_number() OVER (PARTITION BY s.conn ORDER BY s.time, s.id) = 1
)
SELECT `+eventCols+`, e.conn FROM (SELECT * FROM open UNION ALL SELECT * FROM s WHERE s.time >= ? AND s.time <= ?) e
ORDER BY e.time, e.id`, from, from, from, b)
	if err != nil {
		return nil, fmt.Errorf("events connections: %w", err)
	}
	defer rows.Close()
	var evs []*v1.Event
	for rows.Next() {
		var conn string
		ev, err := scanEvent(rows, &conn)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(map[string]string{"connection": conn})
		ev.Raw = string(raw)
		evs = append(evs, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return api.EventsConnectionGaps(evs), nil
}
