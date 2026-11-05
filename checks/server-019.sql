/*
code: server-019
name: Gateway missing
entity: server
category: health
severity: warning
severity_rule: critical when no server of the cluster that answered GATEWAYZ has an outbound gateway to that cluster, while the remote cluster still connects in or its servers answer
kind: operational
lookback: 24h
thresholds:
  - name: min_epochs
    default: "2"
    unit: count
    description: >-
      Consecutive collections the gateway must be missing, with the server answering GATEWAYZ, before the finding
      fires. A firing finding stays while the gateway is missing in the newest collection.
description: >-
  The server has no outbound gateway to a remote cluster that its cluster had a gateway to or from within the
  lookback, in each of the last min_epochs collections in which it answered GATEWAYZ. Every server of a supercluster
  keeps one outbound gateway to each remote cluster; without it, the server's messages for that cluster have to wait
  for the gateway to come back. GATEWAYZ lists only connected gateways and NATS sends no event for a gateway
  disconnect, so a remote cluster is known from the gateways seen within the lookback, and a drop shorter than one
  collect interval is not seen. A remote cluster from which no server of the cluster has an inbound gateway in each
  of the last min_epochs collections, while they answer GATEWAYZ, and none of whose own servers answered, is taken
  as removed and no longer judged, so a cluster removed on purpose fires nothing. A remote cluster whose servers still
  answer stays judged, so a gateway cut in both directions fires. A remote cluster nats-lens does not see itself
  (watched by another collector) and cut in both directions is taken as removed. A server that is restarting
  (restart_window) is not judged, as its gateways connect some time after it answers.
remediation: >-
  The detail names the remote clusters. With the system user, `nats server report gateways` lists each server's
  gateways, and the Topology page shows the pairs of clusters and their drops. When no server of the cluster
  reaches the remote cluster, check that the gateway port (7222 by default) of the
  remote cluster is reachable from this cluster: a load balancer, firewall rule or DNS name in the
  `gateway.gateways` URLs of the NATS configuration. When only this server lacks the gateway, look in its log for
  "Error connecting to gateway".
related_config: [lookback, min_epochs, gateway.gateways]
value_name: remote clusters without a gateway
value_basis: remote clusters of the server's cluster seen within the lookback and not removed, without an outbound gateway from this server
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- The remote clusters each cluster had a gateway to or from within the
-- lookback; an inbound gateway is named by the cluster it comes from.
seen AS (
    SELECT DISTINCT s.cluster, g.gateway
    FROM gateways g JOIN servers s USING (server_id)
    WHERE g.cid IS NOT NULL
      AND g.epoch >= getvariable('window_epoch') AND g.epoch <= getvariable('epoch')
      AND coalesce(s.cluster, '') <> '' AND g.gateway <> s.cluster
),
-- The processes whose GATEWAYZ answered, so whose list of gateways is
-- whole.
answered AS (
    SELECT ee.epoch, s.server_id, s.server_name, s.cluster
    FROM epoch_endpoints ee JOIN servers s USING (server_id)
    WHERE ee.epoch IN (SELECT epoch FROM last) AND ee.endpoint = 'GATEWAYZ' AND ee.state = 'ok'
      AND coalesce(s.cluster, '') <> ''
),
-- A remote cluster is gone, as when it was removed on purpose, when in each
-- of the last min_epochs collections servers of the cluster answered, none
-- of them had a gateway from it, and no server of the remote cluster
-- answered: a remote whose servers still answer is cut off, not removed.
inbound AS (
    SELECT DISTINCT a.epoch, a.cluster, g.gateway
    FROM answered a JOIN gateways g ON g.epoch = a.epoch AND g.server_id = a.server_id
    WHERE g.direction = 'inbound' AND g.cid IS NOT NULL
),
gone AS (
    SELECT k.cluster, k.gateway FROM seen k
    WHERE (SELECT count(DISTINCT a.epoch) FROM answered a WHERE a.cluster = k.cluster) >= getvariable('min_epochs')
      AND NOT EXISTS (SELECT 1 FROM inbound i WHERE i.cluster = k.cluster AND i.gateway = k.gateway)
      AND NOT EXISTS (
          SELECT 1 FROM epoch_endpoints ee JOIN servers rs USING (server_id)
          WHERE ee.epoch IN (SELECT epoch FROM last) AND ee.state = 'ok' AND rs.cluster = k.gateway)
),
known AS (
    SELECT * FROM seen WHERE (cluster, gateway) NOT IN (SELECT (cluster, gateway) FROM gone)
),
-- A restarting process is not judged: its gateways may connect only some
-- time after it answers.
reporters AS (
    SELECT a.* FROM answered a
    WHERE (a.epoch, a.server_id) NOT IN (
        SELECT (epoch, server_id) FROM restarting((SELECT min(epoch) FROM last), getvariable('epoch')))
),
up AS (
    SELECT DISTINCT r.epoch, r.server_name, g.gateway
    FROM reporters r JOIN gateways g ON g.epoch = r.epoch AND g.server_id = r.server_id
    WHERE g.direction = 'outbound' AND g.cid IS NOT NULL
),
gaps AS (
    SELECT DISTINCT r.epoch, r.server_name, r.cluster, k.gateway
    FROM reporters r JOIN known k ON k.cluster = r.cluster
    WHERE (r.epoch, r.server_name, k.gateway) NOT IN (SELECT (epoch, server_name, gateway) FROM up)
),
held AS (
    SELECT server_name, gateway, count(DISTINCT epoch) AS n FROM gaps GROUP BY ALL
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-019' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- Per cluster and remote cluster, the servers that answered GATEWAYZ now
-- and those of them with an outbound gateway to it.
pairs AS (
    SELECT r.cluster, k.gateway, count(DISTINCT r.server_name) AS servers,
        count(DISTINCT u.server_name) AS connected
    FROM reporters r JOIN known k ON k.cluster = r.cluster
    LEFT JOIN up u ON u.epoch = r.epoch AND u.server_name = r.server_name AND u.gateway = k.gateway
    WHERE r.epoch = getvariable('epoch')
    GROUP BY ALL
),
cur AS (
    SELECT g.server_name, g.cluster, g.gateway, p.servers, p.connected
    FROM gaps g JOIN held h USING (server_name, gateway) JOIN pairs p USING (cluster, gateway)
    WHERE g.epoch = getvariable('epoch')
      AND (h.n >= getvariable('min_epochs') OR g.server_name IN (SELECT entity_key FROM firing))
),
per AS (
    SELECT server_name, any_value(cluster) AS cluster, count(*) AS n, bool_or(connected = 0) AS down,
        list(gateway || CASE WHEN connected = 0 THEN ' (no server of ' || cluster || ' has one)'
            ELSE ' (' || connected || ' of ' || servers || ' servers of ' || cluster || ' have one)' END
            ORDER BY connected = 0 DESC, gateway) AS items
    FROM cur GROUP BY server_name
)
SELECT
    server_name AS entity_key,
    CASE WHEN down THEN 'critical' ELSE 'warning' END AS severity,
    server_name || ' has no outbound gateway to ' || CASE WHEN n = 1 THEN 'cluster ' ELSE 'clusters ' END
        || array_to_string(items, ', ') || '.' AS detail,
    n AS value
FROM per
