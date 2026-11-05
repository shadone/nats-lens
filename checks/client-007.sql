/*
code: client-007
name: Unnamed connections
entity: client
category: clients
severity: info
kind: operational
thresholds:
  - name: min_age
    default: 10m
    unit: duration
    description: >-
      How long a connection without a name must have been connected before the finding fires, so a short script or
      a one-off CLI call does not.
description: >-
  Connections without a name (or with nothing before the "@", the Applications page's (unnamed)) have been
  connected for min_age or longer. The client checks, the Applications page and the Events page group clients by
  their connection name, so unnamed clients are merged into one (unnamed) entry and a finding cannot say which
  service it is about. One finding for every such connection, keyed (unnamed); the detail names their accounts,
  users, addresses, client libraries and servers, which help find them. A firing finding stays while any unnamed
  connection is connected, of any age, so a client that reconnects does not resolve and fire again.
remediation: >-
  Give every client a connection name, ideally `service@pod` (in Kubernetes, the pod name from the downward API):
  the client checks then report per service, and a mute covers its pods. In nats.go set `nats.Name(...)`, in
  nats.js the `name` connection option, in nats.py `name=`, in nats.java `Options.Builder.connectionName(...)`, in
  NATS.Net `NatsOpts.Name`. `nats server request connections --filter-account <account>` lists the
  connections with their user, address and library to find the client.
related_config: [min_age]
value_name: connections without a name
value_basis: client connections in the collection without a name, of any age
*/
WITH c AS (
    SELECT s.server_name, coalesce(nullif(c.account, ''), '$G') AS account, nullif(c.authorized_user, '') AS u,
        nullif(c.ip, '') AS ip, nullif(trim(coalesce(c.lang, '') || ' ' || coalesce(c.version, '')), '') AS lib,
        c.start_time
    FROM connections c JOIN servers s USING (server_id)
    WHERE c.epoch = getvariable('epoch') AND coalesce(split_part(c.name, '@', 1), '') = ''
      AND NOT tooling_at(c.name, c.ip, c.epoch)
),
-- Once firing at the previous epoch, any unnamed connection keeps it.
firing AS (
    SELECT 1 FROM findings
    WHERE check_code = 'client-007' AND entity_key = '(unnamed)' AND state = 'firing'
      AND last_seen_epoch = getvariable('prev_epoch')
),
agg AS (
    SELECT count(*) AS total,
        count(*) FILTER (WHERE start_time <= getvariable('epoch_time') - getvariable('min_age')) AS aged,
        list(DISTINCT account ORDER BY account) AS accounts,
        list(DISTINCT u ORDER BY u) FILTER (WHERE u IS NOT NULL) AS users,
        list(DISTINCT ip ORDER BY ip) FILTER (WHERE ip IS NOT NULL) AS ips,
        list(DISTINCT lib ORDER BY lib) FILTER (WHERE lib IS NOT NULL) AS libs,
        list(DISTINCT server_name ORDER BY server_name) AS servers
    FROM c
)
SELECT
    '(unnamed)' AS entity_key,
    total || CASE WHEN total = 1 THEN ' connection' ELSE ' connections' END || ' without a name, '
        || aged || ' connected for ' || ns_text(epoch(getvariable('min_age')) * 1e9) || ' or longer; '
        || CASE WHEN len(accounts) = 1 THEN 'account ' ELSE 'accounts ' END
        || array_to_string(accounts[1:5], ', ') || CASE WHEN len(accounts) > 5 THEN ' and ' || (len(accounts) - 5) || ' more' ELSE '' END
        || CASE WHEN coalesce(len(users), 0) = 0 THEN ''
            ELSE CASE WHEN len(users) = 1 THEN ', user ' ELSE ', users ' END || array_to_string(users[1:5], ', ')
                || CASE WHEN len(users) > 5 THEN ' and ' || (len(users) - 5) || ' more' ELSE '' END END
        || CASE WHEN coalesce(len(ips), 0) = 0 THEN ''
            ELSE ', from ' || array_to_string(ips[1:5], ', ')
                || CASE WHEN len(ips) > 5 THEN ' and ' || (len(ips) - 5) || ' more addresses' ELSE '' END END
        || CASE WHEN coalesce(len(libs), 0) = 0 THEN ''
            ELSE ', ' || array_to_string(libs[1:5], ', ') || CASE WHEN len(libs) > 5 THEN ' and ' || (len(libs) - 5) || ' more' ELSE '' END END
        || ' (' || array_to_string(servers, ', ') || ').' AS detail,
    total AS value
FROM agg
WHERE aged > 0 OR (total > 0 AND EXISTS (SELECT 1 FROM firing))
