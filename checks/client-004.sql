/*
code: client-004
name: Client reconnecting repeatedly
entity: client
category: clients
severity: warning
kind: operational
lookback: 10m
thresholds:
  - name: min_reconnects
    default: "3"
    unit: count
    description: Reconnects of one connection name within the lookback at which the finding fires.
description: >-
  Connections with this name lost their connection min_reconnects times or more within the lookback and came back:
  a client.disconnect whose reason is neither the client closing it ("Client Closed") nor a server stopping
  ("Server Shutdown", which lame duck mode uses too), paired with a client.connect of the same name in the window,
  in either order and from any host; the count is the smaller of the two. Connects collapsed into a client.churn
  event count as connects. Grouped by the name the client gives
  itself, often the service name, so instances sharing a name are merged; a client without a
  name is grouped as (unnamed). Connects alone do not count: a service that scales out opens one connection per
  instance under one name.
remediation: >-
  The reasons in the detail say why the connections end: a slow consumer (client-003; the client reads too slowly),
  a stale connection (it stops answering pings: a blocked event loop, CPU throttling, or a network path that drops
  idle connections), an authentication error (expired credentials). A pod in a crash loop reconnects once per start:
  check `kubectl get pods` for restarts. Fix the cause; reconnecting loses the messages in flight for core NATS
  subscriptions.
related_config: [min_reconnects, lookback]
value_name: reconnects
value_basis: >-
  the smaller of the disconnects not asked for by the client or caused by a server stopping, and the connects, of
  the connection name within the lookback
*/
WITH drops AS (
    SELECT * FROM events
    WHERE kind = 'client.disconnect' AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
      AND coalesce(reason, '') NOT IN ('Client Closed', 'Server Shutdown') AND NOT tooling(client_name, client_host)
),
-- A client.churn event stands for connects collapsed with their
-- self-closed disconnects; its connects count too.
connects AS (
    SELECT client_name,
        sum(CASE WHEN kind = 'client.churn' THEN coalesce(TRY_CAST(raw->>'connects' AS BIGINT), 0) ELSE 1 END)::BIGINT AS n
    FROM events
    WHERE kind IN ('client.connect', 'client.churn') AND NOT tooling(client_name, client_host)
      AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
    GROUP BY client_name
),
-- Drops and connects are paired by name only and in either order: a
-- client often comes back from another address (a new pod), and a server
-- may log a drop after the client already connected again. The count is
-- the smaller of the two.
named AS (
    SELECT d.client_name, count(*) AS dropped, any_value(c.n) AS connected,
        array_to_string(list(DISTINCT coalesce(d.server_name, '?') ORDER BY coalesce(d.server_name, '?')), ', ') AS servers,
        array_to_string(list(DISTINCT coalesce(d.reason, 'no reason') ORDER BY coalesce(d.reason, 'no reason')), ', ') AS reasons,
        max(d.time) AS last_at, arg_max(d.id, (d.received_at, d.id)) AS last_id
    FROM drops d JOIN connects c ON c.client_name IS NOT DISTINCT FROM d.client_name
    GROUP BY d.client_name
)
SELECT
    coalesce(nullif(client_name, ''), '(unnamed)') AS entity_key,
    least(dropped, connected) || ' reconnects in the last ' || getvariable('lookback_text') || ' (' || servers || '): '
        || reasons || '; the last at ' || strftime(last_at, '%H:%M:%S') || ' UTC.' AS detail,
    least(dropped, connected) AS value,
    last_id AS cause_event_id
FROM named
WHERE least(dropped, connected) >= getvariable('min_reconnects')
