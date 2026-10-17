-- The mutes count of an exported hour, so that the newest exported hour is
-- exported again when a mute is created or lifted, and the archive always
-- holds the current mutes.

ALTER TABLE archive_hours ADD COLUMN mute_changes BIGINT;
COMMENT ON COLUMN archive_hours.mute_changes IS 'Mutes plus lifted mutes when the hour was exported; the number grows by one with every mute created or lifted, because mutes are never deleted. The newest exported hour is exported again when the current number differs. Null for hours exported before this column existed and for hours loaded by a rebuild, so the newest of them is exported again once.';
