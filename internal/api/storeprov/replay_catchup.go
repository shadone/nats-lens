package storeprov

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shadone/nats-lens/internal/api"
)

// replayCatchUps reads the runs of a replica behind its leader in the
// collections from A's (from A when there is none) to B's, by
// Placement's rule (placementOf): the leader's report of the replica not
// offline and not current, or with a lag and current unknown, while the
// replica's server reports the asset itself. A report that is neither
// behind nor current, and a collection without one, are skipped; a
// current report ends the run. Each run comes with the first collection
// after it and the first that reported the replica current, both up to
// B. Only the runs FinishReplay may keep are read (api.ReplayCatchUp):
// two collections or more with a peak lag of at least api.CatchUpMinLag
// (or none), or a run that starts within ReplayEpochReach intervals of a
// restarted process's first collection; at most replayRowLimit of them,
// the earliest first. Only the replicas some report shows not current
// are followed, so that a day of collections reads few rows.
func (p *Provider) replayCatchUps(ctx context.Context, f *api.ReplayFacts, names map[string]string) error {
	from := f.A
	if f.EpochA != 0 {
		from = f.EpochAAt
	}
	rows, err := p.db().QueryContext(ctx, `
WITH ep AS (SELECT epoch, started_at FROM epochs WHERE started_at >= $1 AND started_at <= $2),
cand_s AS (
    SELECT DISTINCT r.account, r.stream, r.peer_name FROM stream_replicas r
    WHERE r.epoch IN (SELECT epoch FROM ep) AND NOT coalesce(r.offline, false) AND (NOT r.current OR (r.current IS NULL AND r.lag > 0))
),
cand_c AS (
    SELECT DISTINCT r.account, r.stream, r.consumer, r.peer_name FROM consumer_replicas r
    WHERE r.epoch IN (SELECT epoch FROM ep) AND NOT coalesce(r.offline, false) AND (NOT r.current OR (r.current IS NULL AND r.lag > 0))
),
srep AS (
    SELECT r.* FROM stream_replicas r JOIN cand_s c ON c.account = r.account AND c.stream = r.stream AND c.peer_name = r.peer_name
    WHERE r.epoch IN (SELECT epoch FROM ep)
),
crep AS (
    SELECT r.* FROM consumer_replicas r
    JOIN cand_c c ON c.account = r.account AND c.stream = r.stream AND c.consumer = r.consumer AND c.peer_name = r.peer_name
    WHERE r.epoch IN (SELECT epoch FROM ep)
),
sheld AS (
    SELECT DISTINCT x.epoch, s.server_name, x.account, x.stream FROM stream_stats x JOIN servers s USING (server_id)
    WHERE x.epoch IN (SELECT epoch FROM ep) AND (x.account, x.stream) IN (SELECT account, stream FROM cand_s)
),
cheld AS (
    SELECT DISTINCT x.epoch, s.server_name, x.account, x.stream, x.consumer FROM consumer_stats x JOIN servers s USING (server_id)
    WHERE x.epoch IN (SELECT epoch FROM ep) AND (x.account, x.stream, x.consumer) IN (SELECT account, stream, consumer FROM cand_c)
),
obs AS (
    SELECT r.epoch, r.peer_name AS server, r.account, r.stream, '' AS consumer,
        bool_or(NOT coalesce(r.offline, false) AND (NOT r.current OR (r.current IS NULL AND r.lag > 0))) AS behind,
        bool_or(NOT coalesce(r.offline, false) AND r.current) AS cur, max(r.lag) AS lag
    FROM srep r
    JOIN stream_stats l ON l.epoch = r.epoch AND l.server_id = r.server_id AND l.account = r.account AND l.stream = r.stream AND l.is_leader
    JOIN sheld h ON h.epoch = r.epoch AND h.server_name = r.peer_name AND h.account = r.account AND h.stream = r.stream
    GROUP BY ALL
    UNION ALL
    SELECT r.epoch, r.peer_name, r.account, r.stream, r.consumer,
        bool_or(NOT coalesce(r.offline, false) AND (NOT r.current OR (r.current IS NULL AND r.lag > 0))),
        bool_or(NOT coalesce(r.offline, false) AND r.current), max(r.lag)
    FROM crep r
    JOIN consumer_stats l ON l.epoch = r.epoch AND l.server_id = r.server_id AND l.account = r.account AND l.stream = r.stream
        AND l.consumer = r.consumer AND l.is_leader
    JOIN cheld h ON h.epoch = r.epoch AND h.server_name = r.peer_name AND h.account = r.account AND h.stream = r.stream
        AND h.consumer = r.consumer
    GROUP BY ALL
),
st AS (
    SELECT epoch, server, account, stream, consumer, lag,
        CASE WHEN coalesce(behind, false) THEN 'behind' WHEN coalesce(cur, false) THEN 'current' END AS state
    FROM obs
),
seq AS (
    SELECT st.*, sum(CASE WHEN state = 'current' THEN 1 ELSE 0 END) OVER (PARTITION BY server, account, stream, consumer ORDER BY epoch) AS grp
    FROM st
    WHERE state IS NOT NULL
),
runs AS (
    SELECT server, account, stream, consumer, grp, min(epoch) AS first_epoch, max(epoch) AS last_epoch, count(*) AS n,
        max(lag) AS peak, list(lag ORDER BY epoch DESC)[1] AS last_lag
    FROM seq WHERE state = 'behind'
    GROUP BY ALL
),
restarted AS (
    SELECT s.server_name, s.first_seen_at FROM servers s
    WHERE EXISTS (SELECT 1 FROM servers o WHERE o.server_name = s.server_name AND o.first_epoch < s.first_epoch)
)
SELECT r.server, r.account, r.stream, r.consumer, r.first_epoch, fe.started_at, r.last_epoch, le.started_at, r.n, r.peak, r.last_lag,
    nx.epoch, nx.started_at, c.epoch, ce.started_at
FROM runs r
JOIN ep fe ON fe.epoch = r.first_epoch
JOIN ep le ON le.epoch = r.last_epoch
LEFT JOIN ep nx ON nx.epoch = (SELECT min(epoch) FROM ep WHERE epoch > r.last_epoch)
LEFT JOIN seq c ON c.state = 'current' AND c.grp = r.grp + 1 AND c.server = r.server AND c.account = r.account AND c.stream = r.stream
    AND c.consumer = r.consumer
LEFT JOIN ep ce ON ce.epoch = c.epoch
WHERE (r.n >= 2 AND (r.peak IS NULL OR r.peak >= $4))
   OR EXISTS (SELECT 1 FROM restarted x WHERE x.server_name = r.server AND fe.started_at >= x.first_seen_at
       AND fe.started_at <= x.first_seen_at + to_microseconds($5::BIGINT))
ORDER BY fe.started_at, r.server, r.account, r.stream, r.consumer
LIMIT $3`, from, f.B, replayRowLimit+1, api.CatchUpMinLag, (api.ReplayEpochReach * p.Interval).Microseconds())
	if err != nil {
		return fmt.Errorf("replay catch-ups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c api.ReplayCatchUp
		var acc, stream, consumer string
		var peak, last, next, cur sql.NullInt64
		var nextAt, curAt sql.NullTime
		if err := rows.Scan(&c.Server, &acc, &stream, &consumer, &c.FromEpoch, &c.From, &c.LastEpoch, &c.Last, &c.Behind, &peak, &last,
			&next, &nextAt, &cur, &curAt); err != nil {
			return err
		}
		if len(f.CatchUps) == replayRowLimit {
			f.CatchUpsTruncated = true
			break
		}
		c.Entity = assetRef(acc, stream, consumer, names)
		c.From, c.Last = c.From.UTC(), c.Last.UTC()
		c.PeakLag, c.LastLag = nint64(peak), nint64(last)
		c.NextEpoch, c.Next, c.CurrentEpoch, c.Current = next.Int64, ntime(nextAt), cur.Int64, ntime(curAt)
		f.CatchUps = append(f.CatchUps, c)
	}
	return rows.Err()
}

// nint64 is a nullable integer as a pointer; nil for NULL.
func nint64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
