/*
code: stream-014
name: Stream disappeared
entity: stream
category: agreement
severity: critical
severity_rule: >-
  a stream.deleted event since the last report is critical whatever the servers' state; without one, info while a
  server that held it is restarting or announced its shutdown, warning while one did not answer or has JetStream
  disabled, as long as the ready holders that lack it are fewer than a quorum of its holders; critical once they are
  a quorum
kind: operational
lookback: 1h
description: >-
  A stream, KV bucket or object store that a server reported within the lookback, or later than the server that
  held it was last seen ready, is reported by none now. A stream.deleted advisory since its last report names the
  time and the client that deleted it, whatever the state of the servers; without one, the finding says which
  servers that held it may still hold it. Streams in the inventory are left out (stream-011 reports them). It stays
  firing for the lookback, as long as a server that held it stays away, or until the stream is back. The detail names
  any time nats-lens was not collecting since the last report, when a delete would have left no event.
remediation: >-
  Deleting a stream deletes its messages and its consumers. If the detail names a client, find out why it deleted
  the stream (a cleanup job, a test pointed at the wrong cluster) before recreating it from its definition (a NACK
  resource, or wherever the stream is declared). Add streams and buckets that must exist to the inventory
  file, so stream-011 keeps reporting them.
related_config: [lookback]
value_name: minutes since last reported
value_basis: time since a server last reported the stream
*/
WITH present AS (
    SELECT DISTINCT account, stream FROM stream_stats WHERE epoch = getvariable('epoch')
),
last_report AS (
    SELECT account, stream, max(epoch) AS last_epoch
    FROM stream_stats
    WHERE epoch < getvariable('epoch')
    GROUP BY account, stream
),
candidates AS (
    SELECT r.* FROM last_report r
    WHERE NOT EXISTS (SELECT 1 FROM present p WHERE p.account = r.account AND p.stream = r.stream)
      AND NOT EXISTS (SELECT 1 FROM inventory i WHERE i.kind <> 'consumer' AND i.stream = r.stream
          AND (i.account = r.account OR i.account IN (SELECT account_name FROM accounts WHERE account = r.account)))
),
holders AS (
    SELECT c.account, c.stream, c.last_epoch, s.server_name
    FROM candidates c JOIN stream_stats x ON x.account = c.account AND x.stream = c.stream AND x.epoch = c.last_epoch
    JOIN servers s USING (server_id)
),
-- Holders not ready now, and whether they have stayed away since the last
-- report: those keep the stream a candidate past the lookback.
holder_count AS (
    SELECT account, stream, count(*) AS n FROM holders GROUP BY account, stream
),
-- Holders that are away (not ready, and not merely back from a restart and
-- answering), and whether they have stayed away since the last report
-- (never answered JSZ since): those keep the asset a candidate past the
-- lookback. The finding is demoted while the ready holders that lack the
-- asset are fewer than a quorum of the holders.
waiting AS (
    SELECT h.account, h.stream, list(h.server_name ORDER BY h.server_name) AS names,
        list(n.why ORDER BY h.server_name) AS whys, bool_and(n.planned) AS planned,
        bool_or(NOT EXISTS (SELECT 1 FROM epoch_endpoints e JOIN servers s USING (server_id)
            WHERE s.server_name = h.server_name AND e.endpoint = 'JSZ' AND e.state = 'ok'
              AND e.epoch > h.last_epoch AND e.epoch <= getvariable('epoch'))) AS away_since,
        count(*) AS away
    FROM holders h JOIN not_ready(getvariable('epoch')) n USING (server_name)
    WHERE NOT n.ready_again
    GROUP BY h.account, h.stream
),
gone AS (
    SELECT c.*, (SELECT started_at FROM epochs WHERE epoch = c.last_epoch) AS last_seen_at
    FROM candidates c LEFT JOIN waiting w USING (account, stream)
    WHERE c.last_epoch >= getvariable('window_epoch') OR coalesce(w.away_since, false)
),
-- Times the collector was not running between the last report and now: a
-- delete then left no event.
collector_gaps AS (
    SELECT g.account, g.stream, string_agg('nats-lens was not collecting from ' || strftime(e.time, '%Y-%m-%d %H:%M:%S') || ' to '
            || strftime(e.received_at, '%H:%M:%S') || ' UTC', '; ' ORDER BY e.time) AS text
    FROM gone g JOIN events e
      ON e.kind = 'collector.gap' AND e.received_at <= getvariable('epoch_time') AND e.received_at > g.last_seen_at
    GROUP BY g.account, g.stream
),
cause AS (
    SELECT g.account, g.stream, e.id, e.time, e.client_name, e.client_host
    FROM gone g JOIN events e
      ON e.kind = 'stream.deleted' AND e.stream = g.stream
     AND (e.account IS NULL OR e.account = g.account OR e.account IN (SELECT account_name FROM accounts WHERE account = g.account))
     AND e.received_at <= getvariable('epoch_time') AND e.received_at >= g.last_seen_at - INTERVAL 30 SECOND
    QUALIFY row_number() OVER (PARTITION BY g.account, g.stream ORDER BY e.received_at DESC, e.id DESC) = 1
)
SELECT
    g.account || '/' || g.stream AS entity_key,
    CASE WHEN c.id IS NOT NULL OR w.names IS NULL OR hc.n - w.away >= hc.n // 2 + 1 THEN 'critical'
        WHEN w.planned THEN 'info' ELSE 'warning' END AS severity,
    CASE WHEN g.stream LIKE 'KV\_%' ESCAPE '\' THEN 'KV bucket ' || g.stream[4:] || ' (stream ' || g.stream || ')'
        WHEN g.stream LIKE 'OBJ\_%' ESCAPE '\' THEN 'Object store ' || g.stream[5:] || ' (stream ' || g.stream || ')'
        ELSE 'Stream ' || g.stream END
        || ' was last reported by ' || coalesce(hn.names, 'a server') || ' at ' || strftime(g.last_seen_at, '%Y-%m-%d %H:%M:%S')
        || ' UTC and no server reports it now'
        || CASE WHEN c.id IS NULL AND hc.n - w.away < hc.n // 2 + 1 THEN gap_text(w.names, w.whys) ELSE '' END || '. '
        || CASE WHEN c.id IS NOT NULL THEN 'Deleted ' || strftime(c.time, '%Y-%m-%d %H:%M:%S') || ' UTC by '
                || coalesce(c.client_name, 'an unnamed client') || coalesce(' (' || c.client_host || ')', '') || '.'
            ELSE 'No delete event recorded; the stream may have been removed by the server or a tool that sends no advisory.' END
        || coalesce(' ' || cg.text || '.', '') AS detail,
    round(epoch(getvariable('epoch_time') - g.last_seen_at) / 60, 1) AS value,
    c.id AS cause_event_id
FROM gone g
LEFT JOIN holder_count hc USING (account, stream)
LEFT JOIN (SELECT account, stream, array_to_string(list(DISTINCT server_name ORDER BY server_name), ', ') AS names
           FROM holders GROUP BY account, stream) hn USING (account, stream)
LEFT JOIN waiting w USING (account, stream)
LEFT JOIN collector_gaps cg USING (account, stream)
LEFT JOIN cause c USING (account, stream)
