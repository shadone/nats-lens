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
    description: The span of collections judged; the server process must have answered over all of it.
  - name: busy_share
    default: "0.8"
    unit: ratio
    description: Share of the collections within min_duration that must be busy for the finding to fire.
  - name: min_samples
    default: "5"
    unit: count
    description: Fewest collections with a CPU value within min_duration that can fire.
  - name: resolve_ratio
    default: "0.8"
    unit: ratio
    description: >-
      A firing finding keeps firing while busy_share of the collections stay at or above this share of the busy
      level, and resolves below it, so a CPU that hovers at the threshold does not flap.
description: >-
  The server process used at least busy_ratio of the cores it may use, the smaller of the cores it sees and its
  GOMAXPROCS, in at least busy_share of the collections within min_duration, and at least min_samples of them. A
  saturated server answers clients, routes and raft heartbeats late: round trips grow, slow consumers and stalls
  follow (server-009, server-010), and missed heartbeats move JetStream leaders (system-003). Counted per server
  process: the process must have answered from before min_duration to now, with no gap of more than three collect
  intervals, and a restarting process (restart_window) is not judged.
remediation: >-
  `nats server report cpu` with the system user compares the servers. One server alone points at the work it
  leads: the streams and consumers it is leader for (the Placement page; move some with `nats stream cluster
  step-down <stream>`), or its clients (`nats server request connections --name=nats-0`). Every server busy is load
  the cluster cannot carry: raise the CPU limit of the NATS pods or add servers. In Kubernetes, a CPU limit below the
  node's cores throttles the process; nats-server built with Go 1.25 or later sets GOMAXPROCS to the limit, and an
  older one may report more cores than it may use, so the check then fires late or not at all. `nats server request
  profile cpu --name=nats-0` takes a CPU profile of the server for the NATS maintainers.
related_config: [busy_ratio, min_duration, busy_share, min_samples, resolve_ratio, GOMAXPROCS]
value_name: share of usable CPU
value_basis: CPU of the server process divided by the smaller of its cores and GOMAXPROCS, newest collection
*/
WITH firing AS (
    SELECT entity_key FROM findings
    WHERE check_code = 'server-023' AND state = 'firing' AND last_seen_epoch = getvariable('prev_epoch')
),
bounds AS (
    SELECT getvariable('epoch_time') - getvariable('min_duration') AS from_at
),
-- The collect interval, as the median gap between the collections around
-- the window.
step AS (
    SELECT coalesce(median(gap), INTERVAL 30 SECOND) AS gap FROM (
        SELECT started_at - lag(started_at) OVER (ORDER BY epoch) AS gap FROM epochs
        WHERE epoch <= getvariable('epoch')
          AND started_at >= (SELECT from_at FROM bounds) - INTERVAL 10 MINUTE)
),
cur AS (
    SELECT st.server_id, s.server_name, st.cpu_percent, st.cores, st.gomaxprocs,
        least(nullif(st.cores, 0), nullif(st.gomaxprocs, 0)) AS usable,
        getvariable('busy_ratio') * CASE WHEN s.server_name IN (SELECT entity_key FROM firing)
            THEN getvariable('resolve_ratio') ELSE 1 END AS level
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.cpu_percent IS NOT NULL
      AND st.server_id NOT IN (SELECT server_id FROM restarting(getvariable('epoch'), getvariable('epoch')))
),
-- The process's collections within the window and up to three collect
-- intervals before it, to see that it answered when the window began.
samples AS (
    SELECT c.server_id, e.started_at,
        x.cpu_percent >= c.level * 100 * least(nullif(x.cores, 0), nullif(x.gomaxprocs, 0)) AS busy,
        e.started_at - lag(e.started_at) OVER (PARTITION BY c.server_id ORDER BY e.epoch) AS gap
    FROM cur c JOIN server_stats x ON x.server_id = c.server_id JOIN epochs e ON e.epoch = x.epoch
    WHERE x.epoch <= getvariable('epoch') AND x.cpu_percent IS NOT NULL
      AND e.started_at >= (SELECT from_at FROM bounds) - 3 * (SELECT gap FROM step)
),
judged AS (
    SELECT server_id,
        bool_or(started_at <= (SELECT from_at FROM bounds)) AS covered,
        coalesce(bool_or(gap > 3 * (SELECT gap FROM step)), false) AS broken,
        count(*) FILTER (WHERE started_at >= (SELECT from_at FROM bounds)) AS n,
        count(*) FILTER (WHERE started_at >= (SELECT from_at FROM bounds) AND busy) AS busy_n
    FROM samples GROUP BY server_id
)
SELECT
    c.server_name AS entity_key,
    c.server_name || ' used ' || regexp_replace(CAST(round(c.cpu_percent / 100, 1) AS VARCHAR), '\.0$', '')
        || ' of ' || c.usable || ' usable cores (' || CAST(round(c.cpu_percent / c.usable) AS BIGINT) || '%); '
        || j.busy_n || ' of the ' || j.n || ' collections in the last ' || span_text(getvariable('min_duration'))
        || ' were at least ' || CAST(round(100 * c.level) AS BIGINT) || '% busy'
        || CASE WHEN c.gomaxprocs IS NOT NULL AND c.cores IS NOT NULL AND c.gomaxprocs <> c.cores
            THEN ' (' || c.cores || ' cores, GOMAXPROCS ' || c.gomaxprocs || ')' ELSE '' END || '.' AS detail,
    round(c.cpu_percent / 100 / c.usable, 4) AS value
FROM cur c JOIN judged j USING (server_id)
WHERE c.usable > 0 AND j.covered AND NOT j.broken
  AND j.n >= getvariable('min_samples') AND j.busy_n >= getvariable('busy_share') * j.n
QUALIFY row_number() OVER (PARTITION BY c.server_name ORDER BY c.cpu_percent / c.usable DESC, c.server_id) = 1
