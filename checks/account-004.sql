/*
code: account-004
name: User JWTs expire soon
entity: account
category: clients
severity: warning
severity_rule: critical once a user JWT expired, or when one expires within crit_within
kind: operational
thresholds:
  - name: warn_within
    default: 14d
    unit: duration
    description: How soon after the collection a user JWT's expiry fires the finding.
  - name: crit_within
    default: 3d
    unit: duration
    description: How soon an expiry is critical.
description: >-
  Connections of the account in the collection carry a user JWT that expires within warn_within, or has expired. One
  finding per account; the detail names each such user with the soonest expiry among its connections and how many
  connections hold that expiry, soonest first: the first five users, and a count of the rest. When a user JWT
  expires the server closes the connections that use it, and the client cannot connect again with the same
  credentials. Only the connections the servers list in CONNZ
  are read (up to `connz_limit` per server), as on the Accounts page; nats-lens's own connections and port-forwarded
  tools count too, since their credentials expire as well.
remediation: >-
  Issue the user new credentials with a later expiry (`nsc edit user <name> --expiry <date>`, then
  `nsc generate creds`) and roll them out to the service before the old ones expire: update the secret that holds the
  creds file and restart the pods that read it. A client reads its creds file when it connects, so a reconnect after
  the update is enough for most client libraries.
related_config: [exp]
value_name: days until a user JWT expires
value_basis: from the collection's start to the soonest user JWT expiry among the account's connections, negative once expired
*/
WITH c AS (
    SELECT account, coalesce(nullif(authorized_user, ''), '(no user)') AS u, jwt_expires
    FROM connections
    WHERE epoch = getvariable('epoch') AND user_jwt AND coalesce(account, '') <> ''
      AND jwt_expires <= getvariable('epoch_time') + getvariable('warn_within')
),
users AS (
    SELECT account, u, min(jwt_expires) AS soonest, count(*) FILTER (WHERE jwt_expires = soonest_all) AS conns
    FROM (SELECT *, min(jwt_expires) OVER (PARTITION BY account, u) AS soonest_all FROM c)
    GROUP BY account, u
),
per_account AS (
    SELECT account, min(soonest) AS soonest, count(*) AS n,
        list('user ' || u || ' ' || expiry_text(soonest, getvariable('epoch_time')) || ' (' || conns
            || CASE WHEN conns = 1 THEN ' connection' ELSE ' connections' END || ')' ORDER BY soonest, u) AS users
    FROM users
    GROUP BY account
)
SELECT
    p.account AS entity_key,
    CASE WHEN p.soonest <= getvariable('epoch_time') + getvariable('crit_within') THEN 'critical' ELSE 'warning' END
        AS severity,
    coalesce(l.title, nullif(nullif(a.account_name, ''), p.account), p.account) || ': ' || array_to_string(p.users[1:5], '; ')
        || CASE WHEN p.n > 5 THEN '; and ' || (p.n - 5) || ' more users' ELSE '' END || '.' AS detail,
    round(epoch(p.soonest - getvariable('epoch_time')) / 86400, 2) AS value
FROM per_account p
LEFT JOIN accounts a ON a.account = p.account
LEFT JOIN account_lookups(getvariable('epoch')) l ON l.account = p.account
