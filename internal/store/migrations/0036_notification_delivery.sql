-- Pin a batch before the request, so retries after a crash keep its key.
ALTER TABLE notify_outbox ADD COLUMN batch_end BIGINT;
ALTER TABLE notify_outbox ADD COLUMN batch_at TIMESTAMP;
ALTER TABLE notify_outbox ADD COLUMN finished_at TIMESTAMP;
COMMENT ON COLUMN notify_outbox.batch_end IS 'Last outbox ID of the durable delivery batch; NULL before selection.';
COMMENT ON COLUMN notify_outbox.batch_at IS 'Stable sent_at of the durable delivery batch.';
COMMENT ON COLUMN notify_outbox.finished_at IS 'Time delivered, refused or given up; failures in the last hour use this, not queue age.';
