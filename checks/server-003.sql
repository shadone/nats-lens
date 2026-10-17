/*
code: server-003
name: JetStream not ready
entity: server
category: health
severity: critical
severity_rule: >-
  warning while the server restarted within restart_window, since a restarted server needs a while to recover;
  JetStream disabled on a server configured for it is critical at once
kind: operational
thresholds:
  - name: min_duration
    default: 3m
    unit: duration
    description: How long HEALTHZ must report JetStream not ready, in consecutive collections, before the finding fires.
description: >-
  HEALTHZ on the server has reported a JetStream error (meta layer still recovering, not current with the meta
  leader) in every collection for longer than min_duration, or the server has JetStream configured and JSZ reports it
  disabled. A crash loop whose restarts come faster than min_duration never stays not ready that long in one
  process; server-004 reports it as repeated restarts, critical above max_restarts.
remediation: >-
  A server that just started recovers its JetStream state from disk and joins the meta group; this normally clears
  within a minute or two, which min_duration allows for. If it stays, read the server log for the recovery error.
  Common causes on GKE: the PVC is full (see server-007), or the pod lost its volume and started empty, in which case
  the meta leader resends the assignments and the server catches up (server-012).
related_config: [min_duration]
value_name: minutes not ready
value_basis: time since the first collection of the run in which HEALTHZ reported a JetStream error
*/
WITH ids AS (
    SELECT server_id FROM server_stats WHERE epoch = getvariable('epoch')
    UNION SELECT server_id FROM health WHERE epoch = getvariable('epoch')
),
cur AS (
    SELECT i.server_id, s.server_name, s.jetstream AS configured, st.js_enabled
    FROM ids i JOIN servers s USING (server_id)
    LEFT JOIN server_stats st ON st.epoch = getvariable('epoch') AND st.server_id = i.server_id
),
js_errors AS (
    SELECT h.server_id,
        (SELECT list(x ->> 'error') FROM unnest(CAST(h.errors AS JSON[])) AS u(x) WHERE x ->> 'type' = 'JETSTREAM') AS errs
    FROM health h
    WHERE h.epoch = getvariable('epoch') AND h.errors IS NOT NULL
),
-- The run of epochs with a JetStream error ends at the latest epoch of this
-- process without one.
last_ready AS (
    SELECT h.server_id, max(h.epoch) AS epoch
    FROM health h JOIN js_errors j USING (server_id)
    WHERE h.epoch < getvariable('epoch') AND NOT coalesce(CAST(h.errors AS VARCHAR) LIKE '%"JETSTREAM"%', false)
    GROUP BY h.server_id
),
since AS (
    SELECT j.server_id, j.errs,
        (SELECT min(e.started_at) FROM health h JOIN epochs e USING (epoch)
         WHERE h.server_id = j.server_id AND h.epoch > coalesce(l.epoch, 0) AND h.epoch <= getvariable('epoch')) AS since
    FROM js_errors j LEFT JOIN last_ready l USING (server_id)
    WHERE len(j.errs) > 0
),
restarted AS (
    SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')) WHERE restarted
)
SELECT
    c.server_name AS entity_key,
    CASE WHEN n.server_id IS NULL THEN 'critical'
        WHEN c.server_id IN (SELECT server_id FROM restarted) THEN 'warning' ELSE 'critical' END AS severity,
    c.server_name || CASE
        WHEN n.server_id IS NOT NULL THEN ' healthz reports JetStream not ready for '
            || CAST(date_trunc('second', getvariable('epoch_time') - n.since) AS VARCHAR) || ': ' || array_to_string(n.errs, '; ')
        ELSE ' has JetStream configured but JSZ reports it disabled'
    END || '.' AS detail,
    round(epoch(getvariable('epoch_time') - coalesce(n.since, getvariable('epoch_time'))) / 60, 1) AS value
FROM cur c LEFT JOIN since n USING (server_id)
WHERE (n.server_id IS NOT NULL AND getvariable('epoch_time') - n.since > getvariable('min_duration'))
   OR (c.configured AND c.js_enabled = false)
