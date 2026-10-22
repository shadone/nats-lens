package store

import (
	"fmt"
	"time"
)

// microLookback is how far back an endpoint's previous STATS row is
// looked for: a row older than this does not count as the instance's
// previous one.
const microLookback = time.Hour

// microHoursQuery adds the epoch's increases to micro_endpoint_hours.
// Arguments: the epoch, the hour, the lowest epoch of the lookback, the
// previous epoch's start (NULL without one).
const microHoursQuery = `INSERT INTO micro_endpoint_hours
WITH cur AS (
  SELECT e.account, e.service, e.instance_id, e.endpoint, e.num_requests, e.num_errors, e.processing_time_ns, s.started
  FROM micro_endpoints e JOIN micro_stats s USING (epoch, account, service, instance_id)
  WHERE e.epoch = $1),
prev AS (
  SELECT e.account, e.service, e.instance_id, e.endpoint,
    arg_max(e.num_requests, e.epoch) AS num_requests, arg_max(e.num_errors, e.epoch) AS num_errors,
    arg_max(e.processing_time_ns, e.epoch) AS processing_time_ns, arg_max(s.started, e.epoch) AS started
  FROM micro_endpoints e JOIN micro_stats s USING (epoch, account, service, instance_id)
  WHERE e.epoch >= $3 AND e.epoch < $1
  GROUP BY ALL),
d AS (
  SELECT c.account, c.service, c.endpoint,
    CASE
      WHEN p.num_requests IS NULL THEN c.started IS NOT NULL AND $4::TIMESTAMP IS NOT NULL AND c.started > $4::TIMESTAMP
      ELSE c.num_requests < p.num_requests OR c.num_errors < p.num_errors
        OR c.processing_time_ns < p.processing_time_ns OR c.started IS DISTINCT FROM p.started
    END AS whole,
    p.num_requests IS NULL AS first,
    c.num_requests, c.num_errors, c.processing_time_ns,
    p.num_requests AS p_requests, p.num_errors AS p_errors, p.processing_time_ns AS p_time
  FROM cur c LEFT JOIN prev p USING (account, service, instance_id, endpoint))
SELECT $2::TIMESTAMP, account, service, endpoint,
  sum(CASE WHEN whole THEN num_requests WHEN first THEN 0 ELSE num_requests - p_requests END),
  sum(CASE WHEN whole THEN num_errors WHEN first THEN 0 ELSE num_errors - p_errors END),
  sum(CASE WHEN whole THEN processing_time_ns WHEN first THEN 0 ELSE processing_time_ns - p_time END),
  1
FROM d GROUP BY ALL
ON CONFLICT (hour, account, service, endpoint) DO UPDATE SET
  requests = requests + excluded.requests, errors = errors + excluded.errors,
  processing_time_ns = processing_time_ns + excluded.processing_time_ns, epochs = epochs + 1`

// writeMicroHours adds the increases of this epoch's STATS rows over each
// instance's previous row to the hour of the epoch. An epoch is written
// once, in one transaction, so the sums count each increase once; an
// hour's rows change only while its epochs are written, so the archive's
// final export of the hour holds them whole.
func (w *writer) writeMicroHours() {
	stats := false
	for _, acc := range w.snap.Micro {
		stats = stats || len(acc.Stats) > 0
	}
	if !stats || w.err != nil {
		return
	}
	start := w.snap.StartedAt.UTC()
	var lo int64
	var prevStart any
	if err := w.tx.QueryRowContext(w.ctx, `SELECT coalesce(min(epoch), ?) FROM epochs WHERE started_at >= ? AND epoch < ?`,
		w.epoch, start.Add(-microLookback), w.epoch).Scan(&lo); err != nil {
		w.fail(fmt.Errorf("micro lookback: %w", err))
		return
	}
	if w.prevEpoch > 0 {
		var t time.Time
		if err := w.tx.QueryRowContext(w.ctx, `SELECT started_at FROM epochs WHERE epoch = ?`, w.prevEpoch).Scan(&t); err != nil {
			w.fail(fmt.Errorf("previous epoch start: %w", err))
			return
		}
		prevStart = t
	}
	w.exec(microHoursQuery, w.epoch, start.Truncate(time.Hour), lo, prevStart)
}
