/*
code: stream-008
name: Stream creation time differs across servers
entity: stream
category: agreement
severity: info
severity_rule: >-
  warning when the replicas' contents (first sequence, last sequence and message count) differ as well, in this
  epoch and the previous one
kind: operational
thresholds:
  - name: position_tolerance
    default: "100"
    unit: count
    description: >-
      Messages a replica's first or last sequence or message count may differ from the leader's and still agree.
  - name: position_tolerance_ratio
    default: "0.01"
    unit: ratio
    description: The tolerance as a share of the leader's value, when that is more than position_tolerance.
  - name: position_ahead_tolerance
    default: "10"
    unit: count
    description: Messages a replica's last sequence or count may be ahead of the leader's and still agree.
  - name: position_ahead_tolerance_ratio
    default: "0.001"
    unit: ratio
    description: The ahead tolerance as a share of the leader's position, when that is more; replies are read apart.
description: >-
  The servers that hold the stream report different creation times for it. When their contents agree (a replica's
  last sequence and message count at most position_tolerance, or position_tolerance_ratio of the leader's value if
  that is more, behind the leader and at most position_ahead_tolerance, or position_ahead_tolerance_ratio of the
  leader's value if that is more, ahead of it, its first sequence within the tolerance, current or not), a replica
  was rebuilt at another time (a server restore or a peer replaced) and holds the same messages as the others.
  When the contents differ as well, in two epochs in a row, one replica may hold a stream that was deleted and
  created again under the same name. A replica behind by more than the tolerance in two epochs warns even while it
  catches up, but only here, where the creation times differ. Restarting servers are left out until they have caught up.
remediation: >-
  The detail names the servers on each creation time and whether their contents agree. While they agree, no action
  is needed: the replicas were built at different times. When they differ, look on the Events page for the
  stream.deleted and stream.created events around the newer time and the client that made them, and step the
  stream leader down (`nats stream cluster step-down <stream>`) so the replicas resynchronise.
value_name: distinct creation times
value_basis: creation times the servers report
*/
WITH cur AS (
    SELECT c.account, c.stream, c.server_id, c.created, s.server_name
    FROM stream_config c JOIN servers s USING (server_id)
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch') AND c.created IS NOT NULL
      AND c.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
),
groups AS (
    SELECT account, stream, created, array_to_string(list(server_name ORDER BY server_name), ', ') AS servers
    FROM cur GROUP BY account, stream, created
),
pos AS (
    SELECT x.epoch, cur.account, cur.stream, cur.server_name, coalesce(x.is_leader, false) AS lead,
        x.first_seq, x.last_seq, x.messages
    FROM stream_stats x
    JOIN cur ON cur.server_id = x.server_id AND cur.account = x.account AND cur.stream = x.stream
    WHERE x.epoch IN (getvariable('epoch'), getvariable('prev_epoch'))
),
-- Every replica is compared with the leader, current or not; behind by
-- the tolerance agrees, ahead by more than position_ahead_tolerance never
-- does. A restarting replica catching up is left out by cur.
compared AS (
    SELECT p.*,
        coalesce(max(first_seq) FILTER (WHERE lead) OVER g, max(first_seq) OVER g) AS ref_first,
        coalesce(max(last_seq) FILTER (WHERE lead) OVER g, max(last_seq) OVER g) AS ref_last,
        coalesce(max(messages) FILTER (WHERE lead) OVER g, max(messages) OVER g) AS ref_messages
    FROM pos p
    WINDOW g AS (PARTITION BY p.epoch, p.account, p.stream)
),
differ AS (
    SELECT account, stream, epoch,
        bool_or(abs(first_seq - ref_first) > greatest(getvariable('position_tolerance'), getvariable('position_tolerance_ratio') * ref_first)
             OR last_seq - ref_last > greatest(getvariable('position_ahead_tolerance'), getvariable('position_ahead_tolerance_ratio') * ref_last)
             OR messages - ref_messages > greatest(getvariable('position_ahead_tolerance'), getvariable('position_ahead_tolerance_ratio') * ref_messages)
             OR ref_last - last_seq > greatest(getvariable('position_tolerance'), getvariable('position_tolerance_ratio') * ref_last)
             OR ref_messages - messages > greatest(getvariable('position_tolerance'),
                    getvariable('position_tolerance_ratio') * ref_messages)) AS apart,
        array_to_string(list(server_name || ' first ' || coalesce(first_seq::VARCHAR, '?') || ' last '
            || coalesce(last_seq::VARCHAR, '?') || ' messages ' || coalesce(messages::VARCHAR, '?') ORDER BY server_name), ', ') AS contents
    FROM compared GROUP BY account, stream, epoch
),
verdict AS (
    SELECT n.account, n.stream, n.contents, n.apart AND coalesce(p.apart, false) AS apart
    FROM differ n LEFT JOIN differ p ON p.account = n.account AND p.stream = n.stream AND p.epoch = getvariable('prev_epoch')
    WHERE n.epoch = getvariable('epoch')
)
SELECT
    g.account || '/' || g.stream AS entity_key,
    array_to_string(list(g.servers || ' ' || strftime(g.created, '%Y-%m-%dT%H:%M:%SZ') ORDER BY g.created), '; ') || '. '
        || CASE WHEN any_value(v.apart) THEN 'The replicas'' contents differ: ' || any_value(v.contents) || '.'
                ELSE 'The replicas'' contents agree.' END AS detail,
    CASE WHEN any_value(v.apart) THEN 'warning' ELSE 'info' END AS severity,
    count(*) AS value,
    max(g.created) AS condition_since
FROM groups g LEFT JOIN verdict v ON v.account = g.account AND v.stream = g.stream
GROUP BY g.account, g.stream
HAVING count(*) > 1
