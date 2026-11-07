/*
code: server-022
name: Internal queue backlog
entity: server
category: health
severity: warning
kind: operational
thresholds:
  - name: min_pending
    default: "10000"
    unit: count
    description: >-
      Items waiting in one internal queue at or above which it counts; items being processed (in_progress) are not
      counted.
  - name: min_epochs
    default: "3"
    unit: count
    description: Consecutive collections a queue must hold min_pending items before the finding fires.
  - name: resolve_ratio
    default: "0.5"
    unit: ratio
    description: >-
      A firing finding keeps firing while a queue holds at least this share of min_pending, and resolves once every
      queue is below it, so a queue that hovers at the threshold does not flap.
description: >-
  One of the server's internal queues (IPQUEUESZ), such as a stream's or raft group's, or the JetStream API queue,
  holds at least min_pending items in each of the last min_epochs collections in which the server answered
  IPQUEUESZ. A healthy server empties its queues between two collections; a queue that stays full is a server
  falling behind on that work, and what waits there (messages to store, API requests, raft entries) is late.
remediation: >-
  The detail names the queues, their items now and, when it differs, in the oldest of the last min_epochs
  collections. `nats server request ipqueue --name=nats-0` with the system user lists them. A stream's queue falling
  behind points at its disk (write latency, a full store, server-007) or at more messages than the server can store;
  the JetStream API queue at clients that send many API requests (server-021, and `nats server report jetstream`); a
  raft queue at a slow follower or the network between the servers. Look at the server's CPU (server-023) and
  memory (server-008) too.
related_config: [min_pending, min_epochs, resolve_ratio]
value_name: items waiting
value_basis: items in the server's fullest internal queue
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- IPQUEUESZ lists only queues that are not empty, so a queue the answer
-- leaves out held nothing. A process that restarted within restart_window
-- fills its queues while it catches up and is not judged; one that recovers
-- later, a follower stuck catching up among them, is.
answered AS (
    SELECT ee.epoch, s.server_id, s.server_name
    FROM epoch_endpoints ee JOIN servers s USING (server_id)
    WHERE ee.epoch IN (SELECT epoch FROM last) AND ee.endpoint = 'IPQUEUESZ' AND ee.state = 'ok'
      AND (ee.epoch, ee.server_id) NOT IN (
          SELECT (epoch, server_id) FROM restarting((SELECT min(epoch) FROM last), getvariable('epoch')))
),
queues AS (
    SELECT a.epoch, a.server_name, q.queue, max(q.pending) AS pending
    FROM answered a JOIN ipqueues q ON q.epoch = a.epoch AND q.server_id = a.server_id
    GROUP BY ALL
),
held AS (
    SELECT server_name, queue, count(DISTINCT epoch) FILTER (WHERE pending >= getvariable('min_pending')) AS n,
        max(pending) FILTER (WHERE epoch = (SELECT min(epoch) FROM last)) AS first_pending
    FROM queues GROUP BY ALL
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-022' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
cur AS (
    SELECT q.server_name, q.queue, q.pending, h.first_pending
    FROM queues q JOIN held h USING (server_name, queue)
    WHERE q.epoch = getvariable('epoch')
      AND ((q.pending >= getvariable('min_pending') AND h.n >= getvariable('min_epochs'))
           OR (q.pending >= getvariable('resolve_ratio') * getvariable('min_pending')
               AND q.server_name IN (SELECT entity_key FROM firing)))
),
per AS (
    SELECT server_name, max(pending) AS pending, count(*) AS n,
        list('"' || queue || '" holds ' || pending || ' items' || CASE WHEN first_pending < pending THEN ', up from ' || first_pending
            WHEN first_pending > pending THEN ', down from ' || first_pending ELSE '' END
            ORDER BY pending DESC, queue)[1:3] AS top
    FROM cur GROUP BY server_name
)
SELECT
    server_name AS entity_key,
    server_name || ' is behind on internal queues: ' || array_to_string(top, ', ')
        || CASE WHEN n > 3 THEN ' (+' || (n - 3) || ' more)' ELSE '' END || '.' AS detail,
    pending AS value
FROM per
