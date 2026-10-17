/*
code: stream-003
name: Stream replica lagging
entity: stream
category: health
severity: warning
kind: operational
thresholds:
  - name: max_lag
    default: "1000"
    unit: count
    description: Raft operations an online replica may be behind the leader.
description: >-
  The stream leader reports an online replica more than max_lag raft operations behind. A lagging replica does not
  help the quorum and takes over with old data if it becomes leader.
remediation: >-
  A replica that restarted catches up on its own; watch whether the lag falls between collections. A lag that grows
  means the replica's server cannot keep up: look at its CPU and disk (the PVC's IOPS limit on GKE grows with its
  size) and at server-012. Publishing in large bursts makes followers lag for a while without harm.
related_config: [max_lag]
value_name: raft entries behind
value_basis: largest replica lag the leader reports
*/
WITH lead AS (
    SELECT ss.account, ss.stream, ss.server_id, ss.leader
    FROM stream_stats ss
    WHERE ss.epoch = getvariable('epoch') AND ss.is_leader
),
behind AS (
    SELECT l.account, l.stream, l.leader, r.peer_name, r.lag
    FROM lead l JOIN stream_replicas r
      ON r.epoch = getvariable('epoch') AND r.server_id = l.server_id AND r.account = l.account AND r.stream = l.stream
    WHERE NOT coalesce(r.offline, false) AND r.lag > getvariable('max_lag')
)
SELECT
    account || '/' || stream AS entity_key,
    'Leader ' || any_value(leader) || ' reports ' || array_to_string(list(peer_name || ' ' || lag || ' operations behind'
        ORDER BY peer_name), ', ') || '.' AS detail,
    max(lag) AS value
FROM behind
GROUP BY account, stream
