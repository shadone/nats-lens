/*
code: server-008
name: Memory far above the other servers
entity: server
category: storage
severity: warning
kind: operational
thresholds:
  - name: multiplier
    default: "1.5"
    unit: count
    description: Resident memory above this many times the median of the other servers fires.
  - name: min_bytes
    default: 512MiB
    unit: bytes
    description: Resident memory below this never fires.
description: >-
  The server's resident memory is well above the median of the other servers. nats-server does not report its
  memory limit, so the check compares servers with each other instead of with the limit; in a cluster where every
  server holds a replica of every stream, the servers should use similar memory.
remediation: >-
  Compare the server's connections, subscriptions and leader counts with the others (Servers page): a server that
  leads most streams or holds most client connections uses more memory. If it does not, take a memory profile
  (`nats server request profile heap --name=<server>`, or the /debug/pprof endpoint when profiling is on) before the pod is OOM-killed,
  and check the container memory limit (`kubectl describe pod <name>`).
related_config: [multiplier, min_bytes]
value_name: memory, times the others' median
value_basis: resident memory divided by the median of the other servers
*/
WITH cur AS (
    SELECT st.server_id, s.server_name, st.mem_bytes
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.mem_bytes IS NOT NULL
),
cmp AS (
    SELECT c.*, (SELECT median(o.mem_bytes) FROM cur o WHERE o.server_id <> c.server_id) AS others
    FROM cur c
)
SELECT
    server_name AS entity_key,
    server_name || ' uses ' || format_bytes(mem_bytes) || ', ' || round(mem_bytes / others, 1)
        || ' times the median of the other servers (' || format_bytes(CAST(others AS BIGINT)) || ').' AS detail,
    round(mem_bytes / others, 2) AS value
FROM cmp
WHERE others > 0 AND mem_bytes > getvariable('multiplier') * others AND mem_bytes >= getvariable('min_bytes')
