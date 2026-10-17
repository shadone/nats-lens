/*
code: consumer-004
name: Many redeliveries
entity: consumer
category: consumers
severity: warning
kind: operational
thresholds:
  - name: redelivered_ratio
    default: "0.25"
    unit: ratio
    description: >-
      Share of max_ack_pending (of the unacknowledged messages when max_ack_pending is unlimited) that redelivered
      messages must reach.
  - name: min_redelivered
    default: "10"
    unit: count
    description: >-
      Fewest redelivered messages that can fire, whatever the share; a consumer whose max_ack_pending is smaller
      fires from max_ack_pending.
description: >-
  The consumer's unacknowledged messages that were delivered more than once reach redelivered_ratio of its
  max_ack_pending (of its unacknowledged messages when max_ack_pending is unlimited), and at least min_redelivered
  or max_ack_pending, whichever is smaller:
  they timed out (ack_wait) or were rejected (NAK) and came back.
remediation: >-
  Look for the reason the clients do not finish these messages: errors in the worker's log, NAK events for the
  consumer on the Events page, or work that takes longer than ack_wait (then raise ack_wait, or have the worker send
  progress acknowledgements while it works). A message that fails every time is better terminated by the worker than
  redelivered until max_deliver.
related_config: [redelivered_ratio, min_redelivered, max_ack_pending, ack_wait, max_deliver, backoff]
value_name: redelivered messages
value_basis: num_redelivered, leader's view
*/
WITH lead AS (
    SELECT c.account, c.stream, c.consumer, c.server_id, c.num_redelivered, c.num_ack_pending, s.server_name
    FROM consumer_leader_stats c JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch') AND c.num_redelivered > 0
)
SELECT
    l.account || '/' || l.stream || '/' || l.consumer AS entity_key,
    l.num_redelivered || ' of ' || l.num_ack_pending || ' unacknowledged messages were delivered more than once'
        || coalesce(' (ack_wait ' || ns_text(k.ack_wait_ns) || ', max_deliver ' || k.max_deliver
            || ', max_ack_pending ' || k.max_ack_pending || ')', '') || '.' AS detail,
    l.num_redelivered AS value,
    json_object('ack_wait', ns_text(k.ack_wait_ns), 'max_deliver', CAST(k.max_deliver AS VARCHAR),
        'max_ack_pending', CAST(k.max_ack_pending AS VARCHAR)) AS related_config,
    l.server_name AS related_source
FROM lead l
LEFT JOIN consumer_config k
  ON k.server_id = l.server_id AND k.account = l.account AND k.stream = l.stream AND k.consumer = l.consumer
 AND k.first_epoch <= getvariable('epoch') AND k.last_epoch >= getvariable('epoch')
WHERE l.num_redelivered >= CASE WHEN coalesce(k.max_ack_pending, 0) > 0
        THEN least(getvariable('min_redelivered'), k.max_ack_pending) ELSE getvariable('min_redelivered') END
  AND l.num_redelivered >= getvariable('redelivered_ratio')
    * CASE WHEN coalesce(k.max_ack_pending, 0) > 0 THEN k.max_ack_pending ELSE l.num_ack_pending END
