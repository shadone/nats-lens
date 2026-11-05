/*
code: system-009
name: Event messages dropped
entity: system
category: engine
severity: warning
kind: operational
lookback: 1h
thresholds:
  - name: max_dropped
    default: "0"
    unit: count
    description: Event messages dropped within the lookback above which the finding fires.
description: >-
  nats-lens dropped more than max_dropped event messages within the lookback: system events and JetStream
  advisories that it received and could not store, because its queue was full, writing them kept failing, or its
  connection fell behind and the client library discarded messages as a slow consumer. Checks and pages that read
  events (leader elections, lame duck and shutdown announcements, deleted consumers, client disconnects) may then
  miss what those messages said, and a restart may read as a crash. A slow consumer discards an unknown number of
  messages; each such report counts as one, so the count is a lower bound. The finding resolves once the lookback
  holds no more than max_dropped.
remediation: >-
  The events of kind `collector.messages_dropped` say when and why. A full queue means nats-lens cannot decode and
  store events as fast as they arrive: give it more CPU (its pod may be throttled), or turn off `events.api_audit`,
  which captures every JetStream API call and is the largest stream of events on a busy system. Writes that keep
  failing show as "events not stored" in the nats-lens log; check the disk under `data_dir`. A slow consumer on the
  events connection points at the same causes or at a slow network path to the NATS server.
related_config: [max_dropped, lookback]
value_name: event messages dropped
value_basis: >-
  messages counted by the collector.messages_dropped events received within the lookback, each slow consumer report
  counted as one
*/
WITH dropped AS (
    SELECT received_at, summary, TRY_CAST(json_extract_string(raw, '$.dropped') AS BIGINT) AS n
    FROM events
    WHERE kind = 'collector.messages_dropped'
      AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
),
total AS (
    SELECT CAST(sum(coalesce(n, 1)) AS BIGINT) AS n, count(*) FILTER (WHERE n IS NULL) AS unknown, max(received_at) AS last_at,
        arg_max(summary, received_at) AS last_summary
    FROM dropped
)
SELECT
    '' AS entity_key,
    'nats-lens dropped ' || CASE WHEN t.unknown > 0 THEN 'at least ' ELSE '' END || t.n || ' event '
        || CASE WHEN t.n = 1 THEN 'message' ELSE 'messages' END || ' in the last ' || getvariable('lookback_text')
        || ', the latest at ' || strftime(t.last_at, '%H:%M:%S') || ' UTC: ' || t.last_summary || '.' AS detail,
    t.n AS value
FROM total t
WHERE t.n > getvariable('max_dropped')
