/*
code: server-020
name: Connections near max_connections
entity: server
category: clients
severity: warning
severity_rule: critical at or above crit_ratio of max_connections
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.8"
    unit: ratio
    description: Share of max_connections in use at which the finding fires.
  - name: crit_ratio
    default: "0.95"
    unit: ratio
    description: Share at which it is critical.
  - name: resolve_ratio
    default: "0.9"
    unit: ratio
    description: >-
      A firing finding keeps firing while the share stays at or above this share of warn_ratio, and resolves below
      it, so a count that hovers at the threshold does not flap.
description: >-
  The server holds at least warn_ratio of the client connections its max_connections allows. At the limit it refuses
  every new client connection with "maximum connections exceeded", so clients that reconnect, and new pods, cannot
  connect to this server. The limit is per server; an account's own connection limit is account-001's.
remediation: >-
  Look at who holds the connections: the Connections page filtered to the server, or `nats server request connections
  --name=nats-0` with the system user. Many connections from one service point at a
  connection opened per request or a pool that grows (server-011 for churn). If the load is real, raise
  `max_connections` in the NATS configuration (`nats server request reload` applies it), or spread the clients over
  more servers. A client library connects to another server of the cluster when one refuses it, so the other servers
  fill too.
related_config: [warn_ratio, crit_ratio, resolve_ratio, max_connections]
value_name: share of max_connections in use
value_basis: client connections divided by max_connections
*/
WITH cur AS (
    SELECT st.server_id, s.server_name, st.connections,
        -- max_connections is the process's newest VARZ value: a collection
        -- whose VARZ failed keeps it.
        (SELECT b.max_connections FROM server_stats b
         WHERE b.server_id = st.server_id AND b.epoch <= st.epoch AND b.max_connections IS NOT NULL
         ORDER BY b.epoch DESC LIMIT 1) AS max_connections
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.connections IS NOT NULL
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-020' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- Two processes of one name in a collection: the one with the larger share.
judged AS (
    SELECT server_name, connections, max_connections, connections / max_connections AS ratio
    FROM cur WHERE max_connections > 0
    QUALIFY row_number() OVER (PARTITION BY server_name ORDER BY connections / max_connections DESC, server_id) = 1
)
SELECT
    server_name AS entity_key,
    CASE WHEN ratio >= getvariable('crit_ratio') THEN 'critical' ELSE 'warning' END AS severity,
    server_name || ' holds ' || connections || ' client connections of max_connections ' || max_connections
        || ' (' || regexp_replace(CAST(round(100 * ratio, 1) AS VARCHAR), '\.0$', '') || '%).' AS detail,
    round(ratio, 4) AS value,
    json_object('max_connections', max_connections) AS related_config,
    server_name AS related_source
FROM judged
WHERE ratio >= getvariable('warn_ratio')
   OR (ratio >= getvariable('resolve_ratio') * getvariable('warn_ratio') AND server_name IN (SELECT entity_key FROM firing))
