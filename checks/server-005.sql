/*
code: server-005
name: Version differs from the majority
entity: server
category: agreement
severity: info
kind: operational
description: >-
  The server runs a nats-server version other than the one most servers run. Expected during a rolling upgrade; a
  mixed cluster that stays mixed runs replicas with different behaviour.
remediation: >-
  Finish the rollout: the NATS StatefulSet updates one pod at a time, so check `kubectl rollout status
  statefulset/<name>` and whether a pod is stuck (pending on a node, failing its readiness probe). If the version is
  pinned per pod by mistake, align the image tag in the Helm values.
*/
WITH cur AS (
    SELECT s.server_name, coalesce(st.version, s.version) AS version
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch')
),
counts AS (
    SELECT version, count(*) AS n FROM cur WHERE version IS NOT NULL GROUP BY version
),
top AS (
    SELECT version, n FROM counts ORDER BY n DESC, version DESC LIMIT 1
)
SELECT
    c.server_name AS entity_key,
    c.server_name || ' runs ' || c.version || '; ' || t.n || ' of ' || (SELECT count(*) FROM cur)
        || ' servers run ' || t.version || '.' AS detail
FROM cur c, top t
WHERE c.version IS NOT NULL AND c.version <> t.version AND (SELECT count(*) FROM counts) > 1
