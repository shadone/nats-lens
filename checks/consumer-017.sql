/*
code: consumer-017
name: Consumer paused for a long time
entity: consumer
category: consumers
severity: warning
severity_rule: info while no messages wait for the consumer (num_pending 0 on its leader, or no leader report)
kind: operational
thresholds:
  - name: max_paused
    default: 2h
    unit: duration
    description: How long a consumer may stay paused before the finding fires.
description: >-
  The consumer has been paused for longer than max_paused. The pause counts from the first consumer.paused event
  since the consumer last ran (the newest collection in which a server reported it running, or its newest
  consumer.resumed event), or from the first collection since then in which it reports paused, whichever is
  earlier; a pause extended with a new end is one pause.
  A paused consumer delivers nothing until its pause_until time or until it is resumed, so its backlog grows and
  messages may age out of the stream unread (consumer-014). The detail gives the pause end from the consumer's
  configuration. Whether it is paused now is read from the leader's report, or any replica's when no leader
  reports.
remediation: >-
  A pause is set on purpose, by a person or a service, for example during maintenance of what the consumer writes
  to. Find out who paused it: the Events page shows the consumer.paused event and, with events.api_audit on, the
  API call that paused it.
  If the reason is gone, resume it: `nats consumer resume <stream> <consumer>`. A consumer that stays paused by
  design can be muted with that reason.
related_config: [max_paused]
value_name: hours paused
value_basis: time since the pause began
*/
WITH cur AS (
    SELECT account, stream, consumer, arg_max(created, is_leader) AS created,
        max(num_pending) FILTER (WHERE is_leader) AS num_pending
    FROM consumer_stats
    WHERE epoch = getvariable('epoch')
    GROUP BY account, stream, consumer
    HAVING arg_max(paused, is_leader)
),
-- The same consumer's reports (same creation time) up to this collection.
hist AS (
    SELECT c.account, c.stream, c.consumer, x.epoch, x.paused
    FROM cur c JOIN consumer_stats x
      ON x.account = c.account AND x.stream = c.stream AND x.consumer = c.consumer
     AND x.created IS NOT DISTINCT FROM c.created
    WHERE x.epoch <= getvariable('epoch')
),
ev AS (
    SELECT e.id, e.kind, e.time, e.received_at, e.stream, e.consumer,
        coalesce((SELECT a.account FROM accounts a WHERE e.account IN (a.account, a.account_name)
                  ORDER BY (a.account = e.account) DESC LIMIT 1), e.account) AS account
    FROM events e
    WHERE e.kind IN ('consumer.paused', 'consumer.resumed') AND e.received_at <= getvariable('epoch_time')
      AND e.stream IS NOT NULL AND e.consumer IS NOT NULL AND e.account IS NOT NULL
),
-- When the consumer last ran: the newest collection in which a server
-- reported it running, or the newest consumer.resumed event, if later.
cut AS (
    SELECT c.account, c.stream, c.consumer, greatest(
        (SELECT e.started_at FROM epochs e WHERE e.epoch = (SELECT max(h.epoch) FROM hist h WHERE h.account = c.account
            AND h.stream = c.stream AND h.consumer = c.consumer AND NOT coalesce(h.paused, false))),
        (SELECT max(x.received_at) FROM ev x WHERE x.kind = 'consumer.resumed'
            AND x.account = c.account AND x.stream = c.stream AND x.consumer = c.consumer)) AS at
    FROM cur c
),
runs AS (
    SELECT k.account, k.stream, k.consumer, min(e.started_at) AS run_start
    FROM cut k
    JOIN hist h ON h.account = k.account AND h.stream = k.stream AND h.consumer = k.consumer AND h.paused
    JOIN epochs e ON e.epoch = h.epoch
    WHERE e.started_at > coalesce(k.at, '-infinity'::TIMESTAMP)
    GROUP BY k.account, k.stream, k.consumer
),
-- The pause event that began this pause: the first since the consumer last
-- ran. A later one extends the pause.
began AS (
    SELECT k.account, k.stream, k.consumer, min(p.time) AS at, arg_min(p.id, p.time) AS id
    FROM cut k
    JOIN ev p ON p.kind = 'consumer.paused' AND p.account = k.account AND p.stream = k.stream AND p.consumer = k.consumer
    WHERE p.received_at > coalesce(k.at, '-infinity'::TIMESTAMP)
    GROUP BY k.account, k.stream, k.consumer
),
until AS (
    SELECT k.account, k.stream, k.consumer,
        arg_max(TRY_CAST(k.config->>'pause_until' AS TIMESTAMP), s.is_leader) AS pause_until
    FROM consumer_config k
    JOIN consumer_stats s ON s.server_id = k.server_id AND s.account = k.account AND s.stream = k.stream
     AND s.consumer = k.consumer AND s.epoch = getvariable('epoch')
    WHERE k.first_epoch <= getvariable('epoch') AND k.last_epoch >= getvariable('epoch')
    GROUP BY k.account, k.stream, k.consumer
),
judged AS (
    SELECT c.*, least(r.run_start, b.at) AS since, b.id AS event_id, u.pause_until
    FROM cur c
    JOIN runs r USING (account, stream, consumer)
    LEFT JOIN began b USING (account, stream, consumer)
    LEFT JOIN until u USING (account, stream, consumer)
)
SELECT
    account || '/' || stream || '/' || consumer AS entity_key,
    CASE WHEN coalesce(num_pending, 0) > 0 THEN 'warning' ELSE 'info' END AS severity,
    'Paused since ' || strftime(since, '%Y-%m-%d %H:%M:%S') || ' UTC, for ' || span_text(getvariable('epoch_time') - since)
        || CASE
            WHEN pause_until IS NULL THEN ''
            WHEN pause_until > getvariable('epoch_time') THEN '; the pause ends ' || strftime(pause_until, '%Y-%m-%d %H:%M UTC')
                || ', in ' || span_text(pause_until - getvariable('epoch_time'))
            ELSE '' END
        || '; ' || CASE WHEN num_pending IS NULL THEN 'no leader report of the messages waiting'
            ELSE num_pending || ' messages waiting' END || '.' AS detail,
    round(epoch(getvariable('epoch_time') - since) / 3600, 2) AS value,
    event_id AS cause_event_id,
    since AS condition_since
FROM judged
WHERE getvariable('epoch_time') - since > getvariable('max_paused')
