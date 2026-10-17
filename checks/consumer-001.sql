/*
code: consumer-001
name: Consumer quorum lost
entity: consumer
category: health
severity: critical
kind: operational
thresholds:
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive collections the quorum must be lost before the finding fires; one collection is usually an election.
description: >-
  In each of the last min_epochs collections, the servers that report a replicated consumer do not agree by majority on
  a leader, or the leader they agree on sees fewer online replicas than a quorum, or that leader does not report
  and fewer than a quorum of replicas report at all. A restarting server's report is left out while the others can
  form a majority without it, but never when it says leaderless or, as leader, fewer than a quorum online.
  Nothing is delivered and no acknowledgement is recorded until a leader is elected again.
remediation: >-
  Bring back the servers that hold the consumer's replicas (server-001). A consumer usually loses quorum together
  with its stream (stream-001); if only the consumer did, its raft group may be stopped on a server (server-013) or
  catching up (server-012). For an R3 durable whose replica's server is gone for good, `nats consumer cluster
  peer-remove <stream> <consumer> <server>` lets the meta leader place a new replica.
value_name: replicas online
value_basis: online peers of the consumer's raft group, leader's view
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
scfg AS (
    SELECT account, stream, max(replicas) AS r FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
),
cfg AS (
    SELECT c.account, c.stream, c.consumer, max(CASE WHEN c.replicas > 0 THEN c.replicas ELSE s.r END) AS r
    FROM consumer_config c LEFT JOIN scfg s USING (account, stream)
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
    GROUP BY c.account, c.stream, c.consumer
),
rep_all AS (
    SELECT cs.epoch, cs.account, cs.stream, cs.consumer, s.server_name, nullif(cs.leader, '') AS leader, cs.is_leader,
        1 + coalesce(cs.replicas_reported, 0) - coalesce(cs.replicas_offline, 0) AS online,
        EXISTS (SELECT 1 FROM rs WHERE rs.epoch = cs.epoch AND rs.server_id = cs.server_id) AS restarting,
        EXISTS (SELECT 1 FROM rc WHERE rc.epoch = cs.epoch AND rc.server_id = cs.server_id) AS recovering
    FROM consumer_stats cs JOIN servers s USING (server_id)
    WHERE cs.epoch IN (SELECT epoch FROM last)
),
-- A restarting server's report is left out only when the healthy others
-- (neither restarting nor recovering, and naming a leader) can still form
-- a majority of the replicas, and never when it says leaderless or, as
-- leader, fewer than a quorum online: that is evidence, not noise.
rep AS (
    SELECT a.* FROM rep_all a JOIN cfg c USING (account, stream, consumer)
    WHERE NOT a.restarting OR a.leader IS NULL OR (a.is_leader AND a.online < c.r // 2 + 1)
       OR (SELECT count(*) FROM rep_all b WHERE b.epoch = a.epoch AND b.account = a.account AND b.stream = a.stream AND b.consumer = a.consumer
           AND NOT b.restarting AND NOT b.recovering AND b.leader IS NOT NULL) < c.r // 2 + 1
),
votes AS (
    SELECT epoch, account, stream, consumer, count(*) AS n, mode(leader) AS leader,
        array_to_string(list(server_name || ': ' || coalesce(leader, 'none') ORDER BY server_name), ', ') AS views
    FROM rep GROUP BY epoch, account, stream, consumer
),
judged AS (
    SELECT v.*, c.r,
        (SELECT count(*) FROM rep x WHERE x.epoch = v.epoch AND x.account = v.account AND x.stream = v.stream
           AND x.consumer = v.consumer AND x.leader = v.leader) AS agree,
        (SELECT any_value(x.online) FROM rep x WHERE x.epoch = v.epoch AND x.account = v.account AND x.stream = v.stream
           AND x.consumer = v.consumer AND x.server_name = v.leader AND x.is_leader) AS leader_online
        , (SELECT count(*) FROM rep_all x WHERE x.epoch = v.epoch AND x.account = v.account AND x.stream = v.stream AND x.consumer = v.consumer) AS reporting
    FROM votes v JOIN cfg c USING (account, stream, consumer)
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
    account || '/' || stream || '/' || consumer AS entity_key,
    arg_max(why, epoch) || ', for ' || count(*) || ' collections.' AS detail,
    arg_max(leader_online, epoch) AS value
FROM lost
WHERE why IS NOT NULL
GROUP BY account, stream, consumer
HAVING count(*) >= getvariable('min_epochs') AND max(epoch) = getvariable('epoch')
