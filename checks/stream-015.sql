/*
code: stream-015
name: Mirror lagging
entity: stream
category: health
severity: warning
kind: operational
lookback: 5m
thresholds:
  - name: max_lag
    default: "10000"
    unit: count
    description: Messages a mirror may be behind its origin.
  - name: max_inactive
    default: 2m
    unit: duration
    description: >-
      Time a mirror may go without receiving anything from its origin. The origin sends a heartbeat every second, so a
      healthy mirror hears from it even when nobody publishes.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the lag stays at or above this share of max_lag, or the time without data
      at or above this share of max_inactive, so a mirror near a threshold does not flap.
description: >-
  A mirror stays behind its origin, or has stopped hearing from it (leader's view; only the leader runs the mirror).
  Behind: every leader report within the lookback, covering at least half of it, has the mirror more than max_lag
  messages behind, and the lag did not fall over the window, so a mirror that catches up after a restart or a new
  mirror copying an old origin is not reported. Not hearing: the mirror received nothing, not even the origin's
  heartbeat, for more than max_inactive; or it has never heard from its origin in the two newest leader reports and
  its leader was elected more than max_inactive ago (a new leader reports that it never heard until its copy of the
  origin starts, which takes seconds, longer while the origin has no leader). A firing finding keeps firing down to
  resolve_ratio of the thresholds, and through a collection without a leader report within the lookback. A mirror
  whose newest leader report has an error is left to stream-017, also before stream-017 has seen it for min_epochs
  collections.
remediation: >-
  Run `nats stream info <mirror>` and read the Mirror section: Lag, Last Seen and Error. A mirror that has not been
  seen for long has lost its path to the origin: check that the origin exists (`nats stream info <origin>`, in the
  other account or domain for an external mirror), that its leader is up (stream-001 on the origin), and for a
  mirror across a leaf node or gateway that the link is up. A lag that grows while the mirror is seen means it
  cannot keep up: look at the mirror leader's CPU and disk, and at the origin's publish rate. The mirror restarts its
  copy by itself; `nats stream cluster step-down <mirror>` moves it to another leader, which starts it again.
related_config: [max_lag, max_inactive, resolve_ratio, lookback]
suppressed_by: [stream-017]
value_name: messages behind
value_basis: the mirror's lag behind its origin in the newest leader report
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'stream-015' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- The leader's reports within the lookback, newest first; only the leader
-- runs the mirror, and a follower reports no mirror lag.
lead AS (
    SELECT ss.account, ss.stream, ss.epoch, e.started_at AS ts, s.server_name, ss.mirror_lag AS lag,
        ss.mirror_active_ns AS active_ns, coalesce(ss.leader_since, ss.created) AS leading_since,
        nullif(ss.mirror_error, '') AS error
    FROM stream_stats ss JOIN epochs e USING (epoch) JOIN servers s USING (server_id)
    WHERE ss.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch') AND ss.is_leader
      AND ss.mirror_lag IS NOT NULL
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream, ss.epoch ORDER BY ss.server_id) = 1
),
ranked AS (
    SELECT *, row_number() OVER (PARTITION BY account, stream ORDER BY epoch DESC) AS rn FROM lead
),
win AS (
    SELECT account, stream, count(*) AS n, min(lag) AS min_lag, arg_min(lag, epoch) AS first_lag,
        arg_max(lag, epoch) AS lag, arg_max(active_ns, epoch) AS active_ns, max(epoch) AS last_epoch,
        arg_max(server_name, epoch) AS leader, arg_max(leading_since, epoch) AS leading_since,
        bool_or(error IS NOT NULL) FILTER (WHERE rn = 1) AS has_error,
        count(*) FILTER (WHERE rn <= 2 AND active_ns < 0) AS never_reports, epoch(max(ts) - min(ts)) AS span_s
    FROM ranked GROUP BY account, stream
),
origin AS (
    SELECT account, stream, arg_max(mirror, last_epoch) AS origin
    FROM stream_config
    WHERE last_epoch >= getvariable('window_epoch') AND first_epoch <= getvariable('epoch') AND coalesce(mirror, '') <> ''
    GROUP BY account, stream
),
judged AS (
    SELECT w.*, o.origin, w.account || '/' || w.stream AS entity_key,
        (w.account || '/' || w.stream) IN (SELECT entity_key FROM firing) AS was_firing,
        coalesce(w.never_reports = 2 AND w.leading_since < getvariable('epoch_time') - getvariable('max_inactive'), false)
            AS never_heard
    FROM win w LEFT JOIN origin o USING (account, stream)
    WHERE NOT coalesce(w.has_error, false)
),
flagged AS (
    SELECT *,
        CASE WHEN was_firing THEN lag >= getvariable('resolve_ratio') * getvariable('max_lag')
             ELSE last_epoch = getvariable('epoch') AND n >= 2 AND min_lag > getvariable('max_lag') AND lag >= first_lag
                  AND span_s >= epoch(getvariable('lookback')) / 2
        END AS behind,
        CASE WHEN was_firing
             THEN active_ns < 0 OR active_ns / 1e9 >= getvariable('resolve_ratio') * epoch(getvariable('max_inactive'))
             ELSE last_epoch = getvariable('epoch')
                  AND (never_heard OR active_ns / 1e9 > epoch(getvariable('max_inactive')))
        END AS unheard
    FROM judged
)
SELECT
    entity_key,
    'Mirror of ' || coalesce(origin, 'its origin') || ' on leader ' || leader || ': '
        || array_to_string(list_filter([
            CASE WHEN behind THEN lag || ' messages behind'
                || CASE WHEN was_firing THEN '' ELSE ', not catching up over the last ' || getvariable('lookback_text')
                    || ' (' || first_lag || ' at its start)' END END,
            CASE WHEN unheard AND active_ns < 0 THEN 'has never heard from its origin'
                    || coalesce(' since its leader was elected at ' || strftime(leading_since, '%H:%M:%S UTC'), '')
                 WHEN unheard THEN 'nothing received from its origin for '
                    || span_text(to_microseconds(CAST(active_ns // 1000 AS BIGINT))) END
        ], x -> x IS NOT NULL), '; ')
        || CASE WHEN last_epoch < getvariable('epoch') THEN ' (no leader report in this collection)' ELSE '' END
        || '.' AS detail,
    lag AS value,
    json_object('mirror', origin) AS related_config,
    leader AS related_source
FROM flagged
WHERE behind OR unheard
