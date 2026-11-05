/*
code: stream-019
name: KV bucket history without a byte limit
entity: stream
category: storage
severity: info
kind: operational
lookback: 10m
thresholds:
  - name: min_revisions
    default: "5"
    unit: count
    description: Average revisions the bucket holds per key, its messages divided by its keys, at which it is reported.
  - name: min_bytes
    default: 100MiB
    unit: bytes
    description: Size below which a bucket is not reported, whatever its history.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the revisions per key and the size stay at or above this share of their
      thresholds, so a bucket near a threshold does not flap.
description: >-
  A KV bucket keeps more than one revision per key (history), holds on average min_revisions or more of them per key
  and min_bytes or more in all, and has no byte limit (max_bytes), so it grows by its history times its keys. Read
  from the leader's report. A bucket with a TTL is bounded in time but still multiplies every live key by its
  history. Not reported: a bucket whose max_bytes the inventory gives (-1 included, as a decision to leave it
  unbounded). A bucket with neither a TTL nor a byte limit is stream-005's, which names its history, and this check
  is then quiet for it. A firing finding keeps firing down to resolve_ratio of the thresholds, and through a
  collection without a leader report within the lookback.
remediation: >-
  Decide whether readers need the old revisions: `nats kv history <bucket> <key>` shows them. If not, lower the
  history (`nats stream edit KV_<bucket> --max-msgs-per-subject 1`, or the bucket's definition); the server then
  removes the older revisions. If they are needed, set a byte limit (`nats stream edit KV_<bucket> --max-bytes 1GiB`,
  or `--max-bucket-size` when the bucket is created) so the bucket cannot fill its servers' store; at the limit the
  oldest revisions are removed first. Or declare the bucket's max_bytes in the inventory and mute the finding.
related_config: [min_revisions, min_bytes, resolve_ratio, max_bytes, max_msgs_per_subject]
suppressed_by: [stream-005]
value_name: revisions per key
value_basis: the bucket's messages divided by its keys (subjects), leader's view
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'stream-019' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
newest AS (
    SELECT ss.account, ss.stream, ss.epoch, s.server_name, ss.messages, ss.bytes, ss.num_subjects
    FROM stream_stats ss JOIN servers s USING (server_id)
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader
      AND ss.stream LIKE 'KV\_%' ESCAPE '\' AND ss.num_subjects > 0
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream ORDER BY ss.epoch DESC, ss.server_id) = 1
),
cfg AS (
    SELECT account, stream, arg_max(max_bytes, last_epoch) AS max_bytes,
        arg_max(max_msgs_per_subject, last_epoch) AS history, arg_max(max_age_ns, last_epoch) AS ttl_ns
    FROM stream_config
    WHERE last_epoch >= getvariable('window_epoch') AND first_epoch <= getvariable('epoch')
      AND stream LIKE 'KV\_%' ESCAPE '\'
    GROUP BY account, stream
),
-- Buckets whose max_bytes the inventory gives; the inventory names the
-- account as the servers do or by its readable name.
declared AS (
    SELECT coalesce((SELECT a.account FROM accounts a WHERE i.account IN (a.account, a.account_name)
                     ORDER BY a.account = i.account DESC LIMIT 1), i.account) AS account, i.stream
    FROM inventory i
    WHERE i.kind = 'kv' AND i.max_bytes IS NOT NULL
),
judged AS (
    SELECT n.*, c.max_bytes, c.history, c.ttl_ns, n.messages / n.num_subjects AS per_key,
        (n.account || '/' || n.stream) IN (SELECT entity_key FROM firing) AS was_firing
    FROM newest n JOIN cfg c USING (account, stream)
    WHERE coalesce(c.max_bytes, -1) <= 0 AND c.history > 1
      AND (n.account, n.stream) NOT IN (SELECT account, stream FROM declared)
)
SELECT
    account || '/' || stream AS entity_key,
    'KV bucket ' || stream[4:] || ' keeps up to ' || history || ' revisions per key without a byte limit: '
        || num_subjects || ' keys hold ' || messages || ' messages (' || round(per_key, 1) || ' per key), '
        || format_bytes(bytes) || ', TTL ' || CASE WHEN coalesce(ttl_ns, 0) = 0 THEN 'none' ELSE ns_text(ttl_ns) END
        || CASE WHEN epoch < getvariable('epoch') THEN ' (no leader report in this collection)' ELSE '' END
        || '.' AS detail,
    round(per_key, 2) AS value,
    json_object('max_bytes', CAST(max_bytes AS VARCHAR), 'max_msgs_per_subject', CAST(history AS VARCHAR),
        'max_age', ns_text(ttl_ns)) AS related_config,
    server_name AS related_source
FROM judged
WHERE CASE WHEN was_firing
           THEN per_key >= getvariable('resolve_ratio') * getvariable('min_revisions')
                AND bytes >= getvariable('resolve_ratio') * getvariable('min_bytes')
           ELSE epoch = getvariable('epoch') AND per_key >= getvariable('min_revisions') AND bytes >= getvariable('min_bytes')
      END
