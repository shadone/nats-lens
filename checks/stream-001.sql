/*
code: stream-001
name: Stream quorum lost
entity: stream
category: health
severity: critical
kind: operational
thresholds:
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive collections the quorum must be lost before the finding fires; one collection is usually an election.
description: >-
  In each of the last min_epochs collections, the servers that report a replicated stream do not agree by majority on
  a leader, or the leader they agree on sees fewer online replicas than a quorum (2 of 3 for R3), or that leader does not report and fewer than a quorum of replicas report at all. Publishes to the
  stream fail until a leader is elected again. A restarting server's report is left out while the other reporters
  can form a majority without it (just after a restart it may think it leads alone), but never when it says the
  stream has no leader or, as leader, fewer than a quorum online.
remediation: >-
  Bring back the servers that hold the stream's replicas (server-001 lists the missing ones; the stream page lists
  the peers). During a GKE node upgrade quorum returns when the evicted pod is ready again; if it does not, the
  replicas may be catching up (server-012). If a replica's server is gone for good, remove it with `nats stream
  cluster peer-remove <stream> <server>` so the meta leader places a new one; never do that while the remaining
  replicas are behind.
value_name: replicas online
value_basis: online peers of the stream's raft group, leader's view
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
rs AS (
    SELECT epoch, server_id FROM restarting((SELECT min(epoch) FROM last), getvariable('epoch'))
),
rc AS (
    SELECT epoch, server_id FROM recovering((SELECT min(epoch) FROM last), getvariable('epoch'))
),
cfg AS (
    SELECT account, stream, max(replicas) AS r
    FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
),
rep_all AS (
    SELECT ss.epoch, ss.account, ss.stream, s.server_name, nullif(ss.leader, '') AS leader, ss.is_leader,
        1 + coalesce(ss.replicas_reported, 0) - coalesce(ss.replicas_offline, 0) AS online,
        EXISTS (SELECT 1 FROM rs WHERE rs.epoch = ss.epoch AND rs.server_id = ss.server_id) AS restarting,
        EXISTS (SELECT 1 FROM rc WHERE rc.epoch = ss.epoch AND rc.server_id = ss.server_id) AS recovering
    FROM stream_stats ss JOIN servers s USING (server_id)
    WHERE ss.epoch IN (SELECT epoch FROM last)
),
-- A restarting server's report is left out only when the healthy others
-- (neither restarting nor recovering, and naming a leader) can still form
-- a majority of the replicas, and never when it says leaderless or, as
-- leader, fewer than a quorum online: that is evidence, not noise.
rep AS (
    SELECT a.* FROM rep_all a JOIN cfg c USING (account, stream)
    WHERE NOT a.restarting OR a.leader IS NULL OR (a.is_leader AND a.online < c.r // 2 + 1)
       OR (SELECT count(*) FROM rep_all b WHERE b.epoch = a.epoch AND b.account = a.account AND b.stream = a.stream
           AND NOT b.restarting AND NOT b.recovering AND b.leader IS NOT NULL) < c.r // 2 + 1
),
votes AS (
    SELECT epoch, account, stream, count(*) AS n, mode(leader) AS leader,
        array_to_string(list(server_name || ': ' || coalesce(leader, 'none') ORDER BY server_name), ', ') AS views
    FROM rep GROUP BY epoch, account, stream
),
judged AS (
    SELECT v.*, c.r,
        (SELECT count(*) FROM rep x WHERE x.epoch = v.epoch AND x.account = v.account AND x.stream = v.stream
           AND x.leader = v.leader) AS agree,
        (SELECT any_value(x.online) FROM rep x WHERE x.epoch = v.epoch AND x.account = v.account AND x.stream = v.stream
           AND x.server_name = v.leader AND x.is_leader) AS leader_online,
        (SELECT count(*) FROM rep_all x WHERE x.epoch = v.epoch AND x.account = v.account AND x.stream = v.stream) AS reporting
    FROM votes v JOIN cfg c USING (account, stream)
    WHERE c.r > 1
),
lost AS (
    SELECT *, CASE
        WHEN leader IS NULL OR 2 * agree <= n THEN 'No leader that the reporting servers agree on (' || views || ')'
        WHEN leader_online < r // 2 + 1 THEN 'Leader ' || leader || ' sees ' || leader_online || ' of ' || r
            || ' replicas online; quorum needs ' || (r // 2 + 1)
        WHEN leader_online IS NULL AND reporting < r // 2 + 1 THEN 'Leader ' || leader || ' does not report; only '
            || reporting || ' of ' || r || ' replicas report (' || views || '); quorum needs ' || (r // 2 + 1)
    END AS why
    FROM judged
)
SELECT
    account || '/' || stream AS entity_key,
    arg_max(why, epoch) || ', for ' || count(*) || ' collections.' AS detail,
    arg_max(leader_online, epoch) AS value
FROM lost
WHERE why IS NOT NULL
GROUP BY account, stream
HAVING count(*) >= getvariable('min_epochs') AND max(epoch) = getvariable('epoch')
