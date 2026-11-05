-- Pin a batch before the request, so retries after a crash keep its key.
ALTER TABLE notify_outbox ADD COLUMN batch_end BIGINT;
ALTER TABLE notify_outbox ADD COLUMN batch_at TIMESTAMP;
ALTER TABLE notify_outbox ADD COLUMN finished_at TIMESTAMP;
COMMENT ON COLUMN notify_outbox.batch_end IS 'Last outbox ID of the durable delivery batch; NULL before selection.';
COMMENT ON COLUMN notify_outbox.batch_at IS 'Stable sent_at of the durable delivery batch.';
COMMENT ON COLUMN notify_outbox.finished_at IS 'Time delivered, refused or given up; failures in the last hour use this, not queue age.';

-- Local delivery identity is deliberately not archived. An archive rebuild
-- creates a new identity even when its outbox sequence starts at one again.
CREATE TABLE notify_identity (
    singleton BOOLEAN PRIMARY KEY CHECK (singleton),
    incarnation UUID NOT NULL
);
INSERT INTO notify_identity VALUES (true, uuid());
COMMENT ON TABLE notify_identity IS 'Database incarnation for delivery idempotency, retained on reopen and regenerated on archive rebuild. Contains no configuration or secret.';
COMMENT ON COLUMN notify_identity.singleton IS 'Exactly one local database identity.';
COMMENT ON COLUMN notify_identity.incarnation IS 'Random UUID, unrelated to destination URLs, credentials or deployment configuration.';
