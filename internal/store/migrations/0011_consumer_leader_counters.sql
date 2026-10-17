-- Consumer counters that only the leader tracks are NULL on follower rows,
-- and a view selects the leader's row of every consumer.

COMMENT ON COLUMN consumer_stats.num_pending IS 'Messages matching the filter not yet delivered (the backlog). Only the leader computes it; NULL on follower rows (followers report 0). Rows written before migration 0011 hold the follower''s 0.';
COMMENT ON COLUMN consumer_stats.num_waiting IS 'Pull requests waiting. Only the leader holds pull requests; NULL on follower rows. Rows written before migration 0011 hold the follower''s 0.';
COMMENT ON COLUMN consumer_stats.push_bound IS 'True when a push consumer has an active subscriber. Only the leader tracks interest; NULL on follower rows. Rows written before migration 0011 hold the follower''s false.';
COMMENT ON COLUMN consumer_stats.is_leader IS 'True when the reporting server is the leader, and for a consumer on a server without a JetStream cluster (the only copy).';

CREATE VIEW consumer_leader_stats AS
SELECT * FROM consumer_stats WHERE is_leader;
COMMENT ON VIEW consumer_leader_stats IS 'One row per consumer per epoch: the leader''s row of consumer_stats, which is the only one with num_pending, num_waiting and push_bound. A consumer without a leader in an epoch has no row.';
COMMENT ON COLUMN stream_stats.is_leader IS 'True when the reporting server is the leader, and for a stream on a server without a JetStream cluster (the only copy).';
