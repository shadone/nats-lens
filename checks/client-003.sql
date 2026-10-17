/*
code: client-003
name: Slow consumer disconnects
entity: client
category: clients
severity: warning
kind: operational
lookback: 30m
description: >-
  The servers disconnected connections with this name as slow consumers within the lookback, from the
  client.disconnect events and their reason.
remediation: >-
  A slow consumer disconnect drops every message queued for the client. The client then reconnects and may lose
  more under the same load (server-011). Make the client read faster (more CPU, no blocking work in the message
  handler), split the subscription over a queue group, or take the messages from a JetStream consumer, which
  survives a reconnect. The reason in the detail says whether the write deadline or the pending limit was hit.
related_config: [lookback, write_deadline, max_pending]
value_name: slow consumer disconnects
value_basis: disconnects for a slow consumer within the lookback
*/
SELECT
    coalesce(nullif(client_name, ''), '(unnamed)') AS entity_key,
    count(*) || ' slow consumer ' || CASE WHEN count(*) = 1 THEN 'disconnect' ELSE 'disconnects' END || ' in the last '
        || getvariable('lookback_text') || ' (' || array_to_string(list(DISTINCT server_name ORDER BY server_name), ', ')
        || '), the last at ' || strftime(max(time), '%H:%M:%S') || ' UTC: ' || arg_max(reason, time) || '.' AS detail,
    count(*) AS value,
    arg_max(id, time) AS cause_event_id
FROM events
WHERE kind = 'client.disconnect' AND reason ILIKE '%slow consumer%' AND NOT tooling(client_name, client_host)
  AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
GROUP BY 1
