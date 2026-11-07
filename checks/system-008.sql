/*
code: system-008
name: Archive export behind
entity: system
category: engine
severity: warning
kind: operational
thresholds:
  - name: max_delay
    default: 1h
    unit: duration
    description: >-
      How long a closed hour may wait for its export to the archive, counted from the end of the hour or from the
      start of nats-lens, whichever is later, before the finding fires.
description: >-
  A closed hour that holds collections or events has not been exported to the Parquet archive for longer than
  max_delay. nats-lens exports each hour shortly after it ends, so a wait this long means the exports fail. Until
  they succeed again, a database that is lost or cannot be opened is rebuilt without those hours. The wait counts
  from the end of the hour, or from the start of nats-lens when that is later, so the catch-up after a restart does
  not fire. natslens once does not export, so the check does not fire there. The finding resolves once every closed
  hour is exported.
remediation: >-
  nats-lens logs each failed export as "archive export failed" with its error. Check that the archive directory
  (`archive_dir`, by default the `archive` folder under `data_dir`) exists, is writable by nats-lens and has free
  space, and that the volume is mounted; in Kubernetes, check the persistent volume claim and `kubectl describe pod`
  for mount errors. A full disk also stops the database itself. Once the cause is fixed, the next export catches up
  the hours that are still within `retention`, oldest first.
related_config: [max_delay]
value_name: hours waiting
value_basis: time since the oldest closed hour not yet exported ended, or since nats-lens started when that is later
*/
WITH bound AS (
    -- Hours are exported oldest first, so the hours waiting are those after
    -- the newest one exported as closed.
    SELECT (SELECT max(hour) FROM archive_hours WHERE final) + INTERVAL 1 HOUR AS since
),
open_hour AS (
    SELECT date_trunc('hour', getvariable('epoch_time')) AS h
),
waiting AS (
    SELECT min(t) AS first_at FROM (
        SELECT min(started_at) AS t FROM epochs
        WHERE started_at >= coalesce((SELECT since FROM bound), TIMESTAMP '1970-01-01')
          AND started_at < (SELECT h FROM open_hour)
        UNION ALL
        SELECT min(received_at) FROM events
        WHERE received_at >= coalesce((SELECT since FROM bound), TIMESTAMP '1970-01-01')
          AND received_at < (SELECT h FROM open_hour)
    )
),
oldest AS (
    SELECT date_trunc('hour', first_at) AS h,
        getvariable('epoch_time') - greatest(date_trunc('hour', first_at) + INTERVAL 1 HOUR, getvariable('archive_since')) AS waited
    FROM waiting WHERE first_at IS NOT NULL
),
closed AS (
    SELECT count(DISTINCT h) AS n FROM (
        SELECT date_trunc('hour', started_at) AS h FROM epochs
        WHERE started_at >= (SELECT h FROM oldest) AND started_at < (SELECT h FROM open_hour)
        UNION ALL
        SELECT date_trunc('hour', received_at) FROM events
        WHERE received_at >= (SELECT h FROM oldest) AND received_at < (SELECT h FROM open_hour)
    )
)
SELECT
    '' AS entity_key,
    'The hour ' || strftime(o.h, '%Y-%m-%d %H:00') || ' UTC ended ' || span_text(getvariable('epoch_time') - o.h - INTERVAL 1 HOUR)
        || ' ago and has not been exported to the archive since'
        || CASE WHEN c.n > 1 THEN ', nor have ' || (c.n - 1) || ' later closed ' || CASE WHEN c.n = 2 THEN 'hour' ELSE 'hours' END ELSE '' END
        || '. ' || coalesce('The last export was at ' || strftime((SELECT max(exported_at) FROM archive_hours), '%Y-%m-%d %H:%M') || ' UTC.',
            'No hour has been exported yet.') AS detail,
    round(epoch(o.waited) / 3600, 1) AS value
FROM oldest o, closed c
WHERE getvariable('archive_since') IS NOT NULL AND o.waited > getvariable('max_delay')
