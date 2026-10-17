/*
code: stream-010
name: Stream missing on an assigned server
entity: stream
category: agreement
severity: warning
severity_rule: critical when the stream is missing on more than half of its assigned servers
kind: operational
description: >-
  The stream leader lists a server as a replica, that server answered JSZ in this epoch, and it does not report the
  stream. The replica is assigned but absent: the server lost or never created it. Servers that restarted within
  restart_window are left out.
remediation: >-
  Look at the server's log for errors creating or recovering the stream, and at its healthz (server-002 names the
  stream when the server knows it is missing). If the replica does not appear within a few minutes after a restart,
  remove it with `nats stream cluster peer-remove <stream> <server>` so the meta leader places a new replica that
  copies the stream from the leader.
value_name: assigned servers without the stream
value_basis: assigned peers that answered JSZ and do not report the stream
*/
WITH lead AS (
    SELECT ss.account, ss.stream, ss.server_id, s.server_name AS leader
    FROM stream_stats ss JOIN servers s USING (server_id)
    WHERE ss.epoch = getvariable('epoch') AND ss.is_leader AND ss.cluster IS NOT NULL
),
assigned AS (
    SELECT l.account, l.stream, l.leader, r.peer_name
    FROM lead l JOIN stream_replicas r
      ON r.epoch = getvariable('epoch') AND r.server_id = l.server_id AND r.account = l.account AND r.stream = l.stream
),
answered AS (
    SELECT DISTINCT s.server_name, e.server_id
    FROM epoch_endpoints e JOIN servers s USING (server_id)
    WHERE e.epoch = getvariable('epoch') AND e.endpoint = 'JSZ' AND e.state = 'ok'
      AND e.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
),
missing AS (
    SELECT a.* FROM assigned a JOIN answered w ON w.server_name = a.peer_name
    WHERE NOT EXISTS (
        SELECT 1 FROM stream_stats x
        WHERE x.epoch = getvariable('epoch') AND x.server_id = w.server_id AND x.account = a.account AND x.stream = a.stream)
)
SELECT
    m.account || '/' || m.stream AS entity_key,
    CASE WHEN 2 * count(*) > (SELECT count(*) FROM assigned a WHERE a.account = m.account AND a.stream = m.stream) + 1
        THEN 'critical' ELSE 'warning' END AS severity,
    'Leader ' || any_value(m.leader) || ' assigns the stream to ' || array_to_string(list(m.peer_name ORDER BY m.peer_name), ', ')
        || ', which answered JSZ without it.' AS detail,
    count(*) AS value
FROM missing m
GROUP BY m.account, m.stream
