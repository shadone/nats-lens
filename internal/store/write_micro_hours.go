package store

import (
	"fmt"
	"time"
)

// microLookback is how far back an endpoint's previous STATS row is
// looked for: a row older than this does not count as the instance's
// previous one.
const microLookback = time.Hour

// microHoursQuery adds the epoch's increases to micro_endpoint_hours. A
// row is paired with the instance's previous row of the same endpoint name
// and subject, as one instance may have two endpoints of one name in
// different groups; their increases are summed into the hour of the name.
// When either row has no subject (one STATS without it before INFO
// filled it in), the rows are paired by name alone. Two endpoints of one
// name that both lack a subject cannot be told apart and are taken as one;
// the micro framework always reports subjects.
// An instance's rows are taken once per epoch even if they were stored
// twice, so that a duplicate never multiplies an increase.
// Arguments: the epoch, the hour, the lowest epoch of the lookback, the
// previous epoch's start (NULL without one); times are Unix microseconds
// of UTC, made into TIMESTAMP in SQL, so that no session time zone shifts
// them on the way.
const microHoursQuery = `INSERT INTO micro_endpoint_hours
WITH stats AS (
  SELECT epoch, account, service, instance_id, any_value(started) AS started
  FROM micro_stats WHERE epoch >= $3 AND epoch <= $1 GROUP BY ALL),
cur AS (
  SELECT e.account, e.service, e.instance_id, e.endpoint, coalesce(e.subject, '') AS subj,
    any_value(e.num_requests) AS num_requests,
    any_value(e.num_errors) AS num_errors, any_value(e.processing_time_ns) AS processing_time_ns, any_value(s.started) AS started
  FROM micro_endpoints e JOIN stats s USING (epoch, account, service, instance_id)
  WHERE e.epoch = $1
  GROUP BY ALL),
prev AS (
  SELECT e.account, e.service, e.instance_id, e.endpoint, coalesce(e.subject, '') AS subj,
    arg_max(e.num_requests, e.epoch) AS num_requests, arg_max(e.num_errors, e.epoch) AS num_errors,
    arg_max(e.processing_time_ns, e.epoch) AS processing_time_ns, arg_max(s.started, e.epoch) AS started
  FROM micro_endpoints e JOIN stats s USING (epoch, account, service, instance_id)
  WHERE e.epoch >= $3 AND e.epoch < $1
  GROUP BY ALL),
d AS (
  SELECT c.account, c.service, c.endpoint,
    CASE
      WHEN p.num_requests IS NULL THEN c.started IS NOT NULL AND $4::BIGINT IS NOT NULL AND c.started > make_timestamp($4::BIGINT)
      ELSE c.num_requests < p.num_requests OR c.num_errors < p.num_errors
        OR c.processing_time_ns < p.processing_time_ns OR c.started IS DISTINCT FROM p.started
    END AS whole,
    p.num_requests IS NULL AS first,
    c.num_requests, c.num_errors, c.processing_time_ns,
    p.num_requests AS p_requests, p.num_errors AS p_errors, p.processing_time_ns AS p_time
  FROM cur c LEFT JOIN prev p ON p.account = c.account AND p.service = c.service AND p.instance_id = c.instance_id
    AND p.endpoint = c.endpoint AND (p.subj = c.subj OR p.subj = '' OR c.subj = '')
  QUALIFY row_number() OVER (PARTITION BY c.account, c.service, c.instance_id, c.endpoint, c.subj
    ORDER BY p.subj = c.subj DESC NULLS LAST, p.subj) = 1)
SELECT make_timestamp($2::BIGINT), account, service, endpoint,
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
		prevStart = t.UTC().UnixMicro()
	}
	w.exec(microHoursQuery, w.epoch, start.Truncate(time.Hour).UnixMicro(), lo, prevStart)
}
