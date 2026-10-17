/*
code: consumer-006
name: No delivery while messages wait
entity: consumer
category: consumers
severity: warning
kind: operational
thresholds:
  - name: max_idle
    default: 5m
    unit: duration
    description: How long the consumer may deliver nothing while messages wait.
description: >-
  The consumer's backlog (num_pending, leader's view) has waited longer than max_idle without a delivery: measured
  from the first collection of the current run of collections with messages waiting, or from the last delivery when that is
  later, so the first message after an idle hour does not fire at once. The consumer is not paused. For a pull consumer nobody is fetching; for a push consumer nobody is subscribed, or
  delivery stopped at max_ack_pending (consumer-003).
remediation: >-
  The detail gives the pull requests waiting on the leader: 0 means no client is fetching. Check the service that
  reads the consumer: are its pods running, and connected (Connections page, by name)? A worker that connects but
  never fetches may be bound to another consumer name after a rename.
related_config: [max_idle, max_ack_pending]
value_name: minutes waiting
value_basis: time since the backlog began waiting without a delivery, leader rows
*/
WITH cur AS (
    SELECT * FROM consumer_leader_stats
    WHERE epoch = getvariable('epoch') AND num_pending > 0 AND NOT coalesce(paused, false)
),
last_empty AS (
    SELECT c.account, c.stream, c.consumer,
        (SELECT max(x.epoch) FROM consumer_leader_stats x
         WHERE x.account = c.account AND x.stream = c.stream AND x.consumer = c.consumer
           AND x.epoch < c.epoch AND coalesce(x.num_pending, 0) = 0) AS epoch
    FROM cur c
),
waiting AS (
    SELECT c.*,
        (SELECT min(e.started_at) FROM consumer_leader_stats x JOIN epochs e USING (epoch)
         WHERE x.account = c.account AND x.stream = c.stream AND x.consumer = c.consumer
           AND x.epoch > coalesce(l.epoch, 0) AND x.epoch <= c.epoch AND x.num_pending > 0) AS run_start
    FROM cur c JOIN last_empty l USING (account, stream, consumer)
),
since AS (
    SELECT *, greatest(run_start, coalesce(delivered_last_active, run_start)) AS waiting_since FROM waiting
)
SELECT
    account || '/' || stream || '/' || consumer AS entity_key,
    num_pending || ' messages waiting since ' || strftime(waiting_since, '%Y-%m-%d %H:%M:%S') || ' UTC, nothing delivered '
        || CASE WHEN delivered_last_active IS NULL THEN 'since the consumer was created'
            ELSE 'since ' || strftime(delivered_last_active, '%Y-%m-%d %H:%M:%S') || ' UTC' END
        || '; ' || coalesce(num_waiting, 0) || ' pull requests waiting, ' || coalesce(num_ack_pending, 0) || ' unacknowledged.' AS detail,
    round(epoch(getvariable('epoch_time') - waiting_since) / 60, 1) AS value
FROM since
WHERE getvariable('epoch_time') - waiting_since > getvariable('max_idle')
