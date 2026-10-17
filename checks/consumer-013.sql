/*
code: consumer-013
name: Durable consumer disappeared
entity: consumer
category: agreement
severity: critical
severity_rule: >-
  a consumer.deleted event since the last report is critical whatever the servers' state; a stream.deleted event of
  its stream, which has been created again since, is warning; without an event, info while a server that held it is
  restarting or announced its shutdown and warning while one did not answer or has JetStream disabled, as long as
  the ready holders that lack it are fewer than a quorum of its holders; critical once they are a quorum
kind: operational
lookback: 1h
description: >-
  A durable consumer that a server reported within the lookback, or later than the server that held it was last
  seen ready, is reported by none now while its stream is still there. A consumer.deleted advisory since its last
  report names the time and the client that deleted it, whatever the state of the servers; without one, the
  finding says which servers that held it may still hold it. Internal consumers
  (JS_ names of mirrors and sources), ephemeral and ordered consumers, durables with an inactive threshold (the
  server removes those by design) and durables in the inventory (consumer-010) are left out. It stays firing for
  the lookback, as long as a server that held it stays away, or until the durable is back. The detail names
  any time nats-lens was not collecting since the last report, when a delete would have left no event.
remediation: >-
  Do not restart the owning service as the first move: a service that recreates a missing durable with
  deliver_policy all replays the stream's whole retention, so everything still in the stream is processed a second
  time. Recreate the durable from its previous configuration with deliver_policy by_start_time and an
  opt_start_time just before the last-reported time in the detail; a consumer whose processing is idempotent (a
  cache projector whose writes are last-write-wins, for example) may be recreated with deliver_policy all. Then
  check that its delivered sequence advances; running clients pull by name and resume on their own.
  If the detail names a client, find out why it deleted the durable first: a worker that deletes
  the shared durable in its shutdown path removes it on every pod restart. If no delete event was captured, look on
  the Events page for consumer.deleted around the last-reported time. Add durables that must exist to the inventory
  file, so consumer-010 keeps reporting them.
related_config: [lookback]
value_name: minutes since last reported
value_basis: time since a server last reported the durable
*/
WITH present AS (
    SELECT DISTINCT account, stream, consumer FROM consumer_stats WHERE epoch = getvariable('epoch')
),
streams_now AS (
    SELECT DISTINCT account, stream FROM stream_stats WHERE epoch = getvariable('epoch')
),
last_report AS (
    SELECT account, stream, consumer, max(epoch) AS last_epoch
    FROM consumer_stats
    WHERE epoch < getvariable('epoch')
    GROUP BY account, stream, consumer
),
durables AS (
    SELECT r.*
    FROM last_report r
    WHERE (r.account, r.stream) IN (SELECT account, stream FROM streams_now)
      AND NOT EXISTS (SELECT 1 FROM present p WHERE p.account = r.account AND p.stream = r.stream AND p.consumer = r.consumer)
      AND r.consumer NOT LIKE 'JS\_%' ESCAPE '\'
      AND EXISTS (SELECT 1 FROM consumer_config k WHERE k.account = r.account AND k.stream = r.stream AND k.consumer = r.consumer
          AND k.last_epoch = r.last_epoch AND coalesce(k.durable_name, '') <> '' AND coalesce(k.inactive_threshold_ns, 0) = 0)
      AND NOT EXISTS (SELECT 1 FROM inventory i WHERE i.kind = 'consumer' AND i.stream = r.stream AND i.consumer = r.consumer
          AND (i.account = r.account OR i.account IN (SELECT account_name FROM accounts WHERE account = r.account)))
),
holders AS (
    SELECT d.account, d.stream, d.consumer, d.last_epoch, s.server_name
    FROM durables d JOIN consumer_stats x
      ON x.account = d.account AND x.stream = d.stream AND x.consumer = d.consumer AND x.epoch = d.last_epoch
    JOIN servers s USING (server_id)
),
-- Holders not ready now, and whether they have stayed away since the last
-- report (never answered JSZ since): those keep the durable a candidate
-- past the lookback.
holder_count AS (
    SELECT account, stream, consumer, count(*) AS n FROM holders GROUP BY account, stream, consumer
),
-- Holders that are away (not ready, and not merely back from a restart and
-- answering), and whether they have stayed away since the last report
-- (never answered JSZ since): those keep the asset a candidate past the
-- lookback. The finding is demoted while the ready holders that lack the
-- asset are fewer than a quorum of the holders.
waiting AS (
    SELECT h.account, h.stream, h.consumer, list(h.server_name ORDER BY h.server_name) AS names,
        list(n.why ORDER BY h.server_name) AS whys, bool_and(n.planned) AS planned,
        bool_or(NOT EXISTS (SELECT 1 FROM epoch_endpoints e JOIN servers s USING (server_id)
            WHERE s.server_name = h.server_name AND e.endpoint = 'JSZ' AND e.state = 'ok'
              AND e.epoch > h.last_epoch AND e.epoch <= getvariable('epoch'))) AS away_since,
        count(*) AS away
    FROM holders h JOIN not_ready(getvariable('epoch')) n USING (server_name)
    WHERE NOT n.ready_again
    GROUP BY h.account, h.stream, h.consumer
),
gone AS (
    SELECT d.*, (SELECT started_at FROM epochs WHERE epoch = d.last_epoch) AS last_seen_at
    FROM durables d LEFT JOIN waiting w USING (account, stream, consumer)
    WHERE d.last_epoch >= getvariable('window_epoch') OR coalesce(w.away_since, false)
),
-- Times the collector was not running between the last report and now: a
-- delete then left no event.
collector_gaps AS (
    SELECT g.account, g.stream, g.consumer, string_agg('nats-lens was not collecting from ' || strftime(e.time, '%Y-%m-%d %H:%M:%S') || ' to '
            || strftime(e.received_at, '%H:%M:%S') || ' UTC', '; ' ORDER BY e.time) AS text
    FROM gone g JOIN events e
      ON e.kind = 'collector.gap' AND e.received_at <= getvariable('epoch_time') AND e.received_at > g.last_seen_at
    GROUP BY g.account, g.stream, g.consumer
),
cause AS (
    SELECT g.account, g.stream, g.consumer, e.id, e.time, e.kind, e.client_name, e.client_host
    FROM gone g JOIN events e
      ON e.stream = g.stream AND ((e.kind = 'consumer.deleted' AND e.consumer = g.consumer) OR e.kind = 'stream.deleted')
     AND (e.account IS NULL OR e.account = g.account OR e.account IN (SELECT account_name FROM accounts WHERE account = g.account))
     AND e.received_at <= getvariable('epoch_time') AND e.received_at >= g.last_seen_at - INTERVAL 30 SECOND
    QUALIFY row_number() OVER (PARTITION BY g.account, g.stream, g.consumer
        ORDER BY (e.kind = 'consumer.deleted') DESC, e.received_at DESC, e.id DESC) = 1
)
SELECT
    g.account || '/' || g.stream || '/' || g.consumer AS entity_key,
    CASE WHEN c.kind = 'consumer.deleted' THEN 'critical'
        WHEN c.kind = 'stream.deleted' THEN 'warning'
        WHEN w.names IS NULL OR hc.n - w.away >= hc.n // 2 + 1 THEN 'critical'
        WHEN w.planned THEN 'info' ELSE 'warning' END AS severity,
    'Durable ' || g.consumer || ' on ' || g.stream || ' was last reported by ' || coalesce(hn.names, 'a server') || ' at '
        || strftime(g.last_seen_at, '%Y-%m-%d %H:%M:%S') || ' UTC and no server reports it now'
        || CASE WHEN c.id IS NULL AND hc.n - w.away < hc.n // 2 + 1 THEN gap_text(w.names, w.whys) ELSE '' END || '. '
        || CASE
            WHEN c.kind = 'consumer.deleted' THEN 'Deleted ' || strftime(c.time, '%Y-%m-%d %H:%M:%S') || ' UTC by '
                || coalesce(c.client_name, 'an unnamed client') || coalesce(' (' || c.client_host || ')', '') || '.'
            WHEN c.kind = 'stream.deleted' THEN 'Its stream was deleted ' || strftime(c.time, '%Y-%m-%d %H:%M:%S') || ' UTC by '
                || coalesce(c.client_name, 'an unnamed client') || coalesce(' (' || c.client_host || ')', '')
                || ', and the stream was created again without it.'
            ELSE 'No delete event recorded; the durable may have been renamed or removed by the server.' END
        || coalesce(' ' || cg.text || '.', '') AS detail,
    round(epoch(getvariable('epoch_time') - g.last_seen_at) / 60, 1) AS value,
    c.id AS cause_event_id
FROM gone g
LEFT JOIN holder_count hc USING (account, stream, consumer)
LEFT JOIN (SELECT account, stream, consumer, array_to_string(list(DISTINCT server_name ORDER BY server_name), ', ') AS names
           FROM holders GROUP BY account, stream, consumer) hn USING (account, stream, consumer)
LEFT JOIN waiting w USING (account, stream, consumer)
LEFT JOIN collector_gaps cg USING (account, stream, consumer)
LEFT JOIN cause c USING (account, stream, consumer)
