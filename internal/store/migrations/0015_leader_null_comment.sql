-- Rows written before migration 0011 by a server without JetStream
-- clustering have is_leader NULL; the comments say what that means.

COMMENT ON VIEW consumer_leader_stats IS 'One row per consumer per epoch: the leader''s row of consumer_stats, which is the only one with num_pending, num_waiting and push_bound. A consumer without a leader in an epoch has no row. Rows written before migration 0011 by a server without JetStream clustering have is_leader NULL and are not in this view; select them from consumer_stats with is_leader IS NULL.';
COMMENT ON COLUMN consumer_stats.is_leader IS 'True when the reporting server is the leader, and for a consumer on a server without a JetStream cluster (the only copy). NULL on rows written before migration 0011 by a server without a JetStream cluster.';
COMMENT ON COLUMN stream_stats.is_leader IS 'True when the reporting server is the leader, and for a stream on a server without a JetStream cluster (the only copy). NULL on rows written before migration 0011 by a server without a JetStream cluster.';
