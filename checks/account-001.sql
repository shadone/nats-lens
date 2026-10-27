/*
code: account-001
name: Account near its connection limit
entity: account
category: clients
severity: warning
severity_rule: critical at or above crit_ratio of the limit; at the limit the servers refuse the account's new connections
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.9"
    unit: ratio
    description: Share of the account JWT's connection limit in use at which the finding fires.
  - name: crit_ratio
    default: "1"
    unit: ratio
    description: Share at which it is critical.
description: >-
  The account's connections are at warn_ratio or more of the connection limit in its account JWT, counted as
  nats-server counts them: a server refuses a new client connection of the account when its own clients and leaf
  node connections of the account, plus the client connections the other servers report for it, reach the limit
  ("maximum account active connections exceeded"). Each server counts only its own leaf nodes, so the count is the
  client connections summed over the servers that answered in the collection plus the leaf node connections of the
  server that holds the most of them, the first server to refuse. Of two processes of one server name in the
  collection (the old one shutting down, the new one up), only the later started is counted, as server-016 reads
  certificates. At the limit a service that restarts or scales out cannot connect. A server whose count goes over the
  limit, as when another server reports more clients, closes its newest connections of the account, clients and leaf
  nodes alike, until it is back at the limit. Read from the account's newest lookup within the last 24 hours, as on the
  Accounts page; an account without a JWT (config mode), without a limit (-1) or not looked up within 24 hours is not
  reported.
remediation: >-
  Find which clients hold the connections: the Connections page filtered to the account, grouped by name, and the
  leaf node connections of the server the detail names. A client that opens a connection per request or leaks
  connections fills the limit by itself; fix it first. Over the limit a server closes its newest connections of the
  account, leaf nodes included, so a leaf node site can drop when clients grow elsewhere. If the load is real, raise the limit in the account JWT
  (`nsc edit account <name> --conns <n>`, then push the account JWT to the resolver with `nsc push`).
related_config: [conn]
value_name: connection limit share
value_basis: >-
  client connections summed over the servers of the collection, plus the leaf node connections of the server with the
  most, divided by the account JWT's connection limit
*/
-- Each account's share on each server, one process per server name, and
-- its connections as nats-server counts them (account_servers and
-- account_connections, which the Accounts page reads too): every server's
-- clients and the leaf nodes of the server with the most, which reaches the
-- limit first.
WITH per_server AS (
    SELECT account, server_name, coalesce(conns, 0) AS conns, coalesce(leafnodes, 0) AS leafs
    FROM account_servers(getvariable('epoch'))
    WHERE conns IS NOT NULL OR leafnodes IS NOT NULL
),
st AS (
    SELECT account, coalesce(clients, 0) AS clients, coalesce(limit_leafnodes, 0) AS leafs,
        limit_leafnodes_server AS leaf_server
    FROM account_connections(getvariable('epoch'))
    WHERE clients IS NOT NULL OR limit_leafnodes IS NOT NULL
),
cur AS (
    SELECT d.account, d.title, d.limit_connections, st.clients, st.leafs, st.leaf_server, st.clients + st.leafs AS used,
        (st.clients + st.leafs) / d.limit_connections AS ratio
    FROM account_lookups(getvariable('epoch')) d JOIN st USING (account)
    WHERE d.has_jwt AND d.limit_connections > 0
),
-- The servers that refuse a new client: each whose own leaf nodes, with
-- every server's clients, reach the limit; every server once the clients
-- alone reach it.
refusing AS (
    SELECT c.account, list(p.server_name ORDER BY p.server_name) AS servers
    FROM cur c JOIN per_server p USING (account)
    WHERE c.clients + p.leafs >= c.limit_connections
    GROUP BY c.account
)
SELECT
    account AS entity_key,
    CASE WHEN ratio >= getvariable('crit_ratio') THEN 'critical' ELSE 'warning' END AS severity,
    title || ': ' || CASE
        WHEN leafs > 0 THEN used || ' of ' || limit_connections || ' connections (' || round(100 * ratio, 1) || '%): '
            || clients || ' client connections and ' || leafs || ' leaf node '
            || CASE WHEN leafs = 1 THEN 'connection' ELSE 'connections' END || ' on ' || leaf_server
            || ', which counts its own leaf nodes against the limit'
        ELSE used || ' of ' || limit_connections || ' client connections (' || round(100 * ratio, 1) || '%)'
    END
        || CASE WHEN used < limit_connections THEN '.'
                WHEN clients >= limit_connections THEN '; at the limit, every server refuses new client connections of the account.'
                ELSE '; at the limit, ' || CASE
                        WHEN len(r.servers) = 1 THEN r.servers[1]
                        WHEN len(r.servers) <= 3 THEN array_to_string(r.servers[1:-2], ', ') || ' and ' || r.servers[-1]
                        ELSE array_to_string(r.servers[1:3], ', ') || ' and ' || (len(r.servers) - 3)
                            || CASE WHEN len(r.servers) = 4 THEN ' more server' ELSE ' more servers' END
                    END
                    || CASE WHEN len(r.servers) = 1 THEN ' refuses' ELSE ' refuse' END
                    || ' new client connections of the account.' END
        AS detail,
    round(ratio, 4) AS value,
    json_object('conn', CAST(limit_connections AS VARCHAR)) AS related_config
FROM cur LEFT JOIN refusing r USING (account)
WHERE ratio >= getvariable('warn_ratio')
