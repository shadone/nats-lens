/*
code: stream-009
name: Stream peer set differs across servers
entity: stream
category: agreement
severity: warning
kind: operational
suppressed_by: [stream-001]
thresholds:
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive epochs the servers must disagree before the finding fires.
description: >-
  The servers that hold the stream disagree on which servers are its replicas (each server's own name plus the
  peers it lists) in each of the last min_epochs epochs. A restarting, recovering (JetStream not ready, catching up)
  or leaderless server's view is left out while the healthy servers form a majority of the replicas, and whenever
  it is a strict subset of the majority's view: such a server lists its peers as it finds them. It is quiet while
  stream-001 fires for the stream: a lost quorum shows as differing views too. During a peer move a difference lasts a few epochs; a lasting one
  means a replica follows a group the others have left.
remediation: >-
  Compare `nats stream info <stream>` on each server in the detail. If a server lists a peer the others removed,
  restart that server's pod so it reloads the assignment from the meta leader. If the stream was being moved
  (placement change, peer-remove), wait for the move to finish before acting.
value_name: distinct peer sets
value_basis: peer sets the servers report
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
    SELECT account, stream, max(replicas) AS r FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
),
views_all AS (
    SELECT ss.epoch, ss.account, ss.stream, s.server_name,
        EXISTS (SELECT 1 FROM rs WHERE rs.epoch = ss.epoch AND rs.server_id = ss.server_id)
            OR EXISTS (SELECT 1 FROM rc WHERE rc.epoch = ss.epoch AND rc.server_id = ss.server_id)
            OR coalesce(ss.leader, '') = '' AS unhealthy,
        list_sort(list_distinct(list_append(
            coalesce((SELECT list(r.peer_name) FROM stream_replicas r
                      WHERE r.epoch = ss.epoch AND r.server_id = ss.server_id AND r.account = ss.account AND r.stream = ss.stream), []),
            s.server_name))) AS peer_list
    FROM stream_stats ss JOIN servers s USING (server_id)
    WHERE ss.epoch IN (SELECT epoch FROM last) AND ss.cluster IS NOT NULL
),
-- The view most healthy servers (neither restarting, recovering nor
-- leaderless) hold, or most of all servers when none is healthy.
majority AS (
    SELECT epoch, account, stream,
        coalesce(mode(peer_list) FILTER (WHERE NOT unhealthy), mode(peer_list)) AS peer_list,
        count(*) FILTER (WHERE NOT unhealthy) AS healthy
    FROM views_all GROUP BY epoch, account, stream
),
-- An unhealthy server's view is left out while the healthy servers form a
-- majority of the replicas, and whenever it is a strict subset of the
-- majority's view: a recovering server lists its peers as it finds them.
views AS (
    SELECT v.epoch, v.account, v.stream, v.server_name, array_to_string(v.peer_list, ',') AS peers
    FROM views_all v JOIN majority j USING (epoch, account, stream) LEFT JOIN cfg c USING (account, stream)
    WHERE NOT v.unhealthy
       OR (j.healthy < coalesce(c.r, 1) // 2 + 1
           AND NOT (list_has_all(j.peer_list, v.peer_list) AND len(v.peer_list) < len(j.peer_list)))
),
groups AS (
    SELECT epoch, account, stream, peers, array_to_string(list(server_name ORDER BY server_name), ', ') AS servers
    FROM views GROUP BY epoch, account, stream, peers
),
differ AS (
    SELECT epoch, account, stream,
        array_to_string(list(servers || ' see [' || peers || ']' ORDER BY servers), '; ') AS views, count(*) AS sets
    FROM groups GROUP BY epoch, account, stream
    HAVING count(*) > 1
)
SELECT
    account || '/' || stream AS entity_key,
    arg_max(views, epoch) || '.' AS detail,
    arg_max(sets, epoch) AS value
FROM differ
GROUP BY account, stream
HAVING count(*) >= getvariable('min_epochs') AND max(epoch) = getvariable('epoch')
