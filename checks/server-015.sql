/*
code: server-015
name: Stale connections
entity: server
category: clients
severity: info
kind: operational
lookback: 15m
description: >-
  The server closed connections as stale within the lookback: the client answered none of the server's pings
  (ping_interval, by default 2 minutes, times ping_max, by default 2). The client was gone without closing its
  connection, or too busy to answer.
remediation: >-
  A pod killed without a clean shutdown (OOMKilled, a node lost during a GKE upgrade) leaves its connection to go
  stale; the client.disconnect events with reason "Stale Connection" name it. Many stale connections from a pod that
  is still running mean its event loop or threads are blocked for minutes; look at its CPU and logs.
related_config: [lookback, ping_interval, ping_max]
value_name: stale connections
value_basis: growth of the stale connections counter within the lookback
*/
WITH cur AS (
    SELECT st.server_id, s.server_name, st.stale_connections
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.stale_connections IS NOT NULL
),
base AS (
    SELECT server_id, arg_min(stale_connections, epoch) AS stale
    FROM server_stats
    WHERE epoch >= getvariable('window_epoch') AND epoch < getvariable('epoch')
      AND server_id IN (SELECT server_id FROM cur)
    GROUP BY server_id
)
SELECT
    c.server_name AS entity_key,
    c.server_name || ': ' || (c.stale_connections - b.stale) || ' connections closed as stale in the last '
        || getvariable('lookback_text') || '.' AS detail,
    c.stale_connections - b.stale AS value
FROM cur c JOIN base b USING (server_id)
WHERE c.stale_connections - b.stale > 0
