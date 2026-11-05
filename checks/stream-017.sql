/*
code: stream-017
name: Mirror error
entity: stream
category: health
severity: warning
kind: operational
thresholds:
  - name: min_epochs
    default: "2"
    unit: count
    description: >-
      Consecutive collections in which the leader must report the error before the finding fires; one failed attempt
      during an election of the origin's leader is retried by the server within seconds.
description: >-
  The leader of a mirror reports an error for it in each of the last min_epochs collections, for example when the
  origin stream is gone, was renamed, or is out of reach of the mirror's account. While the error lasts the mirror
  copies nothing. A firing finding stays until a leader report in the last min_epochs collections shows no error.
remediation: >-
  The detail gives the server's error. Run `nats stream info <mirror>` for the Mirror section and
  `nats stream info <origin>` in the origin's account or domain: an origin that was deleted must be created again or
  the mirror pointed elsewhere (a mirror's origin cannot be edited; create a new mirror and move its readers). For an
  external mirror, check the API prefix and that the account exports the origin's API and delivery subjects. Errors
  that name a consumer mean the mirror cannot create the consumer it copies through, often for lack of permission.
related_config: [min_epochs]
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'stream-017' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- Only the leader runs the mirror; a follower reports no mirror state.
lead AS (
    SELECT ss.account, ss.stream, ss.epoch, s.server_name, nullif(ss.mirror_error, '') AS error
    FROM stream_stats ss JOIN servers s USING (server_id)
    WHERE ss.epoch IN (SELECT epoch FROM last) AND ss.is_leader AND ss.mirror_lag IS NOT NULL
    QUALIFY row_number() OVER (PARTITION BY ss.account, ss.stream, ss.epoch ORDER BY ss.server_id) = 1
),
win AS (
    SELECT account, stream, count(error) AS failing, arg_max_null(error, epoch) AS error, max(epoch) AS last_epoch,
        arg_max(server_name, epoch) AS leader
    FROM lead GROUP BY account, stream
),
origin AS (
    SELECT account, stream, arg_max(mirror, last_epoch) AS origin
    FROM stream_config
    WHERE last_epoch >= (SELECT min(epoch) FROM last) AND first_epoch <= getvariable('epoch') AND coalesce(mirror, '') <> ''
    GROUP BY account, stream
)
SELECT
    w.account || '/' || w.stream AS entity_key,
    'Leader ' || w.leader || ' reports the mirror of ' || coalesce(o.origin, 'its origin') || ' failing: ' || w.error
        || CASE WHEN w.last_epoch < getvariable('epoch') THEN ' (no leader report in this collection)' ELSE '' END
        || '.' AS detail,
    json_object('mirror', o.origin) AS related_config,
    w.leader AS related_source
FROM win w LEFT JOIN origin o USING (account, stream)
WHERE w.error IS NOT NULL
  AND (w.failing >= getvariable('min_epochs') AND w.last_epoch = getvariable('epoch')
       OR (w.account || '/' || w.stream) IN (SELECT entity_key FROM firing))
