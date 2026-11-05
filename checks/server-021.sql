/*
code: server-021
name: JetStream API errors high
entity: server
category: health
severity: warning
kind: operational
lookback: 15m
thresholds:
  - name: min_errors
    default: "100"
    unit: count
    description: Fewest JetStream API errors within the lookback that can fire.
  - name: error_ratio
    default: "0.5"
    unit: ratio
    description: Share of the JetStream API requests within the lookback answered with an error at or above which the finding fires.
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive collections the thresholds must be met before the finding fires.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the errors stay at or above this share of both thresholds, and resolves
      once they fall below this share of either, so an error rate that hovers at the threshold does not flap.
description: >-
  The server answered at least error_ratio of the JetStream API requests it served within the lookback with an
  error, and at least min_errors of them, in each of the last min_epochs collections. Counted per server process, so
  a restart starts the count again. nats-server counts every error answer, so clients that ask for what does not
  exist, such as a consumer info before creating the consumer or a message get (`$JS.API.STREAM.MSG.GET`) of a
  message that is not there, count as well; a direct get (`$JS.API.DIRECT.GET`), as KV buckets use, is not counted.
remediation: >-
  JSZ counts the errors but not their kind. Find the callers: the `api.audit` events on the Events page (the
  JetStream API audit advisories of the accounts nats-lens listens to) hold the subject and response of each
  call, and `nats server request jetstream --name=nats-0` with the system user shows the server's API totals.
  Common causes are a client that looks up or creates a stream or consumer on every start or every message (it
  should keep it), a client without permission for the API subject, and JetStream that is not ready (server-003),
  where every request fails. An account at its JetStream limits answers stream and consumer creation with errors as
  well.
related_config: [lookback, min_errors, error_ratio, min_epochs, resolve_ratio]
value_name: share of JetStream API requests failed
value_basis: growth of the API error counter divided by growth of the API request counter within the lookback
suppressed_by: [server-003]
*/
WITH last AS (
    SELECT epoch, started_at FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- Requests and errors within the lookback ending at each of the last
-- epochs, per server process.
per AS (
    SELECT l.epoch, st.server_id, st.js_api_total - b.total AS requests, st.js_api_errors - b.errors AS errors
    FROM last l JOIN server_stats st ON st.epoch = l.epoch
    JOIN LATERAL (
        SELECT arg_min(x.js_api_total, x.epoch) AS total, arg_min(x.js_api_errors, x.epoch) AS errors
        FROM server_stats x JOIN epochs xe USING (epoch)
        WHERE x.server_id = st.server_id AND xe.started_at >= l.started_at - getvariable('lookback')
          AND x.epoch < l.epoch AND x.js_api_total IS NOT NULL AND x.js_api_errors IS NOT NULL
    ) b ON true
    WHERE st.js_api_total IS NOT NULL AND st.js_api_errors IS NOT NULL AND b.total IS NOT NULL
),
judged AS (
    SELECT s.server_name, p.epoch, p.requests, p.errors,
        p.errors >= getvariable('min_errors') AND p.errors >= getvariable('error_ratio') * p.requests AS over,
        p.errors >= getvariable('resolve_ratio') * getvariable('min_errors')
          AND p.errors >= getvariable('resolve_ratio') * getvariable('error_ratio') * p.requests AS holding
    FROM per p JOIN servers s USING (server_id)
    WHERE p.requests > 0
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-021' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
verdict AS (
    SELECT server_name, count(DISTINCT epoch) FILTER (WHERE over) AS over_epochs,
        max(errors) FILTER (WHERE epoch = getvariable('epoch')) AS errors,
        max(requests) FILTER (WHERE epoch = getvariable('epoch')) AS requests,
        bool_or(holding) FILTER (WHERE epoch = getvariable('epoch')) AS holding,
        bool_or(over) FILTER (WHERE epoch = getvariable('epoch')) AS over
    FROM judged GROUP BY server_name
)
SELECT
    server_name AS entity_key,
    server_name || ' answered ' || errors || ' of ' || requests || ' JetStream API requests ('
        || regexp_replace(CAST(round(100 * errors / requests, 1) AS VARCHAR), '\.0$', '') || '%) with an error in the last '
        || getvariable('lookback_text') || '.' AS detail,
    round(errors / requests, 4) AS value
FROM verdict
WHERE errors IS NOT NULL
  AND ((over AND over_epochs >= getvariable('min_epochs'))
       OR (holding AND server_name IN (SELECT entity_key FROM firing)))
