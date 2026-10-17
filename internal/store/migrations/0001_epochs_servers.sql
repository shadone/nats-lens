-- Epochs, server identity and per-server statistics.

CREATE SEQUENCE epoch_seq START 1;

CREATE TABLE epochs (
    epoch             BIGINT PRIMARY KEY,
    started_at        TIMESTAMP NOT NULL,
    finished_at       TIMESTAMP NOT NULL,
    duration_ms       BIGINT NOT NULL,
    expected_servers  INTEGER NOT NULL,
    observed_servers  INTEGER NOT NULL,
    expected_names    VARCHAR[] NOT NULL,
    observed_names    VARCHAR[] NOT NULL,
    missing_names     VARCHAR[] NOT NULL,
    endpoint_failures INTEGER NOT NULL,
    partial           BOOLEAN NOT NULL,
    errors            VARCHAR[] NOT NULL
);
COMMENT ON TABLE epochs IS 'One row per snapshot (epoch) of the NATS system. Every stats row references an epoch.';
COMMENT ON COLUMN epochs.epoch IS 'Epoch number, increasing by one per snapshot; never reused.';
COMMENT ON COLUMN epochs.started_at IS 'UTC time the collector started the snapshot.';
COMMENT ON COLUMN epochs.finished_at IS 'UTC time the collector finished gathering replies.';
COMMENT ON COLUMN epochs.duration_ms IS 'finished_at minus started_at, in milliseconds.';
COMMENT ON COLUMN epochs.expected_servers IS 'Number of server names expected to answer: recently seen servers, servers that answered, and the route and JetStream meta peers they report.';
COMMENT ON COLUMN epochs.observed_servers IS 'Number of servers that answered at least one request in this epoch.';
COMMENT ON COLUMN epochs.expected_names IS 'Server names expected to answer, sorted.';
COMMENT ON COLUMN epochs.observed_names IS 'Server names that answered, sorted.';
COMMENT ON COLUMN epochs.missing_names IS 'Expected server names that did not answer any request, sorted.';
COMMENT ON COLUMN epochs.endpoint_failures IS 'Number of (server, endpoint) pairs in epoch_endpoints whose state is not ok.';
COMMENT ON COLUMN epochs.partial IS 'True when a server is missing, an endpoint did not answer or returned an error, or the collector itself failed. A partial epoch is stored, not dropped.';
COMMENT ON COLUMN epochs.errors IS 'Errors of the collector itself (publish failed, connection lost, undecodable reply).';

CREATE TABLE epoch_endpoints (
    epoch       BIGINT NOT NULL,
    server_id   VARCHAR NOT NULL,
    endpoint    VARCHAR NOT NULL,
    state       VARCHAR NOT NULL,
    error       VARCHAR
);
COMMENT ON TABLE epoch_endpoints IS 'Outcome of every monitoring request for every server that answered in an epoch. Servers that answered nothing are listed in epochs.missing_names instead.';
COMMENT ON COLUMN epoch_endpoints.epoch IS 'Epoch number (epochs.epoch).';
COMMENT ON COLUMN epoch_endpoints.server_id IS 'Server ID (servers.server_id). Changes on every server restart.';
COMMENT ON COLUMN epoch_endpoints.endpoint IS 'Monitoring endpoint: STATSZ, VARZ, CONNZ, ROUTEZ, GATEWAYZ, LEAFZ, ACCOUNTZ, ACCSTATZ (account STATZ), JSZ, HEALTHZ, RAFTZ, IPQUEUESZ.';
COMMENT ON COLUMN epoch_endpoints.state IS 'ok: replied with data; error: replied with an error or an undecodable body; missing: no reply within the collect window.';
COMMENT ON COLUMN epoch_endpoints.error IS 'Error text when state is error.';

CREATE TABLE servers (
    server_id     VARCHAR PRIMARY KEY,
    server_name   VARCHAR NOT NULL,
    host          VARCHAR,
    cluster       VARCHAR,
    domain        VARCHAR,
    version       VARCHAR,
    jetstream     BOOLEAN,
    tags          VARCHAR[],
    start_time    TIMESTAMP,
    first_epoch   BIGINT NOT NULL,
    last_epoch    BIGINT NOT NULL,
    first_seen_at TIMESTAMP NOT NULL,
    last_seen_at  TIMESTAMP NOT NULL
);
COMMENT ON TABLE servers IS 'Server identity. A server gets a new server_id on every restart, so one server_name has one row per process lifetime. Join stats tables on server_id to get the name.';
COMMENT ON COLUMN servers.server_id IS 'Server ID as reported by the server; unique per process start.';
COMMENT ON COLUMN servers.server_name IS 'Configured server name, stable across restarts (for example nats-0).';
COMMENT ON COLUMN servers.host IS 'Host the server reports it listens on.';
COMMENT ON COLUMN servers.cluster IS 'Cluster name.';
COMMENT ON COLUMN servers.domain IS 'JetStream domain, empty when none.';
COMMENT ON COLUMN servers.version IS 'nats-server version.';
COMMENT ON COLUMN servers.jetstream IS 'Whether JetStream is enabled on the server.';
COMMENT ON COLUMN servers.tags IS 'Server tags from its configuration.';
COMMENT ON COLUMN servers.start_time IS 'UTC time the server process started (from VARZ or STATSZ).';
COMMENT ON COLUMN servers.first_epoch IS 'First epoch this server_id answered in.';
COMMENT ON COLUMN servers.last_epoch IS 'Latest epoch this server_id answered in.';
COMMENT ON COLUMN servers.first_seen_at IS 'started_at of first_epoch.';
COMMENT ON COLUMN servers.last_seen_at IS 'started_at of last_epoch.';
