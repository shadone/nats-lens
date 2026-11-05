/*
code: client-008
name: Outdated client library
entity: client
category: clients
severity: info
kind: operational
lookback: 1h
thresholds:
  - name: min_go
    default: "1.37.0"
    unit: version
    description: Oldest nats.go version (language go) that does not count as outdated; 0 judges no go client.
  - name: min_nats_js
    default: "2.28.0"
    unit: version
    description: Oldest nats.js version (language nats.js, Node.js) that does not count as outdated; 0 judges none.
  - name: min_python
    default: "2.9.0"
    unit: version
    description: Oldest nats.py version (language python3) that does not count as outdated; 0 judges none.
  - name: min_java
    default: "2.20.0"
    unit: version
    description: Oldest jnats version (language java) that does not count as outdated; 0 judges none.
  - name: min_dotnet
    default: "2.5.0"
    unit: version
    description: Oldest NATS.Net version (language .NET) that does not count as outdated; 0 judges none.
  - name: min_rust
    default: "0.37.0"
    unit: version
    description: Oldest async-nats version (language rust) that does not count as outdated; 0 judges none.
  - name: min_c
    default: "3.9.0"
    unit: version
    description: Oldest nats.c version (language C) that does not count as outdated; 0 judges none.
description: >-
  Connections of this application used a client library older than the minimum set for its language within the
  lookback. The language and version are what the client sends in its CONNECT and the servers report in CONNZ;
  the version is compared by its first three numbers, so 1.37.0-beta.1 counts as 1.37.0. Languages without a
  minimum (nats.deno, nats.ws, the older NATS.Client for .NET, which sends C#, and others) and connections without
  a version are not judged. The defaults are the release lines that were current two years before they were set,
  in November 2026. Grouped by application, the connection name before the first "@", as the Applications page
  groups it; a client without a name is (unnamed). The detail describes the newest collection within the lookback
  that had such a connection, so a finding resolves a lookback after the last old connection is gone, and a job
  that connects now and then does not flap.
remediation: >-
  An old client library misses fixes in reconnect handling, JetStream and security that the servers no longer
  work around. Upgrade the library in the application named by the finding and deploy it; the Applications page
  shows each application's libraries and versions, and `nats server request connections --filter-account
  <account>` lists each connection's library. Set a language's minimum to 0 under
  `checks.thresholds.client-008` to stop judging it, or raise it to follow your own upgrade policy.
related_config: [min_go, min_nats_js, min_python, min_java, min_dotnet, min_rust, min_c, lookback]
value_name: connections with an outdated library
value_basis: >-
  connections of the application in the newest collection within the lookback that had one, whose library is
  older than its language's minimum
*/
WITH c AS (
    SELECT c.epoch, s.server_name, coalesce(nullif(split_part(c.name, '@', 1), ''), '(unnamed)') AS app,
        c.lang, c.version
    FROM connections c JOIN servers s USING (server_id)
    WHERE c.epoch >= getvariable('window_epoch') AND c.epoch <= getvariable('epoch')
      AND NOT tooling_at(c.name, c.ip, c.epoch)
),
-- Each language and version once: its minimum, and both as lists of three
-- numbers, which compare element by element.
libs AS (
    SELECT lang, version, minimum,
        list_resize(list_transform(string_split(regexp_extract(version, '^v?([0-9]{1,9}(\.[0-9]{1,9}){0,2})', 1), '.'),
            x -> CAST(x AS BIGINT)), 3, 0) AS have,
        list_resize(list_transform(string_split(minimum, '.'), x -> CAST(x AS BIGINT)), 3, 0) AS need
    FROM (
        SELECT DISTINCT lang, version,
            CASE lang
                WHEN 'go' THEN getvariable('min_go')
                WHEN 'nats.js' THEN getvariable('min_nats_js')
                WHEN 'python3' THEN getvariable('min_python')
                WHEN 'java' THEN getvariable('min_java')
                WHEN '.NET' THEN getvariable('min_dotnet')
                WHEN 'rust' THEN getvariable('min_rust')
                WHEN 'C' THEN getvariable('min_c')
            END AS minimum
        FROM c
        WHERE regexp_matches(coalesce(version, ''), '^v?[0-9]')
    )
    WHERE minimum IS NOT NULL
),
old AS (
    SELECT c.*, l.minimum FROM c JOIN libs l ON l.lang = c.lang AND l.version = c.version
    WHERE l.have < l.need
),
latest AS (
    SELECT app, max(epoch) AS epoch FROM old GROUP BY app
),
agg AS (
    SELECT l.app, l.epoch, count(*) AS n,
        list(DISTINCT o.lang || ' ' || o.version || ' (minimum ' || o.minimum || ')' ORDER BY o.lang || ' ' || o.version || ' (minimum ' || o.minimum || ')') AS libs,
        list(DISTINCT o.server_name ORDER BY o.server_name) AS servers
    FROM latest l JOIN old o ON o.app = l.app AND o.epoch = l.epoch
    GROUP BY l.app, l.epoch
),
totals AS (
    SELECT l.app, count(*) AS total
    FROM latest l JOIN c ON c.app = l.app AND c.epoch = l.epoch
    GROUP BY l.app
)
SELECT
    a.app AS entity_key,
    CASE WHEN a.epoch < getvariable('epoch')
        THEN 'In the collection of ' || strftime(e.started_at, '%H:%M:%S') || ' UTC, ' ELSE '' END
        || a.n || ' of ' || t.total || CASE WHEN t.total = 1 THEN ' connection' ELSE ' connections' END || CASE WHEN a.n = 1 THEN ' uses' ELSE ' use' END
        || ' a client library older than its minimum: ' || array_to_string(a.libs[1:5], ', ')
        || CASE WHEN len(a.libs) > 5 THEN ' and ' || (len(a.libs) - 5) || ' more' ELSE '' END
        || ' (' || array_to_string(a.servers, ', ') || ').' AS detail,
    a.n AS value
FROM agg a JOIN totals t USING (app) JOIN epochs e ON e.epoch = a.epoch
