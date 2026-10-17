-- Per-server numbers, routes, gateways, leaf nodes, internal queues and health.

CREATE TABLE server_stats (
    epoch                    BIGINT NOT NULL,
    server_id                VARCHAR NOT NULL,
    version                  VARCHAR,
    start_time               TIMESTAMP,
    uptime_s                 BIGINT,
    config_load_time         TIMESTAMP,
    config_digest            VARCHAR,
    mem_bytes                BIGINT,
    cpu_percent              DOUBLE,
    cores                    INTEGER,
    gomaxprocs               INTEGER,
    connections              INTEGER,
    total_connections        BIGINT,
    max_connections          INTEGER,
    subscriptions            BIGINT,
    routes                   INTEGER,
    leafnodes                INTEGER,
    active_accounts          INTEGER,
    active_servers           INTEGER,
    in_msgs                  BIGINT,
    in_bytes                 BIGINT,
    out_msgs                 BIGINT,
    out_bytes                BIGINT,
    slow_consumers           BIGINT,
    slow_consumers_clients   BIGINT,
    slow_consumers_routes    BIGINT,
    slow_consumers_gateways  BIGINT,
    slow_consumers_leafs     BIGINT,
    stale_connections        BIGINT,
    stalled_clients          BIGINT,
    connz_total              INTEGER,
    connz_returned           INTEGER,
    js_enabled               BOOLEAN,
    js_max_memory            BIGINT,
    js_max_storage           BIGINT,
    js_memory                BIGINT,
    js_storage               BIGINT,
    js_reserved_memory       BIGINT,
    js_reserved_storage      BIGINT,
    js_streams               INTEGER,
    js_streams_leader        INTEGER,
    js_consumers             INTEGER,
    js_consumers_leader      INTEGER,
    js_messages              BIGINT,
    js_bytes                 BIGINT,
    js_ha_assets             INTEGER,
    js_api_level             INTEGER,
    js_api_total             BIGINT,
    js_api_errors            BIGINT,
    js_api_inflight          BIGINT,
    meta_cluster             VARCHAR,
    meta_leader              VARCHAR,
    meta_cluster_size        INTEGER,
    meta_quorum_needed       INTEGER,
    meta_pending             INTEGER,
    meta_peers_offline       INTEGER,
    meta_peers_not_current   INTEGER
);
COMMENT ON TABLE server_stats IS 'One row per server per epoch: process, traffic and JetStream numbers from VARZ (STATSZ when VARZ is missing) and JSZ. Counters are cumulative since server start.';
COMMENT ON COLUMN server_stats.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN server_stats.server_id IS 'Server ID (servers.server_id).';
COMMENT ON COLUMN server_stats.version IS 'nats-server version.';
COMMENT ON COLUMN server_stats.start_time IS 'UTC time the server process started; a change between epochs means a restart.';
COMMENT ON COLUMN server_stats.uptime_s IS 'Seconds since start_time at the time of the epoch.';
COMMENT ON COLUMN server_stats.config_load_time IS 'UTC time the configuration was loaded or last reloaded.';
COMMENT ON COLUMN server_stats.config_digest IS 'Hash of the active configuration; differs between servers with different configuration.';
COMMENT ON COLUMN server_stats.mem_bytes IS 'Resident memory of the server process, bytes.';
COMMENT ON COLUMN server_stats.cpu_percent IS 'CPU use of the server process, percent of one core.';
COMMENT ON COLUMN server_stats.cores IS 'CPU cores visible to the process.';
COMMENT ON COLUMN server_stats.gomaxprocs IS 'GOMAXPROCS of the server process.';
COMMENT ON COLUMN server_stats.connections IS 'Current client connections on this server.';
COMMENT ON COLUMN server_stats.total_connections IS 'Client connections accepted since start (cumulative).';
COMMENT ON COLUMN server_stats.max_connections IS 'Configured connection limit.';
COMMENT ON COLUMN server_stats.subscriptions IS 'Current subscriptions on this server.';
COMMENT ON COLUMN server_stats.routes IS 'Connected route connections.';
COMMENT ON COLUMN server_stats.leafnodes IS 'Connected leaf node connections.';
COMMENT ON COLUMN server_stats.active_accounts IS 'Accounts with activity on this server (from STATSZ).';
COMMENT ON COLUMN server_stats.active_servers IS 'Servers this server knows to be active in the system (from STATSZ).';
COMMENT ON COLUMN server_stats.in_msgs IS 'Messages received from clients, routes, gateways and leaf nodes since start.';
COMMENT ON COLUMN server_stats.in_bytes IS 'Bytes received since start.';
COMMENT ON COLUMN server_stats.out_msgs IS 'Messages sent since start.';
COMMENT ON COLUMN server_stats.out_bytes IS 'Bytes sent since start.';
COMMENT ON COLUMN server_stats.slow_consumers IS 'Connections closed or flagged as slow consumers since start.';
COMMENT ON COLUMN server_stats.slow_consumers_clients IS 'Slow consumer count for client connections since start.';
COMMENT ON COLUMN server_stats.slow_consumers_routes IS 'Slow consumer count for routes since start.';
COMMENT ON COLUMN server_stats.slow_consumers_gateways IS 'Slow consumer count for gateways since start.';
COMMENT ON COLUMN server_stats.slow_consumers_leafs IS 'Slow consumer count for leaf nodes since start.';
COMMENT ON COLUMN server_stats.stale_connections IS 'Connections closed as stale (missed pings) since start.';
COMMENT ON COLUMN server_stats.stalled_clients IS 'Times a client was stalled by a full outbound buffer since start.';
COMMENT ON COLUMN server_stats.connz_total IS 'Total connections CONNZ reported on this server.';
COMMENT ON COLUMN server_stats.connz_returned IS 'Connections stored in the connections table for this server; less than connz_total when the connz limit cut the list.';
COMMENT ON COLUMN server_stats.js_enabled IS 'Whether JetStream is enabled and reported data.';
COMMENT ON COLUMN server_stats.js_max_memory IS 'JetStream memory store limit, bytes.';
COMMENT ON COLUMN server_stats.js_max_storage IS 'JetStream file store limit, bytes.';
COMMENT ON COLUMN server_stats.js_memory IS 'JetStream memory store in use, bytes.';
COMMENT ON COLUMN server_stats.js_storage IS 'JetStream file store in use, bytes. Compare with js_max_storage for disk headroom.';
COMMENT ON COLUMN server_stats.js_reserved_memory IS 'Memory reserved by account limits, bytes.';
COMMENT ON COLUMN server_stats.js_reserved_storage IS 'File storage reserved by account limits, bytes.';
COMMENT ON COLUMN server_stats.js_streams IS 'Streams with a replica on this server.';
COMMENT ON COLUMN server_stats.js_streams_leader IS 'Streams this server leads.';
COMMENT ON COLUMN server_stats.js_consumers IS 'Consumers with a replica on this server.';
COMMENT ON COLUMN server_stats.js_consumers_leader IS 'Consumers this server leads.';
COMMENT ON COLUMN server_stats.js_messages IS 'Messages stored on this server across its stream replicas.';
COMMENT ON COLUMN server_stats.js_bytes IS 'Bytes stored on this server across its stream replicas.';
COMMENT ON COLUMN server_stats.js_ha_assets IS 'Replicated (R>1) streams and consumers on this server.';
COMMENT ON COLUMN server_stats.js_api_level IS 'JetStream API level the server implements.';
COMMENT ON COLUMN server_stats.js_api_total IS 'JetStream API requests received since start.';
COMMENT ON COLUMN server_stats.js_api_errors IS 'JetStream API requests answered with an error since start.';
COMMENT ON COLUMN server_stats.js_api_inflight IS 'JetStream API requests being served now.';
COMMENT ON COLUMN server_stats.meta_cluster IS 'JetStream meta group cluster name, from this server''s view.';
COMMENT ON COLUMN server_stats.meta_leader IS 'Server name of the JetStream meta leader, from this server''s view; empty when there is no leader.';
COMMENT ON COLUMN server_stats.meta_cluster_size IS 'Known size of the JetStream meta group.';
COMMENT ON COLUMN server_stats.meta_quorum_needed IS 'Meta group quorum size this server uses.';
COMMENT ON COLUMN server_stats.meta_pending IS 'Meta group raft entries not yet applied on this server.';
COMMENT ON COLUMN server_stats.meta_peers_offline IS 'Meta group peers this server reports as offline.';
COMMENT ON COLUMN server_stats.meta_peers_not_current IS 'Meta group peers this server reports as not current.';

