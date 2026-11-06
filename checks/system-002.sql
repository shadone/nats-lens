/*
code: system-002
name: No JetStream meta leader
entity: system
category: health
severity: critical
kind: operational
description: >-
  A meta group has no reported leader, or its leader reports more of the group's
  peers offline than its quorum allows. Without one no stream or consumer can be created,
  deleted or updated, and assets cannot move to other servers. Stored RAFT membership scopes the groups;
  cluster and domain labels alone do not establish group identity. Unresolved observations are labelled unscoped.
remediation: >-
  The meta group needs a majority of its servers (2 of 3 in a three-server cluster) to elect a leader. Check how
  many servers answer (server-001) and whether they see each other: `nats server report jetstream` with the system
  user lists the meta group's view. During a GKE node upgrade this resolves once the evicted pod is back; if two
  pods are down at once, check that the PodDisruptionBudget allows only one eviction at a time.
*/
WITH cur AS (
    SELECT s.server_name, g.group_key, g.label, st.meta_leader, st.meta_cluster_size,
        st.meta_quorum_needed, st.meta_peers_offline
    FROM server_stats st JOIN servers s USING (server_id) JOIN meta_check_groups g USING (server_id)
    WHERE st.epoch = getvariable('epoch') AND st.js_enabled AND st.meta_cluster_size > 1
),
no_leader AS (
    SELECT group_key, label || ': None of the ' || count(*) || ' clustered JetStream servers ('
        || array_to_string(list(server_name ORDER BY server_name), ', ') || ') reports a meta leader.' AS detail
    FROM cur
    GROUP BY group_key, label
    HAVING count(*) > 0 AND count(*) FILTER (WHERE coalesce(meta_leader, '') <> '') = 0
        AND count(meta_leader) = count(*)
),
-- The leader's own view of its peers. Missing peer counts remain unknown.
too_few AS (
    SELECT group_key, label || ': The meta leader ' || server_name || ' sees ' || meta_peers_offline || ' of ' || meta_cluster_size
        || ' peers offline; quorum needs ' || meta_quorum_needed || ' online.' AS detail
    FROM cur
    WHERE server_name = meta_leader AND meta_quorum_needed > 0
      AND meta_cluster_size - meta_peers_offline < meta_quorum_needed
),
problems AS (
    SELECT * FROM no_leader
    UNION ALL
    SELECT * FROM too_few t WHERE NOT EXISTS (SELECT 1 FROM no_leader n WHERE n.group_key = t.group_key)
)
SELECT '' AS entity_key, string_agg(detail, ' ' ORDER BY group_key, detail) AS detail
FROM problems HAVING count(*) > 0
