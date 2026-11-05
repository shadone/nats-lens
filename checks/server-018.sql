/*
code: server-018
name: Bytes queued on a route or gateway
entity: server
category: health
severity: warning
severity_rule: critical at or above crit_ratio of max_pending, a larger backlog held in the server's memory
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.25"
    unit: ratio
    description: Share of the server's max_pending queued on one route or gateway connection at which it counts.
  - name: crit_ratio
    default: "0.5"
    unit: ratio
    description: Share of max_pending at which the finding is critical.
  - name: min_epochs
    default: "3"
    unit: count
    description: Consecutive collections the bytes must stay at or above warn_ratio before the finding fires.
  - name: resolve_ratio
    default: "0.5"
    unit: ratio
    description: >-
      A firing finding keeps firing while a connection holds at least this share of the warn_ratio level, and
      resolves once every connection is below it, so a queue that hovers at the threshold does not flap.
description: >-
  Bytes wait to be written on one of the server's route or gateway connections, at or above warn_ratio of the
  server's max_pending, in each of the last min_epochs collections: the remote end or the network between them does
  not take the traffic as fast as it comes. nats-server holds the backlog in memory and does not cap it: unlike a
  client, a route or gateway is not closed at max_pending, which serves here only as the scale. Messages on the link
  arrive late, and the server's memory grows with the backlog (server-008). A write that takes longer than the write
  deadline counts the link as a slow consumer (server-009); by default the server retries the write and closes the
  link only when nothing could be written by the deadline, while `write_timeout: close` in the cluster or gateway
  block makes it close the link at once.
remediation: >-
  The detail names the connections and how much each holds. Compare with the other end: `nats server report routes`
  or `nats server report gateways` with the system user, and the Topology page's pending bytes and round trips. A
  backlog on every link of one server points at that server (its CPU, `nats server report cpu`); one on a single link
  at the network between the two, or at the remote server. A gateway backlog during a burst between regions can be
  normal; one that lasts minutes is not. Route compression (`compression: s2_auto` in the cluster block) lowers the
  bytes on a slow link. Raising max_pending changes only the scale of this check, not what the server does.
related_config: [warn_ratio, crit_ratio, min_epochs, resolve_ratio, max_pending]
value_name: share of max_pending queued
value_basis: the largest pending bytes of one route or gateway connection divided by the server's max_pending
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- Each link's largest queue, a route by its remote server, a gateway by its
-- cluster and direction; with route pooling one remote has several routes.
links AS (
    SELECT epoch, server_id, 'route to ' || coalesce(nullif(remote_name, ''), remote_id) AS link, max(pending_bytes) AS pending
    FROM routes WHERE epoch IN (SELECT epoch FROM last) AND pending_bytes IS NOT NULL
    GROUP BY ALL
    UNION ALL
    SELECT epoch, server_id, CASE direction WHEN 'inbound' THEN 'inbound gateway from ' ELSE direction || ' gateway to ' END || gateway, max(pending_bytes)
    FROM gateways WHERE epoch IN (SELECT epoch FROM last) AND pending_bytes IS NOT NULL
    GROUP BY ALL
),
-- max_pending is the process's newest VARZ value: a collection whose VARZ
-- failed keeps it.
limited AS (
    SELECT l.*, s.server_name,
        (SELECT st.max_pending FROM server_stats st
         WHERE st.server_id = l.server_id AND st.epoch <= l.epoch AND st.max_pending > 0
         ORDER BY st.epoch DESC LIMIT 1) AS max_pending
    FROM links l JOIN servers s USING (server_id)
),
judged AS (
    SELECT *, pending >= getvariable('warn_ratio') * max_pending AS over,
        pending >= getvariable('resolve_ratio') * getvariable('warn_ratio') * max_pending AS holding
    FROM limited WHERE max_pending IS NOT NULL
),
held AS (
    SELECT server_name, link, count(DISTINCT epoch) FILTER (WHERE over) AS n FROM judged GROUP BY ALL
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-018' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
cur AS (
    SELECT j.server_name, j.link, j.pending, j.max_pending
    FROM judged j JOIN held h USING (server_name, link)
    WHERE j.epoch = getvariable('epoch')
      AND ((j.over AND h.n >= getvariable('min_epochs'))
           OR (j.holding AND j.server_name IN (SELECT entity_key FROM firing)))
),
per AS (
    SELECT server_name, max(pending) AS pending, any_value(max_pending) AS max_pending, count(*) AS n,
        list(format_bytes(pending) || ' on the ' || link || ' ('
            || regexp_replace(CAST(round(100 * pending / max_pending, 1) AS VARCHAR), '\.0$', '') || '%)'
            ORDER BY pending DESC, link)[1:3] AS top
    FROM cur GROUP BY server_name
)
SELECT
    server_name AS entity_key,
    CASE WHEN pending >= getvariable('crit_ratio') * max_pending THEN 'critical' ELSE 'warning' END AS severity,
    server_name || ': ' || array_to_string(top, ', ') || CASE WHEN n > 3 THEN ' (+' || (n - 3) || ' more)' ELSE '' END
        || ' waiting to be written, of max_pending ' || format_bytes(max_pending) || '.' AS detail,
    round(pending / max_pending, 4) AS value,
    json_object('max_pending', format_bytes(max_pending)) AS related_config,
    server_name AS related_source
FROM per
