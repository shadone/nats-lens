-- Consumers: identity, configuration versions per server, per-epoch state and replicas.

CREATE TABLE consumers (
    account      VARCHAR NOT NULL,
    stream       VARCHAR NOT NULL,
    consumer     VARCHAR NOT NULL,
    created      TIMESTAMP,
    first_epoch  BIGINT NOT NULL,
    last_epoch   BIGINT NOT NULL,
    PRIMARY KEY (account, stream, consumer)
);
COMMENT ON TABLE consumers IS 'Consumer identity: every (account, stream, consumer) any server reported. Ephemeral and ordered consumers (KV watchers, names like oc_...) come and go.';
COMMENT ON COLUMN consumers.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN consumers.stream IS 'Stream the consumer reads.';
COMMENT ON COLUMN consumers.consumer IS 'Consumer name (the durable name for durables).';
COMMENT ON COLUMN consumers.created IS 'Creation time as most recently reported (earliest across servers in that epoch). A change means the consumer was deleted and created again.';
COMMENT ON COLUMN consumers.first_epoch IS 'First epoch any server reported the consumer.';
COMMENT ON COLUMN consumers.last_epoch IS 'Latest epoch any server reported the consumer; below the current epoch means the consumer is gone.';

CREATE TABLE consumer_config (
    server_id               VARCHAR NOT NULL,
    account                 VARCHAR NOT NULL,
    stream                  VARCHAR NOT NULL,
    consumer                VARCHAR NOT NULL,
    first_epoch             BIGINT NOT NULL,
    last_epoch              BIGINT NOT NULL,
    config_hash             VARCHAR NOT NULL,
    created                 TIMESTAMP,
    durable_name            VARCHAR,
    description             VARCHAR,
    deliver_policy          VARCHAR,
    ack_policy              VARCHAR,
    ack_wait_ns             BIGINT,
    max_deliver             INTEGER,
    backoff_ns              BIGINT[],
    filter_subjects         VARCHAR[],
    replay_policy           VARCHAR,
    max_waiting             INTEGER,
    max_ack_pending         INTEGER,
    max_batch               INTEGER,
    deliver_subject         VARCHAR,
    deliver_group           VARCHAR,
    idle_heartbeat_ns       BIGINT,
    flow_control            BOOLEAN,
    headers_only            BOOLEAN,
    inactive_threshold_ns   BIGINT,
    replicas                INTEGER,
    mem_storage             BOOLEAN,
    config                  JSON
);
COMMENT ON TABLE consumer_config IS 'Consumer configuration as each server reports it, one row per unchanged run of epochs. The row valid at epoch E has first_epoch <= E <= last_epoch.';
COMMENT ON COLUMN consumer_config.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN consumer_config.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN consumer_config.stream IS 'Stream the consumer reads.';
COMMENT ON COLUMN consumer_config.consumer IS 'Consumer name.';
COMMENT ON COLUMN consumer_config.first_epoch IS 'First epoch this server reported this configuration.';
COMMENT ON COLUMN consumer_config.last_epoch IS 'Latest epoch this server reported this configuration.';
COMMENT ON COLUMN consumer_config.config_hash IS 'Hash of the full configuration and creation time; equal hashes mean equal configuration.';
COMMENT ON COLUMN consumer_config.created IS 'Consumer creation time as this server reports it.';
COMMENT ON COLUMN consumer_config.durable_name IS 'Durable name; empty for ephemeral consumers.';
COMMENT ON COLUMN consumer_config.description IS 'Consumer description.';
COMMENT ON COLUMN consumer_config.deliver_policy IS 'Where delivery starts: all, last, new, by_start_sequence, by_start_time, last_per_subject.';
COMMENT ON COLUMN consumer_config.ack_policy IS 'Acknowledgement policy: none, all or explicit.';
COMMENT ON COLUMN consumer_config.ack_wait_ns IS 'Time to wait for an ack before redelivery, nanoseconds.';
COMMENT ON COLUMN consumer_config.max_deliver IS 'Maximum delivery attempts per message; -1 or 0 is unlimited.';
COMMENT ON COLUMN consumer_config.backoff_ns IS 'Redelivery backoff steps, nanoseconds.';
COMMENT ON COLUMN consumer_config.filter_subjects IS 'Subject filters (filter_subject and filter_subjects combined).';
COMMENT ON COLUMN consumer_config.replay_policy IS 'Replay policy: instant or original.';
COMMENT ON COLUMN consumer_config.max_waiting IS 'Maximum pull requests waiting.';
COMMENT ON COLUMN consumer_config.max_ack_pending IS 'Maximum unacknowledged messages; delivery pauses when reached.';
COMMENT ON COLUMN consumer_config.max_batch IS 'Largest pull batch allowed; 0 is unlimited.';
COMMENT ON COLUMN consumer_config.deliver_subject IS 'Push delivery subject; empty for pull consumers.';
COMMENT ON COLUMN consumer_config.deliver_group IS 'Queue group for push delivery.';
COMMENT ON COLUMN consumer_config.idle_heartbeat_ns IS 'Idle heartbeat interval for push consumers, nanoseconds.';
COMMENT ON COLUMN consumer_config.flow_control IS 'True when push flow control is on.';
COMMENT ON COLUMN consumer_config.headers_only IS 'True when only headers are delivered.';
COMMENT ON COLUMN consumer_config.inactive_threshold_ns IS 'Idle time after which the server deletes the consumer, nanoseconds; 0 means never for durables.';
COMMENT ON COLUMN consumer_config.replicas IS 'Configured replica count; 0 means the stream''s replica count.';
COMMENT ON COLUMN consumer_config.mem_storage IS 'True when consumer state is kept in memory only.';
COMMENT ON COLUMN consumer_config.config IS 'Full consumer configuration JSON as reported.';

