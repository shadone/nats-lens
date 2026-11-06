/*
code: stream-016
name: Source lagging
entity: stream
category: health
severity: warning
kind: operational
lookback: 5m
thresholds:
  - name: max_lag
    default: "10000"
    unit: count
    description: Messages the most lagging source of a stream may be behind its origin stream.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the largest source lag stays at or above this share of max_lag, so a
      stream near the threshold does not flap.
description: >-
  A stream that sources other streams stays behind one of them (leader's view; only the leader runs the sources).
  Every leader report within the lookback, covering at least half of it, has the largest lag across its sources above
  max_lag, and that lag did not fall over the window, so a stream that catches up after a restart, or a new source
  copying an old stream, is not reported. This check reads the largest lag across the reported sources. A source that
  stopped hearing from its stream keeps the lag it last had, so a stopped source is not detected unless that lag is
  above max_lag; stream-022 reports individual source silence and stream-023 reports source errors. A firing
  finding keeps firing until the lag falls below resolve_ratio of max_lag, and through a collection without a leader
  report within the lookback.
remediation: >-
  Run `nats stream info <stream>` and read its Source sections: the source with the large Lag, its Last Seen and any
  Error. A source that has not been seen for long has lost its path to the origin stream: check that the stream
  exists and has a leader (stream-001 on it), and for a source across a leaf node or gateway that the link is up. A
  lag that grows while the source is seen means the stream cannot keep up with the stream it sources: look at the
  leader's CPU and disk and at the origin's publish rate. `nats stream cluster step-down <stream>` moves the stream
  to another leader, which starts its sources again.
related_config: [max_lag, resolve_ratio, lookback]
value_name: messages behind
value_basis: the largest lag across the stream's sources in the newest leader report
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'stream-016' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- The leader's reports within the lookback; only the leader runs the
-- sources.
lead AS (
    SELECT ss.account, ss.stream, ss.epoch, e.started_at AS ts, s.server_name, ss.sources_max_lag AS lag,
        ss.sources_count
    FROM stream_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader
      AND ss.sources_max_lag IS NOT NULL
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream, ss.epoch ORDER BY ss.server_id) = 1
),
win AS (
    SELECT account, stream, count(*) AS n, min(lag) AS min_lag, arg_min(lag, epoch) AS first_lag,
        arg_max(lag, epoch) AS lag, arg_max(sources_count, epoch) AS sources_count, max(epoch) AS last_epoch,
        arg_max(server_name, epoch) AS leader, epoch(max(ts) - min(ts)) AS span_s
    FROM lead GROUP BY account, stream
),
names AS (
    SELECT account, stream, arg_max(sources, last_epoch) AS sources
    FROM stream_config
    WHERE last_epoch >= getvariable('window_epoch') AND first_epoch <= getvariable('epoch')
    GROUP BY account, stream
),
flagged AS (
    SELECT w.*, n.sources, (w.account || '/' || w.stream) IN (SELECT entity_key FROM firing) AS was_firing
    FROM win w LEFT JOIN names n USING (account, stream)
)
SELECT
    account || '/' || stream AS entity_key,
    'Leader ' || leader || ': the most lagging of its ' || sources_count
        || CASE WHEN sources_count = 1 THEN ' source' ELSE ' sources' END
        || CASE WHEN len(coalesce(sources, [])) > 0 THEN ' (' || array_to_string(sources, ', ') || ')' ELSE '' END
        || ' is ' || lag || ' messages behind'
        || CASE WHEN was_firing THEN '' ELSE ', not catching up over the last ' || getvariable('lookback_text')
            || ' (' || first_lag || ' at its start)' END
        || CASE WHEN last_epoch < getvariable('epoch') THEN ' (no leader report in this collection)' ELSE '' END
        || '.' AS detail,
    lag AS value,
    leader AS related_source
FROM flagged
WHERE CASE WHEN was_firing THEN lag >= getvariable('resolve_ratio') * getvariable('max_lag')
           ELSE last_epoch = getvariable('epoch') AND n >= 2 AND min_lag > getvariable('max_lag') AND lag >= first_lag
                AND span_s >= epoch(getvariable('lookback')) / 2
      END
