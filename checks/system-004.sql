/*
code: system-004
name: JetStream meta group falling behind
entity: system
category: health
severity: warning
kind: operational
thresholds:
  - name: max_pending
    default: "100"
    unit: count
    description: Meta group operations not yet applied on one server above which that server counts as behind.
  - name: min_duration
    default: 5m
    unit: duration
    description: >-
      How long the meta group must be behind in every collection before the finding fires; a firing finding resolves
      once it has not been behind in any collection for as long.
description: >-
  The meta group has been behind in every collection for at least min_duration: the meta leader sees peers that
  answer but are not current (offline peers are left out, as server-001 and system-002 report them), or a server has
  more than max_pending meta operations not yet applied. A collection in which a server is restarting, within
  checks.restart_window of its start, does not count as behind, as a restarted server catches up the meta group
  first. While the meta group is behind, creating, updating and deleting streams and consumers is slow or times out,
  and a server that is not current cannot take over as meta leader. A firing finding resolves once no collection of
  the last min_duration was behind.
remediation: >-
  `nats server report jetstream` with the system user lists the meta group's peers with their lag and the pending
  operations. A peer that stays behind cannot keep up with the meta leader: look for CPU throttling or long garbage
  collection pauses on that server (raise its CPU request or GOMAXPROCS), slow storage under its JetStream store
  directory, and network trouble between it and the leader. Pending operations that grow on every server point at a
  client that creates or deletes streams or consumers in a tight loop (the `jetstream.api_audit` events and
  `jetstream.api_limit_reached` show who). A peer that never catches up can be stepped down with `nats server raft
  step-down` when it leads, or removed and re-added with `nats server raft peer-remove <name>` once it is replaced.
related_config: [max_pending, min_duration]
value_name: meta operations pending
value_basis: the most meta operations not yet applied on any one server in this collection, restarting servers left out
*/
WITH lo AS (
    -- The collection that started min_duration ago or just before it: the
    -- condition must hold from there on.
    SELECT coalesce(max(epoch), getvariable('epoch') + 1) AS epoch FROM epochs
    WHERE started_at <= getvariable('epoch_time') - getvariable('min_duration')
),
win AS (
    SELECT epoch, started_at FROM epochs
    WHERE epoch BETWEEN (SELECT epoch FROM lo) AND getvariable('epoch')
),
restarting_now AS (
    SELECT epoch, server_id FROM restarting((SELECT epoch FROM lo), getvariable('epoch'))
),
rows AS (
    SELECT st.epoch, st.server_id, s.server_name, st.meta_leader, coalesce(st.meta_pending, 0) AS pending,
        greatest(coalesce(st.meta_peers_not_current, 0) - coalesce(st.meta_peers_offline, 0), 0) AS behind,
        st.meta_cluster_size
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch IN (SELECT epoch FROM win) AND st.js_enabled AND st.meta_cluster_size > 1
),
per_epoch AS (
    SELECT w.epoch, w.started_at,
        EXISTS (SELECT 1 FROM restarting_now r WHERE r.epoch = w.epoch) AS excused,
        (SELECT max(r.behind) FROM rows r WHERE r.epoch = w.epoch AND r.server_name = r.meta_leader) AS behind,
        (SELECT any_value(r.server_name) FROM rows r WHERE r.epoch = w.epoch AND r.server_name = r.meta_leader) AS leader,
        (SELECT any_value(r.meta_cluster_size) FROM rows r WHERE r.epoch = w.epoch AND r.server_name = r.meta_leader) AS size,
        (SELECT max(r.pending) FROM rows r WHERE r.epoch = w.epoch) AS pending,
        (SELECT list(r.server_name || ' ' || r.pending ORDER BY r.pending DESC, r.server_name) FROM rows r
         WHERE r.epoch = w.epoch AND r.pending > getvariable('max_pending')) AS pending_servers
    FROM win w
),
judged AS (
    SELECT *, NOT excused AND (coalesce(behind, 0) > 0 OR coalesce(pending, 0) > getvariable('max_pending')) AS bad
    FROM per_epoch
),
firing AS (
    SELECT 1 FROM findings
    WHERE check_code = 'system-004' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
newest AS (
    SELECT * FROM judged WHERE bad ORDER BY epoch DESC LIMIT 1
)
SELECT
    '' AS entity_key,
    CASE WHEN n.epoch = getvariable('epoch') THEN 'The JetStream meta group is behind'
         ELSE 'The JetStream meta group was behind at ' || strftime(n.started_at, '%H:%M:%S') || ' UTC' END
        || ': ' || array_to_string(list_filter([
            CASE WHEN coalesce(n.behind, 0) > 0 THEN 'the meta leader ' || n.leader || ' sees ' || n.behind || ' of '
                || (n.size - 1) || CASE WHEN n.size - 1 = 1 THEN ' peer' ELSE ' peers' END || ' not current' END,
            CASE WHEN len(n.pending_servers) > 0 THEN 'meta operations pending on '
                || array_to_string(n.pending_servers[1:5], ', ') || CASE WHEN len(n.pending_servers) > 5 THEN ', ...' ELSE '' END
                || ' (more than ' || CAST(getvariable('max_pending') AS BIGINT) || ')' END], x -> x IS NOT NULL), '; ')
        || CASE WHEN (SELECT bool_and(bad) FROM judged)
            THEN '. It has been behind in every collection for at least ' || span_text(getvariable('min_duration')) || '.'
            ELSE '. The finding resolves once no collection of the last ' || span_text(getvariable('min_duration')) || ' is behind.' END AS detail,
    n.pending AS value
FROM newest n
WHERE (SELECT count(*) FROM judged) >= 2 AND (
    (SELECT bool_and(bad) FROM judged)
    OR (EXISTS (SELECT 1 FROM firing) AND (SELECT bool_or(bad) FROM judged WHERE started_at > getvariable('epoch_time') - getvariable('min_duration')))
)
