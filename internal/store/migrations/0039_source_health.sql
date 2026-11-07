-- Individual configured sources, including configurations with no runtime report.
CREATE TABLE stream_source_stats (
    epoch BIGINT NOT NULL,
    server_id VARCHAR NOT NULL,
    account VARCHAR NOT NULL,
    stream VARCHAR NOT NULL,
    source_id VARCHAR NOT NULL,
    name VARCHAR NOT NULL,
    filter_subject VARCHAR NOT NULL DEFAULT '',
    config JSON NOT NULL,
    reported BOOLEAN NOT NULL DEFAULT false,
    lag UBIGINT,
    active_ns BIGINT,
    error_code INTEGER,
    error_id INTEGER,
    error VARCHAR
);
CREATE UNIQUE INDEX stream_source_stats_key ON stream_source_stats (epoch, server_id, account, stream, source_id);
COMMENT ON TABLE stream_source_stats IS 'One row per configured source per JSZ stream report. Joins stream_stats on epoch, server_id, account, stream. Only the leader runs sources; follower and absent or ambiguous runtime reports have reported=false and NULL runtime fields. No row means the configuration did not list this source, or the stream/configuration was not observed; it does not mean healthy.';
COMMENT ON COLUMN stream_source_stats.source_id IS 'SHA256 of the complete source configuration JSON, independent of list position. Includes external prefixes, filters, transforms, starting position and durable consumer. Join stream_stats.created too when comparing history across stream incarnations.';
COMMENT ON COLUMN stream_source_stats.config IS 'Complete configured source from JSZ; no message content. Runtime identity omits durable consumer and starting position: ambiguous matches are not assigned.';
COMMENT ON COLUMN stream_source_stats.reported IS 'True only for a unique runtime identity match on the leader. False is unknown, never a clean error report or never-heard activity.';
COMMENT ON COLUMN stream_source_stats.lag IS 'Exact unsigned messages behind the origin in this source report; NULL when not reported.';
COMMENT ON COLUMN stream_source_stats.active_ns IS 'Nanoseconds since last data or heartbeat; negative means never heard on this leader. NULL means no attributable runtime report.';
COMMENT ON COLUMN stream_source_stats.error_code IS 'HTTP-style API error code; NULL when the reported source has no error or has no report.';
COMMENT ON COLUMN stream_source_stats.error_id IS 'NATS API error identifier (err_code); NULL without an error.';
COMMENT ON COLUMN stream_source_stats.error IS 'API error description; NULL without an error. NATS clears this error on every retry, so one clean report does not prove recovery.';
COMMENT ON COLUMN stream_source_stats.epoch IS 'Collection epoch (epochs.epoch).';
COMMENT ON COLUMN stream_source_stats.server_id IS 'Reporting server (servers.server_id); join stream_stats to determine leadership.';
COMMENT ON COLUMN stream_source_stats.account IS 'Owning account identifier.';
COMMENT ON COLUMN stream_source_stats.stream IS 'Destination stream name.';
COMMENT ON COLUMN stream_source_stats.name IS 'Origin stream name; not unique across external domains, filters or transforms.';
COMMENT ON COLUMN stream_source_stats.filter_subject IS 'Configured source filter; the complete transform and external configuration is in config.';

-- Existing archived stream_stats files omit this column; their source
-- configuration is unknown, rather than an observed empty source list.
ALTER TABLE stream_stats ADD COLUMN source_config_known BOOLEAN DEFAULT false;
COMMENT ON COLUMN stream_stats.source_config_known IS 'JSZ supplied a stream configuration in this report. False means source membership was not observed, not that the source list was empty. Files archived before 0039 rebuild with false.';
