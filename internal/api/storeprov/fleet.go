package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// SystemSummary reads the system's summary from data already collected:
// the newest epoch of the range for the counts, the findings as the
// overview reads them, the strip from the status timeline, and the
// collection state from the collector's stored connection events. The
// handler sets the system reference, and replaces the collection fields
// with the runtime's status when there is one.
func (p *Provider) SystemSummary(ctx context.Context, req *v1.ListSystemsRequest) (*v1.SystemSummary, error) {
	r := req.GetRange()
	res := &v1.SystemSummary{Status: v1.Status_STATUS_NO_DATA}
	var err error
	if res.Freshness, err = p.freshness(ctx, r); err != nil {
		return nil, err
	}
	e, ok, err := p.latestEpoch(ctx, r)
	if err != nil {
		return nil, err
	}
	cur, err := p.firingFindings(ctx, r)
	if err != nil {
		return nil, err
	}
	for _, f := range cur {
		if f.GetMute() == nil {
			res.WorstFinding = f
			break
		}
	}
	if res.Tally, err = p.tally(ctx, r, cur); err != nil {
		return nil, err
	}
	if ok {
		res.Status, _ = statusOf(cur, nil)
		if err := p.epochCounts(ctx, e.epoch, res); err != nil {
			return nil, err
		}
	}
	if err := p.strip(ctx, r, req.GetStripBuckets(), res); err != nil {
		return nil, err
	}
	if err := p.collection(ctx, res); err != nil {
		return nil, err
	}
	return res, nil
}

// epochCounts sets the server, cluster, stream and connection counts of
// one epoch. Connections stay unset when no server reported them.
func (p *Provider) epochCounts(ctx context.Context, epoch int64, res *v1.SystemSummary) error {
	var expected, observed, clusters, streams int32
	var conns sql.NullInt64
	err := p.db().QueryRowContext(ctx, `
SELECT e.expected_servers, e.observed_servers,
  (SELECT count(DISTINCT s.cluster) FROM servers s
     WHERE s.first_epoch <= e.epoch AND s.last_epoch >= e.epoch AND coalesce(s.cluster, '') <> ''),
  (SELECT count(DISTINCT (st.account, st.stream)) FROM stream_stats st WHERE st.epoch = e.epoch),
  (SELECT sum(ss.connections) FROM server_stats ss WHERE ss.epoch = e.epoch)
FROM epochs e WHERE e.epoch = ?`, epoch).Scan(&expected, &observed, &clusters, &streams, &conns)
	if err != nil {
		return fmt.Errorf("system counts: %w", err)
	}
	res.Servers, res.ServersAnswering, res.Clusters, res.Streams = &expected, &observed, &clusters, &streams
	if conns.Valid {
		res.Connections = &conns.Int64
	}
	return nil
}

// strip sets the worst status per bucket over the range: the worst cell of
// the timeline's rows, one per check, in each bucket; no data where no
// check ran ok.
func (p *Provider) strip(ctx context.Context, r *v1.TimeRange, buckets int32, res *v1.SystemSummary) error {
	bucket := api.StripBucket(r, buckets)
	rows, err := p.timelineRows(ctx, r, bucket, nil, true)
	if err != nil {
		return err
	}
	start, end := span(r)
	res.Strip = make([]v1.Status, api.BucketCount(end.Sub(start), bucket))
	for i := range res.Strip {
		res.Strip[i] = v1.Status_STATUS_NO_DATA
		for _, row := range rows {
			if i < len(row.GetCells()) {
				res.Strip[i] = worse(res.Strip[i], row.GetCells()[i])
			}
		}
	}
	res.StripStart, res.StripBucket = ts(start), dur(bucket)
	return nil
}

// collectorFailures are the stored event kinds that say the collector's
// connection does not collect.
var collectorFailures = map[string]v1.CollectionState{
	"collector.not_connected":     v1.CollectionState_COLLECTION_STATE_NOT_CONNECTED,
	"collector.connection_closed": v1.CollectionState_COLLECTION_STATE_CONNECTION_CLOSED,
}

// collection sets the collection state from the stored events of the
// collector's own connection, which outlive a restart of nats-lens: the
// newest failure after both the newest epoch and the newest reconnect
// gives the state and the error, and the oldest such failure the time it
// began. Without such a failure a live collector is collecting once an
// epoch is stored and starting before; without a collector in this
// process the state is unknown.
func (p *Provider) collection(ctx context.Context, res *v1.SystemSummary) error {
	rows, err := p.db().QueryContext(ctx, `
WITH since AS (
  SELECT greatest(
    (SELECT max(started_at) FROM epochs),
    (SELECT max(time) FROM events WHERE kind = 'collector.reconnected'
       AND json_extract_string(raw, '$.connection') = 'collector')) AS t)
SELECT e.kind, coalesce(e.reason, ''), e.time FROM events e, since
WHERE e.kind IN ('collector.not_connected', 'collector.connection_closed')
  AND json_extract_string(e.raw, '$.connection') = 'collector'
  AND (since.t IS NULL OR e.time > since.t)
ORDER BY e.time, e.id`)
	if err != nil {
		return fmt.Errorf("collection state: %w", err)
	}
	defer rows.Close()
	var first time.Time
	for rows.Next() {
		var kind, reason string
		var at time.Time
		if err := rows.Scan(&kind, &reason, &at); err != nil {
			return err
		}
		if first.IsZero() {
			first = at.UTC()
		}
		res.Collection, res.CollectionError = collectorFailures[kind], reason
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !first.IsZero() {
		res.CollectionErrorSince = ts(first)
		return nil
	}
	if !p.Live {
		return nil
	}
	var newest sql.NullInt64
	if err := p.db().QueryRowContext(ctx, `SELECT max(epoch) FROM epochs`).Scan(&newest); err != nil {
		return err
	}
	res.Collection = v1.CollectionState_COLLECTION_STATE_STARTING
	if newest.Valid {
		res.Collection = v1.CollectionState_COLLECTION_STATE_COLLECTING
	}
	return nil
}
