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

-- Older stores have no precise refusal/give-up time. Use their known delivery
-- time, otherwise queue time, rather than inventing a fresh outcome on upgrade.
UPDATE notify_outbox SET finished_at=coalesce(delivered_at,created_at) WHERE state <> 'pending';
CREATE TABLE notify_delivery_rate (
    channel VARCHAR PRIMARY KEY,
    last_finished_at TIMESTAMP NOT NULL,
    last_success_epoch BIGINT
);
COMMENT ON TABLE notify_delivery_rate IS 'Per-channel delivery watermarks, independent of outbox retention and not archived. Removed only when the channel is removed.';
COMMENT ON COLUMN notify_delivery_rate.channel IS 'Configured channel name, with no destination or credentials.';
COMMENT ON COLUMN notify_delivery_rate.last_finished_at IS 'Latest terminal batch outcome; bounds new batches across restarts and retention sweeps even with long collection intervals.';
COMMENT ON COLUMN notify_delivery_rate.last_success_epoch IS 'Latest collection at successful delivery, not the epoch that queued the message; NULL before success or before any collection.';
INSERT INTO notify_delivery_rate
SELECT channel,max(finished_at),
 (SELECT max(epoch) FROM epochs WHERE started_at <= max(o.finished_at) FILTER (WHERE o.state='delivered'))
FROM notify_outbox o WHERE state <> 'pending' GROUP BY channel;
COMMENT ON TABLE notify_outbox IS 'Durable ordered messages. Terminal rows expire after the shorter of seven days and retention, measured from the outcome, with at least one hour retained for reporting. Pending rows never expire in the sweep. Rate watermarks outlive retained rows.';
