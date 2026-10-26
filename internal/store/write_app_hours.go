package store

import (
	"time"
)

// appLookback is how far back a connection's previous row is looked for:
// a row older than this does not count as its previous one.
const appLookback = time.Hour

// appHoursQuery adds the epoch's traffic increases to client_app_hours.
// A connection is (server_id, cid, start_time): a server gives a cid once
// per process, and the start time tells a restarted server's reuse apart.
// Each of the epoch's rows is paired with the connection's newest row of
// the lookback; when any of its counters fell the row counts whole (a new
// connection under the same key), else by the increase. A row without a
// previous one counts whole when the connection started after the
// previous epoch started, else it adds nothing and is not counted: its
// traffic before this epoch was never seen. A connection's rows are taken
// once per epoch even if stored twice, so that a duplicate never
// multiplies an increase.
// Arguments: the epoch, the hour, the lowest epoch of the lookback, the
// previous epoch's start (NULL without one); times are Unix microseconds
// of UTC, made into TIMESTAMP in SQL, so that no session time zone shifts
// them on the way.
const appHoursQuery = `INSERT INTO client_app_hours
WITH cur AS (
  SELECT server_id, cid, start_time, any_value(name) AS name, any_value(account) AS account, any_value(ip) AS ip,
    coalesce(any_value(in_msgs), 0) AS in_msgs, coalesce(any_value(out_msgs), 0) AS out_msgs,
    coalesce(any_value(in_bytes), 0) AS in_bytes, coalesce(any_value(out_bytes), 0) AS out_bytes
  FROM connections WHERE epoch = $1 GROUP BY server_id, cid, start_time),
prev AS (
  SELECT server_id, cid, start_time,
    coalesce(arg_max(in_msgs, epoch), 0) AS in_msgs, coalesce(arg_max(out_msgs, epoch), 0) AS out_msgs,
    coalesce(arg_max(in_bytes, epoch), 0) AS in_bytes, coalesce(arg_max(out_bytes, epoch), 0) AS out_bytes
  FROM connections WHERE epoch >= $3 AND epoch < $1 GROUP BY server_id, cid, start_time),
d AS (
  SELECT coalesce(c.account, '') AS account,
    coalesce(nullif(split_part(coalesce(c.name, ''), '@', 1), ''), '(unnamed)') AS app,
    coalesce(c.ip, '') IN ('127.0.0.1', '::1') AS loopback,
    p.cid IS NOT NULL AS paired,
    CASE
      WHEN p.cid IS NULL THEN c.start_time IS NOT NULL AND $4::BIGINT IS NOT NULL AND c.start_time > make_timestamp($4::BIGINT)
      ELSE c.in_msgs < p.in_msgs OR c.out_msgs < p.out_msgs OR c.in_bytes < p.in_bytes OR c.out_bytes < p.out_bytes
    END AS whole,
    c.in_msgs, c.out_msgs, c.in_bytes, c.out_bytes,
    p.in_msgs AS p_in_msgs, p.out_msgs AS p_out_msgs, p.in_bytes AS p_in_bytes, p.out_bytes AS p_out_bytes
  FROM cur c LEFT JOIN prev p ON p.server_id = c.server_id AND p.cid = c.cid
    AND p.start_time IS NOT DISTINCT FROM c.start_time)
SELECT make_timestamp($2::BIGINT), account, app, loopback,
  sum(CASE WHEN whole THEN in_msgs ELSE in_msgs - p_in_msgs END),
  sum(CASE WHEN whole THEN out_msgs ELSE out_msgs - p_out_msgs END),
  sum(CASE WHEN whole THEN in_bytes ELSE in_bytes - p_in_bytes END),
  sum(CASE WHEN whole THEN out_bytes ELSE out_bytes - p_out_bytes END),
  1
FROM d WHERE paired OR whole GROUP BY ALL
ON CONFLICT (hour, account, app, loopback) DO UPDATE SET
  in_msgs = in_msgs + excluded.in_msgs, out_msgs = out_msgs + excluded.out_msgs,
  in_bytes = in_bytes + excluded.in_bytes, out_bytes = out_bytes + excluded.out_bytes, epochs = epochs + 1`

// writeAppHours adds the increases of this epoch's connection counters
// over each connection's previous row to the hour of the epoch, by
// application. Like writeMicroHours, it runs once per epoch in the
// epoch's transaction, so an hour's rows change only while its epochs are
// written.
func (w *writer) writeAppHours() {
	conns := false
	for _, v := range w.snap.Servers {
		conns = conns || (v.Connz != nil && len(v.Connz.Conns) > 0)
	}
	if !conns || w.err != nil {
		return
	}
	lo, prevStart := w.lookback(appLookback)
	w.exec(appHoursQuery, w.epoch, w.snap.StartedAt.UTC().Truncate(time.Hour).UnixMicro(), lo, prevStart)
}
