package storeprov

import (
	"context"
	"fmt"
	"time"
)

// onCallRates requires a leader counter in the selected collection. Historical
// samples only measure that entity's latest contiguous leader/creation/counter
// run; neither a newer system epoch nor another entity freshens an old report.
// The two calls each read all entities in one batch, without depending on VARZ.
func (p *Provider) onCallRates(ctx context.Context, e epochInfo, consumers bool) (map[string]float64, error) {
	table, counter, entity := "stream_stats", "last_seq", "account || '/' || stream"
	if consumers {
		table, counter, entity = "consumer_stats", "delivered_consumer_seq", "account || '/' || stream || '/' || consumer"
	}
	// ASVS 1.2.4: identifiers are fixed above, never request input. Times compare directly
	// with TIMESTAMP columns to preserve their meaning outside UTC.
	q := fmt.Sprintf(`WITH lead AS (
 SELECT %s AS key, x.server_id, x.created, x.epoch, e.started_at AS t, x.%s AS v
 FROM %s x JOIN epochs e USING (epoch)
 WHERE x.is_leader AND e.started_at >= ? AND e.started_at <= ?
 QUALIFY row_number() OVER (PARTITION BY key, x.epoch ORDER BY x.%s DESC NULLS LAST, x.server_id) = 1),
 previous AS (
 SELECT *, row_number() OVER w AS n, lag(server_id) OVER w AS prev_server,
 lag(created) OVER w AS prev_created, lag(v) OVER w AS prev_v
 FROM lead WINDOW w AS (PARTITION BY key ORDER BY t, epoch)),
 runs AS (
 SELECT *, sum(CASE WHEN n = 1 OR server_id IS DISTINCT FROM prev_server
   OR created IS DISTINCT FROM prev_created OR v < prev_v OR prev_v IS NULL OR v IS NULL
   THEN 1 ELSE 0 END) OVER (PARTITION BY key ORDER BY t, epoch) AS run
 FROM previous),
 current AS (SELECT key, run FROM runs WHERE epoch = ? AND v IS NOT NULL)
 SELECT r.key, (arg_max(r.v, r.t) - arg_min(r.v, r.t)) * 1000.0 /
 date_diff('millisecond', min(r.t), max(r.t))
 FROM runs r JOIN current c ON r.key = c.key AND r.run = c.run
 GROUP BY r.key HAVING count(*) >= 2 AND max(r.t) > min(r.t)`, entity, counter, table, counter)
	rows, err := p.db().QueryContext(ctx, q, e.startedAt.Add(-15*time.Minute), e.startedAt, e.epoch)
	if err != nil {
		return nil, fmt.Errorf("on-call rates: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var key string
		var rate float64
		if err := rows.Scan(&key, &rate); err != nil {
			return nil, err
		}
		if rate >= 0 {
			out[key] = rate
		}
	}
	return out, rows.Err()
}
