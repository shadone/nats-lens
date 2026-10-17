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
  The JetStream meta leader changed more than max_changes times within the lookback, counted from the
  meta.leader_elected advisories and from the leader the servers report in each collection, whichever is higher. The
  first change away from a leader within two collect intervals after it announced lame duck or shutdown, or after
  its process went away and was replaced, is a hand-over and not counted: a rolling restart moves the leader once
  per restarted leader. Later changes away from the same server count. Each change pauses JetStream API calls for
  the election.
remediation: >-
  Leader elections follow servers going away (server-001, server-004) or missed heartbeats between the pods. If no
  server restarted, look for CPU throttling or long garbage collection pauses on the NATS pods (raise the CPU request
  or GOMAXPROCS), and for network trouble between the nodes. During a GKE node upgrade each drained pod moves the
  leadership once; more than one change per drained pod is not expected.
related_config: [max_changes, lookback]
value_name: meta leader changes
value_basis: leader changes observed or elected within the lookback, the larger count
*/
WITH per_epoch AS (
    SELECT st.epoch, e.started_at AS ts, mode(st.meta_leader) AS leader
    FROM server_stats st JOIN epochs e USING (epoch)
    WHERE st.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND coalesce(st.meta_leader, '') <> ''
    GROUP BY st.epoch, e.started_at
),
-- The collect interval, as the median gap between the window's epochs.
step AS (
    SELECT coalesce(median(gap), INTERVAL 30 SECOND) AS gap FROM (
        SELECT started_at - lag(started_at) OVER (ORDER BY epoch) AS gap FROM epochs
        WHERE epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch'))
),
-- A server stepping down on purpose: it announced lame duck or shutdown,
-- or its process went away (its last answer) and a new one replaced it.
stepped AS (
    SELECT server_name, received_at AS ts FROM events
    WHERE kind IN ('server.lameduck', 'server.shutdown')
      AND received_at > getvariable('window_start') - INTERVAL 10 MINUTE AND received_at <= getvariable('epoch_time')
    UNION
    SELECT p.server_name, p.last_seen_at FROM servers p
    WHERE p.last_seen_at > getvariable('window_start') - INTERVAL 10 MINUTE AND p.last_seen_at <= getvariable('epoch_time')
      AND EXISTS (SELECT 1 FROM servers n WHERE n.server_name = p.server_name AND n.first_epoch > p.last_epoch)
),
changes AS (
    SELECT epoch, ts, leader, prev FROM (
        SELECT epoch, ts, leader, lag(leader) OVER (ORDER BY epoch) AS prev FROM per_epoch)
    WHERE prev IS NOT NULL AND leader <> prev
),
-- Each step-down excuses the first change away from that server within
-- two collect intervals after it, and no other.
excused AS (
    SELECT DISTINCT (SELECT min(c.epoch) FROM changes c
        WHERE c.prev = x.server_name AND c.ts > x.ts AND c.ts <= x.ts + 2 * (SELECT gap FROM step)) AS epoch
    FROM stepped x
),
observed AS (
    SELECT (SELECT count(*) FROM changes WHERE epoch NOT IN (SELECT epoch FROM excused WHERE epoch IS NOT NULL)) AS n,
        array_to_string(list(leader ORDER BY epoch) FILTER (WHERE prev IS NULL OR leader <> prev), ' to ') AS path
    FROM (SELECT epoch, leader, lag(leader) OVER (ORDER BY epoch) AS prev FROM per_epoch)
),
elected AS (
    SELECT id, received_at AS ts, server_name,
        coalesce(lag(server_name) OVER (ORDER BY received_at, id),
            (SELECT arg_min(leader, epoch) FROM per_epoch)) AS prev
    FROM events
    WHERE kind = 'meta.leader_elected' AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
),
elected_excused AS (
    SELECT DISTINCT (SELECT arg_min(el.id, el.ts) FROM elected el
        WHERE el.prev = x.server_name AND el.ts > x.ts AND el.ts <= x.ts + 2 * (SELECT gap FROM step)) AS id
    FROM stepped x
),
advised AS (
    SELECT count(*) FILTER (WHERE id NOT IN (SELECT id FROM elected_excused WHERE id IS NOT NULL)) AS n,
        array_to_string(list(server_name ORDER BY ts), ' to ') AS path
    FROM elected
)
SELECT
    '' AS entity_key,
    'The meta leader changed ' || greatest(o.n, e.n) || ' times in the last ' || getvariable('lookback_text') || ': '
        || CASE WHEN e.n >= o.n THEN e.path || ' (advisories)' ELSE o.path || ' (snapshots)' END
        || ', not counting one hand-over after each restart or lame duck.' AS detail,
    greatest(o.n, e.n) AS value
FROM observed o, advised e
WHERE greatest(o.n, e.n) > getvariable('max_changes')
