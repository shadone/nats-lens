/*
code: client-006
name: Repeated authentication errors
entity: client
category: clients
severity: warning
kind: operational
lookback: 15m
thresholds:
  - name: min_failed_logins
    default: "15"
    unit: count
    description: >-
      Failed logins of one user in one account within the lookback at which the finding fires; 15 in 15 minutes is the rate of 60 an
      hour at which the Accounts page marks a group.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the failed logins stay at or above this share of min_failed_logins, and
      resolves once they fall below it, so a client that retries at about the threshold's rate does not flap.
description: >-
  The servers refused the login of this user in this account min_failed_logins times or more within the lookback,
  from the client.auth_error events: a client keeps retrying with credentials that are wrong, revoked or expired,
  so it never connects. Grouped by the user the client gave and the account the server names, from any address and
  server, so the pods of one deployment sharing a credential are one finding; the Accounts page lists the same
  events by user, address and reason. A client that gave no user is grouped as (no user), one that gave a token
  (which nats-server reports as [REDACTED]) as (token); nkey and JWT users appear as their public keys. An event
  without an account, or with $G or N/A, which nats-server's server-wide event names for a client refused before it
  got an account, is keyed (unknown account); only the account-scoped event (from auth callout) names the account
  the client tried. The check needs event capture: without it nats-lens sees no failed login and the
  check stays quiet. A firing finding stays until the count falls below resolve_ratio of min_failed_logins.
remediation: >-
  nats-server gives every refused login the reason "Authentication Failure" ("Proxy Not Trusted" or "Proxy
  Required" when proxy authentication is configured), so the detail cannot tell wrong credentials from revoked or
  expired ones. Find the client from the addresses and client names in the detail and give it valid credentials,
  or stop it if it should no longer run. `nats server request connections --filter-state closed --filter-user
  <user>` lists the refused connections the servers still keep, with their address, name and library; in operator
  mode, when the finding names an account, `nats auth user info <user> <account>` shows whether the user's JWT is
  revoked or expired; with an unknown account, look the user up in each account it may belong to. Failures without a user from addresses you do not know are often a port scanner, or a
  load balancer or HTTP health check that probes the client port: point the probe at the monitoring port's
  /healthz instead. The Accounts page lists the failed logins of the last 24 hours by user, address and reason.
related_config: [min_failed_logins, resolve_ratio, lookback]
value_name: failed logins
value_basis: client.auth_error events of the user in the account within the lookback, on every server
*/
WITH e AS (
    SELECT id, time, received_at,
        CASE WHEN client_user = '[REDACTED]' THEN '(token)' ELSE coalesce(nullif(client_user, ''), '(no user)') END AS u,
        CASE WHEN coalesce(account, '') IN ('', '$G', 'N/A') THEN '(unknown account)' ELSE account END AS acc, nullif(client_host, '') AS host,
        nullif(client_name, '') AS name, coalesce(nullif(reason, ''), 'no reason') AS reason, nullif(server_name, '') AS server
    FROM events
    WHERE kind = 'client.auth_error' AND received_at > getvariable('window_start') AND received_at <= getvariable('epoch_time')
      AND NOT tooling(client_name, client_host)
),
-- Users and accounts whose finding fired at the previous epoch keep firing down to
-- resolve_ratio of the threshold.
firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'client-006' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
g AS (
    SELECT u || ' in ' || acc AS k, u, acc, count(*) AS n,
        list(DISTINCT host ORDER BY host) FILTER (WHERE host IS NOT NULL) AS hosts,
        list(DISTINCT name ORDER BY name) FILTER (WHERE name IS NOT NULL) AS names,
        list(DISTINCT server ORDER BY server) FILTER (WHERE server IS NOT NULL) AS servers,
        list(DISTINCT reason ORDER BY reason) AS reasons,
        max(time) AS last_at, arg_max(id, (received_at, id)) AS last_id
    FROM e
    GROUP BY u, acc
)
SELECT
    k AS entity_key,
    n || CASE WHEN n = 1 THEN ' failed login' ELSE ' failed logins' END || ' in the last ' || getvariable('lookback_text')
        || CASE WHEN round(n * 3600.0 / epoch(getvariable('lookback'))) < 1 THEN ' (less than 1 an hour)'
            ELSE ' (about ' || round(n * 3600.0 / epoch(getvariable('lookback')))::BIGINT || ' an hour)' END
        || CASE u WHEN '(no user)' THEN ' without a user' WHEN '(token)' THEN ' with a token' ELSE ' as ' || u END
        || CASE WHEN acc = '(unknown account)' THEN ' in an unknown account' ELSE ' in account ' || acc END
        || ' from '
        || CASE
            WHEN coalesce(len(hosts), 0) = 0 THEN 'an unknown address'
            WHEN len(hosts) > 5 THEN array_to_string(hosts[1:5], ', ') || ' and ' || (len(hosts) - 5) || ' more addresses'
            ELSE array_to_string(hosts, ', ')
        END
        || CASE WHEN coalesce(len(servers), 0) = 0 THEN '' ELSE ' (' || array_to_string(servers, ', ') || ')' END
        || ': ' || array_to_string(reasons, ', ')
        || '; the last at ' || strftime(last_at, '%H:%M:%S') || ' UTC'
        || CASE
            WHEN coalesce(len(names), 0) = 0 THEN ''
            WHEN len(names) > 5 THEN '. Client names: ' || array_to_string(names[1:5], ', ') || ' and ' || (len(names) - 5) || ' more'
            ELSE '. Client ' || CASE WHEN len(names) = 1 THEN 'name: ' ELSE 'names: ' END || array_to_string(names, ', ')
        END || '.' AS detail,
    n AS value,
    last_id AS cause_event_id
FROM g
WHERE n >= getvariable('min_failed_logins')
   OR (n >= getvariable('resolve_ratio') * getvariable('min_failed_logins') AND k IN (SELECT entity_key FROM firing))
