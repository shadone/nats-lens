-- Streams: identity, configuration versions per server, per-epoch state and replicas.

CREATE TABLE streams (
    account      VARCHAR NOT NULL,
    stream       VARCHAR NOT NULL,
    created      TIMESTAMP,
    first_epoch  BIGINT NOT NULL,
    last_epoch   BIGINT NOT NULL,
    PRIMARY KEY (account, stream)
);
COMMENT ON TABLE streams IS 'Stream identity: every (account, stream) any server reported. KV buckets are streams named KV_<bucket>, object stores OBJ_<bucket>.';
COMMENT ON COLUMN streams.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN streams.stream IS 'Stream name.';
COMMENT ON COLUMN streams.created IS 'Creation time as most recently reported (earliest across servers in that epoch). A change means the stream was deleted and created again.';
COMMENT ON COLUMN streams.first_epoch IS 'First epoch any server reported the stream.';
COMMENT ON COLUMN streams.last_epoch IS 'Latest epoch any server reported the stream; below the current epoch means the stream is gone.';

CREATE TABLE stream_config (
    server_id              VARCHAR NOT NULL,
    account                VARCHAR NOT NULL,
    stream                 VARCHAR NOT NULL,
    first_epoch            BIGINT NOT NULL,
    last_epoch             BIGINT NOT NULL,
    config_hash            VARCHAR NOT NULL,
    created                TIMESTAMP,
    description            VARCHAR,
    subjects               VARCHAR[],
    retention              VARCHAR,
    storage                VARCHAR,
    replicas               INTEGER,
    max_consumers          INTEGER,
    max_msgs               BIGINT,
    max_bytes              BIGINT,
    max_age_ns             BIGINT,
    max_msgs_per_subject   BIGINT,
    max_msg_size           BIGINT,
    discard                VARCHAR,
    duplicate_window_ns    BIGINT,
    mirror                 VARCHAR,
    sources                VARCHAR[],
    placement_cluster      VARCHAR,
    placement_tags         VARCHAR[],
    compression            VARCHAR,
    sealed                 BOOLEAN,
    deny_delete            BOOLEAN,
    deny_purge             BOOLEAN,
    allow_direct           BOOLEAN,
    allow_msg_ttl          BOOLEAN,
    config                 JSON
);
COMMENT ON TABLE stream_config IS 'Stream configuration as each server reports it, one row per unchanged run of epochs. The row valid at epoch E has first_epoch <= E <= last_epoch. Different config_hash values across servers at one epoch mean the servers disagree.';
COMMENT ON COLUMN stream_config.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN stream_config.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN stream_config.stream IS 'Stream name.';
COMMENT ON COLUMN stream_config.first_epoch IS 'First epoch this server reported this configuration.';
COMMENT ON COLUMN stream_config.last_epoch IS 'Latest epoch this server reported this configuration.';
COMMENT ON COLUMN stream_config.config_hash IS 'Hash of the full configuration and creation time; equal hashes mean equal configuration.';
COMMENT ON COLUMN stream_config.created IS 'Stream creation time as this server reports it.';
COMMENT ON COLUMN stream_config.description IS 'Stream description.';
COMMENT ON COLUMN stream_config.subjects IS 'Subjects the stream captures.';
COMMENT ON COLUMN stream_config.retention IS 'Retention policy: limits, interest or workqueue.';
COMMENT ON COLUMN stream_config.storage IS 'Storage type: file or memory.';
COMMENT ON COLUMN stream_config.replicas IS 'Configured replica count (R).';
COMMENT ON COLUMN stream_config.max_consumers IS 'Consumer limit; -1 is unlimited.';
COMMENT ON COLUMN stream_config.max_msgs IS 'Message limit; -1 is unlimited.';
COMMENT ON COLUMN stream_config.max_bytes IS 'Byte limit; -1 is unlimited.';
COMMENT ON COLUMN stream_config.max_age_ns IS 'Maximum message age, nanoseconds; 0 is unlimited.';
COMMENT ON COLUMN stream_config.max_msgs_per_subject IS 'Per-subject message limit; -1 is unlimited. KV history is this value.';
COMMENT ON COLUMN stream_config.max_msg_size IS 'Largest accepted message, bytes; -1 is unlimited.';
COMMENT ON COLUMN stream_config.discard IS 'Discard policy when a limit is hit: old or new.';
COMMENT ON COLUMN stream_config.duplicate_window_ns IS 'Duplicate detection window, nanoseconds.';
COMMENT ON COLUMN stream_config.mirror IS 'Name of the stream this stream mirrors; empty when not a mirror.';
COMMENT ON COLUMN stream_config.sources IS 'Names of the streams this stream sources from.';
COMMENT ON COLUMN stream_config.placement_cluster IS 'Placement cluster, when set.';
COMMENT ON COLUMN stream_config.placement_tags IS 'Placement tags, when set.';
COMMENT ON COLUMN stream_config.compression IS 'Storage compression: none or s2.';
COMMENT ON COLUMN stream_config.sealed IS 'True when the stream is sealed (read-only).';
COMMENT ON COLUMN stream_config.deny_delete IS 'True when message deletes are denied.';
COMMENT ON COLUMN stream_config.deny_purge IS 'True when purges are denied.';
COMMENT ON COLUMN stream_config.allow_direct IS 'True when direct get is allowed.';
COMMENT ON COLUMN stream_config.allow_msg_ttl IS 'True when per-message TTL is allowed.';
COMMENT ON COLUMN stream_config.config IS 'Full stream configuration JSON as reported.';

