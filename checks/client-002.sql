/*
code: client-002
name: High pending bytes
entity: client
category: clients
severity: warning
kind: operational
thresholds:
  - name: max_pending
    default: 4MiB
    unit: bytes
    description: Bytes queued for one connection above which it counts.
description: >-
  The server holds more than max_pending bytes it could not yet write to one or more connections with this name.
  The client reads slower than its messages arrive; when the queue reaches the server's max_pending (64 MiB by
  default) or a write takes longer than write_deadline, the server disconnects it as a slow consumer (client-003).
remediation: >-
  The client is not keeping up with its subscriptions. Check its CPU and whether its message handler blocks (a
  synchronous call per message). Spread the work over a queue group, or move the subscription to a JetStream pull
  consumer so the client takes messages at its own pace.
related_config: [max_pending, write_deadline]
value_name: pending bytes
value_basis: largest pending bytes among the client's connections in the collection
*/
WITH c AS (
    SELECT coalesce(nullif(c.name, ''), '(unnamed)') AS client, s.server_name, c.cid, c.pending_bytes
    FROM connections c JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch') AND c.pending_bytes > getvariable('max_pending') AND NOT tooling_at(c.name, c.ip, c.epoch)
)
SELECT
    client AS entity_key,
    count(*) || CASE WHEN count(*) = 1 THEN ' connection has ' ELSE ' connections have ' END || 'more than '
        || format_bytes(CAST(getvariable('max_pending') AS BIGINT)) || ' queued; most ' || format_bytes(max(pending_bytes))
        || ' on ' || arg_max(server_name || ' cid ' || cid, pending_bytes) || '.' AS detail,
    max(pending_bytes) AS value
FROM c
GROUP BY client
