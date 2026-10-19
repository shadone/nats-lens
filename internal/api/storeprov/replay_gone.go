package storeprov

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// replayGone reads the assets servers held at A and none holds at B: a
// stream reported at EpochA and in no row at EpochB, and a durable
// consumer (the rule of consumer-013: a durable name, no inactive
// threshold, not a JS_ consumer of a mirror or source) reported at
// EpochA and in no row at EpochB whose stream is not gone itself; a gone
// stream counts its durables. Each comes with its holders at A, those of
// them without a JSZ reply at B, its last report and its delete event,
// matched as consumer-013 matches it, up to B.
func (p *Provider) replayGone(ctx context.Context, f *api.ReplayFacts, names map[string]string) error {
	if f.EpochA == 0 || f.EpochB == 0 {
		return nil
	}
	f.GoneRead = true
	rows, err := p.db().QueryContext(ctx, `
WITH durable_a AS (
    SELECT DISTINCT x.account, x.stream, x.consumer FROM consumer_stats x
    WHERE x.epoch = $1 AND x.consumer NOT LIKE 'JS\_%' ESCAPE '\'
      AND EXISTS (SELECT 1 FROM consumer_config k WHERE k.account = x.account AND k.stream = x.stream AND k.consumer = x.consumer
          AND k.first_epoch <= $1 AND k.last_epoch >= $1 AND coalesce(k.durable_name, '') <> '' AND coalesce(k.inactive_threshold_ns, 0) = 0)
),
gone_streams AS (
    SELECT DISTINCT account, stream FROM stream_stats WHERE epoch = $1
    EXCEPT SELECT DISTINCT account, stream FROM stream_stats WHERE epoch = $2
),
gone AS (
    SELECT account, stream, NULL::VARCHAR AS consumer,
        (SELECT coalesce(list(d.consumer ORDER BY d.consumer), []) FROM durable_a d WHERE d.account = g.account AND d.stream = g.stream) AS with_it
    FROM gone_streams g
    UNION ALL
    SELECT account, stream, consumer, []::VARCHAR[] FROM (
        SELECT account, stream, consumer FROM durable_a
        EXCEPT SELECT DISTINCT account, stream, consumer FROM consumer_stats WHERE epoch = $2)
    WHERE (account, stream) NOT IN (SELECT account, stream FROM gone_streams)
),
facts AS (
    SELECT g.*,
        (SELECT list(DISTINCT s.server_name ORDER BY s.server_name) FROM stream_stats x JOIN servers s USING (server_id)
         WHERE g.consumer IS NULL AND x.epoch = $1 AND x.account = g.account AND x.stream = g.stream) AS stream_holders,
        (SELECT list(DISTINCT s.server_name ORDER BY s.server_name) FROM consumer_stats x JOIN servers s USING (server_id)
         WHERE x.epoch = $1 AND x.account = g.account AND x.stream = g.stream AND x.consumer = g.consumer) AS consumer_holders,
        coalesce((SELECT max(x.epoch) FROM stream_stats x WHERE g.consumer IS NULL AND x.account = g.account AND x.stream = g.stream
                  AND x.epoch >= $1 AND x.epoch < $2),
                 (SELECT max(x.epoch) FROM consumer_stats x WHERE x.account = g.account AND x.stream = g.stream
                  AND x.consumer = g.consumer AND x.epoch >= $1 AND x.epoch < $2)) AS last_epoch
    FROM gone g
)
SELECT f.account, f.stream, f.consumer, f.with_it, coalesce(f.stream_holders, f.consumer_holders) AS holders,
    list_filter(coalesce(f.stream_holders, f.consumer_holders), n -> NOT list_contains(ok.names, n)) AS not_answering,
    f.last_epoch, x.started_at,
    -- The delete event, as consumer-013's cause: the newest
    -- consumer.deleted of the consumer, else the newest stream.deleted of
    -- its stream, received from 30 seconds before the last report up to B,
    -- the account matched by ID or name or absent.
    (SELECT e.id FROM events e
     WHERE e.stream = f.stream AND ((e.kind = 'consumer.deleted' AND e.consumer = f.consumer) OR e.kind = 'stream.deleted')
       AND (e.account IS NULL OR e.account = f.account OR e.account IN (SELECT account_name FROM accounts WHERE account = f.account))
       -- Subtract from the TIMESTAMP column, never from a bound time.Time:
       -- that binds as TIMESTAMPTZ and compares in the host's time zone.
       AND e.received_at <= $3 AND e.received_at >= x.started_at - INTERVAL 30 SECOND
     ORDER BY (e.kind = 'consumer.deleted') DESC, e.received_at DESC, e.id DESC LIMIT 1) AS delete_id
FROM facts f JOIN epochs x ON x.epoch = f.last_epoch
CROSS JOIN (SELECT coalesce(list(DISTINCT s.server_name), []) AS names FROM epoch_endpoints e JOIN servers s USING (server_id)
    WHERE e.epoch = $2 AND e.endpoint = 'JSZ' AND e.state = 'ok') ok
ORDER BY f.account, f.stream, f.consumer NULLS FIRST`, f.EpochA, f.EpochB, f.B)
	if err != nil {
		return fmt.Errorf("replay gone: %w", err)
	}
	defer rows.Close()
	var deletes []int64
	deleteOf := map[int]int64{}
	for rows.Next() {
		var g api.ReplayGone
		var acc, stream string
		var consumer sql.NullString
		var holders, away, withIt []any
		var del sql.NullInt64
		if err := rows.Scan(&acc, &stream, &consumer, &withIt, &holders, &away, &g.LastEpoch, &g.LastReported, &del); err != nil {
			return err
		}
		if del.Valid {
			deletes = append(deletes, del.Int64)
			deleteOf[len(f.Gone)] = del.Int64
		}
		g.DurablesWithIt = strs(withIt)
		g.Entity, g.ServersAtA, g.NotAnsweringAtB, g.LastReported = assetRef(acc, stream, consumer.String, names), strs(holders),
			strs(away), g.LastReported.UTC()
		f.Gone = append(f.Gone, g)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	evs, err := p.eventsByID(ctx, deletes)
	if err != nil {
		return err
	}
	for i, id := range deleteOf {
		f.Gone[i].Delete = evs[id]
	}
	return nil
}

// eventsByID reads events by ID in one query.
func (p *Provider) eventsByID(ctx context.Context, ids []int64) (map[int64]*v1.Event, error) {
	out := map[int64]*v1.Event{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := p.db().QueryContext(ctx, `SELECT `+eventCols+`, e.id FROM events e WHERE list_contains(?::BIGINT[], e.id)`, ids)
	if err != nil {
		return nil, fmt.Errorf("replay delete events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		ev, err := scanEvent(rows, &id)
		if err != nil {
			return nil, err
		}
		out[id] = ev
	}
	return out, rows.Err()
}
