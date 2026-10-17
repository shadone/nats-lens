/*
code: server-004
name: Server restarted
entity: server
category: change
severity: info
severity_rule: >-
  critical when the server restarted more than max_restarts times within the lookback without an upgrade or a
  configuration change
kind: operational
lookback: 1h
thresholds:
  - name: max_restarts
    default: "2"
    unit: count
    description: Restarts without an upgrade or configuration change within the lookback above which the finding is critical.
description: >-
  The server process started within the lookback and replaced an earlier one. The detail says whether it came with
  an upgrade (another version) or a configuration change (another config digest); a rolling restart shows here at
  info. Repeated restarts without either are critical. It stays firing for the lookback after the restart.
remediation: >-
  Find why the pod restarted: `kubectl describe pod <name>` (last state, exit code, OOMKilled) and the events of
  its node (`kubectl get events --field-selector involvedObject.name=<name>`). A GKE node upgrade or node
  auto-repair shows as the pod being evicted; repeated restarts without a node event usually mean the container was
  killed for memory (raise the limit, look at server-008) or a liveness probe failed while JetStream recovered (lengthen
  the probe's initial delay). Check that streams kept quorum while the server was down (stream-001, consumer-001).
related_config: [max_restarts, lookback]
value_name: restarts
value_basis: restarts without a version change within the lookback
*/
WITH procs AS (
    SELECT server_id, server_name, version, coalesce(start_time, first_seen_at) AS started, first_epoch, last_epoch,
        lag(server_id) OVER w AS prev_id, lag(version) OVER w AS prev_version, lag(last_epoch) OVER w AS prev_last
    FROM servers
    WINDOW w AS (PARTITION BY server_name ORDER BY first_epoch)
),
restarts AS (
    SELECT p.*,
        (SELECT config_digest FROM server_stats x WHERE x.server_id = p.server_id AND x.epoch = p.first_epoch) AS digest,
        (SELECT config_digest FROM server_stats x WHERE x.server_id = p.prev_id AND x.epoch = p.prev_last) AS prev_digest
    FROM procs p
    WHERE p.prev_id IS NOT NULL
      AND p.first_epoch <= getvariable('epoch')
      AND p.started >= getvariable('window_start') AND p.started <= getvariable('epoch_time')
),
classified AS (
    SELECT *, CASE
        WHEN version IS DISTINCT FROM prev_version THEN 'upgrade'
        WHEN digest IS NOT NULL AND prev_digest IS NOT NULL AND digest <> prev_digest THEN 'config'
        ELSE 'unplanned' END AS why
    FROM restarts
)
SELECT
    server_name AS entity_key,
    CASE WHEN count(*) FILTER (WHERE why = 'unplanned') > getvariable('max_restarts') THEN 'critical' ELSE 'info' END AS severity,
    server_name || ' restarted ' || CASE arg_max(why, started)
            WHEN 'upgrade' THEN 'for an upgrade from ' || coalesce(arg_max(prev_version, started), 'unknown') || ' to '
                || coalesce(arg_max(version, started), 'unknown') || ' at '
            WHEN 'config' THEN 'with a changed configuration at '
            ELSE 'at ' END
        || strftime(max(started), '%H:%M:%S') || ' UTC'
        || CASE WHEN arg_max(why, started) = 'unplanned' THEN ' with version ' || coalesce(arg_max(version, started), 'unknown') || ' unchanged' ELSE '' END
        || '; ' || count(*) || CASE WHEN count(*) = 1 THEN ' restart' ELSE ' restarts' END || ' in the last '
        || getvariable('lookback_text')
        || CASE WHEN count(*) FILTER (WHERE why = 'unplanned') > getvariable('max_restarts')
                AND count(*) FILTER (WHERE why = 'unplanned') <> count(*)
            THEN ', ' || count(*) FILTER (WHERE why = 'unplanned') || ' without an upgrade or configuration change' ELSE '' END
        || '.' AS detail,
    count(*) AS value,
    max(started) AS condition_since
FROM classified
GROUP BY server_name
