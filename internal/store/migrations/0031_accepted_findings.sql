-- Accepted findings: a mute that records a finding as by design, with a
-- review date as its expiry and the entity's growth when it was accepted.
-- Every reader of mutes treats an accepted row as a mute.

-- The column with a default comes last: DuckDB refuses to commit a
-- transaction in which another ALTER follows ADD COLUMN ... DEFAULT on a
-- table that holds rows.
ALTER TABLE mutes ADD COLUMN growth_per_day DOUBLE;
ALTER TABLE mutes ADD COLUMN end_reason VARCHAR;
ALTER TABLE mutes ADD COLUMN accepted BOOLEAN DEFAULT false;

COMMENT ON TABLE mutes IS 'Mutes of checks or of a check for one entity. A muted finding still records transitions but is shown apart and never alerts. Rows are kept after expiry and unmute as history; a mute applies while unmuted_at is null and expires_at is in the future. A row with accepted true is an acceptance: a mute of one entity''s finding as by design, whose expires_at is the review date; nats-lens ends it early when the entity''s growth changes.';
COMMENT ON COLUMN mutes.accepted IS 'True when the row accepts the finding as by design rather than muting it; it then has an entity. False or NULL (rows from before this column, including those loaded from older archive files) for a plain mute.';
COMMENT ON COLUMN mutes.growth_per_day IS 'Growth of the accepted entity, bytes per day: a stream''s leader bytes or a server''s js_storage, the week''s growth (median over the last day''s points of the change since a week earlier, divided by 7). NULL for a plain mute and for other entity types; for an entity under about 8 days old NULL at acceptance, then written by the growth review the first time it can be measured.';
COMMENT ON COLUMN mutes.end_reason IS 'Why nats-lens ended the acceptance early, for example growth changed from +2.1 MiB/day to +4.0 MiB/day over the last 7 days (+9.0 MiB in the last day); unmuted_by is then nats-lens. NULL when a person lifted the row or it ran out.';
COMMENT ON COLUMN archive_hours.mute_changes IS 'Mutes plus lifted mutes plus acceptances with a growth when the hour was exported; the number grows with every mute created or lifted and every growth baseline the review writes, because mutes are never deleted and a growth is never cleared. The newest exported hour is exported again when the current number differs. Null for hours exported before this column existed and for hours loaded by a rebuild, so the newest of them is exported again once.';
