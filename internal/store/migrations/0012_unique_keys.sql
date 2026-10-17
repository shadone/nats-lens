-- Unique keys on the tables where a duplicate row would be a writer bug and
-- the index stays small. The large per-entity stats tables (connections,
-- stream_stats, consumer_stats, raft_stats and the replica tables) get no
-- index: one row per entity per server per epoch is kept by the writer, and
-- an index there would cost memory and insert time on every epoch.

CREATE UNIQUE INDEX epoch_endpoints_key ON epoch_endpoints (epoch, server_id, endpoint);
CREATE UNIQUE INDEX server_stats_key ON server_stats (epoch, server_id);
CREATE UNIQUE INDEX health_key ON health (epoch, server_id);
CREATE UNIQUE INDEX stream_config_key ON stream_config (server_id, account, stream, first_epoch);
CREATE UNIQUE INDEX consumer_config_key ON consumer_config (server_id, account, stream, consumer, first_epoch);
