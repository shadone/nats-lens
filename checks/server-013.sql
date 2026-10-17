/*
code: server-013
name: Raft write error
entity: server
category: health
severity: critical
kind: operational
description: >-
  A raft group on the server reports a write-ahead log error. nats-server stops the group's node after such an
  error, so the stream or consumer has one replica fewer until the server restarts. The error text is only in the
  server log.
remediation: >-
  Read the server log for the write error. The usual cause is the JetStream volume: full (server-007), read-only
  after a disk error, or out of inodes. Check the PVC (`kubectl exec <pod> -- df -h /data`, `df -i`). Fix the disk,
  then restart the pod so the group starts again and catches up from the leader.
value_name: raft groups with a WAL error
value_basis: raft groups whose write-ahead log reports an error
*/
SELECT
    s.server_name AS entity_key,
    s.server_name || ': write-ahead log error on ' || count(*) || CASE WHEN count(*) = 1 THEN ' raft group: ' ELSE ' raft groups: ' END
        || array_to_string(list(coalesce(g.kind || ' ' || g.account || '/' || g.stream || coalesce('/' || g.consumer, ''), r.group_id)
            ORDER BY r.group_id)[1:5], ', ')
        || CASE WHEN count(*) > 5 THEN ', ...' ELSE '' END || ' (' || min(r.wal_error) || ').' AS detail,
    count(*) AS value
FROM raft_stats r JOIN servers s USING (server_id) LEFT JOIN raft_groups g USING (group_id)
WHERE r.epoch = getvariable('epoch') AND r.wal_error IS NOT NULL
GROUP BY s.server_name