CREATE TABLE routes (
    epoch          BIGINT NOT NULL,
    server_id      VARCHAR NOT NULL,
    rid            BIGINT NOT NULL,
    remote_id      VARCHAR,
    remote_name    VARCHAR,
    did_solicit    BOOLEAN,
    is_configured  BOOLEAN,
    ip             VARCHAR,
    port           INTEGER,
    start_time     TIMESTAMP,
    last_activity  TIMESTAMP,
    rtt_ns         BIGINT,
    pending_bytes  BIGINT,
    in_msgs        BIGINT,
    out_msgs       BIGINT,
    in_bytes       BIGINT,
    out_bytes      BIGINT,
    subscriptions  BIGINT,
    account        VARCHAR,
    compression    VARCHAR
);
COMMENT ON TABLE routes IS 'Cluster route connections as each server reports them (ROUTEZ). With route pooling there are several routes per remote.';
COMMENT ON COLUMN routes.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN routes.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN routes.rid IS 'Route connection ID, local to the reporting server.';
COMMENT ON COLUMN routes.remote_id IS 'Server ID of the remote end.';
COMMENT ON COLUMN routes.remote_name IS 'Server name of the remote end.';
COMMENT ON COLUMN routes.did_solicit IS 'True when the reporting server opened the connection.';
COMMENT ON COLUMN routes.is_configured IS 'True when the route is in the configuration rather than discovered.';
COMMENT ON COLUMN routes.ip IS 'Remote IP address.';
COMMENT ON COLUMN routes.port IS 'Remote port.';
COMMENT ON COLUMN routes.start_time IS 'UTC time the route connected; a change means the route reconnected.';
COMMENT ON COLUMN routes.last_activity IS 'UTC time of the last activity on the route.';
COMMENT ON COLUMN routes.rtt_ns IS 'Round trip time, nanoseconds.';
COMMENT ON COLUMN routes.pending_bytes IS 'Bytes waiting to be written to the route.';
COMMENT ON COLUMN routes.in_msgs IS 'Messages received over the route since it connected.';
COMMENT ON COLUMN routes.out_msgs IS 'Messages sent over the route since it connected.';
COMMENT ON COLUMN routes.in_bytes IS 'Bytes received over the route since it connected.';
COMMENT ON COLUMN routes.out_bytes IS 'Bytes sent over the route since it connected.';
COMMENT ON COLUMN routes.subscriptions IS 'Subscriptions the remote has registered over this route.';
COMMENT ON COLUMN routes.account IS 'Account of a pinned per-account route; empty for pooled routes.';
COMMENT ON COLUMN routes.compression IS 'Route compression mode.';

