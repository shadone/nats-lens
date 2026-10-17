-- Events: what the snapshots miss, from system account events, JetStream
-- advisories and the collector itself.

CREATE SEQUENCE event_seq START 1;

CREATE TABLE events (
    id           BIGINT PRIMARY KEY,
    time         TIMESTAMP NOT NULL,
    received_at  TIMESTAMP NOT NULL,
    source       VARCHAR NOT NULL,
    type         VARCHAR,
    kind         VARCHAR NOT NULL,
    server_name  VARCHAR,
    account      VARCHAR,
    stream       VARCHAR,
    consumer     VARCHAR,
    client_name  VARCHAR,
    client_host  VARCHAR,
    client_user  VARCHAR,
    client_lang  VARCHAR,
    reason       VARCHAR,
    summary      VARCHAR NOT NULL,
    raw          JSON
);
CREATE INDEX events_time ON events (time);
CREATE INDEX events_stream_consumer ON events (stream, consumer);
COMMENT ON TABLE events IS 'One row per captured event: client connects and disconnects, server lame duck and shutdown, authentication errors, JetStream advisories and metrics, and the collector''s own gaps and connection changes. Events are kept as long as epochs (retention).';
COMMENT ON COLUMN events.id IS 'Event number, increasing in the order events were written; never reused.';
COMMENT ON COLUMN events.time IS 'UTC time the event happened: the payload''s timestamp when it has one, otherwise received_at. For collector.gap, the start of the gap.';
COMMENT ON COLUMN events.received_at IS 'UTC time the collector received the event. The hourly archive files events by this time.';
COMMENT ON COLUMN events.source IS 'sys: system account event; jetstream: JetStream advisory or metric; collector: written by the collector about itself (gaps, its own connections, dropped messages).';
COMMENT ON COLUMN events.type IS 'Type string of the payload, for example io.nats.jetstream.advisory.v1.consumer_action; null for payloads without one (lame duck, shutdown) and collector events.';
COMMENT ON COLUMN events.kind IS 'Short normalized kind, for example client.disconnect, consumer.deleted, stream.leader_elected, server.shutdown, collector.gap.';
COMMENT ON COLUMN events.server_name IS 'Server the event is about or came from: the sending server for system events, the new leader for leader elections, the collector''s connected server for collector events.';
COMMENT ON COLUMN events.account IS 'Account the event concerns: the client''s account for connection events, the configured account label for JetStream advisories unless the advisory names its own account.';
COMMENT ON COLUMN events.stream IS 'Stream the event concerns.';
COMMENT ON COLUMN events.consumer IS 'Consumer the event concerns.';
COMMENT ON COLUMN events.client_name IS 'Connection name of the client involved. For stream and consumer created, updated and deleted, the client that made the API call, taken from the matching API audit advisory; null when the server acted on its own (for example an inactive consumer removed).';
COMMENT ON COLUMN events.client_host IS 'IP address of the client involved.';
COMMENT ON COLUMN events.client_user IS 'User the client authenticated as.';
COMMENT ON COLUMN events.client_lang IS 'Client library language and version, for example go 1.54.0.';
COMMENT ON COLUMN events.reason IS 'Reason given: the disconnect reason (for example Slow Consumer (Write Deadline)), a termination reason, an API error, or the collector''s connection error.';
COMMENT ON COLUMN events.summary IS 'One human-readable line describing the event.';
COMMENT ON COLUMN events.raw IS 'Payload as received, with user JWTs removed from client objects.';
