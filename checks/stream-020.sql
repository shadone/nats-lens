/*
code: stream-020
name: Stream without a consumer
entity: stream
category: consumers
severity: warning
severity_rule: >-
  info while it does not lose or keep anything: a work-queue stream that is empty, an interest stream that nothing
  was published to within the lookback
kind: operational
lookback: 10m
description: >-
  A work-queue or interest stream has had no consumer in every leader report within the lookback, which cover at
  least half of it, and was created before the lookback began. A work-queue stream keeps its messages until a
  consumer takes them, so they pile up until a limit stops publishes or drops them. An interest stream with no
  consumer stores nothing: the server acknowledges every publish and drops the message. Not reported while
  consumer-010 or consumer-013 reports a consumer of the stream missing, as that finding names the one to bring back.
  A firing finding stays until a leader report within the lookback counts a consumer, also through a collection
  without a leader report.
remediation: >-
  Find the service that should consume the stream: its consumers were deleted, never created, or are ephemeral and
  went away with their client (`nats consumer ls <stream>` lists what is there, the events of the stream name the
  client that deleted one). Start that service or create its durable (`nats consumer add <stream> <name>`, or its NACK
  resource). Until then, publishes to an interest stream are lost; for a work-queue stream, check how close it is to
  its limits (stream-004). A stream that has lost its purpose can be removed with `nats stream rm` once nobody
  publishes to it.
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'stream-020' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- A consumer of the stream that consumer-010 or consumer-013 reports
-- missing says more than this check.
named_missing AS (
    SELECT entity_key FROM findings
    WHERE check_code IN ('consumer-010', 'consumer-013') AND state = 'firing'
      AND last_seen_epoch = getvariable('prev_epoch')
),
lead AS (
    SELECT ss.account, ss.stream, ss.epoch, e.started_at AS ts, s.server_name, ss.consumer_count, ss.messages,
        ss.last_seq, ss.created
    FROM stream_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader
      AND ss.consumer_count IS NOT NULL
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream, ss.epoch ORDER BY ss.server_id) = 1
),
win AS (
    SELECT account, stream, max(consumer_count) AS most, arg_max(consumer_count, epoch) AS consumers,
        arg_max(messages, epoch) AS messages, arg_max(last_seq, epoch) - arg_min(last_seq, epoch) AS published,
        max(epoch) AS last_epoch, arg_max(server_name, epoch) AS leader, min(created) AS created,
        epoch(max(ts) - min(ts)) AS span_s
    FROM lead GROUP BY account, stream
),
cfg AS (
    SELECT account, stream, arg_max(retention, last_epoch) AS retention
    FROM stream_config
    WHERE last_epoch >= getvariable('window_epoch') AND first_epoch <= getvariable('epoch')
    GROUP BY account, stream
),
judged AS (
    SELECT w.*, c.retention, (w.account || '/' || w.stream) IN (SELECT entity_key FROM firing) AS was_firing
    FROM win w JOIN cfg c USING (account, stream)
    WHERE c.retention IN ('workqueue', 'interest')
      AND NOT EXISTS (SELECT 1 FROM named_missing x WHERE starts_with(x.entity_key, w.account || '/' || w.stream || '/'))
)
SELECT
    account || '/' || stream AS entity_key,
    CASE WHEN retention = 'workqueue' AND coalesce(messages, 0) > 0 THEN 'warning'
         WHEN retention = 'interest' AND coalesce(published, 0) > 0 THEN 'warning'
         ELSE 'info' END AS severity,
    CASE retention WHEN 'workqueue' THEN 'Work-queue' ELSE 'Interest' END || ' stream with no consumer for at least '
        || getvariable('lookback_text') || ' (leader ' || leader || '): '
        || CASE
            WHEN retention = 'workqueue' AND coalesce(messages, 0) > 0
                THEN messages || ' messages wait, and nothing removes them until a consumer takes them'
            WHEN retention = 'workqueue' THEN 'it is empty; messages published to it wait until a consumer takes them'
            WHEN coalesce(published, 0) > 0 THEN 'the server drops every message published to it, '
                || published || ' within the last ' || getvariable('lookback_text')
            ELSE 'nothing was published to it within the last ' || getvariable('lookback_text')
                || '; a message published while it has no consumer is dropped'
        END
        || CASE WHEN last_epoch < getvariable('epoch') THEN ' (no leader report in this collection)' ELSE '' END
        || '.' AS detail,
    json_object('retention', retention) AS related_config,
    leader AS related_source
FROM judged
WHERE CASE WHEN was_firing THEN consumers = 0
           ELSE last_epoch = getvariable('epoch') AND most = 0 AND span_s >= epoch(getvariable('lookback')) / 2
                AND coalesce(created <= getvariable('window_start'), true)
      END
