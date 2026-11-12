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

// beforeAssetCountExec and afterAssetCountBatch, when set, run before each
// fill batch's statement and after the batch; for tests.
var beforeAssetCountExec, afterAssetCountBatch func()

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
// none, in batches of assetCountBatch epochs, each its own statement
// under the write lock, so that an epoch write waits for one batch at
// most. It returns how many epochs it filled and logs them. The epochs a
// release before migration 0041 wrote, or a writer outside WriteSnapshot,
// have none; the collector calls it once the store's thread count and
// memory limit are set.
func (s *Store) FillAssetCounts(ctx context.Context) (int64, error) {
	n, _, err := s.FillAssetCountsUntil(ctx, time.Time{})
	return n, err
}

// FillAssetCountsUntil is FillAssetCounts that starts no batch at or after
// until (none when zero); more reports that epochs without counts remain.
func (s *Store) FillAssetCountsUntil(ctx context.Context, until time.Time) (n int64, more bool, err error) {
	begin := time.Now()
	n, more, err = fillAssetCounts(ctx, s.db, func() func() {
		s.mu.Lock(opAssetCounts)
		return s.mu.Unlock
	}, until)
	if n > 0 {
		s.logger().Info("asset counts filled", "epochs", n, "took", time.Since(begin).Round(time.Millisecond), "more", more)
	}
	return n, more, err
}

// fillAssetCounts fills the missing asset counts through db, which may be
// a transaction. lock, when not nil, is held around each batch. No batch
// starts at or after until, unless it is zero; more reports the epochs
// left without counts then.
func fillAssetCounts(ctx context.Context, db execQuerier, lock func() func(), until time.Time) (int64, bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT epoch FROM epochs
WHERE epoch NOT IN (SELECT epoch FROM epoch_asset_counts) ORDER BY epoch`)
	if err != nil {
		return 0, false, fmt.Errorf("epochs without asset counts: %w", err)
	}
	var missing []int64
	for rows.Next() {
		var e int64
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return 0, false, err
		}
		missing = append(missing, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	var n int64
	for i := 0; i < len(missing); i += assetCountBatch {
		if !until.IsZero() && !time.Now().Before(until) {
			return n, true, nil
		}
		batch := missing[i:min(i+assetCountBatch, len(missing))]
		added, err := fillAssetCountBatch(ctx, db, lock, batch[0], batch[len(batch)-1])
		n += added
		if err != nil {
			return n, true, err
		}
		if afterAssetCountBatch != nil {
			afterAssetCountBatch()
		}
	}
	return n, false, nil
}

// fillAssetCountBatch fills the epochs from lo to hi that have no counts.
func fillAssetCountBatch(ctx context.Context, db execQuerier, lock func() func(), lo, hi int64) (int64, error) {
	if lock != nil {
		defer lock()()
	}
	if beforeAssetCountExec != nil {
		beforeAssetCountExec()
	}
	res, err := db.ExecContext(ctx, assetCountsSQL, lo, hi)
	if err != nil {
		return 0, fmt.Errorf("asset counts of epochs %d to %d: %w", lo, hi, err)
	}
	added, _ := res.RowsAffected()
	return added, nil
}
