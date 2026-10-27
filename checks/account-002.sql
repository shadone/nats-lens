/*
code: account-002
name: Account near its JetStream disk limit
entity: account
category: storage
severity: warning
severity_rule: critical at or above crit_ratio of the limit; at the limit the servers refuse the account's new messages on file streams
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.9"
    unit: ratio
    description: Share of the account JWT's JetStream disk limit in use at which the finding fires.
  - name: crit_ratio
    default: "1"
    unit: ratio
    description: Share at which it is critical.
description: >-
  The account's JetStream file storage is at warn_ratio or more of the disk limit in its account JWT. The use is the
  largest any server reports in the collection (each reports the account's total over all replicas), and the limit is
  the one nats-server enforces, comparable with it (tiered limits times their replicas, summed). At the limit the
  servers refuse new messages to the account's file streams and new streams. Read from the account's newest lookup
  within the last 24 hours, as on the Accounts page; an account without a JWT (config mode), without a limit (-1),
  with JetStream disk off (0) or not looked up within 24 hours is not reported.
remediation: >-
  Find the account's largest streams (Streams page, filtered to the account, sorted by bytes) and check their limits:
  a stream without max_bytes or max_age (stream-005) grows until the account's limit stops it. Then set stream limits,
  or raise the account's limit (`nsc edit account <name> --js-disk-storage <size>`, or the tier's limit with
  `--js-tier <n>`, then `nsc push`). The servers' stores must have room for the raised limit (server-007).
related_config: [disk_storage]
value_name: disk limit share
value_basis: >-
  the largest JetStream file storage any server reports for the account, divided by the account JWT's disk limit
*/
WITH st AS (
    SELECT account, max(js_storage) AS used
    FROM account_stats
    WHERE epoch = getvariable('epoch') AND js_storage IS NOT NULL
    GROUP BY account
),
cur AS (
    SELECT d.account, d.title, d.limit_js_disk, st.used, st.used / d.limit_js_disk AS ratio
    FROM account_lookups(getvariable('epoch')) d JOIN st USING (account)
    WHERE d.has_jwt AND d.limit_js_disk > 0
)
SELECT
    account AS entity_key,
    CASE WHEN ratio >= getvariable('crit_ratio') THEN 'critical' ELSE 'warning' END AS severity,
    title || ': JetStream file storage ' || format_bytes(used) || ' of ' || format_bytes(limit_js_disk) || ' ('
        || round(100 * ratio, 1) || '%)'
        || CASE WHEN used >= limit_js_disk THEN '; at the limit, new messages to file streams are refused.' ELSE '.' END
        AS detail,
    round(ratio, 4) AS value,
    json_object('disk_storage', format_bytes(limit_js_disk)) AS related_config
FROM cur
WHERE ratio >= getvariable('warn_ratio')
