-- Notifications: what each channel was last told about each finding, the
-- channels that have had their baseline, and the messages queued for
-- sending. None of these tables is archived: what was sent to a receiver
-- is not something NATS showed, and after a rebuild every channel starts
-- from a new baseline.

CREATE TABLE notify_channels (
    channel           VARCHAR PRIMARY KEY,
    baselined_at      TIMESTAMP NOT NULL,
    baselined_epoch   BIGINT NOT NULL,
    reconciled_at     TIMESTAMP NOT NULL,
    reconciled_epoch  BIGINT NOT NULL
);
COMMENT ON TABLE notify_channels IS 'One row per notification channel (notifications.channels[].name) that receives this system and has had its baseline: the first reconcile for a channel without a row records the findings firing then and queues one baseline message instead of one message per finding. A channel no longer configured loses its row, its notify_state rows and its pending messages at the next retention sweep.';
COMMENT ON COLUMN notify_channels.channel IS 'Channel name, as in the configuration file.';
COMMENT ON COLUMN notify_channels.baselined_at IS 'Start time of the epoch whose reconcile took the baseline.';
COMMENT ON COLUMN notify_channels.baselined_epoch IS 'Epoch whose reconcile took the baseline (epochs.epoch).';
COMMENT ON COLUMN notify_channels.reconciled_at IS 'Start time of the epoch last reconciled for the channel: a mute that ended after it and by the epoch reconciled next is named in the firing message as mute ended.';
COMMENT ON COLUMN notify_channels.reconciled_epoch IS 'Epoch last reconciled for the channel; a reconcile of this epoch or an older one changes nothing for it.';

CREATE TABLE notify_state (
    channel      VARCHAR NOT NULL,
    finding_id   VARCHAR NOT NULL,
    since_epoch  BIGINT NOT NULL,
    severity     VARCHAR NOT NULL,
    state        VARCHAR NOT NULL,
    sent_at      TIMESTAMP NOT NULL,
    epoch        BIGINT NOT NULL,
    PRIMARY KEY (channel, finding_id),
    CHECK (state IN ('firing', 'flapping')),
    CHECK (severity IN ('info', 'warning', 'critical'))
);
COMMENT ON TABLE notify_state IS 'What each channel was last told about each finding in its view: one row per (channel, finding) while the channel knows the finding as firing or flapping. The row goes once the finding''s resolved or muted message is queued. Each reconcile compares the channel''s view at the epoch with these rows. Rows of findings the retention sweep removed are deleted with the sweep.';
COMMENT ON COLUMN notify_state.channel IS 'Channel name (notify_channels.channel).';
COMMENT ON COLUMN notify_state.finding_id IS 'Finding ID (findings.id).';
COMMENT ON COLUMN notify_state.since_epoch IS 'Episode the channel was told about: findings.since_epoch when the last message was queued.';
COMMENT ON COLUMN notify_state.severity IS 'Severity the channel was last sent.';
COMMENT ON COLUMN notify_state.state IS 'firing: the channel was sent firing, changed or reminder; flapping: it was sent flapping, and the finding''s firing, changed and resolved are held back while it flaps.';
COMMENT ON COLUMN notify_state.sent_at IS 'Start time of the epoch whose reconcile queued the last message about the finding for the channel, or took the baseline; a reminder is due repeat_every after it.';
COMMENT ON COLUMN notify_state.epoch IS 'Epoch whose reconcile queued that message (epochs.epoch).';

CREATE SEQUENCE notify_outbox_seq START 1;

CREATE TABLE notify_outbox (
    id               BIGINT PRIMARY KEY,
    channel          VARCHAR NOT NULL,
    created_at       TIMESTAMP NOT NULL,
    epoch            BIGINT NOT NULL,
    kind             VARCHAR NOT NULL,
    finding_id       VARCHAR,
    dedup_key        VARCHAR NOT NULL,
    message          JSON NOT NULL,
    state            VARCHAR NOT NULL DEFAULT 'pending',
    attempts         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMP,
    delivered_at     TIMESTAMP,
    last_status      INTEGER,
    last_error       VARCHAR,
    CHECK (state IN ('pending', 'delivered', 'refused', 'failed'))
);
COMMENT ON TABLE notify_outbox IS 'One row per message queued for a channel, in the order it is to be sent (id). A restart loses none: pending rows are sent by the next natslens run. Delivered, refused and failed rows are deleted by the retention sweep once older than 7 days or than retention, whichever is shorter.';
COMMENT ON COLUMN notify_outbox.id IS 'Message ID, from notify_outbox_seq; the order messages of a channel are sent in.';
COMMENT ON COLUMN notify_outbox.channel IS 'Channel name (notify_channels.channel).';
COMMENT ON COLUMN notify_outbox.created_at IS 'Start time of the epoch whose reconcile queued the message.';
COMMENT ON COLUMN notify_outbox.epoch IS 'Epoch whose reconcile queued the message (epochs.epoch).';
COMMENT ON COLUMN notify_outbox.kind IS 'Message kind: firing, changed, resolved, flapping, muted, reminder or baseline.';
COMMENT ON COLUMN notify_outbox.finding_id IS 'Finding the message is about (findings.id); NULL for a baseline message.';
COMMENT ON COLUMN notify_outbox.dedup_key IS 'The message''s dedup_key: system, finding ID, episode start epoch, kind and epoch, the same at every attempt.';
COMMENT ON COLUMN notify_outbox.message IS 'The message as JSON (notify.Message) without its link, which is built when it is sent so that a changed notifications.ui_url applies. Holds no secret.';
COMMENT ON COLUMN notify_outbox.state IS 'pending: not yet delivered; delivered: a 2xx answer; refused: a 4xx other than 408 and 429; failed: given up after the channel''s give_up_after.';
COMMENT ON COLUMN notify_outbox.attempts IS 'Requests made with the message so far.';
COMMENT ON COLUMN notify_outbox.next_attempt_at IS 'UTC time the next attempt is due after a failure; NULL before the first attempt.';
COMMENT ON COLUMN notify_outbox.delivered_at IS 'UTC time of the 2xx answer; NULL until delivered.';
COMMENT ON COLUMN notify_outbox.last_status IS 'HTTP status of the latest attempt; NULL before the first or when no answer came.';
COMMENT ON COLUMN notify_outbox.last_error IS 'Error of the latest failed attempt, without the URL or any secret; NULL otherwise.';
