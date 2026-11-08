package storeprov

import (
	"context"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// deliveredSpan is how far before the range's end a consumer's delivery
// rate looks.
const deliveredSpan = 15 * time.Minute

// deliveredRates reads the consumer delivery-sequence advance per second in
// the newest comparable run within deliveredSpan before the range's end.
// Adjacent rows need one observed leader, a known coherent creation time,
// nondecreasing sequence and positive elapsed time within three intervals.
// Identity changes, unknown evidence and resets start a new run, including an
// A->B->A return; a run with fewer than two known rows has no rate.
func (p *Provider) deliveredRates(ctx context.Context, r *v1.TimeRange, account, stream, consumer string) (map[string]float64, error) {
	return p.deliveredRatesLimited(ctx, r, account, stream, consumer, api.MaxListRankingRows+1)
}

// deliveredRatesLimited bounds both SQL output and the application map. The
// ordinary helper permits one overflow sentinel for inspection; ranking callers
// use the strict context budget and reject before inserting the overflow row.
// SQL aggregation itself may still examine all matching retained observations.
func (p *Provider) deliveredRatesLimited(ctx context.Context, r *v1.TimeRange, account, stream, consumer string, budget int) (map[string]float64, error) {
	_, end := span(r)
	// ASVS 1.2.4: scope and the overflow read limit are bound parameters.
	where := `AND (?='' OR x.account=?) AND (?='' OR x.stream=?) AND (?='' OR x.consumer=?)`
	query := `/* delivered_rate_summary */ WITH ` + resourceSequenceCTEs(true, where) + `,
 sequence_runs AS (
  SELECT *,sum(CASE WHEN comparable THEN 0 ELSE 1 END) OVER(PARTITION BY account,stream,consumer ORDER BY epoch ROWS UNBOUNDED PRECEDING) AS run_id
  FROM sequence_steps),
 selected AS(SELECT * FROM sequence_runs WHERE epoch IN(SELECT epoch FROM sequence_epochs)),
 newest AS(SELECT account,stream,consumer,max(run_id) AS run_id FROM selected GROUP BY ALL)
 SELECT s.account,s.stream,s.consumer,
 (arg_max(s.seq,s.epoch)::HUGEINT-arg_min(s.seq,s.epoch)::HUGEINT)*1000.0/date_diff('millisecond',min(s.t),max(s.t))
 FROM selected s JOIN newest n USING(account,stream,consumer,run_id)
 WHERE s.created IS NOT NULL AND s.seq>=0
 GROUP BY s.account,s.stream,s.consumer HAVING count(*)>=2 AND max(s.t)>min(s.t)
 ORDER BY s.account,s.stream,s.consumer LIMIT ?`
	rows, err := p.db().QueryContext(ctx, p.metricQuery(metricSQL{query: query}), end.Add(-deliveredSpan), end, account, account, stream, stream, consumer, consumer, budget+1)
	if err != nil {
		return nil, fmt.Errorf("delivered rates: %w", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	observed := 0
	for rows.Next() {
		observed++
		if observed > budget {
			return nil, fmt.Errorf("%w: delivered rate ranking exceeds %d rows; narrow the account or stream filter", api.ErrResourceExhausted, budget)
		}
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
