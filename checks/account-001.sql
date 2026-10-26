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
  The account's client connections, summed over the servers that answered in the collection, are at warn_ratio or
  more of the connection limit in its account JWT. At the limit the servers refuse every new connection of the
  account with "maximum account active connections exceeded", so a service that restarts or scales out cannot
  connect. Read from the account's newest lookup within the last 24 hours, as on the Accounts page; an account
  without a JWT (config mode), without a limit (-1) or not looked up within 24 hours is not reported. Leaf node
  connections do not count against this limit.
remediation: >-
  Find which clients hold the connections: the Connections page filtered to the account, grouped by name. A client
  that opens a connection per request or leaks connections fills the limit by itself; fix it first. If the load is
  real, raise the limit in the account JWT (`nsc edit account <name> --conns <n>`, then push the account JWT to the
  resolver with `nsc push`).
related_config: [conn]
value_name: connection limit share
value_basis: client connections summed over the servers of the collection, divided by the account JWT's connection limit
*/
WITH st AS (
    SELECT account, sum(conns)::BIGINT AS conns
    FROM account_stats
    WHERE epoch = getvariable('epoch') AND conns IS NOT NULL
    GROUP BY account
),
cur AS (
    SELECT d.account, d.title, d.limit_connections, st.conns, st.conns / d.limit_connections AS ratio
    FROM account_lookups(getvariable('epoch')) d JOIN st USING (account)
    WHERE d.has_jwt AND d.limit_connections > 0
)
SELECT
    account AS entity_key,
    CASE WHEN ratio >= getvariable('crit_ratio') THEN 'critical' ELSE 'warning' END AS severity,
    title || ': ' || conns || ' of ' || limit_connections || ' client connections (' || round(100 * ratio, 1) || '%)'
        || CASE WHEN conns >= limit_connections THEN '; at the limit, new connections are refused.' ELSE '.' END AS detail,
    round(ratio, 4) AS value,
    json_object('conn', CAST(limit_connections AS VARCHAR)) AS related_config
FROM cur
WHERE ratio >= getvariable('warn_ratio')
