/*
code: system-002
name: No JetStream meta leader
entity: system
category: health
severity: critical
kind: operational
description: >-
  No server in the JetStream cluster reports a meta leader, or the meta leader reports more of the meta group's
  peers offline than its quorum allows. Without one no stream or consumer can be created,
  deleted or updated, and assets cannot move to other servers.
remediation: >-
  The meta group needs a majority of its servers (2 of 3 in a three-server cluster) to elect a leader. Check how
  many servers answer (server-001) and whether they see each other: `nats server report jetstream` with the system
  user lists the meta group's view. During a GKE node upgrade this resolves once the evicted pod is back; if two
  pods are down at once, check that the PodDisruptionBudget allows only one eviction at a time.
*/
WITH cur AS (
    SELECT s.server_name, st.meta_leader, st.meta_cluster_size, st.meta_quorum_needed, st.meta_peers_offline
    FROM server_stats st JOIN servers s USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.js_enabled AND st.meta_cluster_size > 1
),
no_leader AS (
    SELECT 'None of the ' || count(*) || ' clustered JetStream servers ('
        || array_to_string(list(server_name ORDER BY server_name), ', ') || ') reports a meta leader.' AS detail
    FROM cur
    HAVING count(*) > 0 AND count(*) FILTER (WHERE coalesce(meta_leader, '') <> '') = 0
),
-- The leader's own view of its peers.
too_few AS (
    SELECT 'The meta leader ' || server_name || ' sees ' || meta_peers_offline || ' of ' || meta_cluster_size
        || ' peers offline; quorum needs ' || meta_quorum_needed || ' online.' AS detail
    FROM cur
    WHERE server_name = meta_leader AND meta_quorum_needed > 0
      AND meta_cluster_size - coalesce(meta_peers_offline, 0) < meta_quorum_needed
    LIMIT 1
)
SELECT '' AS entity_key, detail FROM no_leader
UNION ALL
SELECT '' AS entity_key, detail FROM too_few WHERE NOT EXISTS (SELECT 1 FROM no_leader)
