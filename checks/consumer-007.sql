/*
code: consumer-007
name: Push consumer without a subscriber
entity: consumer
category: consumers
severity: warning
kind: operational
thresholds:
  - name: min_unbound
    default: 2m
    unit: duration
    description: How long a durable push consumer may have no subscriber before the finding fires.
description: >-
  A durable push consumer (one with a deliver subject) has had no active subscriber on its deliver subject for
  longer than min_unbound, by the leader's view. Messages for it are not delivered. Ephemeral push consumers, such
  as KV watchers, are left out: the server removes them once they lose their subscriber.
remediation: >-
  Find the service that should subscribe to the deliver subject in the detail and check that it runs and is
  connected. A push consumer whose service moved to pull consumers is left behind: delete it if nothing uses it
  (`nats consumer rm <stream> <consumer>`), after checking nobody else does.
related_config: [min_unbound, deliver_subject]
value_name: minutes without a subscriber
value_basis: time since the push consumer was last bound
*/
WITH push AS (
    SELECT DISTINCT account, stream, consumer, deliver_subject
    FROM consumer_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
      AND coalesce(deliver_subject, '') <> '' AND coalesce(durable_name, '') <> ''
),
cur AS (
    SELECT c.account, c.stream, c.consumer, c.created, p.deliver_subject
    FROM consumer_leader_stats c JOIN push p USING (account, stream, consumer)
    WHERE c.epoch = getvariable('epoch') AND c.push_bound = false AND NOT coalesce(c.paused, false)
),
since AS (
    SELECT c.*,
        (SELECT min(e.started_at) FROM consumer_leader_stats x JOIN epochs e USING (epoch)
         WHERE x.account = c.account AND x.stream = c.stream AND x.consumer = c.consumer
           AND x.created IS NOT DISTINCT FROM c.created AND x.epoch <= getvariable('epoch')
           AND x.epoch > coalesce((SELECT max(y.epoch) FROM consumer_leader_stats y
                WHERE y.account = c.account AND y.stream = c.stream AND y.consumer = c.consumer AND y.push_bound), 0)) AS unbound_since
    FROM cur c
)
SELECT
    account || '/' || stream || '/' || consumer AS entity_key,
    'No subscriber on deliver subject ' || deliver_subject || ' since ' || strftime(unbound_since, '%Y-%m-%d %H:%M:%S') || ' UTC.' AS detail,
    round(epoch(getvariable('epoch_time') - unbound_since) / 60, 1) AS value
FROM since
WHERE getvariable('epoch_time') - unbound_since >= getvariable('min_unbound')
