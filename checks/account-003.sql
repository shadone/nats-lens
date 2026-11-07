/*
code: account-003
name: Account JWT expires soon
entity: account
category: expiry
severity: warning
severity_rule: critical once expired, or when it expires within crit_within
kind: operational
thresholds:
  - name: warn_within
    default: 14d
    unit: duration
    description: How soon after the collection an expiry fires the finding.
  - name: crit_within
    default: 3d
    unit: duration
    description: How soon an expiry is critical.
description: >-
  The account JWT expires within warn_within of the collection, has expired, or the server reports it expired. Once
  it expires the servers stop accepting the account's connections, and its users cannot connect. Read from the
  account's newest lookup within the last 24 hours, as on the Accounts page; an account without a JWT (config mode),
  with a JWT that never expires, or not looked up within 24 hours is not reported.
remediation: >-
  Reissue the account JWT with a later expiry, or none (`nsc edit account <name> --expiry <date>`), and push it to the
  resolver (`nsc push`); the servers pick the new JWT up without a restart. If the expiry is deliberate, as for a
  temporary account, mute this finding with the reason until the account is removed.
related_config: [exp]
value_name: days until the account JWT expires
value_basis: from the collection's start to the JWT's expiry, negative once expired; empty when only the server's expired flag says so
*/
SELECT
    account AS entity_key,
    CASE WHEN expired OR jwt_expires <= getvariable('epoch_time') + getvariable('crit_within') THEN 'critical'
         ELSE 'warning' END AS severity,
    title || ': account JWT ' || CASE
        WHEN jwt_expires IS NULL THEN 'expired, as the server reports it'
        ELSE expiry_text(jwt_expires, getvariable('epoch_time'))
            || CASE WHEN expired AND jwt_expires > getvariable('epoch_time') THEN '; the server reports it expired' ELSE '' END
    END || '.' AS detail,
    round(epoch(jwt_expires - getvariable('epoch_time')) / 86400, 2) AS value,
    CASE WHEN jwt_expires IS NOT NULL THEN json_object('exp', strftime(jwt_expires, '%Y-%m-%d %H:%M:%S UTC')) END
        AS related_config
FROM account_lookups(getvariable('epoch'))
WHERE has_jwt AND (expired OR jwt_expires <= getvariable('epoch_time') + getvariable('warn_within'))
