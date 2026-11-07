/*
code: stream-012
name: Stream configuration differs from the inventory
entity: stream
category: inventory
severity: warning
kind: operational
description: >-
  A stream, KV bucket or object store from the inventory exists, but a setting the inventory gives (replicas,
  retention, storage, discard, max_age, max_bytes, max_msgs, max_msgs_per_subject) has another value on its leader.
remediation: >-
  Decide which is right. If the inventory is, change the stream (`nats stream edit`, or its NACK resource or
  other definition); some settings (storage, retention) cannot be changed in place
  and need the stream recreated. If the stream is right, update the inventory file.
related_config: [inventory_file]
value_name: settings that differ
value_basis: settings compared with the inventory
*/
WITH inv AS (
    SELECT i.*,
        coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                  ORDER BY (a.account = i.account) DESC LIMIT 1), i.account) AS acct
    FROM inventory i WHERE i.kind IN ('stream', 'kv', 'object_store')
),
cur AS (
    SELECT c.*, s.server_name, coalesce(ss.is_leader, false) AS lead
    FROM stream_config c
    JOIN servers s USING (server_id)
    LEFT JOIN stream_stats ss
      ON ss.epoch = getvariable('epoch') AND ss.server_id = c.server_id AND ss.account = c.account AND ss.stream = c.stream
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
),
one AS (
    SELECT * FROM cur QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY lead DESC, server_name) = 1
),
diff AS (
    SELECT i.acct, i.stream, i.kind, i.name, o.server_name, list_filter([
        CASE WHEN i.replicas IS NOT NULL AND i.replicas <> o.replicas THEN 'replicas ' || o.replicas || ' (inventory ' || i.replicas || ')' END,
        CASE WHEN i.retention IS NOT NULL AND i.retention <> o.retention THEN 'retention ' || o.retention || ' (inventory ' || i.retention || ')' END,
        CASE WHEN i.storage IS NOT NULL AND i.storage <> o.storage THEN 'storage ' || o.storage || ' (inventory ' || i.storage || ')' END,
        CASE WHEN i.discard IS NOT NULL AND i.discard <> o.discard THEN 'discard ' || o.discard || ' (inventory ' || i.discard || ')' END,
        CASE WHEN i.max_age_ns IS NOT NULL AND i.max_age_ns <> o.max_age_ns THEN 'max_age ' || ns_text(o.max_age_ns) || ' (inventory ' || ns_text(i.max_age_ns) || ')' END,
        CASE WHEN i.max_bytes IS NOT NULL AND i.max_bytes <> o.max_bytes THEN 'max_bytes ' || o.max_bytes || ' (inventory ' || i.max_bytes || ')' END,
        CASE WHEN i.max_msgs IS NOT NULL AND i.max_msgs <> o.max_msgs THEN 'max_msgs ' || o.max_msgs || ' (inventory ' || i.max_msgs || ')' END,
        CASE WHEN i.max_msgs_per_subject IS NOT NULL AND i.max_msgs_per_subject <> o.max_msgs_per_subject
            THEN 'max_msgs_per_subject ' || o.max_msgs_per_subject || ' (inventory ' || i.max_msgs_per_subject || ')' END
    ], lambda x: x IS NOT NULL) AS differ,
    json_object('replicas', CAST(i.replicas AS VARCHAR), 'retention', i.retention, 'storage', i.storage, 'discard', i.discard,
        'max_age', ns_text(i.max_age_ns), 'max_bytes', CAST(i.max_bytes AS VARCHAR), 'max_msgs', CAST(i.max_msgs AS VARCHAR),
        'max_msgs_per_subject', CAST(i.max_msgs_per_subject AS VARCHAR)) AS expected
    FROM inv i JOIN one o ON o.account = i.acct AND o.stream = i.stream
)
SELECT
    acct || '/' || stream AS entity_key,
    CASE kind WHEN 'kv' THEN 'KV bucket ' || name WHEN 'object_store' THEN 'Object store ' || name ELSE 'Stream ' || stream END
        || ' on ' || server_name || ': ' || array_to_string(differ, ', ') || '.' AS detail,
    len(differ) AS value,
    expected AS related_config,
    'inventory' AS related_source
FROM diff
WHERE len(differ) > 0
