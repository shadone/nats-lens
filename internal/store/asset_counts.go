package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// assetCountsSQL writes the epoch_asset_counts row of every epoch from the
// first to the second parameter (epoch numbers) that has none. The rules
// are those the series cluster.streams, cluster.kv_buckets,
// cluster.object_stores, cluster.durable_consumers and
// cluster.assets_replicated and _single applied to the stored rows before
// the table existed:
//
//   - a stream is any (account, stream) a server reported in stream_stats;
//     KV_ and OBJ_ prefixes make it a KV bucket or an object store;
//   - a durable consumer is any (account, stream, consumer) in
//     consumer_stats whose server's consumer_config row covering the epoch
//     has a durable name;
//   - an asset's replicas are the largest its reporting servers'
//     configurations give; a consumer's 0 means its stream's, and an
//     unknown count is neither replicated nor single.
//
// Stream and consumer reports are filtered on the epoch range directly,
// and the configuration rows on their overlap with it, so that writing one
// epoch reads that epoch's rows only.
const assetCountsSQL = `
INSERT INTO epoch_asset_counts (epoch, streams, kv_buckets, object_stores, durable_consumers, assets_replicated, assets_single)
WITH win AS (
  SELECT epoch FROM epochs
  WHERE epoch BETWEEN $1 AND $2 AND epoch NOT IN (SELECT epoch FROM epoch_asset_counts WHERE epoch BETWEEN $1 AND $2)),
st AS (
  SELECT ss.epoch, ss.account, ss.stream, max(sc.replicas) AS r
  FROM stream_stats ss JOIN win USING (epoch)
  LEFT JOIN (SELECT * FROM stream_config WHERE first_epoch <= $2 AND last_epoch >= $1) sc
    ON sc.server_id = ss.server_id AND sc.account = ss.account AND sc.stream = ss.stream
    AND ss.epoch BETWEEN sc.first_epoch AND sc.last_epoch
  WHERE ss.epoch BETWEEN $1 AND $2
  GROUP BY ss.epoch, ss.account, ss.stream),
co AS (
  SELECT cs.epoch, cs.account, cs.stream, cs.consumer, max(cc.replicas) AS r
  FROM consumer_stats cs JOIN win USING (epoch)
  JOIN (SELECT * FROM consumer_config WHERE first_epoch <= $2 AND last_epoch >= $1) cc
    ON cc.server_id = cs.server_id AND cc.account = cs.account AND cc.stream = cs.stream
    AND cc.consumer = cs.consumer AND cs.epoch BETWEEN cc.first_epoch AND cc.last_epoch
  WHERE cs.epoch BETWEEN $1 AND $2 AND coalesce(cc.durable_name, '') <> ''
  GROUP BY cs.epoch, cs.account, cs.stream, cs.consumer),
s AS (
  SELECT epoch,
    count(*) FILTER (WHERE NOT starts_with(stream, 'KV_') AND NOT starts_with(stream, 'OBJ_')) AS streams,
    count(*) FILTER (WHERE starts_with(stream, 'KV_')) AS kv_buckets,
    count(*) FILTER (WHERE starts_with(stream, 'OBJ_')) AS object_stores,
    count(*) FILTER (WHERE r > 1) AS replicated,
    count(*) FILTER (WHERE r = 1) AS single
  FROM st GROUP BY epoch),
c AS (
  SELECT epoch, count(*) AS durable, count(*) FILTER (WHERE r > 1) AS replicated, count(*) FILTER (WHERE r = 1) AS single
  FROM (SELECT co.epoch, CASE WHEN co.r = 0 THEN st.r ELSE co.r END AS r FROM co LEFT JOIN st USING (epoch, account, stream))
  GROUP BY epoch)
SELECT w.epoch, coalesce(s.streams, 0), coalesce(s.kv_buckets, 0), coalesce(s.object_stores, 0), coalesce(c.durable, 0),
  coalesce(s.replicated, 0) + coalesce(c.replicated, 0), coalesce(s.single, 0) + coalesce(c.single, 0)
FROM win w LEFT JOIN s USING (epoch) LEFT JOIN c USING (epoch)`

// assetCountBatch is how many epochs one fill statement covers: an hour
// at the default 30 s interval, so that the aggregation's memory stays
// that of an hour of reports however large the store is.
const assetCountBatch = 120

type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// writeAssetCounts writes the epoch's asset counts; the epoch's reports
// and configuration rows must be written before.
func (w *writer) writeAssetCounts() {
	w.exec(assetCountsSQL, w.epoch, w.epoch)
}

// FillAssetCounts writes the asset counts of every stored epoch that has
// none, in batches of assetCountBatch epochs, each its own statement, and
// returns how many epochs it filled. Open calls it; the epochs a release
// before migration 0041 wrote, or a writer outside WriteSnapshot, have
// none.
func (s *Store) FillAssetCounts(ctx context.Context) (int64, error) {
	s.mu.Lock(opAssetCounts)
	defer s.mu.Unlock()
	return fillAssetCounts(ctx, s.db)
}

// fillAssetCounts fills the missing asset counts through db, which may be
// a transaction.
func fillAssetCounts(ctx context.Context, db execQuerier) (int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT epoch FROM epochs
WHERE epoch NOT IN (SELECT epoch FROM epoch_asset_counts) ORDER BY epoch`)
	if err != nil {
		return 0, fmt.Errorf("epochs without asset counts: %w", err)
	}
	var missing []int64
	for rows.Next() {
		var e int64
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return 0, err
		}
		missing = append(missing, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var n int64
	for i := 0; i < len(missing); i += assetCountBatch {
		batch := missing[i:min(i+assetCountBatch, len(missing))]
		res, err := db.ExecContext(ctx, assetCountsSQL, batch[0], batch[len(batch)-1])
		if err != nil {
			return n, fmt.Errorf("asset counts of epochs %d to %d: %w", batch[0], batch[len(batch)-1], err)
		}
		added, _ := res.RowsAffected()
		n += added
	}
	return n, nil
}

// fillAssetCountsLogged fills the missing asset counts and logs how many
// and how long when there were any.
func (s *Store) fillAssetCountsLogged(ctx context.Context) error {
	begin := time.Now()
	n, err := s.FillAssetCounts(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		s.logger().Info("asset counts filled", "epochs", n, "took", time.Since(begin).Round(time.Millisecond))
	}
	return nil
}
