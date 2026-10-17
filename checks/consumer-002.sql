/*
code: consumer-002
name: Consumer backlog growing
entity: consumer
category: consumers
severity: warning
kind: trend
lookback: 30m
thresholds:
  - name: min_growth
    default: "100"
    unit: count
    description: Least growth of the backlog over the lookback that can fire.
  - name: min_span
    default: 10m
    unit: duration
    description: Least span of leader data within the lookback needed for a rate.
description: >-
  Over the lookback, messages arrived for the consumer faster than it took them: the backlog (num_pending, leader's
  view) grew by at least min_growth, and grew in both halves of the window, so one burst does not fire it. Fill rate
  is the backlog's growth plus the deliveries; drain rate is the deliveries, summed from the delivered counter's
  increases, so a new leader that reports it lower does not give a negative drain. Only the leader's rows are read, as
  followers do not count the backlog.
remediation: >-
  Add consumers' capacity: more replicas of the worker that pulls from it, or a larger fetch batch. If the drain
  rate is zero, no client is fetching: see consumer-006 and the worker's pods. A producer that started publishing
  much more (one customer's large batch) shows as a jump in the fill rate; the Events page and the stream's message
  rate show when.
related_config: [lookback, min_growth, max_ack_pending]
value_name: backlog growth, messages per second
value_basis: growth of num_pending over the lookback
*/
WITH pts AS (
    SELECT c.account, c.stream, c.consumer, c.epoch, e.started_at AS ts, c.num_pending, c.delivered_consumer_seq, c.created
    FROM consumer_leader_stats c JOIN epochs e USING (epoch)
    WHERE c.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND c.num_pending IS NOT NULL
),
cur AS (
    SELECT account, stream, consumer, created FROM pts WHERE epoch = getvariable('epoch')
),
-- Deliveries are the increases of the delivered counter between epochs; a
-- new leader may report it lower, which is not a negative delivery.
steps AS (
    SELECT p.*, greatest(p.delivered_consumer_seq - lag(p.delivered_consumer_seq) OVER w, 0) AS delivered
    FROM pts p JOIN cur c USING (account, stream, consumer)
    WHERE p.created IS NOT DISTINCT FROM c.created
    WINDOW w AS (PARTITION BY p.account, p.stream, p.consumer ORDER BY p.epoch)
),
agg AS (
    SELECT p.account, p.stream, p.consumer,
        min(p.ts) AS t0, max(p.ts) AS t1,
        arg_min(p.num_pending, p.epoch) AS pending0, arg_max(p.num_pending, p.epoch) AS pending1,
        coalesce(sum(p.delivered), 0) AS deliveries
    FROM steps p
    GROUP BY p.account, p.stream, p.consumer
),
mid AS (
    SELECT a.*, (SELECT arg_min(p.num_pending, abs(epoch(p.ts) - epoch(a.t0 + (a.t1 - a.t0) / 2))) FROM pts p
        WHERE p.account = a.account AND p.stream = a.stream AND p.consumer = a.consumer) AS pending_mid,
        epoch(a.t1 - a.t0) AS secs
    FROM agg a
)
SELECT
    account || '/' || stream || '/' || consumer AS entity_key,
    'Fill ' || round(((pending1 - pending0) + deliveries) / secs, 2) || ' msg/s, drain ' || round(deliveries / secs, 2)
        || ' msg/s over ' || CAST(date_trunc('second', t1 - t0) AS VARCHAR) || '. ' || pending1 || ' pending, up '
        || (pending1 - pending0) || ' since ' || strftime(t0, '%H:%M') || ' UTC.' AS detail,
    round((pending1 - pending0) / secs, 3) AS value
FROM mid
WHERE t1 - t0 >= getvariable('min_span') AND secs > 0
  AND pending1 - pending0 >= getvariable('min_growth')
  AND pending_mid > pending0 AND pending1 > pending_mid
