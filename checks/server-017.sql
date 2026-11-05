/*
code: server-017
name: Route missing
entity: server
category: health
severity: warning
severity_rule: critical when the server has no route to any other server of its cluster that answered
kind: operational
thresholds:
  - name: min_epochs
    default: "3"
    unit: count
    description: >-
      Consecutive collections a route must be missing, with both servers answering, before the finding fires. A
      firing finding stays while the route is missing in the newest collection.
description: >-
  The server's ROUTEZ lists no route to another server of its cluster, while both servers answered, in each of the
  last min_epochs collections. Messages and interest then do not pass directly between the two, so subscribers on one
  miss what publishers on the other send. A server that did not answer is server-001's and is not judged here.
remediation: >-
  The detail names the servers without a route. With the system user, `nats server report routes` lists every
  server's routes and `nats server request routes --name=nats-0` the routes of one server; the Topology page shows a
  route only one end reports. A route that does not come back is usually an address the server cannot reach: check
  the cluster `routes` URLs in the NATS configuration (in Kubernetes the headless service, for example
  `nats://nats-2.nats.default.svc:6222`), that port 6222 is open between the pods (a NetworkPolicy), and the server
  log for "Error trying to connect to route". The server retries the route by itself once the address is reachable.
related_config: [min_epochs, cluster.routes]
value_name: peers without a route
value_basis: servers of the same cluster that answered and that the server's ROUTEZ does not list
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- The servers that answered any request, by process, with their cluster.
answered AS (
    SELECT DISTINCT ee.epoch, s.server_id, s.server_name, s.cluster
    FROM epoch_endpoints ee JOIN servers s USING (server_id)
    WHERE ee.epoch IN (SELECT epoch FROM last) AND ee.state = 'ok' AND coalesce(s.cluster, '') <> ''
),
-- The processes whose ROUTEZ answered, so whose list of routes is whole.
reporters AS (
    SELECT a.* FROM answered a
    WHERE EXISTS (SELECT 1 FROM epoch_endpoints ee WHERE ee.epoch = a.epoch AND ee.server_id = a.server_id
        AND ee.endpoint = 'ROUTEZ' AND ee.state = 'ok')
),
-- A peer is without a route when no reporting process of the server's name
-- lists a route to it, by name or by server ID.
gaps AS (
    SELECT DISTINCT r.epoch, r.server_name, r.cluster, p.server_name AS peer
    FROM reporters r JOIN answered p ON p.epoch = r.epoch AND p.cluster = r.cluster AND p.server_name <> r.server_name
    WHERE NOT EXISTS (
        SELECT 1 FROM routes x JOIN reporters r2 ON r2.epoch = x.epoch AND r2.server_id = x.server_id
        WHERE x.epoch = r.epoch AND r2.server_name = r.server_name
          AND (x.remote_name = p.server_name OR x.remote_id IN (
              SELECT a2.server_id FROM answered a2 WHERE a2.epoch = r.epoch AND a2.server_name = p.server_name)))
),
held AS (
    SELECT server_name, peer, count(DISTINCT epoch) AS n FROM gaps GROUP BY ALL
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-017' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
peers AS (
    SELECT r.server_name, count(DISTINCT p.server_name) AS n, any_value(r.cluster) AS cluster
    FROM reporters r JOIN answered p ON p.epoch = r.epoch AND p.cluster = r.cluster AND p.server_name <> r.server_name
    WHERE r.epoch = getvariable('epoch')
    GROUP BY r.server_name
),
cur AS (
    SELECT g.server_name, list(g.peer ORDER BY g.peer) AS missing
    FROM gaps g JOIN held h USING (server_name, peer)
    WHERE g.epoch = getvariable('epoch')
      AND (h.n >= getvariable('min_epochs') OR g.server_name IN (SELECT entity_key FROM firing))
    GROUP BY g.server_name
)
SELECT
    c.server_name AS entity_key,
    CASE WHEN len(c.missing) >= p.n THEN 'critical' ELSE 'warning' END AS severity,
    c.server_name || CASE WHEN len(c.missing) >= p.n AND p.n > 1
            THEN ' has no route to any of the ' || p.n || ' other servers of cluster ' || p.cluster || ' ('
            ELSE ' has no route to ' END
        || array_to_string(c.missing, ', ') || CASE WHEN len(c.missing) >= p.n AND p.n > 1 THEN ')' ELSE '' END
        || ', while ' || CASE WHEN len(c.missing) = 1 THEN 'both' ELSE 'all' END || ' answered.' AS detail,
    len(c.missing) AS value
FROM cur c JOIN peers p USING (server_name)
