-- Acknowledgements: who works on one episode of a finding, with a note and
-- a quiet time. An acknowledgement is not a mute: the finding stays shown
-- and counted where it is, and the acknowledgement only says that someone
-- is on it.

CREATE SEQUENCE ack_seq START 1;

CREATE TABLE acknowledgements (
    id           BIGINT PRIMARY KEY,
    finding_id   VARCHAR NOT NULL,
    check_code   VARCHAR NOT NULL,
    entity_type  VARCHAR NOT NULL,
    entity_key   VARCHAR NOT NULL,
    since_epoch  BIGINT NOT NULL,
    note         VARCHAR NOT NULL DEFAULT '',
    acked_by     VARCHAR NOT NULL,
    created_at   TIMESTAMP NOT NULL,
    quiet_until  TIMESTAMP,
    ended_at     TIMESTAMP,
    ended_by     VARCHAR,
    end_reason   VARCHAR,
    CHECK (end_reason IN ('released', 'replaced'))
);
COMMENT ON TABLE acknowledgements IS 'Acknowledgements of findings: a person says they work on one episode of a firing finding, with a note and a quiet time. An acknowledgement is in force while the finding fires with the same since_epoch and ended_at is null; it is matched to the finding when findings are read, so a resolve or a new episode ends it without a write. Unlike a mute it hides nothing. The retention sweep deletes an acknowledgement once it was ended, or its episode ended, before the oldest kept epoch.';
COMMENT ON COLUMN acknowledgements.id IS 'Acknowledgement ID, from ack_seq.';
COMMENT ON COLUMN acknowledgements.finding_id IS 'Finding acknowledged (findings.id).';
COMMENT ON COLUMN acknowledgements.check_code IS 'Check code of the finding, for example consumer-010.';
COMMENT ON COLUMN acknowledgements.entity_type IS 'Entity type of the finding: server, account, stream, consumer, connection, client or system.';
COMMENT ON COLUMN acknowledgements.entity_key IS 'Canonical key of the finding''s entity; empty for the system.';
COMMENT ON COLUMN acknowledgements.since_epoch IS 'The episode acknowledged: findings.since_epoch when the acknowledgement was made.';
COMMENT ON COLUMN acknowledgements.note IS 'What the person wrote, at most 500 characters; empty when they wrote nothing.';
COMMENT ON COLUMN acknowledgements.acked_by IS 'Who acknowledged: the IAP user when the request came through IAP, otherwise the name the request gave.';
COMMENT ON COLUMN acknowledgements.created_at IS 'UTC time the acknowledgement was made.';
COMMENT ON COLUMN acknowledgements.quiet_until IS 'UTC time the quiet ends: until then nats_lens_finding_acknowledged is 1 for the finding. NULL for a quiet until the finding resolves.';
COMMENT ON COLUMN acknowledgements.ended_at IS 'UTC time a person ended the acknowledgement or a newer one of the same episode replaced it; NULL otherwise, also after the episode ended.';
COMMENT ON COLUMN acknowledgements.ended_by IS 'Who ended it: the person who handed it back, or who made the acknowledgement that replaced it.';
COMMENT ON COLUMN acknowledgements.end_reason IS 'released: a person handed it back; replaced: a newer acknowledgement of the same episode took its place. NULL while not ended.';

ALTER TABLE archive_hours ADD COLUMN ack_changes BIGINT;
COMMENT ON COLUMN archive_hours.ack_changes IS 'Fingerprint of the acknowledgements when the hour was exported: the bit_xor of hash(id, ended_at) over every row, shifted right by one bit. It changes when an acknowledgement is made, ended or swept. The newest exported hour is exported again when the current fingerprint differs. Null for hours exported before this column existed and for hours loaded by a rebuild, so the newest of them is exported again once.';
