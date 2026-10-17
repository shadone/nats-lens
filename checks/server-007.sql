/*
code: server-007
name: JetStream store usage high
entity: server
category: storage
severity: warning
severity_rule: critical at or above crit_ratio of the limit
kind: operational
thresholds:
  - name: warn_ratio
    default: "0.75"
    unit: ratio
    description: Share of max_file_store or max_memory_store in use at which the finding fires.
  - name: crit_ratio
    default: "0.9"
    unit: ratio
    description: Share at which it is critical.
description: >-
  The server's JetStream file store or memory store holds more than warn_ratio of its configured limit
  (max_file_store, max_memory_store). At the limit the server refuses new messages for streams placed on it.
remediation: >-
  Find the streams that hold the most on the server (Streams page, sorted by bytes) and check their limits:
  an archive stream without max_bytes or max_age (stream-005) grows until the store is full. Then either set limits,
  or grow the store: raise max_file_store in the NATS configuration together with the PVC size (resize the
  PersistentVolumeClaim; GKE expands a pd-balanced volume online). The disk must stay larger than max_file_store.
related_config: [max_file_store, max_memory_store]
value_name: store usage share
value_basis: JetStream storage in use divided by its limit
*/
WITH cur AS (
    SELECT s.server_name, st.js_storage, st.js_max_storage, st.js_memory, st.js_max_memory,
        CASE WHEN st.js_max_storage > 0 THEN st.js_storage / st.js_max_storage END AS file_ratio,
        CASE WHEN st.js_max_memory > 0 THEN st.js_memory / st.js_max_memory END AS mem_ratio
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch')
),
worst AS (
    SELECT *, greatest(coalesce(file_ratio, 0), coalesce(mem_ratio, 0)) AS ratio FROM cur
)
SELECT
    server_name AS entity_key,
    CASE WHEN ratio >= getvariable('crit_ratio') THEN 'critical' ELSE 'warning' END AS severity,
    server_name || ' file store ' || coalesce(format_bytes(js_storage), '?') || ' of ' || coalesce(format_bytes(js_max_storage), '?')
        || coalesce(' (' || round(100 * file_ratio, 1) || '%)', '')
        || ', memory store ' || coalesce(format_bytes(js_memory), '?') || ' of ' || coalesce(format_bytes(js_max_memory), '?')
        || coalesce(' (' || round(100 * mem_ratio, 1) || '%)', '') || '.' AS detail,
    round(ratio, 4) AS value,
    json_object('max_file_store', format_bytes(js_max_storage), 'max_memory_store', format_bytes(js_max_memory)) AS related_config,
    server_name AS related_source
FROM worst
WHERE ratio >= getvariable('warn_ratio')
