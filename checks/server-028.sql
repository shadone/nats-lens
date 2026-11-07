/*
code: server-028
name: Raft node remains a candidate
entity: server
category: health
severity: warning
kind: operational
lookback: 10m
thresholds:
  - name: grace
    default: 2m
    unit: duration
    description: Minimum elapsed span of qualifying retained observations; proposed, not yet confirmed.
  - name: min_epochs
    default: "3"
    unit: count
    description: Minimum eligible samples consecutive among retained collections; proposed, not yet confirmed.
  - name: max_gap
    default: 90s
    unit: duration
    description: Maximum elapsed time between adjacent qualifying retained observations; proposed, not yet confirmed.
description: >-
  A local voting Raft node that previously had a leader reports CANDIDATE with no leader across samples spanning the grace period. This is local election evidence, not a declaration of group-wide quorum loss or a count of elections.
  Successful samples must be consecutive among retained collections with stable process, group, account, peer membership
  and asset creation time. Observations are not proof of continuous capture or of an uninterrupted condition.
  Entirely absent collections within max_gap can bridge; sequence-number holes alone do not reset evidence.
  A retained collection with a missing group or missing or failed RAFTZ capture resets evidence, as do elapsed gaps over max_gap.
  Restart grace, unknown fields, catch-up, paused apply, observers, WAL errors, counter resets and membership changes
  reset the evidence. Unmapped asset groups are not judged. This retained-collection policy is proposed, not yet confirmed.
remediation: >-
  Compare the named local group with its peers and with catch-up, WAL, CPU and route findings. Inspect storage
  latency and scheduling on the reporting server and peer connectivity. A local symptom does not identify a
  failed peer or justify removing a replica. A reset ends the warning without proving recovery; samples cannot reveal changes between observations.
value_name: local candidate groups
value_basis: local groups reporting candidacy in qualifying retained samples spanning the grace period
*/
WITH observed AS (
    SELECT *, coalesce(usable AND identity IS NOT NULL AND state = 'CANDIDATE' AND leader = '' AND ever_had_leader, false) AS bad,
        committed - applied AS backlog
    FROM raft_diagnostic_steps(getvariable('window_epoch'), getvariable('epoch'), getvariable('max_gap'))
),
marked AS (
    SELECT *, CASE WHEN bad AND lag(bad) OVER w AND continuous THEN 0 ELSE 1 END AS boundary
    FROM observed WINDOW w AS (PARTITION BY server_id, group_id, raft_account ORDER BY epoch)
),
runs AS (
    SELECT *, sum(boundary) OVER (PARTITION BY server_id, group_id, raft_account ORDER BY epoch) AS run
    FROM marked
),
held AS (
    SELECT server_id, group_id, raft_account, run, min(started_at) AS since, count(*) AS n
    FROM runs WHERE bad GROUP BY ALL
),
current AS (
    SELECT r.*, h.since FROM runs r JOIN held h USING (server_id, group_id, raft_account, run)
    WHERE r.epoch = getvariable('epoch') AND r.bad AND h.n >= greatest(2, getvariable('min_epochs'))
        AND r.started_at - h.since >= getvariable('grace')
)
SELECT server_name AS entity_key,
    server_name || ' remains a Raft candidate: ' || array_to_string(list(
        asset || ' [' || raft_account || '/' || group_id || '], term ' || term || ', committed ' || committed
        || ', applied ' || applied || ', observed since ' || strftime(since, '%H:%M:%S UTC')
        ORDER BY group_id, raft_account)[1:5], '; ')
        || CASE WHEN count(*) > 5 THEN '; ...' ELSE '' END || '.' AS detail,
    count(*) AS value
FROM current GROUP BY server_name
