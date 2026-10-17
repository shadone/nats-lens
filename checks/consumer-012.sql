/*
code: consumer-012
name: Consumer missing on an assigned server
entity: consumer
category: agreement
severity: warning
kind: operational
description: >-
  The consumer leader lists a server as a replica, that server answered JSZ in this epoch, and it does not report
  the consumer: from that server's view the consumer does not exist. Servers that restarted within restart_window
  are left out.
remediation: >-
  Look at the server's log for errors creating or recovering the consumer, and at its healthz (server-002). A
  replica that does not appear within a few minutes after a restart can be dropped with `nats consumer cluster
  peer-remove <stream> <consumer> <server>`, after which the meta leader places a new one.
value_name: assigned servers without the consumer
value_basis: assigned peers that answered JSZ and do not report the consumer
*/
WITH lead AS (
    SELECT c.account, c.stream, c.consumer, c.server_id, s.server_name AS leader
    FROM consumer_stats c JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch') AND c.is_leader AND c.cluster IS NOT NULL
),
assigned AS (
    SELECT l.account, l.stream, l.consumer, l.leader, r.peer_name
    FROM lead l JOIN consumer_replicas r
      ON r.epoch = getvariable('epoch') AND r.server_id = l.server_id AND r.account = l.account
     AND r.stream = l.stream AND r.consumer = l.consumer
),
answered AS (
    SELECT DISTINCT s.server_name, e.server_id
    FROM epoch_endpoints e JOIN servers s USING (server_id)
    WHERE e.epoch = getvariable('epoch') AND e.endpoint = 'JSZ' AND e.state = 'ok'
      AND e.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
)
SELECT
    a.account || '/' || a.stream || '/' || a.consumer AS entity_key,
    'Leader ' || any_value(a.leader) || ' assigns the consumer to ' || array_to_string(list(a.peer_name ORDER BY a.peer_name), ', ')
        || ', which answered JSZ without it.' AS detail,
    count(*) AS value
FROM assigned a JOIN answered w ON w.server_name = a.peer_name
WHERE NOT EXISTS (
    SELECT 1 FROM consumer_stats x
    WHERE x.epoch = getvariable('epoch') AND x.server_id = w.server_id AND x.account = a.account
      AND x.stream = a.stream AND x.consumer = a.consumer)
GROUP BY a.account, a.stream, a.consumer
