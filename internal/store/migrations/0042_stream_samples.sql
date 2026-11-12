-- One row per stream per epoch with the values the per-stream series,
-- rankings and growth read, and the stream's previous sample, so that a
-- rate over a range reads one row per stream and epoch instead of every
-- replica's report and a window over them. Written with the epoch;
-- epochs stored before this migration are filled in epoch order when the
-- collector opens the system's store, and a rebuild from the archive fills
-- them after loading. The tables are derived from archived rows and are
-- not archived themselves.

CREATE TABLE stream_samples (
    epoch         BIGINT NOT NULL,
    account       VARCHAR NOT NULL,
    stream        VARCHAR NOT NULL,
    bytes         BIGINT,
    messages      BIGINT,
    replica_bytes HUGEINT,
    seq           BIGINT,
    created       TIMESTAMP,
    prev_epoch    BIGINT,
    prev_seq      BIGINT,
    prev_created  TIMESTAMP
);
COMMENT ON TABLE stream_samples IS 'One row per (account, stream) and epoch in which any server reported the stream in stream_stats, with the values the per-stream series read and the stream''s previous sample. Written in the epoch''s transaction, or by the fill, only when every earlier epoch has its samples (stream_sample_epochs), so that the previous sample is the stream''s newest one before the epoch. Never changed afterwards. Swept with its epoch; not archived, as a rebuild computes it again.';
COMMENT ON COLUMN stream_samples.epoch IS 'The epoch (epochs.epoch).';
COMMENT ON COLUMN stream_samples.account IS 'Account of the stream.';
COMMENT ON COLUMN stream_samples.stream IS 'Stream name.';
COMMENT ON COLUMN stream_samples.bytes IS 'The leader''s bytes, else the largest any server reported.';
COMMENT ON COLUMN stream_samples.messages IS 'The leader''s messages, else the largest any server reported.';
COMMENT ON COLUMN stream_samples.replica_bytes IS 'The bytes of every server''s report summed.';
COMMENT ON COLUMN stream_samples.seq IS 'The leader''s last sequence, else the largest any server reported.';
COMMENT ON COLUMN stream_samples.created IS 'The creation time when every server reported one and all agree, else NULL.';
COMMENT ON COLUMN stream_samples.prev_epoch IS 'The newest earlier epoch with a sample of the stream; NULL when none was stored.';
COMMENT ON COLUMN stream_samples.prev_seq IS 'seq of the sample at prev_epoch.';
COMMENT ON COLUMN stream_samples.prev_created IS 'created of the sample at prev_epoch.';

CREATE TABLE stream_sample_heads (
    account VARCHAR NOT NULL,
    stream  VARCHAR NOT NULL,
    epoch   BIGINT NOT NULL,
    seq     BIGINT,
    created TIMESTAMP
);
COMMENT ON TABLE stream_sample_heads IS 'The newest sample of each stream in stream_samples, from which the next epoch''s samples take their previous sample. Replaced for the streams of each epoch whose samples are written; a row whose epoch is swept goes with it. Not archived.';
COMMENT ON COLUMN stream_sample_heads.account IS 'Account of the stream.';
COMMENT ON COLUMN stream_sample_heads.stream IS 'Stream name.';
COMMENT ON COLUMN stream_sample_heads.epoch IS 'The epoch of the stream''s newest sample.';
COMMENT ON COLUMN stream_sample_heads.seq IS 'seq of that sample.';
COMMENT ON COLUMN stream_sample_heads.created IS 'created of that sample.';

CREATE TABLE stream_sample_epochs (
    epoch BIGINT PRIMARY KEY
);
COMMENT ON TABLE stream_sample_epochs IS 'The epochs whose stream samples are written, with or without a stream. Always every stored epoch up to some epoch: an epoch gets its samples only when every earlier one has them. A reader uses stream_samples for a range only when every epoch from the range''s first to its last is here. Swept with its epoch; not archived.';
COMMENT ON COLUMN stream_sample_epochs.epoch IS 'The epoch (epochs.epoch).';
