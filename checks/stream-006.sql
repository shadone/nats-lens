/*
code: stream-006
name: Stream reaches its byte limit soon
entity: stream
category: storage
severity: warning
severity_rule: >-
  critical below crit_days; a limits stream with discard old is left out, as reaching its limit only drops its oldest
  messages
kind: trend
lookback: 6h
thresholds:
  - name: warn_days
    default: "14"
    unit: count
    description: Days until max_bytes below which the finding fires.
  - name: crit_days
    default: "3"
    unit: count
    description: >-
      Days below which the stream is critical: a discard-new stream, or a work-queue or interest stream with discard
      old.
  - name: min_span
    default: 1h
    unit: duration
    description: Least span of data within the lookback needed for a rate.
description: >-
  At the rate the stream grew over the lookback (leader's view), it reaches max_bytes within warn_days. The rate is
  the smaller of the slopes of the window's two halves, and both must be positive, so a one-off burst is not read as
  steady growth.
remediation: >-
  Find out why the stream grows: a consumer that stopped acknowledging on a workqueue or interest stream
  (consumer-002, consumer-006) keeps messages that would otherwise be removed. If the growth is real traffic, raise
  max_bytes in the stream's definition and check the server store has room (server-007, server-014).
related_config: [max_bytes, discard, lookback]
value_name: days until the byte limit
value_basis: days until max_bytes at the current growth
*/
WITH pts AS (
    SELECT ss.account, ss.stream, ss.epoch, e.started_at AS ts, ss.bytes
    FROM stream_stats ss JOIN epochs e USING (epoch)
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader AND ss.bytes IS NOT NULL
),
span AS (
    SELECT account, stream, min(ts) AS t0, max(ts) AS t1, max(epoch) AS last_epoch FROM pts GROUP BY account, stream
),
-- The rate is the smaller of the two halves' slopes, and both must be
-- positive, so that a one-off burst does not read as steady growth.
rate AS (
    SELECT p.account, p.stream, arg_max(p.bytes, p.epoch) AS now_bytes, any_value(s.last_epoch) AS last_epoch,
        regr_slope(p.bytes, epoch(p.ts)) FILTER (WHERE p.ts < s.t0 + (s.t1 - s.t0) / 2) AS first_half,
        regr_slope(p.bytes, epoch(p.ts)) FILTER (WHERE p.ts >= s.t0 + (s.t1 - s.t0) / 2) AS second_half,
        any_value(s.t1 - s.t0) AS span
    FROM pts p JOIN span s USING (account, stream)
    GROUP BY p.account, p.stream
),
cfg AS (
    SELECT account, stream, max(max_bytes) AS max_bytes, any_value(discard) AS discard, any_value(retention) AS retention
    FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
),
eta AS (
    SELECT r.*, least(r.first_half, r.second_half) AS per_second, c.max_bytes, c.discard, c.retention,
        (c.max_bytes - r.now_bytes) / (least(r.first_half, r.second_half) * 86400) AS days
    FROM rate r JOIN cfg c USING (account, stream)
    WHERE r.last_epoch = getvariable('epoch') AND c.max_bytes > 0 AND NOT (c.discard <> 'new' AND c.retention = 'limits')
      AND r.first_half > 0 AND r.second_half > 0 AND r.span >= getvariable('min_span')
)
SELECT
    account || '/' || stream AS entity_key,
    CASE WHEN days < getvariable('crit_days') THEN 'critical' ELSE 'warning' END AS severity,
    'Grows ' || format_bytes(CAST(per_second * 86400 AS BIGINT)) || ' per day over the last ' || getvariable('lookback_text')
        || ': reaches max_bytes ' || format_bytes(max_bytes) || ' in ' || CASE WHEN days < 1 THEN round(days * 24, 1) || ' hours' ELSE round(days, 1) || ' days' END || ' ('
        || format_bytes(now_bytes) || ' now); discard ' || discard || '.' AS detail,
    round(days, 2) AS value,
    json_object('max_bytes', CAST(max_bytes AS VARCHAR), 'discard', discard) AS related_config
FROM eta
WHERE days < getvariable('warn_days')
