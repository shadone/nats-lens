/*
code: stream-011
name: Expected stream or bucket missing
entity: stream
category: inventory
severity: critical
severity_rule: >-
  while a server that held the stream when it was last reported (any server, if it never was) is not ready, the
  stream may still be there: info when that server is restarting or announced its shutdown, warning when it did not
  answer or has JetStream disabled. A delete event since its last report makes it critical in any case
kind: operational
description: >-
  A stream, KV bucket or object store listed in the inventory file is reported by no server. When the events hold
  the stream.deleted advisory, the finding names the time and the client that deleted it.
remediation: >-
  Recreate it from its definition: NACK recreates what it declares (check the NACK controller's log if it did
  not); otherwise apply the definition the stream or bucket was created from. Services that only bind to
  an existing bucket fail their calls until it exists.
  If a client deleted it, the detail names the client: find why it did (a cleanup job, a test pointed at the wrong
  cluster) before recreating, or it happens again.
related_config: [inventory_file]
*/
WITH inv AS (
    SELECT i.*,
        coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                  ORDER BY (a.account = i.account) DESC LIMIT 1), i.account) AS acct
    FROM inventory i WHERE i.kind IN ('stream', 'kv', 'object_store')
),
present AS (
    SELECT DISTINCT account, stream FROM stream_stats WHERE epoch = getvariable('epoch')
),
missing AS (
    SELECT i.*, l.last_epoch, (SELECT started_at FROM epochs WHERE epoch = l.last_epoch) AS last_seen_at
    FROM inv i
    LEFT JOIN (SELECT account, stream, max(epoch) AS last_epoch FROM stream_stats
               WHERE epoch < getvariable('epoch') GROUP BY account, stream) l ON l.account = i.acct AND l.stream = i.stream
    WHERE NOT EXISTS (SELECT 1 FROM present p WHERE p.account = i.acct AND p.stream = i.stream)
),
-- The servers that held the stream when it was last reported; those that
-- are not ready now may still hold it. Without a report, any server may.
holders AS (
    SELECT m.acct, m.stream, list(DISTINCT s.server_name) AS names
    FROM missing m JOIN stream_stats x ON x.account = m.acct AND x.stream = m.stream AND x.epoch = m.last_epoch
    JOIN servers s USING (server_id)
    GROUP BY m.acct, m.stream
),
gap AS (
    SELECT m.acct, m.stream, list(n.server_name ORDER BY n.server_name) AS names, list(n.why ORDER BY n.server_name) AS whys,
        bool_and(n.planned) AS planned
    FROM missing m LEFT JOIN holders h USING (acct, stream)
    JOIN not_ready(getvariable('epoch')) n ON (h.names IS NULL OR list_contains(h.names, n.server_name)) AND NOT n.ready_again
    GROUP BY m.acct, m.stream
),
cause AS (
    SELECT m.acct, m.stream, e.id, e.time, e.client_name, e.client_host
    FROM missing m JOIN events e
      ON e.kind = 'stream.deleted' AND e.stream = m.stream AND (e.account IS NULL OR e.account IN (m.account, m.acct))
     AND e.received_at <= getvariable('epoch_time')
     AND e.received_at >= coalesce(m.last_seen_at - INTERVAL 30 SECOND, TIMESTAMP '1970-01-01')
    QUALIFY row_number() OVER (PARTITION BY m.acct, m.stream ORDER BY e.received_at DESC, e.id DESC) = 1
)
SELECT
    m.acct || '/' || m.stream AS entity_key,
    CASE WHEN c.id IS NOT NULL OR g.names IS NULL
            OR (h.names IS NOT NULL AND len(h.names) - len(g.names) >= len(h.names) // 2 + 1) THEN 'critical'
        WHEN g.planned THEN 'info' ELSE 'warning' END AS severity,
    CASE m.kind WHEN 'kv' THEN 'KV bucket ' || m.name || ' (stream ' || m.stream || ')'
        WHEN 'object_store' THEN 'Object store ' || m.name || ' (stream ' || m.stream || ')'
        ELSE 'Stream ' || m.stream END
        || ' is in the inventory and no server reports it; '
        || CASE WHEN m.last_seen_at IS NOT NULL THEN 'last reported by ' || coalesce(array_to_string(list_sort(h.names), ', '), 'a server')
                || ' at ' || strftime(m.last_seen_at, '%Y-%m-%d %H:%M:%S') || ' UTC'
            ELSE 'not reported within the retention window' END
        || CASE WHEN c.id IS NULL AND (h.names IS NULL OR len(h.names) - len(g.names) < len(h.names) // 2 + 1)
            THEN gap_text(g.names, g.whys) ELSE '' END || '. '
        || CASE
            WHEN c.id IS NOT NULL THEN 'Deleted ' || strftime(c.time, '%Y-%m-%d %H:%M:%S') || ' UTC by '
                || coalesce(c.client_name, 'an unnamed client') || coalesce(' (' || c.client_host || ')', '') || '.'
            ELSE 'No delete event recorded.'
        END AS detail,
    c.id AS cause_event_id,
    json_object('kind', m.kind, 'replicas', CAST(m.replicas AS VARCHAR), 'max_age', ns_text(m.max_age_ns),
        'max_bytes', CAST(m.max_bytes AS VARCHAR), 'max_msgs', CAST(m.max_msgs AS VARCHAR)) AS related_config,
    'inventory' AS related_source
FROM missing m
LEFT JOIN cause c USING (acct, stream)
LEFT JOIN gap g USING (acct, stream)
LEFT JOIN holders h USING (acct, stream)
