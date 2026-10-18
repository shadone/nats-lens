/*
code: client-005
name: Short-lived connections
entity: client
category: clients
severity: warning
kind: operational
lookback: 10m
thresholds:
  - name: min_short_connections
    default: "20"
    unit: count
    description: Short-lived connections of one connection name within the lookback at which the finding fires.
description: >-
  Connections with this name were opened and closed by the client itself, each within seconds, min_short_connections
  times or more within the lookback. These are the connects that event capture folds into client.churn events (a
  client.connect followed by its client.disconnect with reason "Client Closed" within events.churn.pair_within), so
  the check needs events.churn.collapse, which is on by default. Grouped by the name the client gives itself, from
  any host, so instances sharing a name are merged; a client without a name is grouped as (unnamed). Every connection
  costs a TCP and TLS handshake, authentication, and a connect and a disconnect advisory on the server, and a client
  that holds no connection misses the messages published while it is away.
remediation: >-
  A client that opens a connection per request, per reconcile loop or per job step should open one connection when it
  starts and keep it: a NATS connection is meant to live as long as the process and reconnects by itself. Find the
  code path from the hosts in the detail and the client's own logs. A process that crashes, or exits after each unit
  of work, and is restarted looks the same when it closes its connection cleanly: check its pod restarts
  (`kubectl get pods`). A batch job that connects once per run for minutes at a time is not folded and does not count.
related_config: [min_short_connections, lookback]
value_name: short-lived connections
value_basis: >-
  the connects folded into client.churn events of the connection name within the lookback, each closed by the client
  within events.churn.pair_within
*/
WITH churn AS (
    SELECT id, received_at, coalesce(nullif(client_name, ''), '(unnamed)') AS name, nullif(client_host, '') AS host,
        nullif(server_name, '') AS server, raw, coalesce(TRY_CAST(raw->>'connects' AS BIGINT), 0) AS n
    FROM events
    WHERE kind = 'client.churn' AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
      AND NOT tooling(client_name, client_host)
),
-- A summary names its servers in raw.servers (connects per server); one
-- without them falls back to its server_name, which is set when all its
-- connects were on one server.
servers AS (
    SELECT name, server FROM churn WHERE server IS NOT NULL
    UNION
    SELECT name, unnest(coalesce(TRY(json_keys(raw, '$.servers')), []::VARCHAR[])) AS server FROM churn
),
server_lists AS (
    SELECT name, array_to_string(list(DISTINCT server ORDER BY server), ', ') AS servers
    FROM servers WHERE coalesce(server, '') <> ''
    GROUP BY name
),
-- Summaries of one flush share received_at; of those, the highest id was
-- written last.
named AS (
    SELECT name, sum(n)::BIGINT AS connects,
        list(DISTINCT host ORDER BY host) FILTER (WHERE host IS NOT NULL) AS hosts,
        arg_max(id, (received_at, id)) AS last_id
    FROM churn
    GROUP BY name
)
SELECT
    n.name AS entity_key,
    n.connects || ' connections closed by the client within seconds in the last ' || getvariable('lookback_text')
        || ' (about ' || round(n.connects * 60.0 / epoch(getvariable('lookback')))::BIGINT || ' a minute) from '
        || CASE
            WHEN coalesce(len(n.hosts), 0) = 0 THEN 'an unknown host'
            WHEN len(n.hosts) > 5 THEN array_to_string(n.hosts[1:5], ', ') || ' and ' || (len(n.hosts) - 5) || ' more hosts'
            ELSE array_to_string(n.hosts, ', ')
        END
        || coalesce(' (' || s.servers || ')', '') || '.' AS detail,
    n.connects AS value,
    n.last_id AS cause_event_id
FROM named n LEFT JOIN server_lists s ON s.name = n.name
WHERE n.connects >= getvariable('min_short_connections')
