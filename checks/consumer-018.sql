/*
code: consumer-018
name: Consumer replicas degraded
entity: consumer
category: health
severity: warning
kind: operational
suppressed_by: [consumer-001]
thresholds:
  - name: min_epochs
    default: "3"
    unit: count
    description: Consecutive stored collections with the same consumer, leader and membership, each degraded with an online quorum; at least two are required.
  - name: max_gap
    default: 90s
    unit: duration
    description: Maximum time between consecutive observations; a longer collection gap breaks persistence.
  - name: election_grace
    default: 2m
    unit: duration
    description: Minimum age of the consumer, leader election and replica server processes before starting a warning.
description: >-
  The leader repeatedly reports an offline, noncurrent or lagging replica while an online quorum survives.
  Its complete peer list and a quorum of consumer reports agreeing on that leader are required. Follower views
  of other followers are not health evidence. Pending membership, recent creation, election or server start,
  missing reports and unknown peer fields do not start a warning. A pause does not suspend Raft replication.
  An existing warning holds with explicitly unknown evidence until a complete healthy observation, known quorum
  loss, an agreed different consumer incarnation or a known single-replica configuration replaces it.
  A complete leader peer list establishes loss without follower replies; leaderless incarnation changes
  require current reports and configuration to agree. Unknown evidence has no expiry timer.
remediation: >-
  Read the named peers in `nats consumer info <stream> <consumer>` and compare repeated observations.
  Check the replica servers' disk, CPU and network, server restart findings and Raft catch-up findings.
  Lag counts Raft operations, not messages. Restore the degraded peer before another replica is lost.
  Consumer-001 takes precedence when quorum is lost. Keep deployment-specific advice in checks.remediation_file.
value_name: degraded replicas
value_basis: offline, noncurrent or lagging peers in the leader's complete report; absent while evidence is unknown
*/
-- ASVS 1.2.4: data stays in relational expressions; thresholds are typed engine variables.
WITH last AS MATERIALIZED (
    SELECT epoch, started_at FROM epochs WHERE epoch <= getvariable('epoch') ORDER BY epoch DESC
    LIMIT greatest(2, CAST(getvariable('min_epochs') AS INTEGER))
),
cfg AS (
    SELECT e.epoch, c.account, c.stream, c.consumer,
        min(CASE WHEN s.retention IN ('workqueue', 'interest') OR c.replicas = 0 THEN s.replicas ELSE c.replicas END) AS r,
        max(CASE WHEN s.retention IN ('workqueue', 'interest') OR c.replicas = 0 THEN s.replicas ELSE c.replicas END) AS max_r
    FROM last e JOIN consumer_config c ON c.first_epoch <= e.epoch AND c.last_epoch >= e.epoch
    LEFT JOIN stream_config s ON s.account=c.account AND s.stream=c.stream AND s.server_id=c.server_id
        AND s.first_epoch <= e.epoch AND s.last_epoch >= e.epoch
    GROUP BY e.epoch, c.account, c.stream, c.consumer
),
reports AS MATERIALIZED (
    SELECT c.*, s.server_name, coalesce(st.start_time, s.start_time) AS process_start
    FROM consumer_stats c JOIN servers s USING (server_id)
    LEFT JOIN server_stats st ON st.epoch=c.epoch AND st.server_id=c.server_id
    WHERE c.epoch BETWEEN (SELECT min(epoch) FROM last) AND getvariable('epoch')
),
votes AS (
    SELECT epoch, account, stream, consumer, leader, count(*) AS agree
    FROM reports WHERE nullif(leader, '') IS NOT NULL
    GROUP BY epoch, account, stream, consumer, leader
),
leaders AS (
    SELECT c.*, k.r, k.max_r, e.started_at, v.agree,
        count(*) OVER (PARTITION BY c.epoch,c.account,c.stream,c.consumer) AS leaders
    FROM reports c JOIN last e USING (epoch) JOIN cfg k USING (epoch,account,stream,consumer)
    LEFT JOIN votes v USING (epoch,account,stream,consumer,leader)
    WHERE c.is_leader AND c.server_name=c.leader
),
peer_health AS (
    SELECT l.epoch,l.account,l.stream,l.consumer,l.server_id,
        count(p.peer_name) AS peers,
        count(DISTINCT p.peer_name) AS distinct_peers,
        count(*) FILTER (WHERE p.current IS NULL OR p.offline IS NULL OR p.lag IS NULL OR p.lag < 0) AS unknown,
        count(*) FILTER (WHERE p.pending) AS pending,
        count(*) FILTER (WHERE p.offline IS NULL OR p.pending IS NULL OR p.peer_name=l.leader) AS unknown_membership,
        count(*) FILTER (WHERE p.offline=false) AS online_peers,
        count(*) FILTER (WHERE p.offline OR NOT p.current OR p.lag>0) AS degraded,
        string_agg(p.peer_name || ': ' || CASE WHEN p.offline THEN 'offline'
            WHEN p.lag>0 THEN p.lag || ' Raft operations behind' ELSE 'noncurrent' END,
            ', ' ORDER BY p.peer_name) FILTER (WHERE p.offline OR NOT p.current OR p.lag>0) AS why,
        string_agg(p.peer_name, ',' ORDER BY p.peer_name) AS membership,
        max(s.start_time) AS peer_start
    FROM leaders l LEFT JOIN consumer_replicas p USING (epoch,server_id,account,stream,consumer)
    LEFT JOIN servers s ON s.server_name=p.peer_name AND s.first_epoch<=l.epoch AND s.last_epoch>=l.epoch
    GROUP BY l.epoch,l.account,l.stream,l.consumer,l.server_id
),
judged AS (
    SELECT l.*, p.*, l.account || '/' || l.stream || '/' || l.consumer AS entity_key,
        l.r>1 AND l.r=l.max_r AND l.leaders=1 AND l.agree>=l.r//2+1
          AND p.peers=l.r-1 AND p.distinct_peers=p.peers AND p.unknown=0 AND p.pending=0 AS complete,
        -- Loss needs authoritative membership/offline facts, not replies from offline peers.
        l.r>1 AND l.r=l.max_r AND l.leaders=1
          AND p.peers=l.r-1 AND p.distinct_peers=p.peers AND p.unknown_membership=0 AND p.pending=0
          AND 1+p.online_peers<l.r//2+1 AS quorum_lost,
        greatest(l.created,l.leader_since,l.process_start,p.peer_start) < l.started_at-getvariable('election_grace')
          AND l.created IS NOT NULL AND l.leader_since IS NOT NULL AND l.process_start IS NOT NULL AS settled
    FROM leaders l JOIN peer_health p USING (epoch,account,stream,consumer,server_id)
),
windowed AS (
    SELECT entity_key, count(*) AS n, min(epoch) AS first_epoch, max(epoch) AS last_epoch,
        min(started_at) AS since, max(started_at)-min(started_at) AS span,
        count(DISTINCT created) AS incarnations, count(DISTINCT leader) AS leader_count,
        count(DISTINCT membership) AS memberships, count(DISTINCT r) AS sizes,
        bool_and(complete AND settled AND degraded>0 AND 1+online_peers>=r//2+1) AS bad
    FROM judged GROUP BY entity_key
),
current_judgment AS (
    SELECT * FROM judged WHERE epoch=getvariable('epoch')
),
current_config_creation AS (
    SELECT account,stream,consumer, min(created) AS created,
        count(created)=count(*) AND count(DISTINCT created)=1 AS agreed
    FROM consumer_config
    WHERE first_epoch<=getvariable('epoch') AND last_epoch>=getvariable('epoch')
    GROUP BY account,stream,consumer
),
report_creation AS (
    SELECT r.epoch,r.account,r.stream,r.consumer,min(r.created) AS created
    FROM reports r JOIN cfg k USING (epoch,account,stream,consumer)
    JOIN last e USING (epoch)
    WHERE r.epoch=getvariable('epoch') AND k.r=k.max_r
    GROUP BY r.epoch,r.account,r.stream,r.consumer,k.r
    HAVING count(r.created)=count(*) AND count(DISTINCT r.created)=1
        AND count(*)>=greatest(2,k.r//2+1) AND min(r.created)<=min(e.started_at)
),
current_incarnation AS (
    -- ASVS 2.3.1: reset only from agreeing evidence in the evaluated epoch.
    SELECT r.account || '/' || r.stream || '/' || r.consumer AS entity_key,r.created
    FROM report_creation r JOIN current_config_creation cc USING (account,stream,consumer)
    WHERE cc.agreed
        -- A coherent leader view may precede a config refresh. Without a leader,
        -- the current configuration must independently name the same incarnation.
        AND (r.created=cc.created OR EXISTS (
            SELECT 1 FROM current_judgment c WHERE c.account=r.account AND c.stream=r.stream
                AND c.consumer=r.consumer AND c.leaders=1 AND c.created=r.created))
),
fresh AS (
    SELECT c.entity_key, 'Leader ' || c.leader || ': ' || c.why || '; online quorum survives ('
        || (1+c.online_peers) || '/' || c.r || ' online), for ' || w.n || ' collections.' AS detail,
        c.degraded::DOUBLE AS value, json_object('created_us',epoch_us(c.created)::VARCHAR) AS related_config,
        c.leader AS related_source
    FROM current_judgment c JOIN windowed w USING (entity_key)
    WHERE w.n=greatest(2,CAST(getvariable('min_epochs') AS INTEGER)) AND w.bad
        AND w.last_epoch-w.first_epoch=w.n-1 AND w.incarnations=1 AND w.leader_count=1 AND w.memberships=1 AND w.sizes=1
        AND w.span>INTERVAL 0 SECOND AND w.span<=(w.n-1)*getvariable('max_gap')
        AND NOT EXISTS (SELECT 1 FROM last a JOIN last b ON b.epoch=a.epoch+1
            WHERE b.started_at-a.started_at>getvariable('max_gap') OR b.started_at<=a.started_at)
),
held AS (
    SELECT f.entity_key, 'Replica warning remains open; current replica health is unknown or persistence is being re-established. Last evidence: '
        || split_part(f.detail, 'Last evidence: ', -1) AS detail,
        NULL::DOUBLE AS value, f.related_config, f.related_source
    FROM findings f LEFT JOIN current_judgment c USING (entity_key)
    WHERE f.check_code='consumer-018' AND f.state='firing' AND f.updated_epoch<=getvariable('epoch')
      AND NOT EXISTS (SELECT 1 FROM fresh x WHERE x.entity_key=f.entity_key)
      -- A complete healthy report or known quorum loss resolves; ambiguity holds.
      AND NOT coalesce(c.quorum_lost OR (c.complete AND c.degraded=0),false)
      AND NOT EXISTS (SELECT 1 FROM current_incarnation i WHERE i.entity_key=f.entity_key
          AND epoch_us(i.created)::VARCHAR <> f.related_config->>'created_us')
      AND NOT EXISTS (SELECT 1 FROM cfg k WHERE k.epoch=getvariable('epoch') AND k.r=1 AND k.max_r=1
          AND k.account || '/' || k.stream || '/' || k.consumer=f.entity_key)
)
SELECT * FROM fresh UNION ALL SELECT * FROM held
