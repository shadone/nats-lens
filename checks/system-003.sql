/*
code: system-003
name: JetStream meta leader changing often
entity: system
category: health
severity: warning
kind: operational
lookback: 15m
thresholds:
  - name: max_changes
    default: "4"
    unit: count
    description: Leader changes within the lookback above which the finding fires.
description: >-
  A JetStream meta group changed leader more than max_changes times within the lookback, counted independently from the
  meta.leader_elected advisories and from the leader the servers report in each collection, whichever is higher. The
  first change away from a leader within two collect intervals after it announced lame duck or shutdown, or after
  its process went away and was replaced, is a hand-over and not counted: a rolling restart moves the leader once
  per restarted leader. Later changes away from the same server count. Each change pauses JetStream API calls for
  the election. Stored RAFT membership scopes groups independently of cluster and domain labels. The detail
  lists each affected group, and the value is the largest group count. With identified groups, advisories whose
  group cannot be resolved are not counted. Without membership evidence, legacy observations and advisories
  remain unscoped; their count cannot establish independent physical groups.
remediation: >-
  Leader elections follow servers going away (server-001, server-004) or missed heartbeats between the pods. If no
  server restarted, look for CPU throttling or long garbage collection pauses on the NATS pods (raise the CPU request
  or GOMAXPROCS), and for network trouble between the nodes. During a GKE node upgrade each drained pod moves the
  leadership once; more than one change per drained pod is not expected.
related_config: [max_changes, lookback]
value_name: meta leader changes
value_basis: largest per-group count of snapshot changes or scoped election advisories within the lookback
*/
WITH per_epoch AS (
    SELECT st.epoch, g.group_key, g.label, e.started_at AS ts, mode(st.meta_leader) AS leader
    FROM server_stats st JOIN epochs e USING (epoch) JOIN meta_check_groups g USING (server_id)
    WHERE st.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND coalesce(st.meta_leader, '') <> ''
    GROUP BY st.epoch, g.group_key, g.label, e.started_at
),
-- Resolve a name only when every observation maps it to one group.
name_groups AS (
    SELECT s.server_name, min(g.group_key) AS group_key
    FROM meta_check_groups g JOIN servers s USING (server_id)
    GROUP BY s.server_name HAVING count(DISTINCT g.group_key) = 1
    UNION
    SELECT server_name, '' AS group_key FROM servers
    WHERE first_epoch <= getvariable('epoch')
      AND NOT EXISTS (SELECT 1 FROM meta_check_groups WHERE group_key <> '')
),
step AS (
    SELECT coalesce(median(gap), INTERVAL 30 SECOND) AS gap FROM (
        SELECT started_at - lag(started_at) OVER (ORDER BY epoch) AS gap FROM epochs
        WHERE epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch'))
),
stepped AS (
    SELECT n.group_key, ev.server_name, ev.received_at AS ts FROM events ev JOIN name_groups n USING (server_name)
    WHERE ev.kind IN ('server.lameduck', 'server.shutdown')
      AND ev.received_at > getvariable('window_start') - INTERVAL 10 MINUTE AND ev.received_at <= getvariable('epoch_time')
    UNION
    SELECT ng.group_key, p.server_name, p.last_seen_at FROM servers p JOIN name_groups ng USING (server_name)
    WHERE p.last_seen_at > getvariable('window_start') - INTERVAL 10 MINUTE AND p.last_seen_at <= getvariable('epoch_time')
      AND p.last_epoch < getvariable('epoch')
      AND EXISTS (SELECT 1 FROM servers n WHERE n.server_name = p.server_name AND n.first_epoch > p.last_epoch
          AND n.first_epoch <= getvariable('epoch'))
),
changes AS (
    SELECT epoch, group_key, ts, leader, prev FROM (
        SELECT *, lag(leader) OVER (PARTITION BY group_key ORDER BY epoch) AS prev FROM per_epoch)
    WHERE prev IS NOT NULL AND leader <> prev
),
excused AS (
    SELECT DISTINCT x.group_key, (SELECT min(c.epoch) FROM changes c
        WHERE c.group_key = x.group_key AND c.prev = x.server_name AND c.ts > x.ts
          AND c.ts <= x.ts + 2 * (SELECT gap FROM step)) AS epoch
    FROM stepped x
),
observed AS (
    SELECT p.group_key, any_value(p.label) AS label,
        (SELECT count(*) FROM changes c WHERE c.group_key = p.group_key
            AND NOT EXISTS (SELECT 1 FROM excused x WHERE x.group_key = c.group_key AND x.epoch = c.epoch)) AS n,
        array_to_string(list(leader ORDER BY epoch) FILTER (WHERE prev IS NULL OR leader <> prev), ' to ') AS path
    FROM (SELECT *, lag(leader) OVER (PARTITION BY group_key ORDER BY epoch) AS prev FROM per_epoch) p
    GROUP BY p.group_key
),
scoped_events AS (
    SELECT ev.id, ev.received_at AS ts, ev.server_name,
        coalesce(n.group_key, CASE WHEN (SELECT count(*) FROM meta_check_groups WHERE group_key <> '') = 0
            THEN '' ELSE '__unscoped_advisories__' END) AS group_key
    FROM events ev LEFT JOIN name_groups n USING (server_name)
    WHERE ev.kind = 'meta.leader_elected' AND ev.received_at > getvariable('window_start')
      AND ev.received_at <= getvariable('epoch_time')
),
elected AS (
    SELECT *, coalesce(lag(server_name) OVER (PARTITION BY group_key ORDER BY ts, id),
        (SELECT arg_min(p.leader, p.epoch) FROM per_epoch p WHERE p.group_key = ev.group_key)) AS prev
    FROM scoped_events ev
),
elected_excused AS (
    SELECT DISTINCT x.group_key, (SELECT arg_min(el.id, el.ts) FROM elected el
        WHERE el.group_key = x.group_key AND el.prev = x.server_name AND el.ts > x.ts
          AND el.ts <= x.ts + 2 * (SELECT gap FROM step)) AS id
    FROM stepped x
),
advised AS (
    SELECT group_key, count(*) FILTER (WHERE NOT EXISTS
        (SELECT 1 FROM elected_excused x WHERE x.group_key = el.group_key AND x.id = el.id)) AS n,
        array_to_string(list(server_name ORDER BY ts, id), ' to ') AS path
    FROM elected el GROUP BY group_key
),
problems AS (
    SELECT coalesce(o.group_key, e.group_key) AS group_key,
        coalesce(o.label, (SELECT any_value(g.label) FROM meta_check_groups g WHERE g.group_key = e.group_key),
            'Unscoped advisories (meta group identity unknown)') AS label,
        greatest(coalesce(o.n, 0), coalesce(e.n, 0)) AS n,
        CASE WHEN coalesce(e.n, 0) >= coalesce(o.n, 0) THEN e.path || ' (advisories)' ELSE o.path || ' (snapshots)' END AS path
    FROM observed o FULL OUTER JOIN advised e USING (group_key)
)
SELECT '' AS entity_key,
    string_agg(label || ': The meta leader changed ' || n || ' times in the last ' || getvariable('lookback_text') || ': '
        || path || ', not counting one hand-over after each restart or lame duck.', ' ' ORDER BY group_key)
        || CASE WHEN (SELECT count(*) FROM scoped_events WHERE group_key IN ('', '__unscoped_advisories__')) > 0
            AND EXISTS (SELECT 1 FROM meta_check_groups WHERE group_key <> '')
            THEN ' Election advisories with unresolved group identity were not counted.' ELSE '' END AS detail,
    max(n) AS value
FROM problems WHERE n > getvariable('max_changes')
    AND group_key <> '__unscoped_advisories__'
    AND (group_key <> '' OR NOT EXISTS (SELECT 1 FROM meta_check_groups WHERE group_key <> ''))
HAVING count(*) > 0
