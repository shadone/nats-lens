-- Configuration runs break when a server did not report the asset.

COMMENT ON TABLE stream_config IS 'Stream configuration as each server reports it, one row per run of consecutive epochs in which that server reported the same configuration. A run ends when the configuration changes or when the server reports nothing for the stream (server missing, JSZ missing, stream gone), so a run never covers an epoch without a report. To compare servers at epoch E, use only rows with first_epoch <= E AND last_epoch >= E: rows of a restarted server (an old server_id) or a server that stopped reporting end at their last report and must not be read as current.';
COMMENT ON COLUMN stream_config.first_epoch IS 'First epoch of the run.';
COMMENT ON COLUMN stream_config.last_epoch IS 'Last epoch of the run; the server reported this configuration in every epoch from first_epoch to last_epoch.';
COMMENT ON TABLE consumer_config IS 'Consumer configuration as each server reports it, one row per run of consecutive epochs in which that server reported the same configuration. A run ends when the configuration changes or when the server reports nothing for the consumer, so a run never covers an epoch without a report. To compare servers at epoch E, use only rows with first_epoch <= E AND last_epoch >= E.';
COMMENT ON COLUMN consumer_config.first_epoch IS 'First epoch of the run.';
COMMENT ON COLUMN consumer_config.last_epoch IS 'Last epoch of the run; the server reported this configuration in every epoch from first_epoch to last_epoch.';
