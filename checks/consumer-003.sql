/*
code: consumer-003
name: Ack pending near max_ack_pending
entity: consumer
category: consumers
severity: warning
severity_rule: >-
  critical when the consumer has been at max_ack_pending with messages waiting for min_epochs consecutive epochs
  and neither its ack floor nor its delivered sequence moved for at least ack_wait, as delivery has then stopped; a pool at the
  limit that still acknowledges is a saturated pool, at most warning; slots all in use with nothing waiting is no
  finding
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.8"
    unit: ratio
    description: Share of max_ack_pending in flight at which the finding fires.
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive epochs at the limit with messages waiting and no acknowledgement before the finding is critical.
description: >-
  The consumer has delivered messages that are not acknowledged yet up to warn_ratio of max_ack_pending while
  more messages wait (num_pending above 0, leader's view). At the limit the server delivers nothing more until
  acknowledgements arrive, so the waiting messages are held back.
remediation: >-
  A saturated pool (warning) is workers acknowledging as fast as max_ack_pending lets them: if the backlog grows
  (consumer-002), add workers or raise max_ack_pending. A stopped consumer (critical): the clients take messages and
  do not acknowledge them in time: a worker that is slow per message, stuck, or
  that crashed while holding messages (they come back after ack_wait). Look at the worker's pods and logs. If the
  work per message is legitimately long, either run more workers or raise max_ack_pending; raising it without more
  workers only lets more messages wait. Messages that never get acknowledged end at max_deliver (consumer-005).
related_config: [max_ack_pending, ack_wait, max_deliver]
value_name: ack pending share of max_ack_pending
value_basis: num_ack_pending divided by max_ack_pending, leader's view
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
lead AS (
    SELECT c.account, c.stream, c.consumer, c.server_id, c.num_ack_pending, c.num_pending, s.server_name,
        c.ack_floor_stream_seq, c.delivered_stream_seq
    FROM consumer_leader_stats c JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch') AND c.num_pending > 0
),
cfg AS (
    SELECT l.*, k.max_ack_pending, k.ack_wait_ns, k.max_deliver,
        (SELECT count(*) FROM consumer_leader_stats x
         WHERE x.account = l.account AND x.stream = l.stream AND x.consumer = l.consumer
           AND x.epoch IN (SELECT epoch FROM last) AND x.num_pending > 0 AND x.num_ack_pending >= k.max_ack_pending) AS full_epochs,
        -- Since when the ack floor and the delivered sequence have stood
        -- still: the first epoch after the newest one with other values.
        (SELECT min(e.started_at) FROM consumer_leader_stats x JOIN epochs e USING (epoch)
         WHERE x.account = l.account AND x.stream = l.stream AND x.consumer = l.consumer
           AND x.epoch > coalesce((SELECT max(y.epoch) FROM consumer_leader_stats y
               WHERE y.account = l.account AND y.stream = l.stream AND y.consumer = l.consumer
                 AND y.epoch < getvariable('epoch')
                 AND (y.ack_floor_stream_seq IS DISTINCT FROM l.ack_floor_stream_seq
                   OR y.delivered_stream_seq IS DISTINCT FROM l.delivered_stream_seq)), 0)) AS still_since
    FROM lead l JOIN consumer_config k
      ON k.server_id = l.server_id AND k.account = l.account AND k.stream = l.stream AND k.consumer = l.consumer
     AND k.first_epoch <= getvariable('epoch') AND k.last_epoch >= getvariable('epoch')
    WHERE k.max_ack_pending > 0
),
-- Stopped: at the limit with messages waiting for min_epochs epochs, and no
-- acknowledgement or delivery for at least ack_wait (30s when unset), so
-- that workers busy with long messages are not taken for stuck ones.
judged AS (
    SELECT *, full_epochs >= getvariable('min_epochs')
        AND getvariable('epoch_time') - still_since
            >= to_microseconds(CAST(coalesce(nullif(ack_wait_ns, 0), 30000000000) // 1000 AS BIGINT)) AS stopped,
        getvariable('epoch_time') - still_since AS stuck_for
    FROM cfg
)
SELECT
    account || '/' || stream || '/' || consumer AS entity_key,
    CASE WHEN stopped THEN 'critical' ELSE 'warning' END AS severity,
    num_ack_pending || ' of max_ack_pending ' || max_ack_pending || ' unacknowledged; ' || num_pending || ' waiting'
        || CASE
            WHEN stopped THEN '; no acknowledgement and no delivery for '
                || CAST(date_trunc('second', stuck_for) AS VARCHAR) || ': delivery has stopped'
            WHEN num_ack_pending >= max_ack_pending THEN '; pool saturated: the workers acknowledge, but every slot is taken'
            ELSE '' END || '.' AS detail,
    round(num_ack_pending / max_ack_pending, 4) AS value,
    json_object('max_ack_pending', CAST(max_ack_pending AS VARCHAR), 'ack_wait', ns_text(ack_wait_ns),
        'max_deliver', CAST(max_deliver AS VARCHAR)) AS related_config,
    server_name AS related_source
FROM judged
WHERE num_ack_pending >= getvariable('warn_ratio') * max_ack_pending
