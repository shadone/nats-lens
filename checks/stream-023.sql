/*
code: stream-023
name: Source error
entity: stream
category: health
severity: warning
kind: operational
lookback: 5m
thresholds:
  - name: min_epochs
    default: "2"
    unit: count
    description: Error reports required among the last twice min_epochs collections inside the lookback.
  - name: grace
    default: 2m
    unit: duration
    description: Grace after creation, server start, leader election and first observation of this source configuration.
description: >-
  A configured source reports an error in min_epochs of the last twice min_epochs collections inside the lookback.
  NATS clears the error at every retry. A firing finding stays while that window contains an error for a source
  still configured, even through missing runtime reports. A new finding requires a current runtime report and grace
  after creation, server start, leader election and source configuration observation. Only the leader reports source
  state; mirrors are left to stream-017. The detail identifies each failing source by name and configuration ID.
remediation: >-
  Read `nats stream info <stream>` and the named Source section's Error, Last Seen and Lag. Read the origin's
  stream info in its account or domain to verify its existence and leader. For an external source, compare the API
  and delivery prefixes with the account exports and imports. Inspect permissions and links before changing anything;
  NATS retries source setup automatically. Keep deployment-specific advice in checks.remediation_file.
related_config: [min_epochs, grace, lookback]
*/
WITH recent AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(2 * getvariable('min_epochs') AS INTEGER)
), firing AS (
    SELECT entity_key, related_config FROM findings
    WHERE check_code = 'stream-023' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
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
        count(error) FILTER (WHERE reported AND epoch IN (SELECT epoch FROM recent)) AS errors,
        arg_max(error, epoch) FILTER (WHERE reported AND epoch IN (SELECT epoch FROM recent)) AS error,
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
)
SELECT entity_key, 'Leader ' || any_value(leader) || ': ' ||
    string_agg(source_label || ': ' || error
        || CASE WHEN reported_epoch < getvariable('epoch') THEN ' (no runtime report in this collection)' ELSE '' END,
        '; ' ORDER BY source_id) || '.' AS detail,
    json_object('source_ids', string_agg(source_id, ',' ORDER BY source_id),
        'created_us', epoch_us(any_value(created))::VARCHAR) AS related_config,
    any_value(leader) AS related_source
FROM judged
WHERE errors > 0 AND (was_firing OR errors >= getvariable('min_epochs')
    AND current_epoch = getvariable('epoch') AND current_reported
    AND leading_since < getvariable('epoch_time') - getvariable('grace')
    AND span_s >= epoch(getvariable('grace')))
GROUP BY entity_key
