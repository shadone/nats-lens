package checks

// Prelude defines, on the connection that runs the checks, the macros their
// queries may use:
//
//   - ns_text(ns) writes nanoseconds as 7d, 2h, 30s, 100ms or 1.5ms.
//   - restarting(lo, hi) is a table of the servers that answered in an
//     epoch from lo to hi and are restarting: their process started within
//     restart_window and replaced an earlier process of the same name.
//     A JetStream error in HEALTHZ or raft groups catching up do not make a
//     server restarting, since a server in trouble shows the same; for a
//     restarting server they are added to its reasons, for the detail
//     text. Columns: epoch, server_id, server_name, reasons (text list),
//     restarted (always true). Checks that judge the cluster may leave
//     these servers' own reports out.
//   - recovering(lo, hi) is a table of (epoch, server_id) for servers whose
//     HEALTHZ reports a JetStream error or whose raft groups catch up in
//     an epoch from lo to hi. Their reports are not hidden, but they do not
//     count as the healthy majority that lets a restarting server's report
//     be left out.
//   - planned_down(e) is a table of the servers missing in epoch e that
//     announced lame duck or shutdown after their last answer, within
//     restart_window: server_name, announced_at, kind.
//   - not_ready(e) is a table of the server names that may hold an asset
//     without reporting it in epoch e: missing, restarting, reporting
//     JetStream not ready, or answering with JetStream disabled or without
//     JSZ. Columns: server_name, why (a
//     phrase such as "did not answer"), planned (true when the server is
//     restarting or announced its shutdown), ready_again (true when the
//     only reason is a restart and the server answered JSZ in e: it is
//     back and reports what it holds).
//   - tooling_at(name, ip, e) is true for a tooling connection the client
//     checks leave out, judged as of epoch e: one of nats-lens's own
//     connection names, or a client connected from 127.0.0.1 or ::1 (a
//     port-forwarded CLI) while loopback counts as tooling in e
//     (checks.loopback_is_tooling and the guard's decision stored with e,
//     see LoopbackDecision; the epoch checked uses the decision being made,
//     an epoch without one counts as on);
//     always false with checks.include_tooling. The outer coalesce turns
//     the NULL of an unnamed client or a missing address into false, so
//     NOT tooling_at() keeps such rows (TestUnnamedClientsAreNotTooling).
//     tooling(name, ip) is tooling_at for the epoch checked, for events.
//   - gap_text(names, whys) writes " (may be on nats-0, which did not
//     answer)" for the not-ready servers that may hold a missing asset.
//   - account_lookups(e) is a table of the newest account_details row of
//     each account epoch e lists, at or before e and read at most 24 hours
//     before e started (api.AccountDetailsMaxAge, the Accounts page's
//     rule; an older lookup reads as not looked up yet), with title, the
//     account's readable name, else its identifier.
//   - expiry_text(t, now) writes "expires 2026-11-08 09:00 UTC, in 12
//     days" or "expired 2026-10-20 09:00 UTC, 1 day ago"; span_text(iv)
//     writes the span in minutes below an hour, in hours to a tenth below
//     a day and in days to a tenth, the unit chosen after rounding (23 h
//     58 m is 1 day), whole numbers without ".0" and 1 in the singular.
//   - account_servers(e) and account_connections(e) are the queries of
//     AccountServersSQL and AccountConnectionsSQL for epoch e: each
//     account's share on each server process, and its connections as
//     nats-server counts them against the limit.
var Prelude = "CREATE OR REPLACE TEMP MACRO account_servers(ep) AS TABLE " + AccountServersSQL("ep") + ";\n" +
	"CREATE OR REPLACE TEMP MACRO account_connections(ep) AS TABLE " + AccountConnectionsSQL("ep") + ";\n" + prelude

