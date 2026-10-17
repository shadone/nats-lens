/*
code: server-006
name: Configuration reloaded
entity: server
category: change
severity: info
kind: operational
lookback: 1h
description: >-
  The server reloaded its configuration without restarting (config_load_time is later than the process start)
  within the lookback.
remediation: >-
  Confirm the reload was intended: a ConfigMap change picked up by the config reloader sidecar, or `nats-server
  --signal reload`. If only some servers reloaded, their configuration now differs; compare config_digest on the
  servers page.
related_config: [lookback]
*/
SELECT
    s.server_name AS entity_key,
    s.server_name || ' reloaded its configuration at ' || strftime(st.config_load_time, '%H:%M:%S')
        || ' UTC (process started ' || strftime(st.start_time, '%Y-%m-%d %H:%M:%S') || ' UTC)'
        || coalesce('; config digest ' || st.config_digest, '') || '.' AS detail
FROM server_stats st JOIN servers s USING (server_id)
WHERE st.epoch = getvariable('epoch')
  AND st.config_load_time IS NOT NULL AND st.start_time IS NOT NULL
  AND st.config_load_time > st.start_time + INTERVAL 2 SECOND
  AND st.config_load_time >= getvariable('window_start')
