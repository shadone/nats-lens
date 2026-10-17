/*
code: server-014
name: JetStream store full soon
entity: server
category: storage
severity: warning
severity_rule: critical when the store is full in less than crit_days
kind: trend
lookback: 6h
thresholds:
  - name: warn_days
    default: "14"
    unit: count
    description: Days until full below which the finding fires.
  - name: crit_days
    default: "3"
    unit: count
    description: Days until full below which it is critical.
  - name: min_span
    default: 1h
    unit: duration
    description: Least span of data within the lookback needed for a rate.
description: >-
  At the rate the server's JetStream file store grew over the lookback, it reaches max_file_store within warn_days.
  The rate is the smaller of the slopes of the window's two halves, and both must be positive, so a one-off step (a
  restore, a large import) is not read as steady growth. Measured per server process: a restart starts a new window.
remediation: >-
  The Streams page shows which streams grow (stream-006 for those with a byte limit). Set max_age or max_bytes on
  streams that need no unlimited history, or raise max_file_store together with the PVC size before the date in
  the detail. A store that fills stops every stream with a replica on the server from accepting messages.
related_config: [max_file_store, lookback]
value_name: days until full
value_basis: days until the JetStream store is full at the current growth
*/
WITH pts AS (
    SELECT st.server_id, s.server_name, e.started_at AS ts, st.epoch, st.js_storage, st.js_max_storage
    FROM server_stats st JOIN servers s USING (server_id) JOIN epochs e USING (epoch)
    WHERE st.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch')
      AND st.js_storage IS NOT NULL AND st.js_max_storage > 0
),
span AS (
    SELECT server_id, min(ts) AS t0, max(ts) AS t1, max(epoch) AS last_epoch FROM pts GROUP BY server_id
),
-- The rate is the smaller of the two halves' slopes, and both must be
-- positive, so that a one-off step does not read as steady growth.
rate AS (
    SELECT p.server_id, any_value(p.server_name) AS server_name,
        arg_max(p.js_storage, p.epoch) AS now_bytes, arg_max(p.js_max_storage, p.epoch) AS max_bytes,
        regr_slope(p.js_storage, epoch(p.ts)) FILTER (WHERE p.ts < s.t0 + (s.t1 - s.t0) / 2) AS first_half,
        regr_slope(p.js_storage, epoch(p.ts)) FILTER (WHERE p.ts >= s.t0 + (s.t1 - s.t0) / 2) AS second_half,
        any_value(s.t1 - s.t0) AS span, any_value(s.last_epoch) AS last_epoch
    FROM pts p JOIN span s USING (server_id)
    GROUP BY p.server_id
),
eta AS (
    SELECT *, least(first_half, second_half) AS per_second,
        (max_bytes - now_bytes) / (least(first_half, second_half) * 86400) AS days
    FROM rate
    WHERE last_epoch = getvariable('epoch') AND span >= getvariable('min_span') AND first_half > 0 AND second_half > 0
)
SELECT
    server_name AS entity_key,
    CASE WHEN days < getvariable('crit_days') THEN 'critical' ELSE 'warning' END AS severity,
    server_name || ' file store grows ' || format_bytes(CAST(per_second * 86400 AS BIGINT)) || ' per day over the last '
        || getvariable('lookback_text') || ': full in ' || CASE WHEN days < 1 THEN round(days * 24, 1) || ' hours' ELSE round(days, 1) || ' days' END || ' ('
        || format_bytes(now_bytes) || ' of ' || format_bytes(max_bytes) || ').' AS detail,
    round(days, 2) AS value
FROM eta
WHERE days < getvariable('warn_days')