CREATE TABLE gateways (
    epoch          BIGINT NOT NULL,
    server_id      VARCHAR NOT NULL,
    gateway        VARCHAR NOT NULL,
    direction      VARCHAR NOT NULL,
    is_configured  BOOLEAN,
    cid            BIGINT,
    ip             VARCHAR,
    port           INTEGER,
    rtt_ns         BIGINT,
    pending_bytes  BIGINT,
    in_msgs        BIGINT,
    out_msgs       BIGINT,
    in_bytes       BIGINT,
    out_bytes      BIGINT
);
COMMENT ON TABLE gateways IS 'Super-cluster gateway connections as each server reports them (GATEWAYZ). Empty without gateways.';
COMMENT ON COLUMN gateways.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN gateways.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN gateways.gateway IS 'Name of the remote gateway (cluster).';
COMMENT ON COLUMN gateways.direction IS 'outbound or inbound.';
COMMENT ON COLUMN gateways.is_configured IS 'True when the gateway is in the configuration rather than discovered.';
COMMENT ON COLUMN gateways.cid IS 'Connection ID on the reporting server.';
COMMENT ON COLUMN gateways.ip IS 'Remote IP address.';
COMMENT ON COLUMN gateways.port IS 'Remote port.';
COMMENT ON COLUMN gateways.rtt_ns IS 'Round trip time, nanoseconds.';
COMMENT ON COLUMN gateways.pending_bytes IS 'Bytes waiting to be written.';
COMMENT ON COLUMN gateways.in_msgs IS 'Messages received since the connection started.';
COMMENT ON COLUMN gateways.out_msgs IS 'Messages sent since the connection started.';
COMMENT ON COLUMN gateways.in_bytes IS 'Bytes received since the connection started.';
COMMENT ON COLUMN gateways.out_bytes IS 'Bytes sent since the connection started.';

