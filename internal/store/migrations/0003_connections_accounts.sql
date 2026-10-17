-- Client connections and accounts.

CREATE TABLE connections (
    epoch            BIGINT NOT NULL,
    server_id        VARCHAR NOT NULL,
    cid              BIGINT NOT NULL,
    kind             VARCHAR,
    type             VARCHAR,
    name             VARCHAR,
    lang             VARCHAR,
    version          VARCHAR,
    account          VARCHAR,
    authorized_user  VARCHAR,
    ip               VARCHAR,
    port             INTEGER,
    start_time       TIMESTAMP,
    last_activity    TIMESTAMP,
    rtt_ns           BIGINT,
    pending_bytes    BIGINT,
    in_msgs          BIGINT,
    out_msgs         BIGINT,
    in_bytes         BIGINT,
    out_bytes        BIGINT,
    stalls           BIGINT,
    subscriptions    BIGINT,
    tls_version      VARCHAR,
    name_tag         VARCHAR
);
COMMENT ON TABLE connections IS 'Client connections each server holds at the epoch (CONNZ), up to the connz limit per server. A connection lives on exactly one server.';
COMMENT ON COLUMN connections.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN connections.server_id IS 'Server the client is connected to (servers.server_id).';
COMMENT ON COLUMN connections.cid IS 'Connection ID, unique per server process.';
COMMENT ON COLUMN connections.kind IS 'Connection kind, normally Client.';
COMMENT ON COLUMN connections.type IS 'Client type: nats, mqtt or websocket.';
COMMENT ON COLUMN connections.name IS 'Client-supplied connection name; usually names the service that owns the connection.';
COMMENT ON COLUMN connections.lang IS 'Client library language.';
COMMENT ON COLUMN connections.version IS 'Client library version.';
COMMENT ON COLUMN connections.account IS 'Account the connection is bound to (accounts.account).';
COMMENT ON COLUMN connections.authorized_user IS 'User the connection authenticated as.';
COMMENT ON COLUMN connections.ip IS 'Client IP address.';
COMMENT ON COLUMN connections.port IS 'Client port.';
COMMENT ON COLUMN connections.start_time IS 'UTC time the connection was accepted.';
COMMENT ON COLUMN connections.last_activity IS 'UTC time of the last message or protocol activity.';
COMMENT ON COLUMN connections.rtt_ns IS 'Round trip time to the client, nanoseconds.';
COMMENT ON COLUMN connections.pending_bytes IS 'Bytes queued for the client and not yet written; high values precede slow consumer disconnects.';
COMMENT ON COLUMN connections.in_msgs IS 'Messages received from the client since it connected.';
COMMENT ON COLUMN connections.out_msgs IS 'Messages sent to the client since it connected.';
COMMENT ON COLUMN connections.in_bytes IS 'Bytes received from the client since it connected.';
COMMENT ON COLUMN connections.out_bytes IS 'Bytes sent to the client since it connected.';
COMMENT ON COLUMN connections.stalls IS 'Times the client was stalled by a full outbound buffer.';
COMMENT ON COLUMN connections.subscriptions IS 'Subscriptions the connection holds.';
COMMENT ON COLUMN connections.tls_version IS 'TLS version, empty for plain connections.';
COMMENT ON COLUMN connections.name_tag IS 'Name tag of the user or account JWT, when operator mode is used.';

CREATE TABLE accounts (
    account       VARCHAR PRIMARY KEY,
    account_name  VARCHAR,
    is_system     BOOLEAN NOT NULL,
    first_epoch   BIGINT NOT NULL,
    last_epoch    BIGINT NOT NULL
);
COMMENT ON TABLE accounts IS 'Account identity, from ACCOUNTZ, account STATZ and JSZ.';
COMMENT ON COLUMN accounts.account IS 'Account identifier: the name in config mode, the public key in operator mode.';
COMMENT ON COLUMN accounts.account_name IS 'Readable account name (equals account in config mode, the JWT name tag in operator mode).';
COMMENT ON COLUMN accounts.is_system IS 'True for the system account.';
COMMENT ON COLUMN accounts.first_epoch IS 'First epoch any server reported the account.';
COMMENT ON COLUMN accounts.last_epoch IS 'Latest epoch any server reported the account.';

CREATE TABLE account_stats (
    epoch                BIGINT NOT NULL,
    server_id            VARCHAR NOT NULL,
    account              VARCHAR NOT NULL,
    conns                INTEGER,
    leafnodes            INTEGER,
    total_conns          INTEGER,
    subscriptions        BIGINT,
    sent_msgs            BIGINT,
    sent_bytes           BIGINT,
    received_msgs        BIGINT,
    received_bytes       BIGINT,
    slow_consumers       BIGINT,
    js_memory            BIGINT,
    js_storage           BIGINT,
    js_reserved_memory   BIGINT,
    js_reserved_storage  BIGINT,
    js_api_total         BIGINT,
    js_api_errors        BIGINT,
    js_streams           INTEGER,
    js_consumers         INTEGER
);
COMMENT ON TABLE account_stats IS 'Per account, per server, per epoch: connection and traffic numbers from account STATZ (this server''s share) and JetStream usage from JSZ.';
COMMENT ON COLUMN account_stats.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN account_stats.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN account_stats.account IS 'Account identifier (accounts.account).';
COMMENT ON COLUMN account_stats.conns IS 'Client connections in the account on this server.';
COMMENT ON COLUMN account_stats.leafnodes IS 'Leaf node connections in the account on this server.';
COMMENT ON COLUMN account_stats.total_conns IS 'Client plus leaf node connections in the account on this server.';
COMMENT ON COLUMN account_stats.subscriptions IS 'Subscriptions in the account on this server.';
COMMENT ON COLUMN account_stats.sent_msgs IS 'Messages the server sent for the account (cumulative).';
COMMENT ON COLUMN account_stats.sent_bytes IS 'Bytes the server sent for the account (cumulative).';
COMMENT ON COLUMN account_stats.received_msgs IS 'Messages the server received for the account (cumulative).';
COMMENT ON COLUMN account_stats.received_bytes IS 'Bytes the server received for the account (cumulative).';
COMMENT ON COLUMN account_stats.slow_consumers IS 'Slow consumers in the account on this server (cumulative).';
COMMENT ON COLUMN account_stats.js_memory IS 'JetStream memory used by the account, bytes, as this server reports it. In a cluster this is the account total across all replicas, not this server''s share.';
COMMENT ON COLUMN account_stats.js_storage IS 'JetStream file storage used by the account, bytes, as this server reports it. In a cluster this is the account total across all replicas, not this server''s share.';
COMMENT ON COLUMN account_stats.js_reserved_memory IS 'JetStream memory reserved for the account, bytes.';
COMMENT ON COLUMN account_stats.js_reserved_storage IS 'JetStream file storage reserved for the account, bytes.';
COMMENT ON COLUMN account_stats.js_api_total IS 'JetStream API requests for the account (cumulative).';
COMMENT ON COLUMN account_stats.js_api_errors IS 'JetStream API requests for the account answered with an error (cumulative).';
COMMENT ON COLUMN account_stats.js_streams IS 'Streams of the account with a replica on this server.';
COMMENT ON COLUMN account_stats.js_consumers IS 'Consumers of the account with a replica on this server.';
