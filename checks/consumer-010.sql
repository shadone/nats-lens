/*
code: consumer-010
name: Expected consumer missing
entity: consumer
category: inventory
severity: critical
severity_rule: >-
  while a server that held the consumer when it was last reported (any server, if it never was) is not ready, the
  consumer may still be there: info when that server is restarting or announced its shutdown, warning when it did
  not answer or has JetStream disabled. A delete event since its last report makes it critical in any case
kind: operational
description: >-
  A durable consumer listed in the inventory file is reported by no server. When the events hold the
  consumer.deleted advisory (or the stream.deleted advisory of its stream), the finding names the time and the
  client that deleted it.
remediation: >-
  Do not restart the owning service as the first move: a service that recreates a missing durable with
  deliver_policy all replays the stream's whole retention, so everything still in the stream is processed a second
  time. Recreate the durable from its previous configuration with deliver_policy by_start_time and an
  opt_start_time just before the last-reported time in the detail; a consumer whose processing is idempotent (a
  cache projector whose writes are last-write-wins, for example) may be recreated with deliver_policy all. Then
  check that its delivered sequence advances; running clients pull by name and resume on their own. A durable
  created with its stream rather than by a service comes back from the stream's definition. If a client deleted
  it, the detail names the client: a worker that deletes the shared durable in its shutdown path removes it on
  every pod restart, so fix that code first. A consumer whose stream is missing too (stream-011) comes back with
  the stream.
related_config: [inventory_file]
*/
WITH inv AS (
    SELECT i.*,
        coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                  ORDER BY (a.account = i.account) DESC LIMIT 1), i.account) AS acct
    FROM inventory i WHERE i.kind = 'consumer'
),
present AS (
    SELECT DISTINCT account, stream, consumer FROM consumer_stats WHERE epoch = getvariable('epoch')
),
missing AS (
    SELECT i.*, l.last_epoch, (SELECT started_at FROM epochs WHERE epoch = l.last_epoch) AS last_seen_at
    FROM inv i
    LEFT JOIN (SELECT account, stream, consumer, max(epoch) AS last_epoch FROM consumer_stats
               WHERE epoch < getvariable('epoch') GROUP BY account, stream, consumer) l
      ON l.account = i.acct AND l.stream = i.stream AND l.consumer = i.consumer
    WHERE NOT EXISTS (SELECT 1 FROM present p WHERE p.account = i.acct AND p.stream = i.stream AND p.consumer = i.consumer)
),
-- The servers that held the consumer when it was last reported; those that
-- are not ready now may still hold it. Without a report, any server may.
holders AS (
    SELECT m.acct, m.stream, m.consumer, list(DISTINCT s.server_name) AS names
    FROM missing m JOIN consumer_stats x
      ON x.account = m.acct AND x.stream = m.stream AND x.consumer = m.consumer AND x.epoch = m.last_epoch
    JOIN servers s USING (server_id)
    GROUP BY m.acct, m.stream, m.consumer
),
gap AS (
    SELECT m.acct, m.stream, m.consumer, list(n.server_name ORDER BY n.server_name) AS names,
        list(n.why ORDER BY n.server_name) AS whys, bool_and(n.planned) AS planned
    FROM missing m LEFT JOIN holders h USING (acct, stream, consumer)
    JOIN not_ready(getvariable('epoch')) n ON (h.names IS NULL OR list_contains(h.names, n.server_name)) AND NOT n.ready_again
    GROUP BY m.acct, m.stream, m.consumer
),
cause AS (
    SELECT m.acct, m.stream, m.consumer, e.id, e.time, e.kind, e.client_name, e.client_host
    FROM missing m JOIN events e
      ON e.stream = m.stream AND (e.account IS NULL OR e.account IN (m.account, m.acct))
     AND ((e.kind = 'consumer.deleted' AND e.consumer = m.consumer) OR e.kind = 'stream.deleted')
     AND e.received_at <= getvariable('epoch_time')
     AND e.received_at >= coalesce(m.last_seen_at - INTERVAL 30 SECOND, TIMESTAMP '1970-01-01')
    QUALIFY row_number() OVER (PARTITION BY m.acct, m.stream, m.consumer
        ORDER BY (e.kind = 'consumer.deleted') DESC, e.received_at DESC, e.id DESC) = 1
)
SELECT
    m.acct || '/' || m.stream || '/' || m.consumer AS entity_key,
    CASE WHEN c.id IS NOT NULL OR g.names IS NULL
            OR (h.names IS NOT NULL AND len(h.names) - len(g.names) >= len(h.names) // 2 + 1) THEN 'critical'
        WHEN g.planned THEN 'info' ELSE 'warning' END AS severity,
    'Consumer ' || m.consumer || ' on ' || m.stream || ' is in the inventory and no server reports it; '
        || CASE WHEN m.last_seen_at IS NOT NULL THEN 'last reported by ' || coalesce(array_to_string(list_sort(h.names), ', '), 'a server')
                || ' at ' || strftime(m.last_seen_at, '%Y-%m-%d %H:%M:%S') || ' UTC'
            ELSE 'not reported within the retention window' END
        || CASE WHEN c.id IS NULL AND (h.names IS NULL OR len(h.names) - len(g.names) < len(h.names) // 2 + 1)
            THEN gap_text(g.names, g.whys) ELSE '' END || '. '
        || CASE
            WHEN c.id IS NOT NULL THEN CASE WHEN c.kind = 'stream.deleted' THEN 'Its stream was deleted ' ELSE 'Deleted ' END
                || strftime(c.time, '%Y-%m-%d %H:%M:%S') || ' UTC by '
                || coalesce(c.client_name, 'an unnamed client') || coalesce(' (' || c.client_host || ')', '') || '.'
            ELSE 'No delete event recorded.'
        END AS detail,
    c.id AS cause_event_id,
    json_object('durable_name', m.consumer, 'ack_wait', ns_text(m.ack_wait_ns), 'max_deliver', CAST(m.max_deliver AS VARCHAR),
        'max_ack_pending', CAST(m.max_ack_pending AS VARCHAR), 'filter_subjects', array_to_string(m.filter_subjects, ', '),
        'replicas', CAST(m.replicas AS VARCHAR)) AS related_config,
    'inventory' AS related_source
FROM missing m
LEFT JOIN cause c USING (acct, stream, consumer)
LEFT JOIN gap g USING (acct, stream, consumer)
LEFT JOIN holders h USING (acct, stream, consumer)
