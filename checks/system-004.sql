/*
code: system-004
name: JetStream meta group falling behind
entity: system
category: health
severity: warning
kind: operational
thresholds:
  - name: min_duration
    default: 5m
    unit: duration
    description: >-
      How long the meta group must be behind in every collection before the finding fires; a firing finding resolves
      once it has not been behind in any collection for as long.
description: >-
  The meta leader has seen peers that answer but are not current in every collection for at least min_duration:
  they have not stored the meta group's latest operations. Offline peers are left out, as server-001 and system-002
  report them. A collection in which no server reports itself as meta leader, or the leader's JSZ is missing, tells
  nothing: it neither breaks the hold nor counts towards resolving. A collection in which a server is restarting,
  within checks.restart_window of its start, or in which a server new to nats-lens joined within restart_window,
  does not count as behind, as such a server catches up the meta group first. The condition must hold from a
  collection that started between min_duration and twice min_duration ago, so a long gap in collection does not
  count as held. While the meta group is behind, creating, updating and deleting streams and consumers is slow or
  times out, and a peer that is not current cannot take over as meta leader. A firing finding resolves once no
  collection of the last min_duration was behind.
remediation: >-
  `nats server report jetstream` with the system user lists the meta group's peers with their lag as the leader sees
  them. A peer that stays behind cannot keep up with the meta leader: look for CPU throttling or long garbage
  collection pauses on that server (raise its CPU request or GOMAXPROCS), slow storage under its JetStream store
  directory, and network trouble between it and the leader. A client that creates or deletes streams or consumers in
  a tight loop loads the meta group too (the `jetstream.api_audit` events and `jetstream.api_limit_reached` show
  who). A peer that never catches up can be removed with `nats server raft peer-remove <name>` once its server is
  replaced.
related_config: [min_duration]
value_name: peers not current
value_basis: answering meta group peers the meta leader reports as not current in this collection
*/
WITH cand AS (
    SELECT epoch, started_at FROM epochs
    WHERE started_at >= getvariable('epoch_time') - 2 * getvariable('min_duration') AND epoch <= getvariable('epoch')
),
-- Servers whose reports do not count: restarted within restart_window, or
-- new to nats-lens (first of their name, after its first collection)
-- within restart_window.
excused AS (
    SELECT epoch, server_id FROM restarting((SELECT coalesce(min(epoch), getvariable('epoch')) FROM cand), getvariable('epoch'))
    UNION
    SELECT c.epoch, s.server_id FROM cand c JOIN servers s ON s.first_epoch <= c.epoch AND s.last_epoch >= c.epoch
    WHERE s.first_seen_at > c.started_at - getvariable('restart_window')
      AND s.first_epoch > (SELECT min(epoch) FROM epochs)
      AND NOT EXISTS (SELECT 1 FROM servers p WHERE p.server_name = s.server_name AND p.first_epoch < s.first_epoch)
),
leaders AS (
    SELECT st.epoch, any_value(s.server_name) AS leader, max(st.meta_cluster_size) AS size,
        max(greatest(coalesce(st.meta_peers_not_current, 0) - coalesce(st.meta_peers_offline, 0), 0)) AS behind
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch IN (SELECT epoch FROM cand) AND st.js_enabled AND st.meta_cluster_size > 1
      AND s.server_name = st.meta_leader
    GROUP BY st.epoch
),
-- A collection without the leader's own report is unknown: neither behind
-- nor clean.
judged AS (
    SELECT c.epoch, c.started_at, l.leader, l.size, l.behind,
        NOT EXISTS (SELECT 1 FROM excused x WHERE x.epoch = c.epoch) AND l.behind > 0 AS bad
    FROM cand c JOIN leaders l USING (epoch)
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
firing AS (
    SELECT 1 FROM findings
    WHERE check_code = 'system-004' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
newest AS (
    SELECT * FROM judged WHERE bad ORDER BY epoch DESC LIMIT 1
),
fires AS (
    SELECT (SELECT count(*) FROM held) >= 2 AND (SELECT bool_and(bad) FROM held) AS new,
        EXISTS (SELECT 1 FROM firing)
            AND (coalesce((SELECT bool_or(bad) FROM recent), false) OR NOT EXISTS (SELECT 1 FROM recent)) AS kept
)
SELECT
    '' AS entity_key,
    CASE WHEN n.epoch = getvariable('epoch') THEN 'The JetStream meta group is behind'
         ELSE 'The JetStream meta group was behind at ' || strftime(n.started_at, '%H:%M:%S') || ' UTC' END
        || ': the meta leader ' || n.leader || ' sees ' || n.behind || ' of ' || (n.size - 1)
        || CASE WHEN n.size - 1 = 1 THEN ' peer' ELSE ' peers' END || ' not current'
        || CASE WHEN f.new
            THEN '. It has been behind in every collection for at least ' || span_text(getvariable('min_duration')) || '.'
            ELSE '. The finding resolves once no collection of the last ' || span_text(getvariable('min_duration')) || ' is behind.' END AS detail,
    n.behind AS value
FROM newest n, fires f
WHERE coalesce(f.new, false) OR f.kept
