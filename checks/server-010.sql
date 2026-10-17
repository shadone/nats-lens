/*
code: server-010
name: Stalled clients
entity: server
category: clients
severity: warning
kind: operational
lookback: 15m
description: >-
  The server's stalled client counter rose within the lookback: a publisher was paused because a subscriber's
  outbound buffer was full. A stall slows the publisher; it is the step before a slow consumer disconnect.
remediation: >-
  The Connections page sorted by pending bytes shows which subscriber is behind (client-002). Speed that client up,
  or split its subscription across a queue group. Stalls on a server that also shows slow consumers (server-009)
  mean the same client is now being disconnected.
related_config: [lookback]
value_name: stalled clients
value_basis: growth of the stalled clients counter within the lookback
*/
WITH cur AS (
    SELECT st.server_id, s.server_name, st.stalled_clients
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.stalled_clients IS NOT NULL
),
base AS (
    SELECT server_id, arg_min(stalled_clients, epoch) AS stalled
    FROM server_stats
    WHERE epoch >= getvariable('window_epoch') AND epoch < getvariable('epoch')
      AND server_id IN (SELECT server_id FROM cur)
    GROUP BY server_id
)
SELECT
    c.server_name AS entity_key,
    c.server_name || ': ' || (c.stalled_clients - b.stalled) || ' client stalls in the last '
        || getvariable('lookback_text') || '.' AS detail,
    c.stalled_clients - b.stalled AS value
FROM cur c JOIN base b USING (server_id)
WHERE c.stalled_clients - b.stalled > 0
