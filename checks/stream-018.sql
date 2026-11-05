/*
code: stream-018
name: Single replica in a cluster
entity: stream
category: storage
severity: info
severity_rule: warning for an object store, whose objects are often the only copy; info for a mirror, whose origin holds the data
kind: operational
thresholds:
  - name: min_servers
    default: "3"
    unit: count
    description: JetStream servers the stream's cluster must have before one replica is reported.
description: >-
  A stream, KV bucket or object store has one replica while its cluster has min_servers or more JetStream servers
  (those seen in the last day), so losing that one server's disk loses its messages, and while that server is down
  the stream is unavailable. Not reported: an asset whose replicas the inventory gives (one replica there is by
  design; another number is stream-012's), and a stream on a server without a cluster. While the stream's only server
  is not ready, its last configuration is kept, so a restart does not resolve and fire the finding again.
remediation: >-
  If the data matters, raise the replicas: `nats stream edit <stream> --replicas 3` (for a KV bucket `KV_<bucket>`, for
  an object store `OBJ_<bucket>`), or change the definition the stream comes from (a NACK resource, or the code that
  creates the bucket) so a later deploy does not set it back. The new replicas copy the stream from the current one;
  for a large stream do it outside busy hours and watch stream-003. If one replica is by design, for a cache or data
  that is rebuilt on start, declare `replicas: 1` for it in the inventory, or mute the finding with that reason.
related_config: [min_servers, inventory_file]
value_name: JetStream servers in the cluster
value_basis: distinct names of JetStream servers in the stream's cluster seen within the last day
*/
WITH nr AS (
    SELECT server_name FROM not_ready(getvariable('epoch'))
),
-- The current configuration, or while the stream's only reporting server
-- is not ready, the configuration it reported last.
cfg AS (
    SELECT c.account, c.stream, c.replicas, coalesce(c.mirror, '') AS mirror, s.server_name, s.cluster, s.domain
    FROM stream_config c JOIN servers s USING (server_id)
    WHERE c.first_epoch <= getvariable('epoch')
      AND (c.last_epoch >= getvariable('epoch')
           OR (s.server_name IN (SELECT server_name FROM nr)
               AND c.last_epoch = (SELECT st.last_epoch FROM streams st WHERE st.account = c.account AND st.stream = c.stream)))
    QUALIFY row_number() OVER (PARTITION BY c.account, c.stream ORDER BY c.last_epoch DESC, s.server_name) = 1
),
sizes AS (
    SELECT cluster, coalesce(domain, '') AS domain, count(DISTINCT server_name) AS servers
    FROM servers
    WHERE jetstream AND coalesce(cluster, '') <> '' AND last_seen_at >= getvariable('epoch_time') - INTERVAL 1 DAY
    GROUP BY cluster, coalesce(domain, '')
),
-- Assets whose replicas the inventory gives; the inventory names the
-- account as the servers do or by its readable name.
declared AS (
    SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                     ORDER BY a.account = i.account DESC LIMIT 1), i.account) AS account, i.stream
    FROM inventory i
    WHERE i.kind IN ('stream', 'kv', 'object_store') AND i.replicas IS NOT NULL
)
SELECT
    c.account || '/' || c.stream AS entity_key,
    CASE WHEN c.mirror <> '' THEN 'info' WHEN c.stream LIKE 'OBJ\_%' ESCAPE '\' THEN 'warning' ELSE 'info' END AS severity,
    CASE WHEN c.stream LIKE 'KV\_%' ESCAPE '\' THEN 'KV bucket ' || c.stream[4:]
         WHEN c.stream LIKE 'OBJ\_%' ESCAPE '\' THEN 'Object store ' || c.stream[5:]
         ELSE 'Stream ' || c.stream END
        || ' has 1 replica, on ' || c.server_name || ', in cluster ' || c.cluster || ' of ' || z.servers
        || ' JetStream servers'
        || CASE WHEN c.mirror <> '' THEN '; it mirrors ' || c.mirror || ', which holds the data'
                WHEN c.stream LIKE 'OBJ\_%' ESCAPE '\' THEN '; its objects are lost with that server''s disk'
                ELSE '; its messages are lost with that server''s disk' END
        || CASE WHEN c.server_name IN (SELECT server_name FROM nr) THEN ' (that server is not ready now)' ELSE '' END
        || '.' AS detail,
    z.servers AS value,
    json_object('replicas', CAST(c.replicas AS VARCHAR)) AS related_config,
    c.server_name AS related_source
FROM cfg c JOIN sizes z ON z.cluster = c.cluster AND z.domain = coalesce(c.domain, '')
WHERE c.replicas = 1 AND z.servers >= getvariable('min_servers')
  AND (c.account, c.stream) NOT IN (SELECT account, stream FROM declared)
