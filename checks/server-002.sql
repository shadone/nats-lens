/*
code: server-002
name: Healthz not ok
entity: server
category: health
severity: critical
kind: operational
description: >-
  The server's HEALTHZ (with details) did not report ok. Errors that only concern JetStream readiness are left to
  server-003, so a starting server is reported once. A restarting server (see restart_window) reports its streams
  and consumers as not current while it catches up, and may fail without detail; those are left out too. A server
  in a crash loop restarts again before its errors stand out here: server-004 reports the repeated restarts.
remediation: >-
  Read the errors in the detail: they name the account, stream or consumer the server cannot serve. A stream or
  consumer error right after a restart usually clears once the server caught up (watch server-012). If it stays,
  compare the asset on every server (`nats stream info <stream> --json` against each server) and look for
  stream-010 or consumer-012. Persistent errors on one server only point at its disk: check the PVC usage and the
  server log for write errors.
value_name: healthz errors
value_basis: errors in the server's HEALTHZ details
*/
WITH restarting AS (
    SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch'))
),
h AS (
    SELECT h.*, s.server_name, h.server_id IN (SELECT server_id FROM restarting) AS is_restarting,
        coalesce((SELECT list(DISTINCT x ->> 'type') FROM unnest(CAST(h.errors AS JSON[])) AS u(x)), []) AS types
    FROM health h JOIN servers s USING (server_id)
    WHERE h.epoch = getvariable('epoch') AND coalesce(h.status, '') <> 'ok'
)
SELECT
    server_name AS entity_key,
    server_name || ' healthz ' || coalesce(status, 'unknown') || coalesce(' (' || status_code || ')', '')
        || coalesce(': ' || nullif(error, ''), '')
        || CASE WHEN error_count > 1 THEN ' (+' || (error_count - 1) || ' more)' ELSE '' END || '.' AS detail,
    error_count AS value
FROM h
WHERE CASE WHEN is_restarting
    THEN list_filter(types, lambda t: t NOT IN ('JETSTREAM', 'STREAM', 'CONSUMER')) <> []
    ELSE len(types) = 0 OR list_filter(types, lambda t: t <> 'JETSTREAM') <> [] END
