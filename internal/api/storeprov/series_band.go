package storeprov

import (
	"context"
	"fmt"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// bandMetrics maps a band metric to the server_stats column it spreads
// across servers. The busiest server's own series is server.<column>.
var bandMetrics = map[string]string{
	"cluster.cpu_percent": "cpu_percent",
	"cluster.mem_bytes":   "mem_bytes",
}

// bandSeries reads a band metric: per epoch the median, lowest and
// highest server value. A bucket's value is the mean of its medians and
// its edges the lowest and highest value of any epoch in it, so a spike
// stays visible at every step. The series' min and max are the edges'.
// The server with the highest mean over the range and its own averaged
// series come with it; that line lies inside the band. A server that did
// not report the column is left out; an epoch where none did has no
// point.
func (p *Provider) bandSeries(ctx context.Context, metric, col string, e *v1.EntityRef, r *v1.TimeRange, maxPoints int32) (*v1.Series, error) {
	step := api.Step(r, maxPoints, max(p.Interval, time.Second))
	start, end := span(r)
	q := `WITH per_epoch AS (` + bandObservationSQL(col, true) + `)
SELECT time_bucket(to_microseconds(?::BIGINT), t, TIMESTAMP '1970-01-01 00:00:00') AS b,
  avg(v)::DOUBLE, min(lo)::DOUBLE, max(hi)::DOUBLE
FROM per_epoch GROUP BY b ORDER BY b`
	rows, err := p.db().QueryContext(ctx, q, start, end, step.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("series %s: %w", metric, err)
	}
	defer rows.Close()
	out := &v1.Series{Metric: metric, Entity: e, Unit: api.Metrics[metric].Unit, Step: dur(step)}
	for rows.Next() {
		var b time.Time
		var v, lo, hi float64
		if err := rows.Scan(&b, &v, &lo, &hi); err != nil {
			return nil, err
		}
		out.Points = append(out.Points, &v1.Point{Time: ts(b.UTC()), Value: v, Low: ptr(lo), High: ptr(hi)})
		if out.Min == nil || lo < out.GetMin() {
			out.Min = ptr(lo)
		}
		if out.Max == nil || hi > out.GetMax() {
			out.Max = ptr(hi)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, p.addBusiest(ctx, out, col, r, maxPoints)
}

// addBusiest sets the series' top server, the one with the highest mean
// of the column over the range, and its values in the same buckets.
func (p *Provider) addBusiest(ctx context.Context, out *v1.Series, col string, r *v1.TimeRange, maxPoints int32) error {
	start, end := span(r)
	var name string
	err := p.db().QueryRowContext(ctx, fmt.Sprintf(`
SELECT s.server_name FROM server_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
WHERE e.started_at >= ? AND e.started_at < ? AND ss.%[1]s IS NOT NULL
GROUP BY s.server_name ORDER BY avg(ss.%[1]s) DESC, s.server_name LIMIT 1`, col), start, end).Scan(&name)
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("busiest server: %w", err)
	}
	top := refOf(v1.EntityType_ENTITY_TYPE_SERVER, []any{name})
	s, err := p.series(ctx, "server."+col, top, r, maxPoints)
	if err != nil {
		return err
	}
	out.Top, out.TopPoints = top, s.GetPoints()
	return nil
}

// bandObservationSQL is the shared pre-bucket metric definition. NULL-only
// collections remain available to observation metadata; charts omit them.
func bandObservationSQL(col string, knownOnly bool) string {
	filter := ""
	if knownOnly {
		filter = fmt.Sprintf(" AND ss.%s IS NOT NULL", col)
	}
	return fmt.Sprintf(`SELECT e.started_at AS t, quantile_cont(ss.%[1]s,0.5) AS v, min(ss.%[1]s) AS lo, max(ss.%[1]s) AS hi
 FROM server_stats ss JOIN epochs e USING(epoch)
 WHERE e.started_at >= ? AND e.started_at < ? %[2]s GROUP BY e.epoch,e.started_at`, col, filter)
}
