/*
code: server-009
name: Slow consumers
entity: server
category: clients
severity: warning
severity_rule: critical when the route, gateway or leaf slow consumer counters grew, indicating pressure on traffic between servers
kind: operational
lookback: 15m
description: >-
  The server's slow consumer counter rose within the lookback: it closed or flagged connections that could not
  read their messages fast enough. The aggregate includes clients, routes, gateways and leaf connections.
  Retries may increment the counter without a disconnect. It does not measure JetStream consumer backlog.
  Counted per server process, so a restart starts the count again.
remediation: >-
  client-003 names the clients that were disconnected as slow consumers, with the reason. A client is a slow
  consumer when it does not read its socket fast enough: a pod short of CPU, a subscriber doing blocking work in its
  message handler, or a burst larger than the client's pending limits. Give the client more CPU, process messages
  off the handler, or move a heavy fan-out subscription to a JetStream pull consumer. A route slow consumer points
  at the network or CPU between NATS pods.
related_config: [lookback, write_deadline, max_pending]
value_name: slow consumers
value_basis: growth of the slow consumer counter within the lookback
*/
WITH cur AS (
    SELECT st.server_id, s.server_name, st.slow_consumers, st.slow_consumers_clients, st.slow_consumers_routes,
        st.slow_consumers_gateways, st.slow_consumers_leafs
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch')
),
base AS (
    SELECT server_id, arg_min(slow_consumers, epoch) AS sc, arg_min(slow_consumers_clients, epoch) AS clients,
        arg_min(slow_consumers_routes, epoch) AS routes, arg_min(slow_consumers_gateways, epoch) AS gateways,
        arg_min(slow_consumers_leafs, epoch) AS leafs
    FROM server_stats
    WHERE epoch >= getvariable('window_epoch') AND epoch < getvariable('epoch')
      AND server_id IN (SELECT server_id FROM cur)
    GROUP BY server_id
),
d AS (
    SELECT c.server_name, c.slow_consumers - b.sc AS total,
        coalesce(c.slow_consumers_clients - b.clients, 0) AS clients,
        coalesce(c.slow_consumers_routes - b.routes, 0) + coalesce(c.slow_consumers_gateways - b.gateways, 0)
            + coalesce(c.slow_consumers_leafs - b.leafs, 0) AS links
    FROM cur c JOIN base b USING (server_id)
)
SELECT
    server_name AS entity_key,
    CASE WHEN links > 0 THEN 'critical' ELSE 'warning' END AS severity,
    server_name || ': ' || total || ' slow consumers in the last ' || getvariable('lookback_text')
        || ' (' || clients || ' clients, ' || links || ' routes, gateways or leaf nodes).' AS detail,
    total AS value
FROM d
WHERE total > 0
