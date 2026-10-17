/*
code: consumer-008
name: Consumer configuration differs across servers
entity: consumer
category: agreement
severity: warning
kind: operational
description: >-
  The servers that hold a replica of the consumer report different configurations for it (ack policy, ack_wait,
  max_deliver, filters, max_ack_pending and the other settings; the creation time is consumer-009). Every setting
  is compared on its own, a missing value included; restarting servers are left out until they have caught up.
remediation: >-
  The detail lists which servers agree and which settings differ. Right after a consumer update this clears within
  an epoch or two. If it stays, apply the definition again (the service that creates the consumer at startup does
  this on its next start, or `nats consumer edit`), and if the replicas still differ, step the consumer leader down
  (`nats consumer cluster step-down <stream> <consumer>`).
value_name: distinct configurations
value_basis: configuration hashes the servers report
*/
WITH cur AS (
    SELECT c.*, s.server_name, {'deliver_policy': c.deliver_policy, 'ack_policy': c.ack_policy, 'ack_wait_ns': c.ack_wait_ns, 'max_deliver': c.max_deliver, 'backoff_ns': c.backoff_ns, 'filter_subjects': c.filter_subjects, 'replay_policy': c.replay_policy, 'max_waiting': c.max_waiting, 'max_ack_pending': c.max_ack_pending, 'max_batch': c.max_batch, 'deliver_subject': c.deliver_subject, 'deliver_group': c.deliver_group, 'idle_heartbeat_ns': c.idle_heartbeat_ns, 'flow_control': c.flow_control, 'headers_only': c.headers_only, 'inactive_threshold_ns': c.inactive_threshold_ns, 'replicas': c.replicas, 'mem_storage': c.mem_storage, 'description': c.description} AS fingerprint
    FROM consumer_config c JOIN servers s USING (server_id)
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
      AND c.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
),
withref AS (
    SELECT *,
        first_value(deliver_policy) OVER w AS ref_deliver_policy,
        first_value(ack_policy) OVER w AS ref_ack_policy,
        first_value(ack_wait_ns) OVER w AS ref_ack_wait_ns,
        first_value(max_deliver) OVER w AS ref_max_deliver,
        first_value(backoff_ns) OVER w AS ref_backoff_ns,
        first_value(filter_subjects) OVER w AS ref_filter_subjects,
        first_value(replay_policy) OVER w AS ref_replay_policy,
        first_value(max_waiting) OVER w AS ref_max_waiting,
        first_value(max_ack_pending) OVER w AS ref_max_ack_pending,
        first_value(max_batch) OVER w AS ref_max_batch,
        first_value(deliver_subject) OVER w AS ref_deliver_subject,
        first_value(deliver_group) OVER w AS ref_deliver_group,
        first_value(idle_heartbeat_ns) OVER w AS ref_idle_heartbeat_ns,
        first_value(flow_control) OVER w AS ref_flow_control,
        first_value(headers_only) OVER w AS ref_headers_only,
        first_value(inactive_threshold_ns) OVER w AS ref_inactive_threshold_ns,
        first_value(replicas) OVER w AS ref_replicas,
        first_value(mem_storage) OVER w AS ref_mem_storage,
        first_value(description) OVER w AS ref_description
    FROM cur
    WINDOW w AS (PARTITION BY account, stream, consumer ORDER BY server_name ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)
),
groups AS (
    SELECT account, stream, consumer, fingerprint, array_to_string(list(server_name ORDER BY server_name), ', ') AS servers
    FROM cur GROUP BY account, stream, consumer, fingerprint
),
fields AS (
    SELECT account, stream, consumer, list_filter([
        CASE WHEN bool_or(deliver_policy IS DISTINCT FROM ref_deliver_policy) THEN 'deliver_policy' END,
        CASE WHEN bool_or(ack_policy IS DISTINCT FROM ref_ack_policy) THEN 'ack_policy' END,
        CASE WHEN bool_or(ack_wait_ns IS DISTINCT FROM ref_ack_wait_ns) THEN 'ack_wait' END,
        CASE WHEN bool_or(max_deliver IS DISTINCT FROM ref_max_deliver) THEN 'max_deliver' END,
        CASE WHEN bool_or(backoff_ns IS DISTINCT FROM ref_backoff_ns) THEN 'backoff' END,
        CASE WHEN bool_or(filter_subjects IS DISTINCT FROM ref_filter_subjects) THEN 'filter_subjects' END,
        CASE WHEN bool_or(replay_policy IS DISTINCT FROM ref_replay_policy) THEN 'replay_policy' END,
        CASE WHEN bool_or(max_waiting IS DISTINCT FROM ref_max_waiting) THEN 'max_waiting' END,
        CASE WHEN bool_or(max_ack_pending IS DISTINCT FROM ref_max_ack_pending) THEN 'max_ack_pending' END,
        CASE WHEN bool_or(max_batch IS DISTINCT FROM ref_max_batch) THEN 'max_batch' END,
        CASE WHEN bool_or(deliver_subject IS DISTINCT FROM ref_deliver_subject) THEN 'deliver_subject' END,
        CASE WHEN bool_or(deliver_group IS DISTINCT FROM ref_deliver_group) THEN 'deliver_group' END,
        CASE WHEN bool_or(idle_heartbeat_ns IS DISTINCT FROM ref_idle_heartbeat_ns) THEN 'idle_heartbeat' END,
        CASE WHEN bool_or(flow_control IS DISTINCT FROM ref_flow_control) THEN 'flow_control' END,
        CASE WHEN bool_or(headers_only IS DISTINCT FROM ref_headers_only) THEN 'headers_only' END,
        CASE WHEN bool_or(inactive_threshold_ns IS DISTINCT FROM ref_inactive_threshold_ns) THEN 'inactive_threshold' END,
        CASE WHEN bool_or(replicas IS DISTINCT FROM ref_replicas) THEN 'replicas' END,
        CASE WHEN bool_or(mem_storage IS DISTINCT FROM ref_mem_storage) THEN 'mem_storage' END,
        CASE WHEN bool_or(description IS DISTINCT FROM ref_description) THEN 'description' END
    ], x -> x IS NOT NULL) AS differ
    FROM withref GROUP BY account, stream, consumer
)
SELECT
    f.account || '/' || f.stream || '/' || f.consumer AS entity_key,
    (SELECT count(*) FROM groups g WHERE g.account = f.account AND g.stream = f.stream AND g.consumer = f.consumer)
        || ' configurations (' || (SELECT array_to_string(list(g.servers ORDER BY g.servers), ' | ') FROM groups g
            WHERE g.account = f.account AND g.stream = f.stream AND g.consumer = f.consumer) || '); differing: '
        || array_to_string(f.differ, ', ') || '.' AS detail,
    (SELECT count(*) FROM groups g WHERE g.account = f.account AND g.stream = f.stream AND g.consumer = f.consumer) AS value
FROM fields f
WHERE len(f.differ) > 0