CREATE TABLE consumer_stats (
    epoch                   BIGINT NOT NULL,
    server_id               VARCHAR NOT NULL,
    account                 VARCHAR NOT NULL,
    stream                  VARCHAR NOT NULL,
    consumer                VARCHAR NOT NULL,
    created                 TIMESTAMP,
    delivered_consumer_seq  BIGINT,
    delivered_stream_seq    BIGINT,
    delivered_last_active   TIMESTAMP,
    ack_floor_consumer_seq  BIGINT,
    ack_floor_stream_seq    BIGINT,
    ack_floor_last_active   TIMESTAMP,
    num_ack_pending         BIGINT,
    num_redelivered         BIGINT,
    num_waiting             BIGINT,
    num_pending             BIGINT,
    push_bound              BOOLEAN,
    paused                  BOOLEAN,
    cluster                 VARCHAR,
    raft_group              VARCHAR,
    leader                  VARCHAR,
    leader_since            TIMESTAMP,
    is_leader               BOOLEAN,
    replicas_reported       INTEGER,
    replicas_current        INTEGER,
    replicas_offline        INTEGER,
    reported_at             TIMESTAMP
);
COMMENT ON TABLE consumer_stats IS 'Consumer state per server per epoch, from the server''s JSZ. A server reports only consumers it holds a replica of: an R1 consumer (for example a KV watcher''s ordered consumer) appears on one server only.';
COMMENT ON COLUMN consumer_stats.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN consumer_stats.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN consumer_stats.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN consumer_stats.stream IS 'Stream the consumer reads.';
COMMENT ON COLUMN consumer_stats.consumer IS 'Consumer name.';
COMMENT ON COLUMN consumer_stats.created IS 'Consumer creation time as this server reports it.';
COMMENT ON COLUMN consumer_stats.delivered_consumer_seq IS 'Consumer sequence of the last delivered message.';
COMMENT ON COLUMN consumer_stats.delivered_stream_seq IS 'Stream sequence of the last delivered message.';
COMMENT ON COLUMN consumer_stats.delivered_last_active IS 'UTC time of the last delivery.';
COMMENT ON COLUMN consumer_stats.ack_floor_consumer_seq IS 'Consumer sequence below which everything is acknowledged.';
COMMENT ON COLUMN consumer_stats.ack_floor_stream_seq IS 'Stream sequence below which everything is acknowledged.';
COMMENT ON COLUMN consumer_stats.ack_floor_last_active IS 'UTC time of the last acknowledgement.';
COMMENT ON COLUMN consumer_stats.num_ack_pending IS 'Delivered messages not yet acknowledged.';
COMMENT ON COLUMN consumer_stats.num_redelivered IS 'Messages delivered more than once and not yet acknowledged.';
COMMENT ON COLUMN consumer_stats.num_waiting IS 'Pull requests waiting. Only the leader tracks them; read it on the row where is_leader.';
COMMENT ON COLUMN consumer_stats.num_pending IS 'Messages matching the filter not yet delivered (the backlog). Only the leader computes it and followers report 0; read it on the row where is_leader.';
COMMENT ON COLUMN consumer_stats.push_bound IS 'True when a push consumer has an active subscriber.';
COMMENT ON COLUMN consumer_stats.paused IS 'True when the consumer is paused.';
COMMENT ON COLUMN consumer_stats.cluster IS 'Cluster the consumer is placed in.';
COMMENT ON COLUMN consumer_stats.raft_group IS 'Raft group ID of the consumer (raft_groups.group_id).';
COMMENT ON COLUMN consumer_stats.leader IS 'Server name of the consumer leader, from this server''s view.';
COMMENT ON COLUMN consumer_stats.leader_since IS 'UTC time the current leader was elected, when reported.';
COMMENT ON COLUMN consumer_stats.is_leader IS 'True when the reporting server is the leader.';
COMMENT ON COLUMN consumer_stats.replicas_reported IS 'Other replicas listed by this server (consumer_replicas rows).';
COMMENT ON COLUMN consumer_stats.replicas_current IS 'Of those, replicas reported current.';
COMMENT ON COLUMN consumer_stats.replicas_offline IS 'Of those, replicas reported offline.';
COMMENT ON COLUMN consumer_stats.reported_at IS 'UTC time the server gathered this consumer info.';

