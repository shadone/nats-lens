-- Per-hour row counts and a final flag on archived hours, so that an hour
-- whose rows changed after it was exported (late events, a late epoch) is
-- exported again, and the open hour can be archived provisionally.

ALTER TABLE archive_hours ADD COLUMN epochs BIGINT;
ALTER TABLE archive_hours ADD COLUMN events BIGINT;
ALTER TABLE archive_hours ADD COLUMN final BOOLEAN;
ALTER TABLE archive_hours ADD COLUMN table_rows JSON;
COMMENT ON TABLE archive_hours IS 'Hours whose rows were exported to the Parquet archive (archive-dir/<table>/date=YYYY-MM-DD/hour=HH.parquet, listed by archive-dir/manifests/date=YYYY-MM-DD/hour=HH.json). A closed hour is exported again when its epoch or event count differs from the recorded one; the open hour is exported provisionally (final false) every archive_provisional_every. Missed hours are caught up while they are inside the retention window. A rebuild from the archive records the hours it loaded.';
COMMENT ON COLUMN archive_hours.epochs IS 'Epochs that started in the hour, as exported. Null for hours exported before this column existed, which are exported again once.';
COMMENT ON COLUMN archive_hours.events IS 'Events received in the hour, as exported.';
COMMENT ON COLUMN archive_hours.final IS 'False for a provisional export of an hour that had not closed yet; it is exported again once closed.';
COMMENT ON COLUMN archive_hours.table_rows IS 'Rows exported per table, as a JSON object of table name to count.';
COMMENT ON COLUMN events.id IS 'Event number, increasing in the order events were written. After a rebuild from the archive numbering continues after the highest number the archive recorded; only numbers handed out after the last archive tick before a database was lost are used again.';
COMMENT ON COLUMN epochs.epoch IS 'Epoch number, increasing by one per snapshot. After a rebuild from the archive numbering continues after the highest number the archive recorded; only numbers handed out after the last archive tick before a database was lost are used again.';
