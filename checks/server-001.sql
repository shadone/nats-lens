/*
code: server-001
name: Server not reporting
entity: server
category: health
severity: warning
severity_rule: >-
  info while the server announced lame duck or shutdown before it went away, within restart_window (a planned
  restart); critical when at least half of the expected servers are missing, as the JetStream meta group then has no
  quorum
kind: operational
description: >-
  A server that answered within server_forget_after, or that another server still lists as a route or
  JetStream meta peer, answered no monitoring request in this epoch. A server that announced lame duck or shutdown
  first is reported at info for restart_window, as a rolling restart takes it away on purpose. With half the
  servers or more missing, the detail counts the replicated streams and consumers left without a quorum, the cause
  of their stream-001 and consumer-001 findings.
remediation: >-
  Check the server's pod (`kubectl get pod <name> -o wide` in the NATS namespace) and its log. During a GKE node
  upgrade the pod is evicted and rescheduled; it should answer again within a few minutes, and the restart shows as
  server-004 once it is back. If the pod runs and is ready, the server answers clients but not the system account:
  check its routes (`nats server ls` with the system user) and the collector's connection events. A server removed
  for good stays expected until server_forget_after (default 1h) has passed and no peer lists it.
related_config: [server_forget_after]
value_name: minutes since last answer
value_basis: time since the server last answered
*/
WITH e AS (
    SELECT * FROM epochs WHERE epoch = getvariable('epoch')
),
missing AS (
    SELECT unnest(missing_names) AS name, expected_servers, len(missing_names) AS n_missing FROM e
),
last_seen AS (
    SELECT server_name, max(last_seen_at) AS last_at FROM servers GROUP BY server_name
),
planned AS (
    SELECT * FROM planned_down(getvariable('epoch'))
),
-- Replicated streams and consumers with fewer than a quorum of replicas
-- answering in this epoch: the per-asset quorum findings this outage causes.
scfg AS (
    SELECT account, stream, max(replicas) AS r FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch') GROUP BY account, stream
),
lost AS (
    SELECT
        (SELECT count(*) FROM scfg c WHERE c.r > 1 AND (SELECT count(*) FROM stream_stats x
            WHERE x.epoch = getvariable('epoch') AND x.account = c.account AND x.stream = c.stream) < c.r // 2 + 1) AS streams,
        (SELECT count(*) FROM (
            SELECT k.account, k.stream, k.consumer, max(CASE WHEN k.replicas > 0 THEN k.replicas ELSE c.r END) AS r
            FROM consumer_config k LEFT JOIN scfg c USING (account, stream)
            WHERE k.first_epoch <= getvariable('epoch') AND k.last_epoch >= getvariable('epoch')
            GROUP BY k.account, k.stream, k.consumer) k
         WHERE k.r > 1 AND (SELECT count(*) FROM consumer_stats x WHERE x.epoch = getvariable('epoch')
            AND x.account = k.account AND x.stream = k.stream AND x.consumer = k.consumer) < k.r // 2 + 1) AS consumers
)
SELECT
    m.name AS entity_key,
    CASE WHEN 2 * m.n_missing >= m.expected_servers THEN 'critical' WHEN p.server_name IS NOT NULL THEN 'info' ELSE 'warning' END AS severity,
    m.name || ' answered no monitoring request; ' || m.n_missing || ' of ' || m.expected_servers
        || ' expected servers missing'
        || coalesce('; last answered ' || strftime(l.last_at, '%Y-%m-%d %H:%M:%S') || ' UTC', '; never answered')
        || coalesce('; announced ' || replace(replace(p.kind, 'server.lameduck', 'lame duck'), 'server.shutdown', 'shutdown')
            || ' at ' || strftime(p.announced_at, '%H:%M:%S') || ' UTC', '')
        || CASE WHEN 2 * m.n_missing >= m.expected_servers AND (lo.streams > 0 OR lo.consumers > 0)
            THEN '; ' || lo.streams || CASE WHEN lo.streams = 1 THEN ' replicated stream and ' ELSE ' replicated streams and ' END
                || lo.consumers || CASE WHEN lo.consumers = 1 THEN ' consumer have' ELSE ' consumers have' END
                || ' fewer than a quorum of replicas answering (stream-001, consumer-001)' ELSE '' END
        || '.' AS detail,
    round(epoch(getvariable('epoch_time') - l.last_at) / 60, 1) AS value
FROM missing m
LEFT JOIN last_seen l ON l.server_name = m.name
LEFT JOIN planned p ON p.server_name = m.name
CROSS JOIN lost lo
