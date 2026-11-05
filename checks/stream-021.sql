/*
code: stream-021
name: Many deleted messages inside a stream
entity: stream
category: storage
severity: info
kind: operational
lookback: 10m
thresholds:
  - name: min_deleted
    default: "1000000"
    unit: count
    description: Deleted messages inside the stream's sequence range (interior deletes) at which it is reported.
  - name: min_deleted_ratio
    default: "0.5"
    unit: ratio
    description: Share of the sequence range, messages plus interior deletes, that the deletes must also reach.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the deletes and their share stay at or above this share of their
      thresholds, so a stream near a threshold does not flap.
description: >-
  The stream's leader reports min_deleted or more deleted messages between its first and last sequence, and they are
  min_deleted_ratio or more of that range. Interior deletes come from messages removed out of order: acknowledged
  out of order on a work-queue or interest stream, deleted one by one, or replaced by a newer revision of a KV key.
  Every replica keeps a record of the gaps in memory and carries it in the snapshots a replica catches up from.
  Many of them usually mean one old message holds the start of the stream: an unacknowledged work item, or a KV key
  that has not been written since. A firing finding keeps firing down to resolve_ratio of the thresholds, and
  through a collection without a leader report within the lookback.
remediation: >-
  Look at the oldest message: `nats stream info <stream>` gives the first sequence and its age, `nats stream get
  <stream> <sequence>` the message. On a work-queue or interest stream it is usually a message no consumer
  acknowledges: find the consumer with the low ack floor (`nats consumer report <stream>`) and let it acknowledge or
  terminate the message. In a KV bucket it is a key not written for long: write or delete it, or set a TTL. Once the
  old message goes the gaps before the next one go with it. `nats stream purge <stream> --seq <sequence>` removes
  everything before a sequence, only when those messages are not needed.
related_config: [min_deleted, min_deleted_ratio, resolve_ratio]
value_name: deleted messages
value_basis: interior deletes between the first and last sequence, leader's view
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'stream-021' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
newest AS (
    SELECT ss.account, ss.stream, ss.epoch, s.server_name, ss.num_deleted, ss.messages, ss.first_seq, ss.last_seq,
        ss.first_ts, ss.num_deleted / (coalesce(ss.messages, 0) + ss.num_deleted) AS ratio
    FROM stream_stats ss JOIN servers s USING (server_id)
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader AND ss.num_deleted > 0
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream ORDER BY ss.epoch DESC, ss.server_id) = 1
),
judged AS (
    SELECT n.*, (n.account || '/' || n.stream) IN (SELECT entity_key FROM firing) AS was_firing
    FROM newest n
)
SELECT
    account || '/' || stream AS entity_key,
    CASE WHEN stream LIKE 'KV\_%' ESCAPE '\' THEN 'KV bucket ' || stream[4:]
         WHEN stream LIKE 'OBJ\_%' ESCAPE '\' THEN 'Object store ' || stream[5:]
         ELSE 'Stream ' || stream END
        || ' holds ' || num_deleted || ' deleted messages between sequence ' || first_seq || ' and ' || last_seq
        || ' (' || round(ratio * 100, 1) || '% of the range, ' || coalesce(messages, 0) || ' messages kept)'
        || CASE WHEN first_ts IS NOT NULL
                THEN '; the oldest message is ' || span_text(getvariable('epoch_time') - first_ts) || ' old' ELSE '' END
        || CASE WHEN epoch < getvariable('epoch') THEN ' (no leader report in this collection)' ELSE '' END
        || '.' AS detail,
    num_deleted AS value,
    server_name AS related_source
FROM judged
WHERE CASE WHEN was_firing
           THEN num_deleted >= getvariable('resolve_ratio') * getvariable('min_deleted')
                AND ratio >= getvariable('resolve_ratio') * getvariable('min_deleted_ratio')
           ELSE epoch = getvariable('epoch') AND num_deleted >= getvariable('min_deleted')
                AND ratio >= getvariable('min_deleted_ratio')
      END
