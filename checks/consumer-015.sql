/*
code: consumer-015
name: Pull requests at max_waiting
entity: consumer
category: consumers
severity: warning
kind: operational
thresholds:
  - name: warn_ratio
    default: "1"
    unit: ratio
    description: Share of max_waiting that the waiting pull requests must reach; at 1 the server refuses new fetches.
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive collections at warn_ratio before the finding fires.
  - name: resolve_ratio
    default: "0.9"
    unit: ratio
    description: A firing finding stays until the waiting pull requests fall below this share of max_waiting.
description: >-
  A pull consumer holds as many waiting pull requests as max_waiting allows (num_waiting, leader's view, which
  nats-server counts after it drops expired requests), in each of the last min_epochs collections. While the
  queue is full the server answers every new fetch with "409 Exceeded MaxWaiting", or drops it silently for a
  client that asked for heartbeats, so a worker that starts or reconnects gets nothing. A firing finding stays
  until the count falls below resolve_ratio of max_waiting, so a queue that hovers at the limit does not flap.
  Paused consumers are left out, as their requests wait by design.
remediation: >-
  Each idle fetch with an expiry holds a slot until it expires, so the usual causes are more workers than
  max_waiting, or clients that fetch with long expiries in many goroutines. Count the workers of the service
  (Connections page, by name) and compare with max_waiting; `nats consumer info <stream> <consumer>` shows the
  waiting requests. Use fewer concurrent fetches per worker or shorter expiries. nats-server cannot change
  max_waiting on an existing consumer: a larger limit means a new durable (`nats consumer add <stream>
  <consumer> --pull --max-waiting <n>`), created with deliver_policy by_start_time so that it does not replay the
  stream.
related_config: [max_waiting, warn_ratio, resolve_ratio]
value_name: waiting pull requests share of max_waiting
value_basis: num_waiting divided by max_waiting, leader's view
*/
WITH last AS (
    SELECT epoch FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
cfg AS (
    SELECT k.server_id, k.account, k.stream, k.consumer, k.max_waiting
    FROM consumer_config k
    WHERE k.first_epoch <= getvariable('epoch') AND k.last_epoch >= getvariable('epoch')
      AND coalesce(k.deliver_subject, '') = '' AND k.max_waiting > 0
),
cur AS (
    SELECT c.account, c.stream, c.consumer, c.created, c.num_waiting, c.num_pending, k.max_waiting, s.server_name
    FROM consumer_leader_stats c
    JOIN cfg k USING (server_id, account, stream, consumer)
    JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch') AND c.num_waiting IS NOT NULL AND NOT coalesce(c.paused, false)
),
-- Collections of the last min_epochs in which the same consumer (same
-- creation time) was at warn_ratio of the limit on its leader.
full_runs AS (
    SELECT c.account, c.stream, c.consumer, count(*) AS n
    FROM cur c JOIN consumer_leader_stats x
      ON x.account = c.account AND x.stream = c.stream AND x.consumer = c.consumer
     AND x.created IS NOT DISTINCT FROM c.created
    WHERE x.epoch IN (SELECT epoch FROM last)
      AND x.num_waiting >= getvariable('warn_ratio') * c.max_waiting
    GROUP BY c.account, c.stream, c.consumer
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'consumer-015' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
)
SELECT
    c.account || '/' || c.stream || '/' || c.consumer AS entity_key,
    c.num_waiting || ' pull requests waiting of max_waiting ' || c.max_waiting
        || CASE WHEN c.num_waiting >= c.max_waiting THEN ': new fetches are refused'
            ELSE ', close to the limit' END
        || '; ' || coalesce(c.num_pending, 0) || ' messages waiting.' AS detail,
    round(c.num_waiting / c.max_waiting, 4) AS value,
    json_object('max_waiting', CAST(c.max_waiting AS VARCHAR)) AS related_config,
    c.server_name AS related_source
FROM cur c LEFT JOIN full_runs f USING (account, stream, consumer)
WHERE coalesce(f.n, 0) >= getvariable('min_epochs')
   OR (c.num_waiting >= getvariable('resolve_ratio') * c.max_waiting
       AND c.account || '/' || c.stream || '/' || c.consumer IN (SELECT entity_key FROM firing))
