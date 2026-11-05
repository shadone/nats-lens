/*
code: system-010
name: Notifications not delivered
entity: system
category: engine
severity: warning
kind: operational
thresholds:
  - name: pending_for
    default: 10m
    unit: duration
    description: How long a notification may remain pending before the finding fires.
description: >-
  A channel has notifications pending for longer than pending_for, or refused or gave up notifications in the last
  hour. A recent refusal or give-up is critical; an old pending message is warning. The detail names the channels,
  their oldest pending age and latest HTTP status, never a destination URL.
remediation: >-
  Check the named channel's receiver and its credentials. Use `natslens notify test` to test the current destination.
  A refused message is not retried; after fixing the destination, use the Findings page to review missed findings.
  A channel that fails cannot report itself: monitor the other channels and the notification metrics too.
related_config: [pending_for]
*/
WITH channels AS (
 SELECT channel,
   min(created_at) FILTER (WHERE state='pending') AS oldest,
   count(*) FILTER (WHERE state IN ('refused','failed') AND finished_at > getvariable('epoch_time') - INTERVAL 1 HOUR
     AND finished_at <= getvariable('epoch_time')) AS failed,
   arg_max(coalesce(last_status,0), coalesce(finished_at, next_attempt_at, created_at)) AS status
 FROM notify_outbox WHERE created_at <= getvariable('epoch_time') GROUP BY channel
), bad AS (
 SELECT *, CASE WHEN oldest IS NULL THEN 'none pending' ELSE
   CAST(round(epoch(getvariable('epoch_time') - oldest)) AS BIGINT) || 's pending' END AS age
 FROM channels WHERE failed > 0 OR oldest < getvariable('epoch_time') - getvariable('pending_for')
)
SELECT '' AS entity_key,
 CASE WHEN max(failed)>0 THEN 'critical' ELSE 'warning' END AS severity,
 'Notifications not delivered: ' || string_agg(channel || ' (' || age || ', last HTTP status ' || status || ')', '; ' ORDER BY channel) || '.' AS detail
FROM bad HAVING count(*) > 0
