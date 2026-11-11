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
	q, args, counter, err := p.observationQuery(ctx, metric, e, r)
	if err != nil {
		return nil, err
	}
	out, err := scanObservations(p.db().QueryRowContext(ctx, q, args...), counter)
	if err != nil {
		return nil, fmt.Errorf("observations %s: %w", metric, err)
	}
	return out, nil
}

func (p *Provider) observationQuery(ctx context.Context, metric string, e *v1.EntityRef, r *v1.TimeRange) (string, []any, bool, error) {
	start, end := span(r)
	if end.Sub(start) > api.MaxObservationRange {
		return "", nil, false, fmt.Errorf("%w: observation range exceeds 30 days", api.ErrInvalid)
	}
	m, ok := metricQueries[metric]
	edges := "NULL::DOUBLE, NULL::DOUBLE"
	if col, band := bandMetrics[metric]; band {
		// col is selected only from the compile-time band catalogue, never input SQL.
		m = metricSQL{query: bandObservationSQL(col, false)}
		ok = true
		edges = "min(lo)::DOUBLE, max(hi)::DOUBLE"
	}
	if !ok {
		return "", nil, false, fmt.Errorf("%w: unknown observation metric", api.ErrInvalid)
	}
	total := "NULL::VARCHAR"
	if m.sum {
		total = "sum(v)::VARCHAR"
	}
	lookback := p.seriesLookback(metric, start)
	m, err := p.resolveMetric(ctx, m, lookback, end)
	if err != nil {
		return "", nil, false, err
	}
	args := append([]any{lookback, end}, entityArgs(e)...)
	prefix := `WITH per_epoch AS (` + p.metricQuery(m) + `), selected AS (SELECT * FROM per_epoch WHERE t >= ? AND t < ?)`
	counterColumns := `NULL::VARCHAR,0::BIGINT,0::BIGINT,NULL::TIMESTAMP,NULL::TIMESTAMP,0::BIGINT,0::BIGINT`
	if m.counter != nil {
		evidence := p.metricQuery(metricSQL{query: counterEvidence(m.counter.column, m.counter.filter)})
		// UTC microseconds keep the shared bounds independent of the DuckDB session TZ.
		prefix = `WITH counter_evidence AS (` + evidence + `), range_bounds AS (
   SELECT make_timestamp(?::BIGINT) AS range_start, make_timestamp(?::BIGINT) AS range_end),
   per_epoch AS (` + counterRateProjection + `),
   selected AS (SELECT t,v FROM per_epoch CROSS JOIN range_bounds WHERE t >= range_start AND t < range_end),
   counter_selected AS (SELECT counter_evidence.*,range_start FROM counter_evidence CROSS JOIN range_bounds WHERE t >= range_start AND t < range_end),
   counter_summary AS (SELECT
    (sum(delta) FILTER (WHERE delta IS NOT NULL AND previous_at >= range_start))::VARCHAR AS increase,
    count(*) FILTER (WHERE delta IS NOT NULL AND previous_at >= range_start) AS pairs,
    count(DISTINCT server_id) FILTER (WHERE delta IS NOT NULL AND previous_at >= range_start) AS ids,
    min(previous_at) FILTER (WHERE delta IS NOT NULL AND previous_at >= range_start) AS first_at,
    max(t) FILTER (WHERE delta IS NOT NULL AND previous_at >= range_start) AS last_at,
    count(*) - count(*) FILTER (WHERE delta IS NOT NULL AND previous_at >= range_start) AS excluded,
    count(*) FILTER (WHERE delta IS NOT NULL AND previous_at < range_start) AS boundary
    FROM counter_selected)`
		args = append(args, start.UnixMicro(), end.UnixMicro())
		counterColumns = `(SELECT increase FROM counter_summary),(SELECT pairs FROM counter_summary),
   (SELECT ids FROM counter_summary),(SELECT first_at FROM counter_summary),(SELECT last_at FROM counter_summary),
   (SELECT excluded FROM counter_summary),(SELECT boundary FROM counter_summary)`
	} else {
		args = append(args, start, end)
	}
	q := `/* observation_summary */ ` + prefix + `, observation_latest AS (
 SELECT CASE WHEN count(v)=count(*) AND min(v)=max(v) THEN max(v)::DOUBLE END AS v
 FROM selected WHERE t=(SELECT max(t) FROM selected))
SELECT count(*),count(v),min(t),max(t),(SELECT v FROM observation_latest),avg(v)::DOUBLE,min(v)::DOUBLE,max(v)::DOUBLE,
first(t ORDER BY v DESC NULLS LAST,t) FILTER (WHERE v IS NOT NULL),` + total + `,` + edges + `,` + counterColumns + ` FROM selected`
	return q, args, m.counter != nil, nil
}

type observationScanner interface{ Scan(...any) error }

func scanObservations(row observationScanner, hasCounter bool, prefix ...any) (*v1.ObservationSummary, error) {
	out := &v1.ObservationSummary{}
	var first, last, peak sql.NullTime
	var latest, mean, low, high, minimumLow, maximumHigh sql.NullFloat64
	var sum, increase sql.NullString
	var counterFirst, counterLast sql.NullTime
	counter := &v1.CounterComparisonSummary{}
	if err := row.Scan(append(prefix, &out.SampleCount, &out.KnownCount, &first, &last, &latest, &mean, &low, &high, &peak, &sum, &minimumLow, &maximumHigh, &increase, &counter.ComparisonCount, &counter.ServerIdCount, &counterFirst, &counterLast, &counter.ExcludedReports, &counter.BoundaryReports)...); err != nil {
		return nil, err
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
	if hasCounter {
		if increase.Valid {
			if !observationInteger.MatchString(increase.String) {
				return nil, fmt.Errorf("%w: invalid counter comparison total", api.ErrInvalid)
			}
			counter.ObservedIncrease = ptr(increase.String)
		}
		if counterFirst.Valid {
			counter.FirstComparedAt = ts(counterFirst.Time.UTC())
		}
		if counterLast.Valid {
			counter.LastComparedAt = ts(counterLast.Time.UTC())
		}
		out.CounterComparisons = counter
	}
	return out, nil
}
