/*
code: server-011
name: Connection churn
entity: server
category: clients
severity: warning
kind: operational
lookback: 10m
thresholds:
  - name: min_new
    default: "50"
    unit: count
    description: Fewest new connections within the lookback that can fire.
  - name: churn_ratio
    default: "0.5"
    unit: ratio
    description: New connections within the lookback at or above this share of the open connections fire.
  - name: min_epochs
    default: "2"
    unit: count
    description: Consecutive epochs the thresholds must be met before the finding fires.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the connections accepted stay at or above this share of both thresholds,
      and resolves once they fall below this share of either, so churn that hovers at the threshold does not flap.
description: >-
  The server accepted many new connections within the lookback compared with the connections it holds, in each of
  the last min_epochs epochs: clients are reconnecting over and over, or something opens a connection per request.
  A firing finding stays until the count falls below resolve_ratio of the thresholds. The detail names the clients
  that connected most within the lookback, from the client.connect and client.churn events.
remediation: >-
  The detail names the clients that connected most. Look at their client.connect, client.disconnect and client.churn
  events on the Events page: the client name and the disconnect reason say who reconnects and why (authentication error, slow consumer, stale connection, the client
  closing). A service that opens a connection per request should keep one connection for its lifetime. A pod in a
  crash loop reconnects on every start: check `kubectl get pods` for restarts.
related_config: [lookback, min_new, churn_ratio, min_epochs, resolve_ratio]
value_name: connections accepted
value_basis: growth of total connections within the lookback
*/
WITH last AS (
    SELECT epoch, started_at FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT CAST(getvariable('min_epochs') AS INTEGER)
),
-- Connections accepted within the lookback ending at each of the last
-- epochs, per server process.
per AS (
    SELECT l.epoch, st.server_id, st.total_connections, coalesce(st.connections, 0) AS open,
        (SELECT arg_min(b.total_connections, b.epoch) FROM server_stats b JOIN epochs be USING (epoch)
         WHERE b.server_id = st.server_id AND be.started_at >= l.started_at - getvariable('lookback')
           AND b.epoch < l.epoch AND b.total_connections IS NOT NULL) AS base
    FROM last l JOIN server_stats st ON st.epoch = l.epoch
    WHERE st.total_connections IS NOT NULL
),
judged AS (
    SELECT s.server_name, p.epoch, p.total_connections - p.base AS accepted, p.open,
        p.total_connections - p.base >= getvariable('min_new')
          AND p.total_connections - p.base >= getvariable('churn_ratio') * greatest(p.open, 1) AS over,
        p.total_connections - p.base >= getvariable('resolve_ratio') * getvariable('min_new')
          AND p.total_connections - p.base >= getvariable('resolve_ratio') * getvariable('churn_ratio') * greatest(p.open, 1) AS holding
    FROM per p JOIN servers s USING (server_id)
    WHERE p.base IS NOT NULL
),
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-011' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
verdict AS (
    SELECT server_name, count(*) FILTER (WHERE over) AS over_epochs,
        any_value(accepted) FILTER (WHERE epoch = getvariable('epoch')) AS accepted,
        any_value(open) FILTER (WHERE epoch = getvariable('epoch')) AS open,
        bool_or(holding) FILTER (WHERE epoch = getvariable('epoch')) AS holding
    FROM judged GROUP BY server_name
),
clients AS (
    SELECT server_name, client_name, sum(n)::BIGINT AS n FROM (
        SELECT server_name, coalesce(nullif(client_name, ''), '(unnamed)') AS client_name,
            CASE WHEN kind = 'client.churn' THEN coalesce(TRY_CAST(raw->>'connects' AS BIGINT), 0) ELSE 1 END AS n
        FROM events
        WHERE kind IN ('client.connect', 'client.churn') AND server_name IS NOT NULL AND NOT tooling(client_name, client_host)
          AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time'))
    GROUP BY ALL
),
top AS (
    SELECT server_name, array_to_string(list(client_name || ' ' || n ORDER BY n DESC, client_name)[1:3], ', ') AS names
    FROM clients GROUP BY server_name
)
SELECT
    v.server_name AS entity_key,
    v.server_name || ' accepted ' || v.accepted || ' connections in the last ' || getvariable('lookback_text')
        || ' and holds ' || v.open || ': clients are reconnecting.'
        || coalesce(' Most connects: ' || t.names || '.', '') AS detail,
    v.accepted AS value
FROM verdict v LEFT JOIN top t USING (server_name)
WHERE v.accepted IS NOT NULL
  AND (v.over_epochs >= getvariable('min_epochs')
       OR (v.holding AND v.server_name IN (SELECT entity_key FROM firing)))
