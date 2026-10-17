/*
code: stream-005
name: Stream without any limit
entity: stream
category: storage
severity: info
kind: operational
description: >-
  The stream has no max_msgs, max_bytes or TTL (max_age), so it grows until the JetStream store of its servers is
  full. Deliberate for an archive, but then its growth needs watching (server-014). This holds for KV buckets and
  object stores too: a bucket's history per key is bounded, but its keys are not. Not reported: a stream the
  inventory declares unbounded on purpose (max_age 0 and max_bytes -1, both given explicitly), and a KV bucket the
  inventory lists that allows per-key TTLs (allow_msg_ttl), whose writers bound its keys. Another bucket that allows
  them is reported, saying so: nats-lens cannot see whether its writers set them.
remediation: >-
  If the stream needs no unlimited history, set max_age or max_bytes in its definition (a NACK resource, or
  wherever the stream is declared). If unbounded growth is intended, as for an archive stream,
  declare it in the inventory with max_age 0 and max_bytes -1 (or mute this finding with that
  reason and an expiry), and keep an eye on server-014 for the date the store fills. For a KV bucket or object
  store, set a TTL (`nats kv add --ttl`, or the bucket's definition) or max_bytes; its keys accumulate without
  bound otherwise.
related_config: [max_msgs, max_bytes, max_age, max_msgs_per_subject, allow_msg_ttl]
*/
WITH cur AS (
    SELECT c.*, s.server_name, coalesce(ss.is_leader, false) AS lead
    FROM stream_config c
    JOIN servers s USING (server_id)
    LEFT JOIN stream_stats ss
      ON ss.epoch = getvariable('epoch') AND ss.server_id = c.server_id AND ss.account = c.account AND ss.stream = c.stream
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
),
one AS (
    SELECT * FROM cur
    QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY lead DESC, server_name) = 1
),
-- Streams the inventory declares unbounded on purpose; the inventory names
-- the account as the servers do or by its readable name.
unbounded AS (
    SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                     ORDER BY a.account = i.account DESC LIMIT 1), i.account) AS account, i.stream
    FROM inventory i
    WHERE i.kind IN ('stream', 'kv', 'object_store') AND i.max_age_ns = 0 AND i.max_bytes = -1
),
-- KV buckets the inventory lists: one that allows per-message TTLs is then
-- taken to be bounded by its writers.
listed_kv AS (
    SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                     ORDER BY a.account = i.account DESC LIMIT 1), i.account) AS account, i.stream
    FROM inventory i WHERE i.kind = 'kv'
)
SELECT
    account || '/' || stream AS entity_key,
    CASE
        WHEN stream LIKE 'KV\_%' ESCAPE '\' THEN 'KV bucket ' || stream[4:]
            || ': keys accumulate without bound; set a TTL or max_bytes, or mute if intended (max_bytes '
            || max_bytes || ', TTL ' || ns_text(max_age_ns)
            || CASE WHEN coalesce(max_msgs_per_subject, -1) > 0 THEN ', history ' || max_msgs_per_subject || ' per key' ELSE '' END
            || CASE WHEN coalesce(allow_msg_ttl, false)
                    THEN '); it allows per-message TTLs; nats-lens cannot see whether writers set them.'
                    ELSE ', no per-key TTLs).' END
        WHEN stream LIKE 'OBJ\_%' ESCAPE '\' THEN 'Object store ' || stream[5:]
            || ': objects accumulate without bound; set a TTL or max_bytes, or mute if intended (max_bytes '
            || max_bytes || ', TTL ' || ns_text(max_age_ns) || ').'
        ELSE 'max_msgs ' || max_msgs || ', max_bytes ' || max_bytes || ', max_age ' || ns_text(max_age_ns)
            || ': nothing bounds the stream.'
    END AS detail,
    json_object('max_msgs', CAST(max_msgs AS VARCHAR), 'max_bytes', CAST(max_bytes AS VARCHAR),
        'max_age', ns_text(max_age_ns), 'max_msgs_per_subject', CAST(max_msgs_per_subject AS VARCHAR),
        'allow_msg_ttl', CAST(coalesce(allow_msg_ttl, false) AS VARCHAR)) AS related_config,
    server_name AS related_source
FROM one
WHERE coalesce(max_msgs, -1) <= 0 AND coalesce(max_bytes, -1) <= 0 AND coalesce(max_age_ns, 0) = 0
  AND (account, stream) NOT IN (SELECT account, stream FROM unbounded)
  AND NOT (stream LIKE 'KV\_%' ESCAPE '\' AND coalesce(allow_msg_ttl, false)
           AND (account, stream) IN (SELECT account, stream FROM listed_kv))
