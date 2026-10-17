-- Hours already written to the Parquet archive.

CREATE TABLE archive_hours (
    hour         TIMESTAMP PRIMARY KEY,
    rows         BIGINT NOT NULL,
    files        INTEGER NOT NULL,
    exported_at  TIMESTAMP NOT NULL
);
COMMENT ON TABLE archive_hours IS 'Hours whose rows were exported to the Parquet archive (archive-dir/<table>/date=YYYY-MM-DD/hour=HH.parquet), so that the exporter does not repeat them. An hour is exported once it has closed; missed hours are caught up while they are inside the retention window. A rebuild from the archive marks the hours it loaded.';
COMMENT ON COLUMN archive_hours.hour IS 'UTC start of the hour.';
COMMENT ON COLUMN archive_hours.rows IS 'Rows written across all tables for the hour.';
COMMENT ON COLUMN archive_hours.files IS 'Parquet files written for the hour; tables with no rows get no file.';
COMMENT ON COLUMN archive_hours.exported_at IS 'UTC time the hour was exported, or loaded by a rebuild.';
