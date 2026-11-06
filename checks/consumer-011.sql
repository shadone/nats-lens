/*
code: consumer-011
name: Consumer configuration differs from the inventory
entity: consumer
category: inventory
severity: warning
kind: operational
description: >-
  A consumer from the inventory exists, but a setting the inventory gives (ack_wait, max_deliver, max_ack_pending,
  subject filters, replicas) has another value on its leader. A replicas value of 0 on the consumer means the
  stream's replica count.
remediation: >-
  Decide which is right. The service that creates the consumer at startup sets its configuration, so a difference
  usually means the service's code or its environment changed: update the inventory if the change was intended, or
  fix the service's consumer options. `nats consumer edit` changes most settings in place; the filter of a durable
  that has delivered messages is better changed by creating a new durable.
related_config: [inventory_file]
value_name: settings that differ
value_basis: settings compared with the inventory
*/
WITH inv AS (
    SELECT i.*,
        coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                  ORDER BY (a.account = i.account) DESC LIMIT 1), i.account) AS acct
    FROM inventory i WHERE i.kind = 'consumer'
),
scfg AS (
    SELECT account, stream, max(replicas) AS r FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
),
cur AS (
    SELECT c.*, s.server_name, coalesce(cs.is_leader, false) AS lead,
        CASE WHEN c.replicas > 0 THEN c.replicas ELSE sc.r END AS effective_replicas
    FROM consumer_config c
    JOIN servers s USING (server_id)
    LEFT JOIN scfg sc USING (account, stream)
    LEFT JOIN consumer_stats cs
      ON cs.epoch = getvariable('epoch') AND cs.server_id = c.server_id AND cs.account = c.account
     AND cs.stream = c.stream AND cs.consumer = c.consumer
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
),
one AS (
    SELECT * FROM cur QUALIFY row_number() OVER (PARTITION BY account, stream, consumer ORDER BY lead DESC, server_name) = 1
),
diff AS (
    SELECT i.acct, i.stream, i.consumer, o.server_name, list_filter([
        CASE WHEN i.ack_wait_ns IS NOT NULL AND i.ack_wait_ns <> o.ack_wait_ns
            THEN 'ack_wait ' || ns_text(o.ack_wait_ns) || ' (inventory ' || ns_text(i.ack_wait_ns) || ')' END,
        CASE WHEN i.max_deliver IS NOT NULL AND i.max_deliver <> o.max_deliver
            THEN 'max_deliver ' || o.max_deliver || ' (inventory ' || i.max_deliver || ')' END,
        CASE WHEN i.max_ack_pending IS NOT NULL AND i.max_ack_pending <> o.max_ack_pending
            THEN 'max_ack_pending ' || o.max_ack_pending || ' (inventory ' || i.max_ack_pending || ')' END,
        CASE WHEN i.filter_subjects IS NOT NULL AND list_sort(i.filter_subjects) <> list_sort(o.filter_subjects)
            THEN 'filter [' || array_to_string(list_sort(o.filter_subjects), ', ') || '] (inventory ['
                || array_to_string(list_sort(i.filter_subjects), ', ') || '])' END,
        CASE WHEN i.replicas IS NOT NULL AND i.replicas <> o.effective_replicas
            THEN 'replicas ' || o.effective_replicas || ' (inventory ' || i.replicas || ')' END
    ], lambda x: x IS NOT NULL) AS differ,
    json_object('ack_wait', ns_text(i.ack_wait_ns), 'max_deliver', CAST(i.max_deliver AS VARCHAR),
        'max_ack_pending', CAST(i.max_ack_pending AS VARCHAR), 'filter_subjects', array_to_string(i.filter_subjects, ', '),
        'replicas', CAST(i.replicas AS VARCHAR)) AS expected
    FROM inv i JOIN one o ON o.account = i.acct AND o.stream = i.stream AND o.consumer = i.consumer
)
SELECT
    acct || '/' || stream || '/' || consumer AS entity_key,
    'Consumer ' || consumer || ' on ' || server_name || ': ' || array_to_string(differ, ', ') || '.' AS detail,
    len(differ) AS value,
    expected AS related_config,
    'inventory' AS related_source
FROM diff
WHERE len(differ) > 0
