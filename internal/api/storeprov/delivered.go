package storeprov

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// deliveredSpan is how far before the range's end a consumer's delivery
// rate looks.
const deliveredSpan = 15 * time.Minute

// deliveredRates reads each consumer's delivery rate over the deliveredSpan
// before the range's end: the increase of the leader's delivered consumer
// sequence per second, between the first and the last leader row of the
// newest run. As in consumer.delivered_rate, a run is one leading server
// and one creation time, so a leader move or a recreated consumer starts
// over. A consumer with fewer than two leader rows in its newest run has
// no rate.
func (p *Provider) deliveredRates(ctx context.Context, r *v1.TimeRange, account, stream, consumer string) (map[string]float64, error) {
	_, end := span(r)
	rows, err := p.db().QueryContext(ctx, `
WITH lead AS (
  SELECT x.account, x.stream, x.consumer, x.server_id, x.created, e.started_at AS t, x.delivered_consumer_seq AS v
  FROM consumer_stats x JOIN epochs e USING (epoch)
  WHERE x.is_leader AND x.delivered_consumer_seq IS NOT NULL AND e.started_at >= ? AND e.started_at < ?
    AND (? = '' OR x.account = ?) AND (? = '' OR x.stream = ?) AND (? = '' OR x.consumer = ?)
  QUALIFY row_number() OVER (PARTITION BY x.account, x.stream, x.consumer, e.epoch
    ORDER BY x.delivered_consumer_seq DESC, x.server_id) = 1),
newest AS (
  SELECT account, stream, consumer, arg_max(server_id, t) AS server_id, arg_max(created, t) AS created
  FROM lead GROUP BY ALL)
SELECT l.account, l.stream, l.consumer,
  (arg_max(l.v, l.t) - arg_min(l.v, l.t)) * 1000.0 / date_diff('millisecond', min(l.t), max(l.t))
FROM lead l JOIN newest n ON n.account = l.account AND n.stream = l.stream AND n.consumer = l.consumer
  AND n.server_id = l.server_id AND n.created IS NOT DISTINCT FROM l.created
GROUP BY ALL HAVING count(*) >= 2 AND max(l.t) > min(l.t)`,
		end.Add(-deliveredSpan), end, account, account, stream, stream, consumer, consumer)
	if err != nil {
		return nil, fmt.Errorf("delivered rates: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var acc, st, name string
		var v float64
		if err := rows.Scan(&acc, &st, &name, &v); err != nil {
			return nil, err
		}
		if v >= 0 {
			out[acc+"/"+st+"/"+name] = v
		}
	}
	return out, rows.Err()
}

// sortConsumers orders a consumer list as the request asks and applies
// its limit. DELIVERED_RATE puts the highest rate first and consumers
// without one last, each group by key; the default keeps the order given.
func sortConsumers(cs []*v1.ConsumerSummary, sort v1.ConsumerSort, limit int32) []*v1.ConsumerSummary {
	if sort == v1.ConsumerSort_CONSUMER_SORT_DELIVERED_RATE {
		slices.SortStableFunc(cs, func(a, b *v1.ConsumerSummary) int {
			if (a.DeliveredPerSecond == nil) != (b.DeliveredPerSecond == nil) {
				if a.DeliveredPerSecond == nil {
					return 1
				}
				return -1
			}
			return cmp.Or(cmp.Compare(b.GetDeliveredPerSecond(), a.GetDeliveredPerSecond()),
				cmp.Compare(a.GetEntity().GetKey(), b.GetEntity().GetKey()))
		})
	}
	if limit > 0 && int(limit) < len(cs) {
		cs = cs[:limit]
	}
	return cs
}
