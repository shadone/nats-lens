/*
code: stream-002
name: Stream replica offline
entity: stream
category: health
severity: warning
severity_rule: info while every offline replica's server is restarting or announced lame duck or shutdown
kind: operational
description: >-
  The stream leader reports one or more of the stream's replicas as offline. The stream keeps working while a
  quorum is online, but one more lost replica stops it (stream-001). A replica whose server is restarting or
  announced its shutdown, as in a rolling restart, is info.
remediation: >-
  The detail names the offline replicas and how long ago the leader last saw them. A replica on a server that is
  down (server-001) comes back with the server. A replica offline on a server that answers points at the routes
  between the two pods, or at the replica's raft group being stopped on that server (server-013).
value_name: replicas offline
value_basis: replicas the leader reports offline
*/
WITH lead AS (
    SELECT ss.account, ss.stream, ss.server_id, ss.leader
    FROM stream_stats ss
    WHERE ss.epoch = getvariable('epoch') AND ss.is_leader
),
off AS (
    SELECT l.account, l.stream, l.leader, r.peer_name, r.active_ns, n.planned, n.why
    FROM lead l JOIN stream_replicas r
      ON r.epoch = getvariable('epoch') AND r.server_id = l.server_id AND r.account = l.account AND r.stream = l.stream
    LEFT JOIN not_ready(getvariable('epoch')) n ON n.server_name = r.peer_name AND n.planned
    WHERE r.offline
)
SELECT
    account || '/' || stream AS entity_key,
    CASE WHEN bool_and(coalesce(planned, false)) THEN 'info' ELSE 'warning' END AS severity,
    'Leader ' || any_value(leader) || ' reports ' || count(*) || CASE WHEN count(*) = 1 THEN ' replica' ELSE ' replicas' END
        || ' offline: ' || array_to_string(list(peer_name || coalesce(' (' || nullif(concat_ws('; ',
                CASE WHEN active_ns >= 1e9 THEN 'last seen ' || CAST(round(active_ns / 1e9) AS BIGINT) || 's ago' END,
                CASE WHEN planned THEN why END), '') || ')', '')
            ORDER BY peer_name), ', ') || '.' AS detail,
    count(*) AS value
FROM off
GROUP BY account, stream
