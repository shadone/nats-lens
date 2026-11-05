/*
code: system-006
name: Mixed JetStream API levels
entity: system
category: agreement
severity: warning
kind: operational
thresholds:
  - name: min_duration
    default: 30m
    unit: duration
    description: >-
      How long the answering servers must report more than one API level in every collection before the finding
      fires; a firing finding resolves once no collection of the last min_duration was mixed.
description: >-
  In every collection for at least min_duration, the JetStream servers of one JetStream domain that answered
  reported more than one JetStream API level. A client's request for a newer API feature, such as a stream or consumer setting that came
  with a later nats-server release, then works or fails depending on the server that handles it, and a replica on an
  older server may not apply the setting. A server that reports level 0 runs a release before levels were
  reported. A collection in which a server is restarting, within checks.restart_window of its start, does not count,
  so a rolling upgrade that keeps moving does not fire; one that stops half-way does, min_duration after its last
  restart; an upgrade whose restarts are more than about 40 minutes apart (restart_window plus min_duration) fires
  between them. The condition must hold from a collection that started between min_duration and twice min_duration
  ago, so a long gap in collection does not count as held. Servers whose versions differ but share one API level do not fire; server-005 reports every version
  that differs from the majority, at info. A firing finding resolves once no collection of the last min_duration was
  mixed.
remediation: >-
  Finish the upgrade so that every server runs the same nats-server release: `nats server list` with the system user
  shows each server's version. In Kubernetes the StatefulSet updates one pod at a time; check `kubectl rollout status
  statefulset/<name>` and whether a pod is stuck pending or failing its readiness probe. Until every server is on the
  new release, do not use the settings it introduced on new streams and consumers.
related_config: [min_duration]
value_name: JetStream API levels
value_basis: >-
  distinct API levels the answering JetStream servers of one JetStream domain report in this collection, the most of
  any domain, restarting servers left out
*/
WITH cand AS (
    SELECT epoch, started_at FROM epochs
    WHERE started_at >= getvariable('epoch_time') - 2 * getvariable('min_duration') AND epoch <= getvariable('epoch')
),
restarting_now AS (
    SELECT epoch, server_id FROM restarting((SELECT coalesce(min(epoch), getvariable('epoch')) FROM cand), getvariable('epoch'))
),
rows AS (
    SELECT st.epoch, coalesce(s.domain, '') AS domain, s.server_name, st.js_api_level AS level
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch IN (SELECT epoch FROM cand) AND st.js_enabled AND st.js_api_level IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM restarting_now r WHERE r.epoch = st.epoch AND r.server_id = st.server_id)
),
levels AS (
    SELECT epoch, domain, count(DISTINCT level) AS n FROM rows GROUP BY epoch, domain
),
-- A collection without an answering JetStream server is unknown.
judged AS (
    SELECT c.epoch, c.started_at, max(l.n) AS n,
        max(l.n) > 1 AND NOT EXISTS (SELECT 1 FROM restarting_now r WHERE r.epoch = c.epoch) AS bad
    FROM cand c JOIN levels l USING (epoch)
    GROUP BY c.epoch, c.started_at
),
lo AS (
    SELECT max(epoch) AS epoch FROM judged WHERE started_at <= getvariable('epoch_time') - getvariable('min_duration')
),
held AS (
    SELECT * FROM judged WHERE epoch >= (SELECT epoch FROM lo)
),
recent AS (
    SELECT * FROM judged WHERE started_at > getvariable('epoch_time') - getvariable('min_duration')
),
fires AS (
    SELECT (SELECT count(*) FROM held) >= 2 AND (SELECT bool_and(bad) FROM held) AS new,
        EXISTS (SELECT 1 FROM findings WHERE check_code = 'system-006' AND state = 'firing'
                AND last_seen_epoch = getvariable('prev_epoch'))
            AND (coalesce((SELECT bool_or(bad) FROM recent), false) OR NOT EXISTS (SELECT 1 FROM recent)) AS kept
),
newest AS (
    SELECT * FROM judged WHERE bad ORDER BY epoch DESC LIMIT 1
),
by_level AS (
    SELECT r.domain, r.level, count(*) AS servers, list(r.server_name ORDER BY r.server_name) AS names
    FROM rows r JOIN newest n USING (epoch)
    JOIN levels l ON l.epoch = r.epoch AND l.domain = r.domain AND l.n > 1
    GROUP BY r.domain, r.level
),
by_domain AS (
    SELECT domain, count(*) AS n, array_to_string(list(
        'level ' || level || CASE WHEN level = 0 THEN ' (none reported)' ELSE '' END || ' on '
        || array_to_string(names[1:5], ', ') || CASE WHEN servers > 5 THEN ' and ' || (servers - 5) || ' more' ELSE '' END
        ORDER BY level DESC), '; ') AS text
    FROM by_level GROUP BY domain
),
text AS (
    SELECT array_to_string(list(
        CASE WHEN domain <> '' THEN 'In JetStream domain ' || domain || ', the' ELSE 'The' END
        || ' JetStream servers ' || CASE WHEN (SELECT epoch FROM newest) = getvariable('epoch') THEN 'report ' ELSE 'reported ' END
        || n || ' API levels: ' || text || '.' ORDER BY domain), ' ') AS text
    FROM by_domain
)
SELECT
    '' AS entity_key,
    CASE WHEN n.epoch = getvariable('epoch') THEN '' ELSE 'At ' || strftime(n.started_at, '%H:%M:%S') || ' UTC: ' END
        || t.text
        || CASE WHEN f.new
            THEN ' The levels have been mixed in every collection for at least ' || span_text(getvariable('min_duration')) || '.'
            ELSE ' The finding resolves once no collection of the last ' || span_text(getvariable('min_duration')) || ' is mixed.' END AS detail,
    n.n AS value
FROM newest n, text t, fires f
WHERE coalesce(f.new, false) OR f.kept