CREATE TABLE leafnodes (
    epoch          BIGINT NOT NULL,
    server_id      VARCHAR NOT NULL,
    lid            BIGINT NOT NULL,
    name           VARCHAR,
    is_spoke       BOOLEAN,
    account        VARCHAR,
    ip             VARCHAR,
    port           INTEGER,
    rtt_ns         BIGINT,
    in_msgs        BIGINT,
    out_msgs       BIGINT,
    in_bytes       BIGINT,
    out_bytes      BIGINT,
    subscriptions  BIGINT,
    compression    VARCHAR
);
COMMENT ON TABLE leafnodes IS 'Leaf node connections as each server reports them (LEAFZ). Empty without leaf nodes.';
COMMENT ON COLUMN leafnodes.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN leafnodes.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN leafnodes.lid IS 'Leaf connection ID on the reporting server.';
COMMENT ON COLUMN leafnodes.name IS 'Server name of the remote leaf node.';
COMMENT ON COLUMN leafnodes.is_spoke IS 'True when the remote is a spoke (it connected to this hub).';
COMMENT ON COLUMN leafnodes.account IS 'Account the leaf connection is bound to.';
COMMENT ON COLUMN leafnodes.ip IS 'Remote IP address.';
COMMENT ON COLUMN leafnodes.port IS 'Remote port.';
COMMENT ON COLUMN leafnodes.rtt_ns IS 'Round trip time, nanoseconds.';
COMMENT ON COLUMN leafnodes.in_msgs IS 'Messages received since the connection started.';
COMMENT ON COLUMN leafnodes.out_msgs IS 'Messages sent since the connection started.';
COMMENT ON COLUMN leafnodes.in_bytes IS 'Bytes received since the connection started.';
COMMENT ON COLUMN leafnodes.out_bytes IS 'Bytes sent since the connection started.';
COMMENT ON COLUMN leafnodes.subscriptions IS 'Subscriptions over the leaf connection.';
COMMENT ON COLUMN leafnodes.compression IS 'Compression mode.';

CREATE TABLE ipqueues (
    epoch        BIGINT NOT NULL,
    server_id    VARCHAR NOT NULL,
    queue        VARCHAR NOT NULL,
    pending      BIGINT NOT NULL,
    in_progress  BIGINT NOT NULL
);
COMMENT ON TABLE ipqueues IS 'Server-internal queues that were not empty at the epoch (IPQUEUESZ). A queue that stays long here is a server falling behind.';
COMMENT ON COLUMN ipqueues.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN ipqueues.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN ipqueues.queue IS 'Internal queue name, for example a stream''s or raft group''s queue.';
COMMENT ON COLUMN ipqueues.pending IS 'Items waiting in the queue.';
COMMENT ON COLUMN ipqueues.in_progress IS 'Items being processed.';

CREATE TABLE health (
    epoch        BIGINT NOT NULL,
    server_id    VARCHAR NOT NULL,
    status       VARCHAR,
    status_code  INTEGER,
    error        VARCHAR,
    error_count  INTEGER NOT NULL,
    errors       JSON
);
COMMENT ON TABLE health IS 'HEALTHZ result with details, one row per server per epoch.';
COMMENT ON COLUMN health.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN health.server_id IS 'Reporting server ID (servers.server_id).';
COMMENT ON COLUMN health.status IS 'ok, or unavailable / na with an error.';
COMMENT ON COLUMN health.status_code IS 'HTTP-style status code; 200 when healthy.';
COMMENT ON COLUMN health.error IS 'First health error text.';
COMMENT ON COLUMN health.error_count IS 'Number of entries in errors.';
COMMENT ON COLUMN health.errors IS 'Detailed health errors: JSON array of {type, account, stream, consumer, error}.';
