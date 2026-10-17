-- Record a checksum of every applied migration, so that an edited
-- migration file is detected at start instead of silently diverging.

ALTER TABLE migrations ADD COLUMN sha256 VARCHAR;
COMMENT ON COLUMN migrations.sha256 IS 'SHA-256 (hex) of the migration file as applied. Rows applied before this column existed are filled from the files of the first binary that has it. Start fails when a file no longer matches: never edit an applied migration, add a new one.';
