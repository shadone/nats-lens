/*
code: consumer-014
name: Oldest unread message about to expire
entity: consumer
category: consumers
severity: warning
severity_rule: critical once the oldest unread message has used critical_ratio of the stream's max_age
kind: trend
lookback: 10m
thresholds:
  - name: warn_ratio
    default: "0.8"
    unit: ratio
    description: Share of max_age the consumer's oldest unread message must have used before the finding fires.
  - name: critical_ratio
    default: "0.95"
    unit: ratio
    description: Share of max_age at which the finding is critical.
  - name: resolve_ratio
    default: "0.7"
    unit: ratio
    description: A firing finding stays until the oldest unread message has used less than this share of max_age.
description: >-
  A durable consumer of a stream with max_age has a message it has not read or not acknowledged that has used
  warn_ratio of max_age, and is not catching up: over the lookback that message's age fell by less than a tenth
  of the time that passed. Unless the consumer acknowledges it in time, the stream removes it unread. A message is
  dated by the stream's history: it was published no later than the first collection in which the stream's last
  sequence (leader's view) reached it, so the age is exact to a collection interval. The message is the one after
  the ack floor while messages are unacknowledged (the newest delivered one when that one has left the stream),
  or the one after the delivered sequence while only undelivered messages wait (the stream's first message when
  that one has left). For a consumer with a filter, whose next sequences may not match it, the age is a lower
  bound: the newest delivered message's while messages are unacknowledged, else the time since the backlog
  began (the first collection since the delivered sequence last moved in which messages waited). History before
  the stored collections, or older than the stream's own max_age and the lookback, is not read, so a message that old
  reads younger, never older. A firing finding stays until the message has used less than resolve_ratio of
  max_age, or the consumer catches up. Ephemeral and ordered consumers are left out, and so is a consumer without
  a leader report, or whose stream has none, in this collection, or with such reports over less than half the
  lookback.
remediation: >-
  The consumer reads slower than its stream ages messages out, or has stopped. If it has stopped, see consumer-006
  and consumer-003; a paused consumer (consumer-017) can be resumed with `nats consumer resume <stream>
  <consumer>`. If it reads but too slowly, add workers or raise max_ack_pending so that more are in flight. To keep
  the messages longer while it catches up, raise the stream's max_age: `nats stream edit <stream> --max-age <age>`,
  after checking the stream's storage room (server-007). A message that every worker fails keeps the ack floor
  where it is: it ends at max_deliver (consumer-005), or, with max_deliver unlimited, it is the message that
  expires.
related_config: [warn_ratio, critical_ratio, resolve_ratio, max_age]
value_name: share of max_age used by the oldest unread message
value_basis: estimated age of the consumer's oldest unread message divided by the stream's max_age, leader's views
*/
WITH scfg0 AS (
    SELECT account, stream, max(max_age_ns) AS max_age_ns, any_value(subjects) AS subjects
    FROM stream_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream
    HAVING max(max_age_ns) > 0
),
-- lo is the first collection of the stream's own window, its max_age and
-- the lookback: older history is not read, so that a stream with a long
-- max_age does not widen the scan of the others. A message older than
-- that is dated at the window's first collection, which is still past its
-- max_age.
scfg AS (
    SELECT s.*, coalesce((SELECT min(x.epoch) FROM epochs x WHERE x.started_at >= getvariable('epoch_time')
        - getvariable('lookback') - to_microseconds(CAST(s.max_age_ns // 1000 AS BIGINT))), 0) AS lo
    FROM scfg0 s
),
ccfg AS (
    SELECT account, stream, consumer, any_value(filter_subjects) AS filter_subjects
    FROM consumer_config
    WHERE first_epoch <= getvariable('epoch') AND last_epoch >= getvariable('epoch')
    GROUP BY account, stream, consumer
    HAVING bool_and(coalesce(durable_name, '') <> '')
),
cand AS (
    SELECT k.*, s.max_age_ns, s.lo,
        coalesce(len(k.filter_subjects), 0) = 0
            OR list_sort(k.filter_subjects) = list_sort(coalesce(s.subjects, []::VARCHAR[])) AS unfiltered,
        to_microseconds(CAST(s.max_age_ns // 1000 AS BIGINT)) AS max_age
    FROM ccfg k JOIN scfg s USING (account, stream)
),
now_created AS (
    SELECT account, stream, consumer, created FROM consumer_leader_stats WHERE epoch = getvariable('epoch')
),
-- The consumer's leader rows in the lookback, of its current creation.
lead AS (
    SELECT c.*, e.started_at, k.unfiltered, k.max_age, k.max_age_ns, k.lo
    FROM consumer_leader_stats c JOIN epochs e USING (epoch) JOIN cand k USING (account, stream, consumer)
    JOIN now_created n ON n.account = c.account AND n.stream = c.stream AND n.consumer = c.consumer
     AND n.created IS NOT DISTINCT FROM c.created
    WHERE c.epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch')
),
-- With the stream leader's row of the same collection.
paired AS (
    SELECT l.*, s.first_seq, s.first_ts, s.created AS stream_created,
        l.epoch = getvariable('epoch') AS is_now
    FROM lead l JOIN stream_stats s ON s.epoch = l.epoch AND s.account = l.account AND s.stream = l.stream AND s.is_leader
),
-- The newest and the oldest pair of the lookback are judged.
pts AS (
    SELECT * FROM paired p
    WHERE p.is_now
       OR p.epoch = (SELECT min(y.epoch) FROM paired y WHERE y.account = p.account AND y.stream = p.stream AND y.consumer = p.consumer)
),
-- The stream sequence whose publish time bounds the oldest unread
-- message's, or the time itself where no sequence does.
target AS (
    SELECT p.*, CASE
        WHEN coalesce(p.num_ack_pending, 0) > 0 AND p.unfiltered AND p.ack_floor_stream_seq + 1 >= p.first_seq
            THEN p.ack_floor_stream_seq + 1
        WHEN coalesce(p.num_ack_pending, 0) > 0 THEN p.delivered_stream_seq
        WHEN coalesce(p.num_pending, 0) > 0 AND p.unfiltered AND p.delivered_stream_seq + 1 > p.first_seq
            THEN p.delivered_stream_seq + 1
    END AS seq,
    CASE
        WHEN coalesce(p.num_ack_pending, 0) = 0 AND coalesce(p.num_pending, 0) > 0 AND p.unfiltered
         AND p.delivered_stream_seq + 1 <= p.first_seq THEN p.first_ts
    END AS first_time,
    coalesce(p.num_ack_pending, 0) = 0 AND coalesce(p.num_pending, 0) > 0 AND NOT p.unfiltered AS backlog_start
    FROM pts p
    WHERE coalesce(p.num_ack_pending, 0) > 0 OR coalesce(p.num_pending, 0) > 0
),
-- When a sequence was first reported: the first collection in which the
-- stream's last sequence (leader's view) reached it, a time no earlier than
-- its publication. Found in two steps that keep the joins small: every
-- 64th collection first, then the 64 collections before the one found
-- (or before the collection judged, when none was). A missing report can
-- only make the time later and the age younger. Only the stream's window
-- from lo is read.
seqs AS (
    SELECT DISTINCT account, stream, stream_created, epoch, seq, lo FROM target WHERE seq IS NOT NULL
),
coarse AS (
    SELECT q.account, q.stream, q.stream_created, q.epoch, q.seq, q.lo, coalesce(min(s.epoch), any_value(q.epoch)) AS hi
    FROM seqs q LEFT JOIN stream_stats s
      ON s.account = q.account AND s.stream = q.stream AND s.created IS NOT DISTINCT FROM q.stream_created
     AND s.is_leader AND s.epoch % 64 = 0 AND s.epoch <= q.epoch AND s.last_seq >= q.seq AND s.epoch >= q.lo
    WHERE s.epoch IS NULL OR s.epoch >= (SELECT min(lo) FROM seqs)
    GROUP BY q.account, q.stream, q.stream_created, q.epoch, q.seq, q.lo
),
reached AS (
    SELECT c.account, c.stream, c.epoch, c.seq, min(e.started_at) AS first_at
    FROM coarse c, (VALUES (0), (1)) b(d)
    JOIN stream_stats s ON s.account = c.account AND s.stream = c.stream
     AND s.created IS NOT DISTINCT FROM c.stream_created AND s.epoch // 64 = c.hi // 64 - b.d
    JOIN epochs e ON e.epoch = s.epoch
    WHERE s.is_leader AND s.epoch > c.hi - 64 AND s.epoch <= c.hi AND s.last_seq >= c.seq AND s.epoch >= c.lo
    GROUP BY c.account, c.stream, c.epoch, c.seq
),
-- The backlog's start for a filtered consumer with nothing unacknowledged:
-- the first leader row after the newest one with no backlog, another
-- delivered sequence or another creation. Its oldest unread message
-- already waited then. Each consumer's history in the stream's window is
-- read once for both judged collections; a backlog older than the window
-- starts at its first row.
bpts AS (
    SELECT account, stream, consumer, any_value(lo) AS lo, any_value(created) AS created,
        max(epoch) FILTER (WHERE is_now) AS e_now, any_value(delivered_stream_seq) FILTER (WHERE is_now) AS d_now,
        max(epoch) FILTER (WHERE NOT is_now) AS e_then, any_value(delivered_stream_seq) FILTER (WHERE NOT is_now) AS d_then
    FROM target WHERE backlog_start
    GROUP BY account, stream, consumer
),
bhist AS (
    SELECT b.*, z.epoch AS ze, coalesce(z.num_pending, 0) = 0 OR z.created IS DISTINCT FROM b.created AS gone,
        z.delivered_stream_seq AS zd
    FROM bpts b JOIN consumer_leader_stats z
      ON z.account = b.account AND z.stream = b.stream AND z.consumer = b.consumer AND z.epoch >= b.lo
    WHERE z.epoch >= (SELECT coalesce(min(lo), 0) FROM bpts) AND z.epoch <= getvariable('epoch')
),
breaks AS (
    SELECT account, stream, consumer, any_value(e_now) AS e_now, any_value(e_then) AS e_then,
        max(ze) FILTER (WHERE ze < e_now AND (gone OR zd IS DISTINCT FROM d_now)) AS brk_now,
        max(ze) FILTER (WHERE ze < e_then AND (gone OR zd IS DISTINCT FROM d_then)) AS brk_then
    FROM bhist GROUP BY account, stream, consumer
),
starts AS (
    SELECT h.account, h.stream, h.consumer,
        min(h.ze) FILTER (WHERE h.ze > coalesce(b.brk_now, 0) AND h.ze <= b.e_now) AS s_now,
        min(h.ze) FILTER (WHERE h.ze > coalesce(b.brk_then, 0) AND h.ze <= b.e_then) AS s_then,
        any_value(b.e_now) AS e_now, any_value(b.e_then) AS e_then
    FROM bhist h JOIN breaks b USING (account, stream, consumer)
    GROUP BY h.account, h.stream, h.consumer
),
backlog AS (
    SELECT account, stream, consumer, e_now AS epoch, s_now AS start_epoch FROM starts WHERE e_now IS NOT NULL
    UNION ALL
    SELECT account, stream, consumer, e_then, s_then FROM starts WHERE e_then IS NOT NULL
),
bounded AS (
    SELECT t.*, CASE
        WHEN t.seq IS NOT NULL THEN h.first_at
        WHEN t.first_time IS NOT NULL THEN least(t.first_time, t.started_at)
        WHEN t.backlog_start THEN (SELECT e.started_at FROM epochs e WHERE e.epoch = g.start_epoch)
    END AS published_by
    FROM target t LEFT JOIN backlog g USING (account, stream, consumer, epoch)
    LEFT JOIN reached h USING (account, stream, epoch, seq)
),
aged AS (
    SELECT *, started_at - published_by AS age FROM bounded WHERE published_by IS NOT NULL
),
now_then AS (
    SELECT n.*, o.age AS age_then, o.started_at AS then_at,
        epoch(n.age) / (n.max_age_ns / 1e9) AS ratio
    FROM aged n JOIN aged o ON o.account = n.account AND o.stream = n.stream AND o.consumer = n.consumer AND NOT o.is_now
    WHERE n.is_now
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'consumer-014' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
judged AS (
    SELECT *, account || '/' || stream || '/' || consumer AS entity_key,
        epoch(age) - epoch(age_then) > -epoch(started_at - then_at) / 10 AS not_catching_up
    FROM now_then
    WHERE started_at - then_at >= getvariable('lookback') / 2
)
SELECT
    entity_key,
    CASE WHEN ratio >= getvariable('critical_ratio') THEN 'critical' ELSE 'warning' END AS severity,
    'The oldest unread message is ' || CASE WHEN unfiltered THEN 'about ' ELSE 'at least ' END || span_text(age)
        || ' old, ' || CAST(round(100 * ratio) AS BIGINT) || '% of max_age ' || ns_text(max_age_ns) || ': '
        || CASE WHEN published_by + max_age <= started_at THEN 'it is due to leave the stream now'
            ELSE 'it leaves the stream by ' || strftime(published_by + max_age, '%Y-%m-%d %H:%M UTC') || ', in '
                || span_text(published_by + max_age - started_at) END
        || '. ' || span_text(started_at - then_at) || ' earlier it was ' || span_text(age_then)
        || ' old: the consumer is not catching up. ' || coalesce(num_pending, 0) || ' waiting, '
        || coalesce(num_ack_pending, 0) || ' unacknowledged' || CASE WHEN coalesce(paused, false) THEN '; the consumer is paused'
            ELSE '' END || '.' AS detail,
    round(ratio, 4) AS value,
    json_object('max_age', ns_text(max_age_ns)) AS related_config
FROM judged
WHERE not_catching_up
  AND (ratio >= getvariable('warn_ratio') OR (ratio >= getvariable('resolve_ratio') AND entity_key IN (SELECT entity_key FROM firing)))
