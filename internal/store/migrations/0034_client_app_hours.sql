-- The hourly traffic of each client application, summed from the
-- connections' counters as each epoch is written.

CREATE TABLE client_app_hours (
    hour       TIMESTAMP NOT NULL,
    account    VARCHAR NOT NULL,
    app        VARCHAR NOT NULL,
    loopback   BOOLEAN NOT NULL,
    in_msgs    BIGINT NOT NULL,
    out_msgs   BIGINT NOT NULL,
    in_bytes   BIGINT NOT NULL,
    out_bytes  BIGINT NOT NULL,
    epochs     INTEGER NOT NULL
);
CREATE UNIQUE INDEX client_app_hours_key ON client_app_hours (hour, account, app, loopback);
COMMENT ON TABLE client_app_hours IS 'Per hour, the messages and bytes each client application sent and received, summed over its connections: the increase of the connections counters between each row and the same connection''s previous row (same server_id, cid and start_time) within the hour before, added to the hour of the later row''s epoch as each epoch is written. A counter that fell counts the new values whole. A connection''s first row counts its values when it started after the previous epoch started, else nothing, so a system seen for the first time adds no history. Traffic of a connection that closed between two collections after its last row is lost, and a server whose list was cut at connz_limit adds only its listed connections. Swept with the hours wholly before the retention window (now minus retention) and archived by hour.';
COMMENT ON COLUMN client_app_hours.hour IS 'Start of the hour, in UTC, of the epochs whose increases are summed (epochs.started_at truncated to the hour). An increase over a gap between two rows is added whole to the later row''s hour.';
COMMENT ON COLUMN client_app_hours.account IS 'Account of the connections (connections.account); empty for the global account $G.';
COMMENT ON COLUMN client_app_hours.app IS 'The application: the connection name before the first "@" (the whole name when it has none, as nats-lens''s own connection names), (unnamed) for a connection without a name or with nothing before the "@".';
COMMENT ON COLUMN client_app_hours.loopback IS 'True for the connections from a loopback address (127.0.0.1 or ::1), kept apart so that a reader can leave them out as tooling while the epoch''s loopback decision holds.';
COMMENT ON COLUMN client_app_hours.in_msgs IS 'Messages the application''s connections sent to the servers in the hour (the increase of connections.in_msgs).';
COMMENT ON COLUMN client_app_hours.out_msgs IS 'Messages the servers delivered to the application''s connections in the hour (the increase of connections.out_msgs).';
COMMENT ON COLUMN client_app_hours.in_bytes IS 'Bytes the application''s connections sent to the servers in the hour (the increase of connections.in_bytes).';
COMMENT ON COLUMN client_app_hours.out_bytes IS 'Bytes the servers delivered to the application''s connections in the hour (the increase of connections.out_bytes).';
COMMENT ON COLUMN client_app_hours.epochs IS 'Epochs in the hour that added to the row: those with at least one of its connections paired with a previous row or counted from its first row. A connection first seen without history adds nothing and does not count.';
