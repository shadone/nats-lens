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
  dated by the stream's history: it was published no later than the start of the first collection in which the
  stream's last sequence (leader's view) reached it, so the age is exact to a collection interval, and may read a
  few seconds older, as a collection starts before the servers answer. The message is the one after the ack floor
  while messages are unacknowledged, or the one after the delivered sequence while only undelivered messages wait
  (the stream's first message when that one has left), and the age reads "about". That holds only for a
  consumer without a filter on a stream without deleted sequences (num_deleted 0): a deleted sequence after the
  consumer's position (a KV overwrite, a purge by subject, a work queue) can be far older than its next message.
  Otherwise, and when the message after the ack floor has left the stream, the age is a lower bound and reads "at
  least": the newest delivered message's while messages are unacknowledged, else the time since the backlog began
  (the earliest collection with the same delivered sequence in which messages waited, found among every 64th
  collection and then the 64 before, after the newest two collections in a row in which no message waited; one
  such collection alone, as a new leader may report, does not restart the age). In that case a
  stuck unacknowledged message of a consumer that keeps delivering newer ones is not seen. History before the
  stored collections, or older than the stream's own max_age and the lookback, is not read, so a message that
  old reads younger. An age reads older only when sequences after the consumer's position were deleted, or when
  messages that waited left the stream (deleted or aged out) while newer ones kept the backlog from emptying.
  A firing finding stays until the message has used less than resolve_ratio of max_age, or the consumer catches
  up. Ephemeral and ordered consumers are left out, and so is a consumer without a leader report, or whose stream
  has none, in this collection, or with such reports over less than half the lookback.
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
        l.epoch = getvariable('epoch') AS is_now,
        l.unfiltered AND coalesce(s.num_deleted, 0) = 0 AS exact
    FROM lead l JOIN stream_stats s ON s.epoch = l.epoch AND s.account = l.account AND s.stream = l.stream AND s.is_leader
),
-- The newest and the oldest pair of the lookback are judged.
pts AS (
    SELECT * FROM paired p
    WHERE p.is_now
       OR p.epoch = (SELECT min(y.epoch) FROM paired y WHERE y.account = p.account AND y.stream = p.stream AND y.consumer = p.consumer)
),
-- The stream sequence whose publish time bounds the oldest unread
-- message's, or the time itself where no sequence does. The sequence after
-- the consumer's position is its next message only without a filter and
-- without deleted sequences in the stream (num_deleted, which does not say
-- where they are); otherwise the bounds that hold anyway are taken, which
-- read "at least".
target AS (
    SELECT p.*, CASE
        WHEN coalesce(p.num_ack_pending, 0) > 0 AND p.exact AND p.ack_floor_stream_seq + 1 >= p.first_seq
            THEN p.ack_floor_stream_seq + 1
        WHEN coalesce(p.num_ack_pending, 0) > 0 THEN p.delivered_stream_seq
        WHEN coalesce(p.num_pending, 0) > 0 AND p.exact AND p.delivered_stream_seq + 1 > p.first_seq
            THEN p.delivered_stream_seq + 1
    END AS seq,
    CASE
        WHEN coalesce(p.num_ack_pending, 0) = 0 AND coalesce(p.num_pending, 0) > 0 AND p.exact
         AND p.delivered_stream_seq + 1 <= p.first_seq THEN p.first_ts
    END AS first_time,
    coalesce(p.num_ack_pending, 0) = 0 AND coalesce(p.num_pending, 0) > 0 AND NOT p.exact AS backlog_start,
    p.exact AND (coalesce(p.num_ack_pending, 0) = 0 OR p.ack_floor_stream_seq + 1 >= p.first_seq) AS about
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
-- The backlog's start, where the next sequence does not date the oldest
-- unread message: the earliest leader row of the same creation with the
-- delivered sequence of the judged collection and messages waiting, after
-- the newest two collections in a row in which no message waited. As nothing was
-- delivered since, a message that waited then still waits, so any such row
-- bounds the publication; the earliest found is used. It is searched like
-- the dating of a sequence: every 64th collection first, then the 64
-- collections before the one found (or before the collection judged, when
-- none was), so the cost per consumer does not grow with its history. A row
-- the search skips can only make the start later and the age younger.
bpts AS (
    SELECT account, stream, consumer, lo, created, epoch, delivered_stream_seq AS d
    FROM target WHERE backlog_start
),
bcoarse AS (
    SELECT p.account, p.stream, p.consumer, p.lo, p.created, p.epoch, p.d,
        coalesce(min(z.epoch), any_value(p.epoch)) AS hi
    FROM bpts p LEFT JOIN consumer_leader_stats z
      ON z.account = p.account AND z.stream = p.stream AND z.consumer = p.consumer AND z.epoch % 64 = 0
     AND z.epoch >= p.lo AND z.epoch <= p.epoch AND z.created IS NOT DISTINCT FROM p.created
     AND z.delivered_stream_seq = p.d AND z.num_pending > 0
    GROUP BY p.account, p.stream, p.consumer, p.lo, p.created, p.epoch, p.d
),
bfirst AS (
    SELECT c.account, c.stream, c.consumer, c.epoch, any_value(c.created) AS created, any_value(c.d) AS d,
        min(z.epoch) AS start_epoch
    FROM bcoarse c, (VALUES (0), (1)) b(k)
    JOIN consumer_leader_stats z ON z.account = c.account AND z.stream = c.stream AND z.consumer = c.consumer
     AND z.epoch // 64 = c.hi // 64 - b.k
    -- Bound the scan before joining consumers. DuckDB 2.0 can otherwise
    -- join their full history before applying the two-block condition.
    WHERE z.epoch BETWEEN (SELECT min(hi) - 63 FROM bcoarse) AND (SELECT max(hi) FROM bcoarse)
      AND z.epoch > c.hi - 64 AND z.epoch <= c.hi AND z.epoch >= c.lo AND z.created IS NOT DISTINCT FROM c.created
      AND z.delivered_stream_seq = c.d AND z.num_pending > 0
    GROUP BY c.account, c.stream, c.consumer, c.epoch
),
-- The newest collection after that start that ends two collections in a
-- row in which the same consumer's leader reported no message waiting: the
-- backlog emptied there, its messages deleted or aged out, and the one
-- waiting now came later. One such collection alone, as a new leader may
-- report, does not restart the age.
bzero AS (
    SELECT f.account, f.stream, f.consumer, f.epoch, z.epoch AS ze
    FROM bfirst f JOIN consumer_leader_stats z
      ON z.account = f.account AND z.stream = f.stream AND z.consumer = f.consumer AND z.num_pending = 0
     AND z.created IS NOT DISTINCT FROM f.created AND z.epoch >= f.start_epoch AND z.epoch < f.epoch
    WHERE z.epoch >= (SELECT min(start_epoch) FROM bfirst)
),
bempty AS (
    SELECT a.account, a.stream, a.consumer, a.epoch, max(a.ze) AS empty
    FROM bzero a JOIN bzero b ON b.account = a.account AND b.stream = a.stream AND b.consumer = a.consumer
     AND b.epoch = a.epoch AND b.ze = a.ze - 1
    GROUP BY a.account, a.stream, a.consumer, a.epoch
),
-- The first row with messages waiting after the backlog emptied: searched
-- within the three blocks of 64 collections from it, and, when the leader
-- did not report there, among all rows after it.
bnear AS (
    SELECT g.account, g.stream, g.consumer, g.epoch, min(z.epoch) AS start_epoch
    FROM bempty g JOIN bfirst f USING (account, stream, consumer, epoch), (VALUES (0), (1), (2)) b(k)
    JOIN consumer_leader_stats z ON z.account = g.account AND z.stream = g.stream AND z.consumer = g.consumer
     AND z.epoch // 64 = g.empty // 64 + b.k
    WHERE z.epoch > g.empty AND z.epoch <= g.epoch AND z.created IS NOT DISTINCT FROM f.created
      AND z.delivered_stream_seq = f.d AND z.num_pending > 0
    GROUP BY g.account, g.stream, g.consumer, g.epoch
),
bfar AS (
    SELECT g.account, g.stream, g.consumer, g.epoch, min(z.epoch) AS start_epoch
    FROM bempty g JOIN bfirst f USING (account, stream, consumer, epoch)
    JOIN consumer_leader_stats z ON z.account = g.account AND z.stream = g.stream AND z.consumer = g.consumer
    WHERE z.epoch > g.empty AND z.epoch <= g.epoch AND z.created IS NOT DISTINCT FROM f.created
      AND z.delivered_stream_seq = f.d AND z.num_pending > 0
      AND NOT EXISTS (SELECT 1 FROM bnear n WHERE n.account = g.account AND n.stream = g.stream
          AND n.consumer = g.consumer AND n.epoch = g.epoch)
    GROUP BY g.account, g.stream, g.consumer, g.epoch
),
brefill AS (
    SELECT * FROM bnear UNION ALL SELECT * FROM bfar
),
backlog AS (
    SELECT f.account, f.stream, f.consumer, f.epoch,
        CASE WHEN g.empty IS NULL THEN f.start_epoch ELSE r.start_epoch END AS start_epoch
    FROM bfirst f LEFT JOIN bempty g USING (account, stream, consumer, epoch)
    LEFT JOIN brefill r USING (account, stream, consumer, epoch)
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
    'The oldest unread message is ' || CASE WHEN about THEN 'about ' ELSE 'at least ' END || span_text(age)
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
