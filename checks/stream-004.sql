/*
code: stream-004
name: Stream near its message or byte limit
entity: stream
category: storage
severity: warning
severity_rule: >-
  warning from warn_ratio and critical from crit_ratio, since at the limit publishes fail (discard new) or work and
  interest streams drop messages nobody processed (discard old); a limits stream with discard old is a ring buffer
  that drops its oldest messages before max_age, which is info
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.8"
    unit: ratio
    description: Share of max_msgs or max_bytes at which the finding fires.
  - name: crit_ratio
    default: "0.95"
    unit: ratio
    description: Share at which a discard-new stream is critical.
description: >-
  The stream holds more than warn_ratio of its max_msgs or max_bytes, as its leader reports it.
remediation: >-
  With discard new the stream rejects publishes at the limit: the publisher gets an error (for a work queue of
  jobs, a job cannot be submitted). Make the consumers drain it (consumer-002, consumer-006) or raise the limit
  in the stream's definition (a NACK resource, or wherever the stream is declared). With discard old, check
  that dropping the oldest messages before max_age is acceptable for the stream's readers.
related_config: [max_msgs, max_bytes, discard, retention, max_age]
value_name: share of the limit
value_basis: messages or bytes divided by the stream's limit, the larger of the two
*/
WITH lead AS (
    SELECT ss.account, ss.stream, ss.server_id, ss.messages, ss.bytes, ss.leader
    FROM stream_stats ss
    WHERE ss.epoch = getvariable('epoch') AND ss.is_leader
),
cfg AS (
    SELECT l.*, c.max_msgs, c.max_bytes, c.discard, c.max_age_ns, c.retention,
        CASE WHEN c.max_msgs > 0 THEN l.messages / c.max_msgs END AS msgs_ratio,
        CASE WHEN c.max_bytes > 0 THEN l.bytes / c.max_bytes END AS bytes_ratio
    FROM lead l JOIN stream_config c
      ON c.server_id = l.server_id AND c.account = l.account AND c.stream = l.stream
     AND c.first_epoch <= getvariable('epoch') AND c.last_epoch >= getvariable('epoch')
),
r AS (
    SELECT *, greatest(coalesce(msgs_ratio, 0), coalesce(bytes_ratio, 0)) AS ratio FROM cfg
)
SELECT
    account || '/' || stream AS entity_key,
    CASE
        WHEN discard <> 'new' AND retention = 'limits' THEN 'info'
        WHEN ratio >= getvariable('crit_ratio') THEN 'critical'
        ELSE 'warning'
    END AS severity,
    concat_ws(', ',
        CASE WHEN msgs_ratio IS NOT NULL THEN messages || ' of ' || max_msgs || ' messages (' || round(100 * msgs_ratio, 1) || '%)' END,
        CASE WHEN bytes_ratio IS NOT NULL THEN format_bytes(bytes) || ' of ' || format_bytes(max_bytes) || ' (' || round(100 * bytes_ratio, 1) || '%)' END)
        || '; retention ' || retention || ', discard ' || discard || '.' AS detail,
    round(ratio, 4) AS value,
    json_object('max_msgs', CAST(max_msgs AS VARCHAR), 'max_bytes', CAST(max_bytes AS VARCHAR), 'discard', discard,
        'retention', retention, 'max_age', ns_text(max_age_ns)) AS related_config,
    leader AS related_source
FROM r
WHERE ratio >= getvariable('warn_ratio')
