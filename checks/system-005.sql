/*
code: system-005
name: JetStream meta group size differs from the servers
entity: system
category: agreement
severity: warning
kind: operational
thresholds:
  - name: min_duration
    default: 10m
    unit: duration
    description: >-
      How long the size must differ in every collection before the finding fires; a firing finding resolves once it
      has not differed in any collection for as long.
description: >-
  In every collection for at least min_duration, more JetStream servers answered than the meta group has peers in the
  meta leader's view, or an answering JetStream server reports a meta group of another size than the leader does.
  Such a server is not a voting member of the meta group, or does not know its current peers, for example after a
  peer was removed by hand while its server kept running, or after a server joined with a stale state. Servers that
  do not answer are server-001's and are not counted; servers are compared within their JetStream domain. A
  collection in which a server is restarting, within checks.restart_window of its start, or in which a server new to
  nats-lens joined within restart_window, does not count, as such a server reports its meta group late. A collection
  in which no server of any domain reports itself as meta leader is unknown: it neither breaks the hold nor counts
  towards resolving. The condition must hold from a collection that started between min_duration and twice
  min_duration ago, so a long gap in collection does not count as held. A firing finding resolves once no collection
  of the last min_duration differed.
remediation: >-
  `nats server report jetstream` with the system user lists the meta group's peers as the leader sees them; compare
  it with `nats server list`. A server missing from the meta group was removed with `nats server raft peer-remove`
  or never joined: restart it so that it joins again, and check that its `server_name` is stable across restarts (a
  new name is a new peer). A server that reports a smaller group may be cut off from the routes of the others: check
  `nats server report routes` (or the Topology page) for it. If a peer was removed for good, make sure its server is
  stopped, as it otherwise forms a group of its own.
related_config: [min_duration]
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
rows AS (
    SELECT st.epoch, coalesce(s.domain, '') AS domain, s.server_name, coalesce(st.meta_leader, '') AS leader,
        st.meta_cluster_size AS size
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch IN (SELECT epoch FROM cand) AND st.js_enabled AND st.meta_cluster_size IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM excused x WHERE x.epoch = st.epoch AND x.server_id = st.server_id)
),
-- The leader most servers of the domain name, and its own view of the size.
leaders AS (
    SELECT epoch, domain, mode(leader) AS leader FROM rows WHERE leader <> '' GROUP BY epoch, domain
),
groups AS (
    SELECT l.epoch, l.domain, l.leader, ls.size AS leader_size, count(*) AS answering,
        list(r.server_name || ' reports a meta group of ' || r.size
            || CASE WHEN r.leader = '' THEN ' and no leader' WHEN r.leader <> l.leader THEN ' led by ' || r.leader ELSE '' END
            ORDER BY r.server_name) FILTER (WHERE r.size <> ls.size) AS differing
    FROM leaders l
    JOIN rows ls ON ls.epoch = l.epoch AND ls.domain = l.domain AND ls.server_name = l.leader
    JOIN rows r ON r.epoch = l.epoch AND r.domain = l.domain
    GROUP BY l.epoch, l.domain, l.leader, ls.size
),
bad_groups AS (
    SELECT * FROM groups WHERE answering > leader_size OR len(differing) > 0
),
-- A collection without any leader's own report is unknown.
judged AS (
    SELECT c.epoch, c.started_at,
        NOT EXISTS (SELECT 1 FROM excused x WHERE x.epoch = c.epoch)
            AND EXISTS (SELECT 1 FROM bad_groups g WHERE g.epoch = c.epoch) AS bad
    FROM cand c WHERE EXISTS (SELECT 1 FROM groups g WHERE g.epoch = c.epoch)
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
        EXISTS (SELECT 1 FROM findings WHERE check_code = 'system-005' AND state = 'firing'
                AND last_seen_epoch = getvariable('prev_epoch'))
            AND (coalesce((SELECT bool_or(bad) FROM recent), false) OR NOT EXISTS (SELECT 1 FROM recent)) AS kept
),
newest AS (
    SELECT * FROM judged WHERE bad ORDER BY epoch DESC LIMIT 1
),
texts AS (
    SELECT array_to_string(list(
        CASE WHEN g.domain <> '' THEN 'In JetStream domain ' || g.domain || ', the' ELSE 'The' END
        || ' meta group has ' || g.leader_size || ' peers in the view of its leader ' || g.leader || ' and '
        || g.answering || ' JetStream ' || CASE WHEN g.answering = 1 THEN 'server answers' ELSE 'servers answer' END
        || CASE WHEN len(g.differing) > 0
            THEN ': ' || array_to_string(g.differing[1:5], ', ') || CASE WHEN len(g.differing) > 5 THEN ', ...' ELSE '' END
            ELSE '' END
        || '.' ORDER BY g.domain), ' ') AS text
    FROM bad_groups g JOIN newest n USING (epoch)
)
SELECT
    '' AS entity_key,
    CASE WHEN n.epoch = getvariable('epoch') THEN '' ELSE 'At ' || strftime(n.started_at, '%H:%M:%S') || ' UTC: ' END
        || t.text
        || CASE WHEN f.new
            THEN ' It has differed in every collection for at least ' || span_text(getvariable('min_duration')) || '.'
            ELSE ' The finding resolves once no collection of the last ' || span_text(getvariable('min_duration')) || ' differs.' END AS detail
FROM newest n, texts t, fires f
WHERE coalesce(f.new, false) OR f.kept
