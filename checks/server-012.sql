/*
code: server-012
name: Raft groups catching up for too long
entity: server
category: health
severity: warning
kind: operational
thresholds:
  - name: max_catchup
    default: 5m
    unit: duration
    description: How long a raft group may report catching up on the server before the finding fires.
description: >-
  RAFTZ on the server has reported one or more raft groups as catching up in every collection for longer than
  max_catchup. A replica that is catching up does not count towards the group's quorum.
remediation: >-
  A server that restarted (server-004) catches up every stream and consumer it holds; large streams take longer.
  Watch whether the lag in the detail falls between collections. If it does not, the server cannot get the data from the
  leader: check the routes between the pods, the leader's load, and the server log for "catchup" errors. A replica
  stuck for good can be dropped and rebuilt with `nats stream cluster peer-remove <stream> <server>`, after which the
  meta leader places a new replica.
related_config: [max_catchup]
value_name: minutes catching up
value_basis: time the longest catching-up raft group has been catching up
*/
WITH cur AS (
    SELECT r.server_id, r.group_id FROM raft_stats r
    WHERE r.epoch = getvariable('epoch') AND r.catching_up
),
last_done AS (
    SELECT r.server_id, r.group_id, max(r.epoch) AS epoch
    FROM raft_stats r JOIN cur USING (server_id, group_id)
    WHERE NOT coalesce(r.catching_up, false)
    GROUP BY r.server_id, r.group_id
),
runs AS (
    SELECT c.server_id, c.group_id,
        (SELECT min(e.started_at) FROM raft_stats r JOIN epochs e USING (epoch)
         WHERE r.server_id = c.server_id AND r.group_id = c.group_id AND r.catching_up
           AND r.epoch > coalesce(l.epoch, 0)) AS since
    FROM cur c LEFT JOIN last_done l USING (server_id, group_id)
),
long AS (
    SELECT s.server_name, r.group_id, r.since,
        coalesce(g.kind || ' ' || g.account || '/' || g.stream || coalesce('/' || g.consumer, ''), r.group_id) AS asset
    FROM runs r JOIN servers s USING (server_id) LEFT JOIN raft_groups g USING (group_id)
    WHERE getvariable('epoch_time') - r.since > getvariable('max_catchup')
)
SELECT
    server_name AS entity_key,
    server_name || ' has been catching up on ' || count(*) || CASE WHEN count(*) = 1 THEN ' raft group' ELSE ' raft groups' END
        || ' for up to ' || CAST(date_trunc('second', getvariable('epoch_time') - min(since)) AS VARCHAR) || ': '
        || array_to_string(list(asset ORDER BY since)[1:5], ', ') || CASE WHEN count(*) > 5 THEN ', ...' ELSE '' END
        || '.' AS detail,
    round(epoch(getvariable('epoch_time') - min(since)) / 60, 1) AS value
FROM long
GROUP BY server_name
