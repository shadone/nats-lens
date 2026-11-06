/*
code: stream-022
name: Source not seen
entity: stream
category: health
severity: warning
kind: operational
lookback: 5m
thresholds:
  - name: max_inactive
    default: 2m
    unit: duration
    description: Time without source data or heartbeats; also the creation, server-start and leader-election grace.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: A firing finding resolves below this share of max_inactive.
description: >-
  A configured source has stopped hearing from its origin, including its idle heartbeats. A negative activity means
  never heard, requiring two newest reports and observations spanning max_inactive. Creation, server start and leader
  election must precede max_inactive. Only the leader is authoritative. A new finding needs a current runtime report;
  a firing finding holds its last known silence through missing reports inside the lookback. A removed or changed
  source does not inherit the old source's history. Any error for that source inside the lookback leaves it to
  stream-023, including retries that clear the error; other silent sources in the same stream are still reported.
  Mirror findings stay with stream-015 and stream-017. Aggregate lag stays with stream-016.
remediation: >-
  Read `nats stream info <stream>` and the named Source section's Last Seen, Lag and Error. Read the origin's stream
  info in its account or domain and check its leader, then inspect the configured API and delivery prefixes, account
  permissions and leaf or gateway links. Healthy idle sources receive heartbeats without published messages.
  NATS retries the copy itself; verify the path and configuration before considering a change. Keep deployment-specific
  commands outside this repository in checks.remediation_file.
related_config: [max_inactive, resolve_ratio, lookback]
*/
WITH firing AS (
    SELECT entity_key, related_config FROM findings
    WHERE check_code = 'stream-022' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- Keep the authoritative stream report, including a report with no sources.
assets AS (
    SELECT ss.*, e.started_at AS ts, s.server_name,
        greatest(ss.created, ss.leader_since, coalesce(st.start_time, s.start_time)) AS leading_since
    FROM stream_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
    LEFT JOIN server_stats st ON st.epoch = ss.epoch AND st.server_id = ss.server_id
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream, ss.epoch ORDER BY ss.server_id) = 1
),
latest AS (
    SELECT * FROM assets WHERE source_config_known
    QUALIFY row_number() OVER (PARTITION BY account, stream ORDER BY epoch DESC) = 1
),
-- A current configuration removes old sources immediately. Missing JSZ or a
-- missing leader keeps the last configuration only inside the bounded window.
current_sources AS (
    SELECT a.account, a.stream, a.created, a.epoch AS current_epoch, a.leading_since,
        a.server_name AS leader, src.source_id, src.name, src.config, src.reported AS current_reported
    FROM latest a JOIN stream_source_stats src USING (epoch, server_id, account, stream)
),
breaks AS (
    SELECT c.account, c.stream, c.source_id, max(a.epoch) AS reset_epoch
    FROM current_sources c JOIN assets a ON a.account=c.account AND a.stream=c.stream
    LEFT JOIN stream_source_stats src ON src.epoch=a.epoch AND src.server_id=a.server_id
        AND src.account=c.account AND src.stream=c.stream AND src.source_id=c.source_id
    WHERE a.source_config_known AND (src.source_id IS NULL OR a.created IS DISTINCT FROM c.created)
    GROUP BY c.account, c.stream, c.source_id
),
history AS (
    SELECT c.*, a.epoch, a.ts, src.reported, src.active_ns, src.lag, src.error,
        row_number() OVER (PARTITION BY c.account, c.stream, c.source_id ORDER BY a.epoch DESC) AS rn
    FROM current_sources c JOIN assets a ON a.account=c.account AND a.stream=c.stream
        AND a.created IS NOT DISTINCT FROM c.created
    JOIN stream_source_stats src ON src.epoch=a.epoch AND src.server_id=a.server_id
        AND src.account=c.account AND src.stream=c.stream AND src.source_id=c.source_id
    LEFT JOIN breaks b ON b.account=c.account AND b.stream=c.stream AND b.source_id=c.source_id
    WHERE a.source_config_known AND a.epoch > coalesce(b.reset_epoch, 0)
),
win AS (
    SELECT account, stream, source_id, any_value(name) AS name, any_value(config) AS config,
        any_value(created) AS created, any_value(current_epoch) AS current_epoch, any_value(current_reported) AS current_reported,
        any_value(leading_since) AS leading_since, any_value(leader) AS leader,
        arg_max(active_ns, epoch) FILTER (WHERE reported) AS active_ns,
        max(epoch) FILTER (WHERE reported) AS reported_epoch,
        count(*) FILTER (WHERE reported AND active_ns < 0 AND rn <= 2) AS never_reports,
        count(error) FILTER (WHERE reported) AS errors, arg_max(error, epoch) FILTER (WHERE reported) AS error,
        epoch(max(ts)-min(ts)) AS span_s
    FROM history GROUP BY account, stream, source_id
),
judged AS (
    SELECT *, account || '/' || stream AS entity_key,
        name
            || coalesce(' API ' || nullif(json_extract_string(config, '$.external.api'), ''), '')
            || coalesce(' delivery ' || nullif(json_extract_string(config, '$.external.deliver'), ''), '')
            || coalesce(' filter ' || nullif(json_extract_string(config, '$.filter_subject'), ''), '')
            || coalesce(' transforms ' || json_extract(config, '$.subject_transforms')::VARCHAR, '')
            || coalesce(' consumer ' || nullif(json_extract_string(config, '$.consumer.name'), ''), '')
            || ' [' || substr(source_id,1,12) || ']' AS source_label,
        EXISTS (SELECT 1 FROM firing f WHERE f.entity_key = win.account || '/' || win.stream
            AND list_contains(string_split(json_extract_string(f.related_config, '$.source_ids'), ','), win.source_id)
            AND json_extract_string(f.related_config, '$.created_us') = epoch_us(win.created)::VARCHAR) AS was_firing
    FROM win
),
flagged AS (
    SELECT *, source_label || ': '
        || CASE WHEN active_ns < 0 THEN 'never heard from its origin'
                ELSE 'not heard from its origin for ' || ns_text(active_ns) END
        || CASE WHEN reported_epoch < getvariable('epoch') THEN ' (no runtime report in this collection)' ELSE '' END AS reason
    FROM judged
    WHERE errors = 0 AND (
        was_firing AND (active_ns < 0 OR active_ns / 1e9 >= getvariable('resolve_ratio') * epoch(getvariable('max_inactive')))
        OR NOT was_firing AND current_epoch = getvariable('epoch') AND current_reported
            AND leading_since < getvariable('epoch_time') - getvariable('max_inactive')
            AND (active_ns / 1e9 > epoch(getvariable('max_inactive'))
                OR active_ns < 0 AND never_reports = 2 AND span_s >= epoch(getvariable('max_inactive')))
    )
)
SELECT entity_key, 'Leader ' || any_value(leader) || ': ' || string_agg(reason, '; ' ORDER BY source_id) || '.' AS detail,
    json_object('source_ids', string_agg(source_id, ',' ORDER BY source_id),
        'created_us', epoch_us(any_value(created))::VARCHAR) AS related_config,
    any_value(leader) AS related_source
FROM flagged GROUP BY entity_key
