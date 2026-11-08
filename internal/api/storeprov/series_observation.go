package storeprov

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

var observationInteger = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func (p *Provider) seriesLookback(metric string, start time.Time) time.Time {
	if metric == "cluster.slow_consumers" || metric == "server.slow_consumers" {
		return p.slowCounterLookback(start)
	}
	return start.Add(-2 * max(p.Interval, time.Minute))
}

// observationSummary reuses the authoritative per-collection metric SQL. It
// materializes one aggregate row, not an unbounded history in application maps.
// Existing request/batch deadlines include this optional second metric read.
func (p *Provider) observationSummary(ctx context.Context, metric string, e *v1.EntityRef, r *v1.TimeRange) (*v1.ObservationSummary, error) {
	start, end := span(r)
	if end.Sub(start) > api.MaxObservationRange {
		return nil, fmt.Errorf("%w: observation range exceeds 30 days", api.ErrInvalid)
	}
	m, ok := metricQueries[metric]
	edges := "NULL::DOUBLE, NULL::DOUBLE"
	if col, band := bandMetrics[metric]; band {
		// col is selected only from the compile-time band catalogue, never input SQL.
		m = metricSQL{query: fmt.Sprintf(`SELECT e.started_at AS t, quantile_cont(ss.%[1]s,0.5) AS v, min(ss.%[1]s) AS lo, max(ss.%[1]s) AS hi FROM server_stats ss JOIN epochs e USING(epoch) WHERE e.started_at >= ? AND e.started_at < ? GROUP BY e.epoch,e.started_at`, col)}
		ok = true
		edges = "min(lo)::DOUBLE, max(hi)::DOUBLE"
	}
	if !ok {
		return nil, fmt.Errorf("%w: unknown observation metric", api.ErrInvalid)
	}
	total := "NULL::VARCHAR"
	if m.sum {
		total = "sum(v)::VARCHAR"
	}
	q := `WITH per_epoch AS (` + p.metricQuery(m) + `), selected AS (SELECT * FROM per_epoch WHERE t >= ? AND t < ?)
SELECT count(*),count(v),min(t),max(t),first(v ORDER BY t DESC)::DOUBLE,avg(v)::DOUBLE,min(v)::DOUBLE,max(v)::DOUBLE,
first(t ORDER BY v DESC NULLS LAST,t) FILTER (WHERE v IS NOT NULL),` + total + `,` + edges + ` FROM selected`
	args := append([]any{p.seriesLookback(metric, start), end}, entityArgs(e)...)
	args = append(args, start, end)
	out := &v1.ObservationSummary{}
	var first, last, peak sql.NullTime
	var latest, mean, low, high, minimumLow, maximumHigh sql.NullFloat64
	var sum sql.NullString
	if err := p.db().QueryRowContext(ctx, q, args...).Scan(&out.SampleCount, &out.KnownCount, &first, &last, &latest, &mean, &low, &high, &peak, &sum, &minimumLow, &maximumHigh); err != nil {
		return nil, fmt.Errorf("observations %s: %w", metric, err)
	}
	if first.Valid {
		out.FirstSampleAt = ts(first.Time.UTC())
	}
	if last.Valid {
		out.LastSampleAt = ts(last.Time.UTC())
	}
	if peak.Valid {
		out.MaximumAt = ts(peak.Time.UTC())
	}
	if latest.Valid {
		out.Latest = ptr(latest.Float64)
	}
	if mean.Valid {
		out.Mean = ptr(mean.Float64)
	}
	if low.Valid {
		out.Minimum = ptr(low.Float64)
	}
	if high.Valid {
		out.Maximum = ptr(high.Float64)
	}
	if minimumLow.Valid {
		out.MinimumLow = ptr(minimumLow.Float64)
	}
	if maximumHigh.Valid {
		out.MaximumHigh = ptr(maximumHigh.Float64)
	}
	if sum.Valid {
		if !observationInteger.MatchString(sum.String) {
			return nil, fmt.Errorf("%w: observation total is not a nonnegative integer", api.ErrInvalid)
		}
		out.ObservedTotal = ptr(sum.String)
	}
	return out, nil
}
