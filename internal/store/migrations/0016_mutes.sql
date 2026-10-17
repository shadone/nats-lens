-- Mutes: an operator's decision to keep a check, or a check for one
-- entity, out of the alerting set until an expiry.

CREATE SEQUENCE mute_seq START 1;

CREATE TABLE mutes (
    id           BIGINT PRIMARY KEY,
    check_code   VARCHAR NOT NULL,
    entity_type  VARCHAR,
    entity_key   VARCHAR,
    reason       VARCHAR NOT NULL,
    muted_by     VARCHAR NOT NULL,
    created_at   TIMESTAMP NOT NULL,
    expires_at   TIMESTAMP NOT NULL,
    unmuted_at   TIMESTAMP,
    unmuted_by   VARCHAR,
    CHECK ((entity_type IS NULL) = (entity_key IS NULL)),
    CHECK (expires_at > created_at)
);
COMMENT ON TABLE mutes IS 'Mutes of checks or of a check for one entity. A muted finding still records transitions but is shown apart and never alerts. Rows are kept after expiry and unmute as history; a mute applies while unmuted_at is null and expires_at is in the future.';
COMMENT ON COLUMN mutes.id IS 'Mute ID, from mute_seq.';
COMMENT ON COLUMN mutes.check_code IS 'Code of the muted check, for example stream-031.';
COMMENT ON COLUMN mutes.entity_type IS 'Entity type the mute is limited to: server, account, stream, consumer, connection, client or system; null when the mute covers every entity of the check.';
COMMENT ON COLUMN mutes.entity_key IS 'Canonical key of the entity (server name, account/stream, account/stream/consumer, ...); null when the mute covers every entity of the check.';
COMMENT ON COLUMN mutes.reason IS 'Why the finding is muted, as the operator wrote it.';
COMMENT ON COLUMN mutes.muted_by IS 'Who muted: the IAP user when the request came through IAP, otherwise the name the request gave.';
COMMENT ON COLUMN mutes.created_at IS 'UTC time the mute was created.';
COMMENT ON COLUMN mutes.expires_at IS 'UTC time the mute stops applying. Every mute has one.';
COMMENT ON COLUMN mutes.unmuted_at IS 'UTC time the mute was lifted before its expiry; null otherwise.';
COMMENT ON COLUMN mutes.unmuted_by IS 'Who lifted the mute.';
