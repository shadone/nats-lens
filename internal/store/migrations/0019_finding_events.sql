-- Finding transitions are also written as events, so that the event list
-- and the timeline's event lane show them with the NATS events.

COMMENT ON TABLE events IS 'One row per captured event: client connects and disconnects, server lame duck and shutdown, authentication errors, JetStream advisories and metrics, the collector''s own gaps and connection changes, and the transitions of findings. Events are kept as long as epochs (retention).';
COMMENT ON COLUMN events.source IS 'sys: system account event; jetstream: JetStream advisory or metric; collector: written by the collector about itself (gaps, its own connections, dropped messages); finding: a finding''s transition, written with it by the checks.';
COMMENT ON COLUMN events.kind IS 'Short normalized kind, for example client.disconnect, consumer.deleted, stream.leader_elected, server.shutdown, collector.gap, finding.firing, finding.changed, finding.resolved.';
