-- Raft groups: identity with the asset each group serves, and per-server state.

CREATE TABLE raft_groups (
    group_id     VARCHAR PRIMARY KEY,
    kind         VARCHAR NOT NULL,
    account      VARCHAR,
    stream       VARCHAR,
    consumer     VARCHAR,
    first_epoch  BIGINT NOT NULL,
    last_epoch   BIGINT NOT NULL
);
COMMENT ON TABLE raft_groups IS 'Raft group identity and the JetStream asset it replicates. Group IDs are unique in the system.';
COMMENT ON COLUMN raft_groups.group_id IS 'Raft group ID: _meta_ for the meta group, S-... for streams, C-... for consumers.';
COMMENT ON COLUMN raft_groups.kind IS 'meta, stream, consumer, or other.';
COMMENT ON COLUMN raft_groups.account IS 'Account of the stream or consumer the group replicates, from JSZ; null for the meta group or when not yet matched.';
COMMENT ON COLUMN raft_groups.stream IS 'Stream the group replicates (for consumer groups, the consumer''s stream).';
COMMENT ON COLUMN raft_groups.consumer IS 'Consumer the group replicates; null for stream and meta groups.';
COMMENT ON COLUMN raft_groups.first_epoch IS 'First epoch any server reported the group.';
COMMENT ON COLUMN raft_groups.last_epoch IS 'Latest epoch any server reported the group.';

CREATE TABLE raft_stats (
    epoch             BIGINT NOT NULL,
    server_id         VARCHAR NOT NULL,
    group_id          VARCHAR NOT NULL,
    raft_account      VARCHAR,
    state             VARCHAR,
    size              INTEGER,
    quorum_needed     INTEGER,
    observer          BOOLEAN,
    paused            BOOLEAN,
    overrun           BOOLEAN,
    overrun_count     BIGINT,
    committed         BIGINT,
    applied           BIGINT,
    catching_up       BOOLEAN,
    leader            VARCHAR,
    leader_name       VARCHAR,
    leader_since      TIMESTAMP,
    ever_had_leader   BOOLEAN,
    term              BIGINT,
    voted_for         VARCHAR,
    pterm             BIGINT,
    pindex            BIGINT,
    ipq_proposal_len  INTEGER,
    ipq_entry_len     INTEGER,
    ipq_resp_len      INTEGER,
    ipq_apply_len     INTEGER,
    wal_msgs          BIGINT,
    wal_bytes         BIGINT,
    wal_first_seq     BIGINT,
    wal_last_seq      BIGINT,
    wal_error         VARCHAR,
    peers_total       INTEGER,
    peers_known       INTEGER,
    peers             JSON
);
COMMENT ON TABLE raft_stats IS 'Raft group state per server per epoch (RAFTZ). Each server reports the groups it is a member of.';
COMMENT ON COLUMN raft_stats.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN raft_stats.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN raft_stats.group_id IS 'Raft group ID (raft_groups.group_id).';
COMMENT ON COLUMN raft_stats.raft_account IS 'Account the raft traffic runs in, as RAFTZ keys it (the system account unless account NRG is on).';
COMMENT ON COLUMN raft_stats.state IS 'This server''s role: LEADER, FOLLOWER, CANDIDATE or CLOSED.';
COMMENT ON COLUMN raft_stats.size IS 'Number of peers in the group.';
COMMENT ON COLUMN raft_stats.quorum_needed IS 'Peers needed for quorum.';
COMMENT ON COLUMN raft_stats.observer IS 'True when this server is an observer (does not vote).';
COMMENT ON COLUMN raft_stats.paused IS 'True when apply is paused.';
COMMENT ON COLUMN raft_stats.overrun IS 'True when the group fell too far behind and was reset.';
COMMENT ON COLUMN raft_stats.overrun_count IS 'Times the group was overrun.';
COMMENT ON COLUMN raft_stats.committed IS 'Highest committed log index.';
COMMENT ON COLUMN raft_stats.applied IS 'Highest applied log index; committed minus applied is the apply backlog.';
COMMENT ON COLUMN raft_stats.catching_up IS 'True while this server is catching up from the leader.';
COMMENT ON COLUMN raft_stats.leader IS 'Raft peer ID of the group leader, from this server''s view; empty when there is no leader. Peer IDs are 8-character hashes of server names (stream_replicas.peer_id).';
COMMENT ON COLUMN raft_stats.leader_name IS 'Server name of the leader, resolved from the peer IDs and names servers reported in the same epoch; null when not resolvable.';
COMMENT ON COLUMN raft_stats.leader_since IS 'UTC time the current leader was elected.';
COMMENT ON COLUMN raft_stats.ever_had_leader IS 'False when the group never elected a leader since this server started.';
COMMENT ON COLUMN raft_stats.term IS 'Current raft term; increases with every election.';
COMMENT ON COLUMN raft_stats.voted_for IS 'Raft peer ID this server voted for in the current term.';
COMMENT ON COLUMN raft_stats.pterm IS 'Term of the last log entry.';
COMMENT ON COLUMN raft_stats.pindex IS 'Index of the last log entry.';
COMMENT ON COLUMN raft_stats.ipq_proposal_len IS 'Proposals queued.';
COMMENT ON COLUMN raft_stats.ipq_entry_len IS 'Entries queued for append.';
COMMENT ON COLUMN raft_stats.ipq_resp_len IS 'Append responses queued.';
COMMENT ON COLUMN raft_stats.ipq_apply_len IS 'Committed entries queued for apply.';
COMMENT ON COLUMN raft_stats.wal_msgs IS 'Entries in the write-ahead log.';
COMMENT ON COLUMN raft_stats.wal_bytes IS 'Bytes in the write-ahead log.';
COMMENT ON COLUMN raft_stats.wal_first_seq IS 'First index in the write-ahead log.';
COMMENT ON COLUMN raft_stats.wal_last_seq IS 'Last index in the write-ahead log.';
COMMENT ON COLUMN raft_stats.wal_error IS 'Write-ahead log error as reported (raw JSON text), when any.';
COMMENT ON COLUMN raft_stats.peers_total IS 'Peers listed by this server.';
COMMENT ON COLUMN raft_stats.peers_known IS 'Of those, peers marked known.';
COMMENT ON COLUMN raft_stats.peers IS 'Peers as reported: JSON object of peer ID to {name, known, last_replicated_index, last_seen}.';
