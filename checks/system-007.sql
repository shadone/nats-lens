/*
code: system-007
name: Collections often incomplete
entity: system
category: engine
severity: warning
kind: trend
lookback: 30m
thresholds:
  - name: warn_share
    default: "0.25"
    unit: ratio
    description: Share of the lookback's collections that are incomplete at or above which the finding fires.
  - name: min_collections
    default: "10"
    unit: count
    description: Collections the lookback must hold before the check judges, so that a short history or a gap does not fire.
  - name: resolve_ratio
    default: "0.5"
    unit: ratio
    description: >-
      A firing finding keeps firing while the share stays at or above this part of warn_share, and resolves below
      it, so a share that hovers at the threshold does not flap.
description: >-
  At least warn_share of the collections in the lookback were incomplete: a server that answered left a monitoring
  request unanswered or answered it with an error, or nats-lens itself failed during the collection (a request it
  could not publish, a reply it could not read). The checks judge each collection on what it holds, so findings
  that need the missing replies can be missed, or resolve and fire again. Servers that answered nothing are left
  out, as server-001 reports them, and so are the replies of a server within checks.restart_window of its start or
  after it announced lame duck or shutdown. The notes the store adds to a collection's errors (values a newer server
  sent, a configuration not stored) are no failure and do not count. A firing finding stays until the share falls
  below resolve_ratio of warn_share.
remediation: >-
  The detail names the servers and requests that failed most. A request that runs out of time on a busy server
  (often JSZ, CONNZ or RAFTZ on a server with many streams, consumers or connections) needs a longer collect window:
  raise `timeout` in the nats-lens configuration, keeping it well below `interval`, or lower the load the server is
  under. An error such as "permissions violation" means the user nats-lens connects with may not send that request:
  grant it `$SYS.REQ.>` in the system account. nats-lens logs every collection as `epoch stored`, at warning level
  with `partial=true` and its own errors when it is incomplete.
related_config: [warn_share, min_collections, resolve_ratio, lookback]
value_name: percent of collections incomplete
value_basis: incomplete collections in the lookback over all collections in it, servers that answered nothing left out
*/
WITH win AS (
    -- The store adds its notes (values a newer server sent, a configuration
    -- not stored) to the errors with the prefix "note: "; they are no
    -- failure of the collection.
    SELECT epoch, started_at, finished_at, list_filter(errors, lambda x: NOT starts_with(x, 'note: ')) AS errors
    FROM epochs WHERE epoch BETWEEN getvariable('window_epoch') AND getvariable('epoch')
),
restarting_now AS (
    SELECT epoch, server_id FROM restarting(getvariable('window_epoch'), getvariable('epoch'))
),
-- A server that announced lame duck or shutdown within restart_window
-- before the collection ended is going away on purpose.
announced AS (
    SELECT DISTINCT w.epoch, ev.server_name
    FROM win w JOIN events ev ON ev.kind IN ('server.lameduck', 'server.shutdown')
     AND ev.received_at > w.started_at - getvariable('restart_window') AND ev.received_at <= w.finished_at
),
failures AS (
    SELECT ee.epoch, s.server_name, ee.endpoint, ee.state, ee.error
    FROM epoch_endpoints ee JOIN servers s USING (server_id)
    WHERE ee.epoch IN (SELECT epoch FROM win) AND ee.state <> 'ok'
      AND NOT EXISTS (SELECT 1 FROM restarting_now r WHERE r.epoch = ee.epoch AND r.server_id = ee.server_id)
      AND NOT EXISTS (SELECT 1 FROM announced a WHERE a.epoch = ee.epoch AND a.server_name = s.server_name)
),
judged AS (
    SELECT w.epoch, len(w.errors) > 0 AS collector_failed,
        len(w.errors) > 0 OR EXISTS (SELECT 1 FROM failures f WHERE f.epoch = w.epoch) AS incomplete
    FROM win w
),
totals AS (
    SELECT count(*) AS n, count(*) FILTER (WHERE incomplete) AS bad, count(*) FILTER (WHERE collector_failed) AS own
    FROM judged
),
firing AS (
    SELECT 1 FROM findings
    WHERE check_code = 'system-007' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
-- The requests that failed most, each with its latest error.
worst AS (
    SELECT endpoint || ' from ' || server_name || ' ' || CASE WHEN bool_and(state = 'missing') THEN 'unanswered' ELSE 'failed' END
        || ' in ' || count(DISTINCT epoch)
        || coalesce(' (' || arg_max(nullif(error, ''), epoch) || ')', '') AS text,
        count(DISTINCT epoch) AS times, server_name, endpoint
    FROM failures GROUP BY server_name, endpoint
),
own_error AS (
    SELECT arg_max(errors[1], epoch) AS error FROM win WHERE len(errors) > 0
)
SELECT
    '' AS entity_key,
    t.bad || ' of ' || t.n || ' collections in the last ' || getvariable('lookback_text') || ' were incomplete ('
        || CAST(round(100.0 * t.bad / t.n) AS BIGINT) || '%): '
        || array_to_string(list_filter([
            (SELECT array_to_string(list(text ORDER BY times DESC, server_name, endpoint)[1:3], '; ')
                || CASE WHEN count(*) > 3 THEN '; and ' || (count(*) - 3) || ' more' ELSE '' END FROM worst),
            CASE WHEN t.own > 0 THEN 'nats-lens failed itself in ' || t.own
                || coalesce(' (' || (SELECT error FROM own_error) || ')', '') END], lambda x: x IS NOT NULL AND x <> ''), '; ')
        || '. Findings that need the missing replies may be missed.' AS detail,
    round(100.0 * t.bad / t.n, 1) AS value
FROM totals t
WHERE t.n >= getvariable('min_collections') AND t.bad > 0 AND (
    t.bad >= getvariable('warn_share') * t.n
    OR (EXISTS (SELECT 1 FROM firing) AND t.bad >= getvariable('resolve_ratio') * getvariable('warn_share') * t.n)
)
