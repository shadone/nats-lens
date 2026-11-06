/*
code: stream-008
name: Stream creation time differs across servers
entity: stream
category: agreement
severity: info
severity_rule: >-
  warning when the replicas' contents (first sequence, last sequence and message count) differ as well, in this
  collection and the previous one
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
  The servers that hold the stream report different creation timestamps. This is a metadata disagreement,
  not proof of a restore, rebuild, recreation or unhealthy replica. Sequence boundaries and message counts
  agree when a replica's last sequence and count are at most position_tolerance, or position_tolerance_ratio
  of the leader's value if that is more, behind it, and at most position_ahead_tolerance, or
  position_ahead_tolerance_ratio of the leader's value if that is more, ahead. Its first sequence must be within
  position_tolerance, or position_tolerance_ratio of the leader's first sequence if that is more.
  Currentness does not change this comparison. Differences beyond these tolerances in two collections raise
  a warning, including a replica that is catching up. Servers that restarted within restart_window are left out.
  Matching boundaries and counts do not prove identical messages or explain the timestamp difference.
remediation: >-
  Compare the reported timestamps with first and last sequences, message counts, replica currentness and lag,
  and configuration across servers. Inspect captured stream.created and stream.deleted lifecycle events
  around the reported times, and server logs if available. Matching boundaries and counts do not prove identical
  messages; timestamps alone do not prove a restore or rebuild. Missing captured events do not rule out a
  lifecycle change. Investigate persistent position, count or configuration differences before planning any
  intervention. A leader step-down does not guarantee repair; do not delete or recreate a stream based only
  on this metadata finding, as that can lose messages.
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
