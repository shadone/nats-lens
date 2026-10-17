-- The configuration hash no longer includes the creation time; a new
-- creation time still starts a new run. Rows written before this migration
-- keep their old hashes until their run ends.

COMMENT ON COLUMN stream_config.config_hash IS 'Hash of the full configuration, without the creation time; equal hashes mean equal configuration. A new run starts when the hash or the creation time changes.';
COMMENT ON COLUMN consumer_config.config_hash IS 'Hash of the full configuration, without the creation time; equal hashes mean equal configuration. A new run starts when the hash or the creation time changes.';
