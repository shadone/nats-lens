/*
code: consumer-009
name: Consumer creation time differs across servers
entity: consumer
category: agreement
severity: info
severity_rule: >-
  warning when the replicas' delivered or acknowledged positions differ as well, in this collection and the previous one
kind: operational
thresholds:
  - name: position_tolerance
    default: "100"
    unit: count
    description: Messages a replica's delivered or ack floor position may trail or lead the leader's and still agree.
  - name: position_tolerance_ratio
    default: "0.01"
    unit: ratio
    description: The tolerance as a share of the leader's position, when that is more than position_tolerance.
  - name: position_ahead_tolerance
    default: "10"
    unit: count
    description: Messages a replica may be ahead of the leader and still agree; a follower is never ahead for long.
  - name: position_ahead_tolerance_ratio
    default: "0.001"
    unit: ratio
    description: The ahead tolerance as a share of the leader's position, when that is more; replies are read apart.
description: >-
  The servers that hold the consumer report different creation timestamps. This is a metadata disagreement,
  not proof of a restore, rebuild, recreation or unhealthy replica. Positions agree when a replica is at most
  position_tolerance messages, or position_tolerance_ratio of the leader's position if that is more, behind it,
  and at most position_ahead_tolerance messages, or position_ahead_tolerance_ratio if that is more, ahead.
  Currentness does not change this position comparison. Differing positions in two collections raise a warning,
  including a replica that is catching up. Servers that restarted within restart_window are left out.
remediation: >-
  Compare the reported timestamps with delivered and acknowledged positions, replica currentness and lag,
  and configuration across servers. Inspect captured consumer.created and consumer.deleted lifecycle events
  around the reported times, and server logs if available. Matching positions do not prove identical state or
  explain the timestamp difference; timestamps alone do not prove a restore or rebuild. Missing captured events
  do not rule out a lifecycle change. Investigate persistent position or configuration differences before planning
  any intervention. A leader step-down does not guarantee repair; do not delete or recreate a durable based only
  on this metadata finding, as that can lose its position or replay messages.
value_name: distinct creation times
value_basis: creation times the servers report
*/
WITH cur AS (
    SELECT c.account, c.stream, c.consumer, c.server_id, c.created, s.server_name
    FROM consumer_config c JOIN servers s USING (server_id)
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch') AND c.created IS NOT NULL
      AND c.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
),
groups AS (
    SELECT account, stream, consumer, created, array_to_string(list(server_name ORDER BY server_name), ', ') AS servers
    FROM cur GROUP BY account, stream, consumer, created
),
pos AS (
    SELECT x.epoch, cur.account, cur.stream, cur.consumer, cur.server_name, coalesce(x.is_leader, false) AS lead,
        x.delivered_stream_seq, x.ack_floor_stream_seq
    FROM consumer_stats x
    JOIN cur ON cur.server_id = x.server_id AND cur.account = x.account AND cur.stream = x.stream
      AND cur.consumer = x.consumer
    WHERE x.epoch IN (getvariable('epoch'), getvariable('prev_epoch'))
),
-- Every replica is compared with the leader, current or not: one that
-- kept an old copy is often listed as not current. Behind by the
-- tolerance agrees (a busy follower); ahead of the leader by more than
-- position_ahead_tolerance never does. A restarting replica catching up
-- is left out by cur.
compared AS (
    SELECT p.*,
        coalesce(max(delivered_stream_seq) FILTER (WHERE lead) OVER g, max(delivered_stream_seq) OVER g) AS ref_delivered,
        coalesce(max(ack_floor_stream_seq) FILTER (WHERE lead) OVER g, max(ack_floor_stream_seq) OVER g) AS ref_ack
    FROM pos p
    WINDOW g AS (PARTITION BY p.epoch, p.account, p.stream, p.consumer)
),
differ AS (
    SELECT account, stream, consumer, epoch,
        bool_or(delivered_stream_seq - ref_delivered > greatest(getvariable('position_ahead_tolerance'), getvariable('position_ahead_tolerance_ratio') * ref_delivered)
             OR ack_floor_stream_seq - ref_ack > greatest(getvariable('position_ahead_tolerance'), getvariable('position_ahead_tolerance_ratio') * ref_ack)
             OR ref_delivered - delivered_stream_seq > greatest(getvariable('position_tolerance'),
                    getvariable('position_tolerance_ratio') * ref_delivered)
             OR ref_ack - ack_floor_stream_seq > greatest(getvariable('position_tolerance'),
                    getvariable('position_tolerance_ratio') * ref_ack)) AS apart,
        array_to_string(list(server_name || ' delivered ' || coalesce(delivered_stream_seq::VARCHAR, '?')
            || ' ack floor ' || coalesce(ack_floor_stream_seq::VARCHAR, '?') ORDER BY server_name), ', ') AS positions
    FROM compared GROUP BY account, stream, consumer, epoch
),
verdict AS (
    SELECT n.account, n.stream, n.consumer, n.positions,
        n.apart AND coalesce(p.apart, false) AS apart
    FROM differ n LEFT JOIN differ p ON p.account = n.account AND p.stream = n.stream AND p.consumer = n.consumer
      AND p.epoch = getvariable('prev_epoch')
    WHERE n.epoch = getvariable('epoch')
)
SELECT
    g.account || '/' || g.stream || '/' || g.consumer AS entity_key,
    array_to_string(list(g.servers || ' ' || strftime(g.created, '%Y-%m-%dT%H:%M:%SZ') ORDER BY g.created), '; ') || '. '
        || CASE WHEN any_value(v.apart) THEN 'Delivered and acknowledged positions differ: ' || any_value(v.positions) || '.'
                ELSE 'Delivered and acknowledged positions agree.' END AS detail,
    CASE WHEN any_value(v.apart) THEN 'warning' ELSE 'info' END AS severity,
    count(*) AS value,
    max(g.created) AS condition_since
FROM groups g LEFT JOIN verdict v ON v.account = g.account AND v.stream = g.stream AND v.consumer = g.consumer
GROUP BY g.account, g.stream, g.consumer
HAVING count(*) > 1
