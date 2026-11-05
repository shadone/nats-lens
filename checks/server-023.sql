/*
code: server-023
name: CPU saturated
entity: server
category: health
severity: warning
kind: operational
thresholds:
  - name: busy_ratio
    default: "0.9"
    unit: ratio
    description: Share of the cores the server may use (the smaller of its cores and GOMAXPROCS) at or above which a collection counts as busy.
  - name: min_duration
    default: 5m
    unit: duration
    description: How long every collection of the server process must be busy before the finding fires.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while the CPU stays at or above this share of the busy level, and resolves
      below it, so a CPU that hovers at the threshold does not flap.
description: >-
  The server process used at least busy_ratio of the cores it may use, the smaller of the cores it sees and its
  GOMAXPROCS, in every collection for min_duration. A saturated server answers clients, routes and raft heartbeats
  late: round trips grow, slow consumers and stalls follow (server-009, server-010), and missed heartbeats move
  JetStream leaders (system-003). Counted per server process.
remediation: >-
  `nats server report cpu` with the system user compares the servers. One server alone points at the work it
  leads: the streams and consumers it is leader for (the Placement page; move some with `nats stream cluster
  step-down <stream>`), or its clients (`nats server request connections --name=nats-0`). Every server busy is load
  the cluster cannot carry: raise the CPU limit of the NATS pods or add servers. In Kubernetes, a CPU limit below the
  node's cores throttles the process; nats-server built with Go 1.25 or later sets GOMAXPROCS to the limit, and an
  older one may report more cores than it may use, so the check then fires late or not at all. `nats server request
  profile` takes a CPU profile of the server for the NATS maintainers.
related_config: [busy_ratio, min_duration, resolve_ratio, GOMAXPROCS]
value_name: share of usable CPU
value_basis: CPU of the server process divided by the smaller of its cores and GOMAXPROCS, newest collection
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-023' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
cur AS (
    SELECT st.server_id, s.server_name, st.cpu_percent, st.cores, st.gomaxprocs,
        least(nullif(st.cores, 0), nullif(st.gomaxprocs, 0)) AS usable,
        getvariable('busy_ratio') * CASE WHEN s.server_name IN (SELECT entity_key FROM firing)
            THEN getvariable('resolve_ratio') ELSE 1 END AS level
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.cpu_percent IS NOT NULL
),
busy AS (
    SELECT * FROM cur WHERE usable > 0 AND cpu_percent >= level * 100 * usable
),
-- The run of busy collections ends at the latest earlier collection of
-- this process below the level.
calm AS (
    SELECT b.server_id, max(x.epoch) AS epoch
    FROM busy b JOIN server_stats x ON x.server_id = b.server_id
    WHERE x.epoch < getvariable('epoch') AND x.cpu_percent IS NOT NULL
      AND x.cpu_percent < b.level * 100 * least(nullif(x.cores, 0), nullif(x.gomaxprocs, 0))
    GROUP BY b.server_id
),
since AS (
    SELECT b.*, (SELECT min(e.started_at) FROM server_stats x JOIN epochs e USING (epoch)
        WHERE x.server_id = b.server_id AND x.epoch > coalesce(c.epoch, 0) AND x.epoch <= getvariable('epoch')
          AND x.cpu_percent IS NOT NULL) AS since
    FROM busy b LEFT JOIN calm c USING (server_id)
)
SELECT
    server_name AS entity_key,
    server_name || ' used ' || regexp_replace(CAST(round(cpu_percent / 100, 1) AS VARCHAR), '\.0$', '') || ' of ' || usable || ' usable cores ('
        || CAST(round(cpu_percent / usable) AS BIGINT) || '%) and has been at least ' || CAST(round(100 * level) AS BIGINT) || '% busy for '
        || span_text(getvariable('epoch_time') - since)
        || CASE WHEN gomaxprocs IS NOT NULL AND cores IS NOT NULL AND gomaxprocs <> cores
            THEN ' (' || cores || ' cores, GOMAXPROCS ' || gomaxprocs || ')' ELSE '' END || '.' AS detail,
    round(cpu_percent / 100 / usable, 4) AS value,
    since AS condition_since
FROM since
WHERE getvariable('epoch_time') - since >= getvariable('min_duration')
QUALIFY row_number() OVER (PARTITION BY server_name ORDER BY cpu_percent / usable DESC, server_id) = 1
