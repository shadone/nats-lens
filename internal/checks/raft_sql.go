package checks

// raftDiagnosticPrelude reads only the bounded evaluation window. All values
// remain SQL data (ASVS 1.2.4); no peer/account strings become query text.
// RAFTZ's peers exclude self; known=false denotes a pending membership change
// in nats-server v2.15.0 server/monitor.go:4334-4383. Compare local observations,
// never combine same-named _meta_ groups from separate processes/domains.
const raftDiagnosticPrelude = `;
CREATE OR REPLACE TEMP MACRO raft_diagnostic_steps(lo, hi, gap_limit) AS TABLE
WITH times AS (
    SELECT epoch, started_at, lag(epoch) OVER (ORDER BY epoch) AS previous_epoch
    FROM epochs WHERE epoch BETWEEN lo AND hi
),
assets AS (
    SELECT epoch, server_id, raft_group AS group_id, 'stream ' || account || '/' || stream AS asset,
        created FROM stream_stats WHERE epoch BETWEEN lo AND hi AND raft_group IS NOT NULL
    UNION ALL
    SELECT epoch, server_id, raft_group, 'consumer ' || account || '/' || stream || '/' || consumer,
        created FROM consumer_stats WHERE epoch BETWEEN lo AND hi AND raft_group IS NOT NULL
),
unique_assets AS (
    SELECT epoch, server_id, group_id, min(asset) AS asset, min(created) AS created
    FROM assets GROUP BY ALL HAVING count(*) = 1
),
observations AS (
    SELECT r.*, s.server_name, e.started_at, e.previous_epoch,
        coalesce(a.asset, r.group_id) AS asset,
        -- Include creation time from THIS epoch; mutable raft_groups cannot
        -- establish an asset incarnation when evaluating historical data.
        coalesce(a.asset || ':' || a.created::VARCHAR, CASE WHEN r.group_id = '_meta_' THEN '_meta_' END)
            || ':' || list_sort(json_keys(r.peers))::VARCHAR AS identity,
        count(*) OVER (PARTITION BY r.epoch, r.server_id, r.group_id, r.raft_account) = 1
        AND s.start_time <= e.started_at - getvariable('restart_window')
        AND (SELECT count(*) FROM servers p WHERE p.server_name = s.server_name
             AND p.first_epoch <= r.epoch AND p.last_epoch >= r.epoch) = 1
        AND r.raft_account IS NOT NULL AND r.raft_account <> ''
        AND r.size >= 2 AND r.peers_total = r.size - 1 AND r.peers_known = r.size - 1
        AND len(json_keys(r.peers)) = r.size - 1
        AND (r.group_id = '_meta_' OR (a.created IS NOT NULL AND EXISTS (
            SELECT 1 FROM epoch_endpoints ee WHERE ee.epoch = r.epoch AND ee.server_id = r.server_id
                AND ee.endpoint = 'JSZ' AND ee.state = 'ok')))
        AND EXISTS (SELECT 1 FROM epoch_endpoints ee WHERE ee.epoch = r.epoch AND ee.server_id = r.server_id
            AND ee.state = 'ok' AND (ee.endpoint = 'RAFTZ:' || r.raft_account
                OR (ee.endpoint = 'RAFTZ' AND NOT EXISTS (
                    SELECT 1 FROM epoch_endpoints explicit WHERE explicit.epoch = r.epoch
                    AND explicit.server_id = r.server_id AND explicit.endpoint = 'RAFTZ:' || r.raft_account))))
        AND NOT r.observer AND NOT r.paused AND NOT r.overrun AND NOT r.catching_up
        AND r.wal_error IS NULL AND r.committed >= r.applied AND r.applied >= 0
        AND r.term > 0 AND r.pindex >= r.committed AND r.overrun_count >= 0 AS usable
    FROM raft_stats r JOIN times e USING (epoch) JOIN servers s USING (server_id)
    LEFT JOIN unique_assets a USING (epoch, server_id, group_id)
    WHERE r.epoch BETWEEN lo AND hi
),
stepped AS (
    SELECT *, lag(epoch) OVER w AS prior_epoch, lag(started_at) OVER w AS prior_time,
        lag(identity) OVER w AS prior_identity, lag(committed) OVER w AS prior_committed,
        lag(applied) OVER w AS prior_applied, lag(term) OVER w AS prior_term,
        lag(pindex) OVER w AS prior_pindex, lag(overrun_count) OVER w AS prior_overrun
    FROM observations WINDOW w AS (PARTITION BY server_id, group_id, raft_account ORDER BY epoch)
)
SELECT *, coalesce(prior_epoch = previous_epoch AND started_at > prior_time
    AND started_at - prior_time <= gap_limit AND identity = prior_identity
    AND committed >= prior_committed AND applied >= prior_applied AND term >= prior_term
    AND pindex >= prior_pindex AND overrun_count = prior_overrun, false) AS continuous
FROM stepped;
`
