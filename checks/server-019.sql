/*
code: server-019
name: Gateway missing
entity: server
category: health
severity: warning
severity_rule: critical when no server of the cluster that answered GATEWAYZ has an outbound gateway to that cluster
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
  The server has no outbound gateway to a remote cluster that a server of its cluster had one to within the
  lookback, in each of the last min_epochs collections in which it answered GATEWAYZ. Every server of a supercluster
  keeps one outbound gateway to each remote cluster; without it, the server's messages for that cluster have to wait
  for the gateway to come back. GATEWAYZ lists only connected gateways and NATS sends no event for a gateway
  disconnect, so a remote cluster is known from the gateways seen within the lookback, and a drop shorter than one
  collect interval is not seen.
remediation: >-
  The detail names the remote clusters. With the system user, `nats server report gateways` lists each server's
  gateways, and the Topology page shows the pairs of clusters and their drops. When the whole remote cluster is
  missing, check that its servers run and that the gateway port (7222 by default) is reachable from this cluster: a
  load balancer, firewall rule or DNS name in the `gateway.gateways` URLs of the NATS configuration. When only this
  server lacks the gateway, look in its log for "Error connecting to gateway". A remote cluster removed on purpose
  stays known for the lookback; mute the finding until then.
related_config: [lookback, min_epochs, gateway.gateways]
value_name: remote clusters without a gateway
value_basis: remote clusters of the server's cluster seen within the lookback, without an outbound gateway from this server
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- The remote clusters each cluster had an outbound gateway to within the
-- lookback.
known AS (
    SELECT DISTINCT s.cluster, g.gateway
    FROM gateways g JOIN servers s USING (server_id)
    WHERE g.direction = 'outbound' AND g.cid IS NOT NULL
      AND g.epoch >= getvariable('window_epoch') AND g.epoch <= getvariable('epoch')
      AND coalesce(s.cluster, '') <> '' AND g.gateway <> s.cluster
),
-- The processes whose GATEWAYZ answered, so whose list of gateways is
-- whole.
reporters AS (
    SELECT ee.epoch, s.server_id, s.server_name, s.cluster
    FROM epoch_endpoints ee JOIN servers s USING (server_id)
    WHERE ee.epoch IN (SELECT epoch FROM last) AND ee.endpoint = 'GATEWAYZ' AND ee.state = 'ok'
      AND coalesce(s.cluster, '') <> ''
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
