-- Asset counts per epoch, written with the epoch, so that the Overview's
-- counts and asset trends read one row per epoch instead of every stream
-- and consumer report of the range. Epochs stored before this migration
-- are filled in batches when the collector opens the system's store,
-- after setting its thread count and memory limit; a rebuild from the
-- archive fills them after loading. The table is derived from archived
-- rows and is not archived itself.

CREATE TABLE epoch_asset_counts (
    epoch             BIGINT PRIMARY KEY,
    streams           BIGINT NOT NULL,
    kv_buckets        BIGINT NOT NULL,
    object_stores     BIGINT NOT NULL,
    durable_consumers BIGINT NOT NULL,
    assets_replicated BIGINT NOT NULL,
    assets_single     BIGINT NOT NULL
);
COMMENT ON TABLE epoch_asset_counts IS 'One row per epoch: the streams, KV buckets, object stores and durable consumers any server reported in it, and the streams and durable consumers by configured replicas. Written in the epoch''s transaction from its stream_stats, consumer_stats, stream_config and consumer_config rows, and never changed afterwards. Counts every epoch, also those whose JSZ replies are incomplete: readers leave those out where a count would dip. Swept with its epoch; not archived, as a rebuild computes it again from the archived rows.';
COMMENT ON COLUMN epoch_asset_counts.epoch IS 'The epoch (epochs.epoch).';
COMMENT ON COLUMN epoch_asset_counts.streams IS 'Distinct (account, stream) in stream_stats whose name starts with neither KV_ nor OBJ_.';
COMMENT ON COLUMN epoch_asset_counts.kv_buckets IS 'Distinct (account, stream) in stream_stats whose name starts with KV_.';
COMMENT ON COLUMN epoch_asset_counts.object_stores IS 'Distinct (account, stream) in stream_stats whose name starts with OBJ_.';
COMMENT ON COLUMN epoch_asset_counts.durable_consumers IS 'Distinct (account, stream, consumer) in consumer_stats with a consumer_config row of the same server covering the epoch whose durable_name is not empty.';
COMMENT ON COLUMN epoch_asset_counts.assets_replicated IS 'Streams and durable consumers whose configured replicas, the largest any reporting server''s configuration gives, is above 1. A consumer with replicas 0 has its stream''s count; one whose count is unknown is in neither this nor assets_single.';
COMMENT ON COLUMN epoch_asset_counts.assets_single IS 'Streams and durable consumers whose configured replicas, as for assets_replicated, is 1.';