CREATE TABLE consumer_replicas (
    epoch      BIGINT NOT NULL,
    server_id  VARCHAR NOT NULL,
    account    VARCHAR NOT NULL,
    stream     VARCHAR NOT NULL,
    consumer   VARCHAR NOT NULL,
    peer_name  VARCHAR NOT NULL,
    peer_id    VARCHAR,
    current    BOOLEAN,
    offline    BOOLEAN,
    active_ns  BIGINT,
    lag        BIGINT,
    pending    BOOLEAN
);
COMMENT ON TABLE consumer_replicas IS 'Consumer replica peers as each server reports them; a server lists the peers other than itself.';
COMMENT ON COLUMN consumer_replicas.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN consumer_replicas.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN consumer_replicas.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN consumer_replicas.stream IS 'Stream the consumer reads.';
COMMENT ON COLUMN consumer_replicas.consumer IS 'Consumer name.';
COMMENT ON COLUMN consumer_replicas.peer_name IS 'Server name of the replica.';
COMMENT ON COLUMN consumer_replicas.peer_id IS 'Raft peer ID of the replica.';
COMMENT ON COLUMN consumer_replicas.current IS 'True when the replica was seen recently and is caught up.';
COMMENT ON COLUMN consumer_replicas.offline IS 'True when the replica has not been seen recently.';
COMMENT ON COLUMN consumer_replicas.active_ns IS 'Nanoseconds since the replica was last seen.';
COMMENT ON COLUMN consumer_replicas.lag IS 'Raft operations the replica is behind.';
COMMENT ON COLUMN consumer_replicas.pending IS 'True when the peer is assigned but not yet part of the raft group, or is being removed.';