const prelude = `CREATE OR REPLACE TEMP MACRO account_lookups(ep) AS TABLE
SELECT d.*, coalesce(nullif(nullif(a.account_name, ''), a.account), nullif(nullif(d.account_name, ''), a.account), a.account) AS title
FROM account_details d JOIN accounts a ON a.account = d.account AND a.first_epoch <= ep AND a.last_epoch >= ep
WHERE d.epoch <= ep AND d.looked_up_at >= (SELECT started_at FROM epochs WHERE epoch = ep) - INTERVAL 24 HOUR
QUALIFY row_number() OVER (PARTITION BY d.account ORDER BY d.epoch DESC, d.looked_up_at DESC) = 1;
CREATE OR REPLACE TEMP MACRO unit_text(x, one, many) AS
    CASE WHEN x = trunc(x) THEN CAST(CAST(x AS BIGINT) AS VARCHAR) ELSE CAST(x AS VARCHAR) END
    || ' ' || CASE WHEN x = 1 THEN one ELSE many END;
CREATE OR REPLACE TEMP MACRO span_text(iv) AS CASE
    WHEN round(epoch(iv) / 60) < 1 THEN 'less than a minute'
    WHEN round(epoch(iv) / 60) < 60 THEN unit_text(round(epoch(iv) / 60), 'minute', 'minutes')
    WHEN round(epoch(iv) / 3600, 1) < 24 THEN unit_text(round(epoch(iv) / 3600, 1), 'hour', 'hours')
    ELSE unit_text(round(epoch(iv) / 86400, 1), 'day', 'days')
END;
CREATE OR REPLACE TEMP MACRO expiry_text(t, now) AS CASE
    WHEN t IS NULL THEN NULL
    WHEN t <= now THEN 'expired ' || strftime(t, '%Y-%m-%d %H:%M UTC') || ', ' || span_text(now - t) || ' ago'
    ELSE 'expires ' || strftime(t, '%Y-%m-%d %H:%M UTC') || ', in ' || span_text(t - now)
END;
CREATE OR REPLACE TEMP MACRO tooling_at(name, ip, e) AS
    coalesce(getvariable('exclude_tooling') AND (list_contains(getvariable('tooling_names'), name)
        OR (getvariable('loopback_setting')
            AND CASE WHEN e = getvariable('epoch') THEN getvariable('loopback_now')
                     ELSE coalesce((SELECT x.loopback_tooling FROM epochs x WHERE x.epoch = e), true) END
            AND ip IN ('127.0.0.1', '::1'))), false);
CREATE OR REPLACE TEMP MACRO tooling(name, ip) AS tooling_at(name, ip, getvariable('epoch'));
CREATE OR REPLACE TEMP MACRO ns_text(ns) AS CASE
    WHEN ns IS NULL THEN NULL
    WHEN CAST(ns AS HUGEINT) = 0 THEN '0'
    WHEN CAST(ns AS HUGEINT) % 86400000000000 = 0 THEN CAST(CAST(ns AS HUGEINT) // 86400000000000 AS VARCHAR) || 'd'
    WHEN CAST(ns AS HUGEINT) % 3600000000000 = 0 THEN CAST(CAST(ns AS HUGEINT) // 3600000000000 AS VARCHAR) || 'h'
    WHEN CAST(ns AS HUGEINT) % 60000000000 = 0 THEN CAST(CAST(ns AS HUGEINT) // 60000000000 AS VARCHAR) || 'm'
    WHEN CAST(ns AS HUGEINT) % 1000000000 = 0 THEN CAST(CAST(ns AS HUGEINT) // 1000000000 AS VARCHAR) || 's'
    WHEN CAST(ns AS HUGEINT) % 1000000 = 0 THEN CAST(CAST(ns AS HUGEINT) // 1000000 AS VARCHAR) || 'ms'
    ELSE CAST(round(ns / 1e6, 3) AS VARCHAR) || 'ms'
END;

CREATE OR REPLACE TEMP MACRO restarting(lo, hi) AS TABLE
WITH reasons AS (
    SELECT st.epoch, st.server_id, 'restarted ' || strftime(coalesce(st.start_time, s.start_time), '%H:%M:%S') AS reason, 1 AS rank
    FROM server_stats st JOIN servers s USING (server_id) JOIN epochs e USING (epoch)
    WHERE st.epoch BETWEEN lo AND hi
      AND coalesce(st.start_time, s.start_time) > e.started_at - getvariable('restart_window')
      AND EXISTS (SELECT 1 FROM servers p WHERE p.server_name = s.server_name AND p.first_epoch < s.first_epoch)
    UNION ALL
    SELECT h.epoch, h.server_id, 'JetStream not ready', 2
    FROM health h
    WHERE h.epoch BETWEEN lo AND hi AND CAST(h.errors AS VARCHAR) LIKE '%"JETSTREAM"%'
    UNION ALL
    SELECT DISTINCT r.epoch, r.server_id, 'catching up', 3
    FROM raft_stats r
    WHERE r.epoch BETWEEN lo AND hi AND r.catching_up
)
SELECT r.epoch, r.server_id, s.server_name, list(r.reason ORDER BY r.rank) AS reasons, bool_or(r.rank = 1) AS restarted
FROM reasons r JOIN servers s USING (server_id)
GROUP BY r.epoch, r.server_id, s.server_name
HAVING bool_or(r.rank = 1);

CREATE OR REPLACE TEMP MACRO recovering(lo, hi) AS TABLE
SELECT DISTINCT epoch, server_id FROM (
    SELECT h.epoch, h.server_id FROM health h
    WHERE h.epoch BETWEEN lo AND hi AND CAST(h.errors AS VARCHAR) LIKE '%"JETSTREAM"%'
    UNION ALL
    SELECT r.epoch, r.server_id FROM raft_stats r
    WHERE r.epoch BETWEEN lo AND hi AND r.catching_up
);

CREATE OR REPLACE TEMP MACRO planned_down(ep) AS TABLE
WITH missing AS (
    SELECT unnest(missing_names) AS server_name, started_at FROM epochs WHERE epoch = ep
),
-- The last epoch before ep in which the server answered, so that a later
-- process of the same name does not count when an older epoch is checked.
last_seen AS (
    SELECT m.server_name,
        (SELECT max(e.started_at) FROM epochs e WHERE e.epoch < ep AND list_contains(e.observed_names, m.server_name)) AS last_at
    FROM missing m
)
SELECT m.server_name, max(ev.time) AS announced_at, arg_max(ev.kind, ev.received_at) AS kind
FROM missing m
JOIN last_seen l USING (server_name)
JOIN events ev ON ev.server_name = m.server_name AND ev.kind IN ('server.lameduck', 'server.shutdown')
 AND ev.received_at >= l.last_at - INTERVAL 30 SECOND AND ev.received_at <= m.started_at
 AND ev.received_at > m.started_at - getvariable('restart_window')
GROUP BY m.server_name;

CREATE OR REPLACE TEMP MACRO not_ready(ep) AS TABLE
SELECT server_name, string_agg(why, ' and ' ORDER BY rank) AS why, bool_or(planned) AS planned,
    bool_and(ready_again) AS ready_again
FROM (
    SELECT unnest(missing_names) AS server_name, 'did not answer' AS why, false AS planned, 2 AS rank,
        false AS ready_again FROM epochs WHERE epoch = ep
    UNION ALL
    SELECT server_name, 'announced ' || replace(replace(kind, 'server.lameduck', 'lame duck'), 'server.shutdown', 'shutdown')
        || ' at ' || strftime(announced_at, '%H:%M:%S'), true, 1, false FROM planned_down(ep)
    UNION ALL
    SELECT r.server_name, 'is restarting (' || array_to_string(r.reasons, ', ') || ')', true, 1,
        len(r.reasons) = 1 AND EXISTS (SELECT 1 FROM epoch_endpoints e
            WHERE e.epoch = ep AND e.server_id = r.server_id AND e.endpoint = 'JSZ' AND e.state = 'ok')
    FROM restarting(ep, ep) r
    UNION ALL
    SELECT s.server_name, 'reports JetStream not ready', false, 3, false
    FROM health h JOIN servers s USING (server_id)
    WHERE h.epoch = ep AND CAST(h.errors AS VARCHAR) LIKE '%"JETSTREAM"%'
      AND h.server_id NOT IN (SELECT server_id FROM restarting(ep, ep))
    UNION ALL
    SELECT s.server_name, 'has JetStream disabled', false, 3, false
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = ep AND st.js_enabled = false
    UNION ALL
    SELECT s.server_name, CASE e.state WHEN 'missing' THEN 'did not answer JSZ' ELSE 'answered JSZ with an error' END,
        false, 4, false
    FROM epoch_endpoints e JOIN servers s USING (server_id)
    WHERE e.epoch = ep AND e.endpoint = 'JSZ' AND e.state <> 'ok'
)
GROUP BY server_name;

CREATE OR REPLACE TEMP MACRO gap_text(names, whys) AS CASE
    WHEN names IS NULL OR len(names) = 0 THEN ''
    WHEN len(names) = 1 THEN ' (may be on ' || names[1] || ', which ' || whys[1] || ')'
    ELSE ' (may be on ' || array_to_string(names, ', ') || ': '
        || array_to_string(list_transform(list_zip(names, whys), lambda x: x[1] || ' ' || x[2]), '; ') || ')'
END`
