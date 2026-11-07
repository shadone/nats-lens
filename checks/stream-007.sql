/*
code: stream-007
name: Stream configuration differs across servers
entity: stream
category: agreement
severity: warning
kind: operational
description: >-
  The servers that hold a replica of the stream report different configurations for it (subjects, retention,
  limits, replicas, placement and the other settings; the creation time is stream-008). Every setting is compared
  on its own, a missing value included. Servers that restarted within restart_window are left out. Replicas with
  different limits remove different messages, so their contents drift apart.
remediation: >-
  The detail lists which servers agree and which settings differ. A difference right after a stream update that
  clears within a collection or two is the update reaching every replica. If it stays, one replica missed the update:
  apply the definition again (`nats stream edit` with the intended values, or let NACK reconcile it), and if that
  does not converge, step the leader down (`nats stream cluster step-down <stream>`) so the replicas resynchronise.
value_name: distinct configurations
value_basis: configuration hashes the servers report
*/
WITH cur AS (
    SELECT c.*, s.server_name, {'subjects': c.subjects, 'retention': c.retention, 'storage': c.storage, 'replicas': c.replicas, 'max_consumers': c.max_consumers, 'max_msgs': c.max_msgs, 'max_bytes': c.max_bytes, 'max_age_ns': c.max_age_ns, 'max_msgs_per_subject': c.max_msgs_per_subject, 'max_msg_size': c.max_msg_size, 'discard': c.discard, 'duplicate_window_ns': c.duplicate_window_ns, 'mirror': c.mirror, 'sources': c.sources, 'placement_cluster': c.placement_cluster, 'placement_tags': c.placement_tags, 'compression': c.compression, 'sealed': c.sealed, 'deny_delete': c.deny_delete, 'deny_purge': c.deny_purge, 'allow_direct': c.allow_direct, 'allow_msg_ttl': c.allow_msg_ttl, 'description': c.description} AS fingerprint
    FROM stream_config c JOIN servers s USING (server_id)
    WHERE c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
      AND c.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
),
withref AS (
    SELECT *,
        first_value(subjects) OVER w AS ref_subjects,
        first_value(retention) OVER w AS ref_retention,
        first_value(storage) OVER w AS ref_storage,
        first_value(replicas) OVER w AS ref_replicas,
        first_value(max_consumers) OVER w AS ref_max_consumers,
        first_value(max_msgs) OVER w AS ref_max_msgs,
        first_value(max_bytes) OVER w AS ref_max_bytes,
        first_value(max_age_ns) OVER w AS ref_max_age_ns,
        first_value(max_msgs_per_subject) OVER w AS ref_max_msgs_per_subject,
        first_value(max_msg_size) OVER w AS ref_max_msg_size,
        first_value(discard) OVER w AS ref_discard,
        first_value(duplicate_window_ns) OVER w AS ref_duplicate_window_ns,
        first_value(mirror) OVER w AS ref_mirror,
        first_value(sources) OVER w AS ref_sources,
        first_value(placement_cluster) OVER w AS ref_placement_cluster,
        first_value(placement_tags) OVER w AS ref_placement_tags,
        first_value(compression) OVER w AS ref_compression,
        first_value(sealed) OVER w AS ref_sealed,
        first_value(deny_delete) OVER w AS ref_deny_delete,
        first_value(deny_purge) OVER w AS ref_deny_purge,
        first_value(allow_direct) OVER w AS ref_allow_direct,
        first_value(allow_msg_ttl) OVER w AS ref_allow_msg_ttl,
        first_value(description) OVER w AS ref_description
    FROM cur
    WINDOW w AS (PARTITION BY account, stream ORDER BY server_name ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)
),
groups AS (
    SELECT account, stream, fingerprint, array_to_string(list(server_name ORDER BY server_name), ', ') AS servers
    FROM cur GROUP BY account, stream, fingerprint
),
fields AS (
    SELECT account, stream, list_filter([
        CASE WHEN bool_or(subjects IS DISTINCT FROM ref_subjects) THEN 'subjects' END,
        CASE WHEN bool_or(retention IS DISTINCT FROM ref_retention) THEN 'retention' END,
        CASE WHEN bool_or(storage IS DISTINCT FROM ref_storage) THEN 'storage' END,
        CASE WHEN bool_or(replicas IS DISTINCT FROM ref_replicas) THEN 'replicas' END,
        CASE WHEN bool_or(max_consumers IS DISTINCT FROM ref_max_consumers) THEN 'max_consumers' END,
        CASE WHEN bool_or(max_msgs IS DISTINCT FROM ref_max_msgs) THEN 'max_msgs' END,
        CASE WHEN bool_or(max_bytes IS DISTINCT FROM ref_max_bytes) THEN 'max_bytes' END,
        CASE WHEN bool_or(max_age_ns IS DISTINCT FROM ref_max_age_ns) THEN 'max_age' END,
        CASE WHEN bool_or(max_msgs_per_subject IS DISTINCT FROM ref_max_msgs_per_subject) THEN 'max_msgs_per_subject' END,
        CASE WHEN bool_or(max_msg_size IS DISTINCT FROM ref_max_msg_size) THEN 'max_msg_size' END,
        CASE WHEN bool_or(discard IS DISTINCT FROM ref_discard) THEN 'discard' END,
        CASE WHEN bool_or(duplicate_window_ns IS DISTINCT FROM ref_duplicate_window_ns) THEN 'duplicate_window' END,
        CASE WHEN bool_or(mirror IS DISTINCT FROM ref_mirror) THEN 'mirror' END,
        CASE WHEN bool_or(sources IS DISTINCT FROM ref_sources) THEN 'sources' END,
        CASE WHEN bool_or(placement_cluster IS DISTINCT FROM ref_placement_cluster) THEN 'placement_cluster' END,
        CASE WHEN bool_or(placement_tags IS DISTINCT FROM ref_placement_tags) THEN 'placement_tags' END,
        CASE WHEN bool_or(compression IS DISTINCT FROM ref_compression) THEN 'compression' END,
        CASE WHEN bool_or(sealed IS DISTINCT FROM ref_sealed) THEN 'sealed' END,
        CASE WHEN bool_or(deny_delete IS DISTINCT FROM ref_deny_delete) THEN 'deny_delete' END,
        CASE WHEN bool_or(deny_purge IS DISTINCT FROM ref_deny_purge) THEN 'deny_purge' END,
        CASE WHEN bool_or(allow_direct IS DISTINCT FROM ref_allow_direct) THEN 'allow_direct' END,
        CASE WHEN bool_or(allow_msg_ttl IS DISTINCT FROM ref_allow_msg_ttl) THEN 'allow_msg_ttl' END,
        CASE WHEN bool_or(description IS DISTINCT FROM ref_description) THEN 'description' END
    ], lambda x: x IS NOT NULL) AS differ
    FROM withref GROUP BY account, stream
)
SELECT
    f.account || '/' || f.stream AS entity_key,
    (SELECT count(*) FROM groups g WHERE g.account = f.account AND g.stream = f.stream) || ' configurations ('
        || (SELECT array_to_string(list(g.servers ORDER BY g.servers), ' | ') FROM groups g
            WHERE g.account = f.account AND g.stream = f.stream) || '); differing: ' || array_to_string(f.differ, ', ') || '.' AS detail,
    (SELECT count(*) FROM groups g WHERE g.account = f.account AND g.stream = f.stream) AS value
FROM fields f
WHERE len(f.differ) > 0