CREATE TABLE stream_stats (
    epoch              BIGINT NOT NULL,
    server_id          VARCHAR NOT NULL,
    account            VARCHAR NOT NULL,
    stream             VARCHAR NOT NULL,
    created            TIMESTAMP,
    messages           BIGINT,
    bytes              BIGINT,
    first_seq          BIGINT,
    last_seq           BIGINT,
    first_ts           TIMESTAMP,
    last_ts            TIMESTAMP,
    num_subjects       BIGINT,
    num_deleted        BIGINT,
    consumer_count     INTEGER,
    cluster            VARCHAR,
    raft_group         VARCHAR,
    leader             VARCHAR,
    leader_since       TIMESTAMP,
    is_leader          BOOLEAN,
    replicas_reported  INTEGER,
    replicas_current   INTEGER,
    replicas_offline   INTEGER,
    mirror_lag         BIGINT,
    mirror_active_ns   BIGINT,
    mirror_error       VARCHAR,
    sources_count      INTEGER,
    sources_max_lag    BIGINT
);
COMMENT ON TABLE stream_stats IS 'Stream state per server per epoch, from the server''s JSZ. A server reports only the streams it holds a replica of, so an R1 stream has one row per epoch and an R3 stream three.';
COMMENT ON COLUMN stream_stats.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN stream_stats.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN stream_stats.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN stream_stats.stream IS 'Stream name.';
COMMENT ON COLUMN stream_stats.created IS 'Stream creation time as this server reports it.';
COMMENT ON COLUMN stream_stats.messages IS 'Messages stored in this server''s replica.';
COMMENT ON COLUMN stream_stats.bytes IS 'Bytes stored in this server''s replica.';
COMMENT ON COLUMN stream_stats.first_seq IS 'First sequence in the replica.';
COMMENT ON COLUMN stream_stats.last_seq IS 'Last sequence in the replica; lower than the leader''s means this replica lags.';
COMMENT ON COLUMN stream_stats.first_ts IS 'UTC time of the first message.';
COMMENT ON COLUMN stream_stats.last_ts IS 'UTC time of the last message.';
COMMENT ON COLUMN stream_stats.num_subjects IS 'Distinct subjects stored.';
COMMENT ON COLUMN stream_stats.num_deleted IS 'Deleted messages inside the sequence range (interior deletes).';
COMMENT ON COLUMN stream_stats.consumer_count IS 'Consumers on the stream, as this server counts them.';
COMMENT ON COLUMN stream_stats.cluster IS 'Cluster the stream is placed in.';
COMMENT ON COLUMN stream_stats.raft_group IS 'Raft group ID of the stream (raft_groups.group_id).';
COMMENT ON COLUMN stream_stats.leader IS 'Server name of the stream leader, from this server''s view; empty when there is no leader.';
COMMENT ON COLUMN stream_stats.leader_since IS 'UTC time the current leader was elected, when reported.';
COMMENT ON COLUMN stream_stats.is_leader IS 'True when the reporting server is the leader.';
COMMENT ON COLUMN stream_stats.replicas_reported IS 'Other replicas listed by this server (stream_replicas rows).';
COMMENT ON COLUMN stream_stats.replicas_current IS 'Of those, replicas reported current.';
COMMENT ON COLUMN stream_stats.replicas_offline IS 'Of those, replicas reported offline.';
COMMENT ON COLUMN stream_stats.mirror_lag IS 'Messages the mirror is behind its origin; null when not a mirror.';
COMMENT ON COLUMN stream_stats.mirror_active_ns IS 'Nanoseconds since the mirror last received data.';
COMMENT ON COLUMN stream_stats.mirror_error IS 'Mirror error description, when any.';
COMMENT ON COLUMN stream_stats.sources_count IS 'Number of sources; null when the stream has none.';
COMMENT ON COLUMN stream_stats.sources_max_lag IS 'Largest lag across sources.';

CREATE TABLE stream_replicas (
    epoch      BIGINT NOT NULL,
    server_id  VARCHAR NOT NULL,
    account    VARCHAR NOT NULL,
    stream     VARCHAR NOT NULL,
    peer_name  VARCHAR NOT NULL,
    peer_id    VARCHAR,
    current    BOOLEAN,
    offline    BOOLEAN,
    active_ns  BIGINT,
    lag        BIGINT,
    pending    BOOLEAN
);
COMMENT ON TABLE stream_replicas IS 'Stream replica peers as each server reports them. A server lists the peers other than itself; the leader''s rows are the authoritative view of follower health.';
COMMENT ON COLUMN stream_replicas.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN stream_replicas.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN stream_replicas.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN stream_replicas.stream IS 'Stream name.';
COMMENT ON COLUMN stream_replicas.peer_name IS 'Server name of the replica.';
COMMENT ON COLUMN stream_replicas.peer_id IS 'Raft peer ID of the replica (stable hash of the server name).';
COMMENT ON COLUMN stream_replicas.current IS 'True when the replica was seen recently and is caught up.';
COMMENT ON COLUMN stream_replicas.offline IS 'True when the replica has not been seen recently.';
COMMENT ON COLUMN stream_replicas.active_ns IS 'Nanoseconds since the replica was last seen.';
COMMENT ON COLUMN stream_replicas.lag IS 'Raft operations the replica is behind.';
COMMENT ON COLUMN stream_replicas.pending IS 'True when the peer is assigned but not yet part of the raft group, or is being removed.';
